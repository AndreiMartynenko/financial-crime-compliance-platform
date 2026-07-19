package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCreateCustomerPersistsCustomerAndAuditEvent(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	customer := testCustomer(now)
	event := domain.AuditEvent{
		ID: "760267e3-7c95-4c91-a6fa-1ffcd7a19831", AggregateType: "customer", AggregateID: customer.ID,
		EventType: "customer.onboarded", Actor: "integration-test", OccurredAt: now,
		Payload: map[string]any{"risk_score": float64(0)},
	}
	cleanupCustomer(t, pool, customer)

	repo := NewRepository(pool)
	if err := repo.CreateCustomer(ctx, customer, event); err != nil {
		t.Fatal(err)
	}
	events, err := repo.ListAuditEvents(ctx, customer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != event.ID || events[0].Actor != event.Actor {
		t.Fatalf("unexpected audit events: %+v", events)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM customers WHERE id = $1", customer.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("customer count = %d, want 1", count)
	}
	reviewEvent := domain.AuditEvent{
		ID: "38019ab2-50f4-4d53-a45a-e1bf34065c72", AggregateType: "customer", AggregateID: customer.ID,
		EventType: "customer.approved", Actor: "checker@example.test", OccurredAt: now.Add(time.Second),
		Payload: map[string]any{"reason": "verified"},
	}
	reviewed, err := repo.ReviewCustomer(ctx, customer.ID, domain.ReviewApprove, reviewEvent.Actor, reviewEvent)
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.Status != domain.CustomerActive || reviewed.ReviewedBy != reviewEvent.Actor {
		t.Fatalf("unexpected review state: %+v", reviewed)
	}
	loaded, err := repo.GetCustomer(ctx, customer.ID)
	if err != nil || loaded.Status != domain.CustomerActive {
		t.Fatalf("loaded customer=%+v err=%v", loaded, err)
	}
	customers, err := repo.ListCustomers(ctx, domain.CustomerActive, application.PageRequest{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	foundCustomer := false
	for _, listed := range customers {
		if listed.ID == customer.ID {
			foundCustomer = true
		}
	}
	if !foundCustomer {
		t.Fatalf("customer %s not found in page", customer.ID)
	}
	auditPage, err := repo.ListAuditEventsPage(ctx, customer.ID, application.PageRequest{Limit: 3})
	if err != nil || len(auditPage) != 2 {
		t.Fatalf("audit page=%+v err=%v", auditPage, err)
	}
	events, err = repo.ListAuditEvents(ctx, customer.ID)
	if err != nil || len(events) != 2 || events[1].EventType != "customer.approved" {
		t.Fatalf("unexpected review audit events: %+v err=%v", events, err)
	}
}

func TestCreateCustomerRollsBackWhenAuditInsertFails(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	now := time.Now().UTC()
	customer := testCustomer(now)
	event := domain.AuditEvent{
		ID:            "e94cc5b8-aa03-43e9-ae26-771ac22f22a5",
		AggregateType: "invalid", // Violates the aggregate-type check after the customer insert.
		AggregateID:   customer.ID,
		EventType:     "customer.onboarded",
		Actor:         "integration-test",
		OccurredAt:    now,
		Payload:       map[string]any{},
	}

	cleanupCustomer(t, pool, customer)

	err := NewRepository(pool).CreateCustomer(ctx, customer, event)
	if err == nil {
		t.Fatal("expected audit insert to fail")
	}
	var count int
	if queryErr := pool.QueryRow(ctx, "SELECT count(*) FROM customers WHERE id = $1", customer.ID).Scan(&count); queryErr != nil {
		t.Fatal(queryErr)
	}
	if count != 0 {
		t.Fatal(errors.New("customer insert was not rolled back"))
	}
}

func TestReviewCustomerRollsBackWhenAuditInsertFails(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	customer := testCustomer(now)
	cleanupCustomer(t, pool, customer)
	createdEvent := domain.AuditEvent{
		ID: "ea6bd4bf-6009-45d4-8769-e37b2044775b", AggregateType: "customer", AggregateID: customer.ID,
		EventType: "customer.submitted", Actor: customer.CreatedBy, OccurredAt: now, Payload: map[string]any{},
	}
	repo := NewRepository(pool)
	if err := repo.CreateCustomer(ctx, customer, createdEvent); err != nil {
		t.Fatal(err)
	}
	failedReviewEvent := domain.AuditEvent{
		ID:            createdEvent.ID, // Duplicate primary key forces the audit insert to fail after the status update.
		AggregateType: "customer", AggregateID: customer.ID, EventType: "customer.approved", Actor: "checker@example.test",
		OccurredAt: now.Add(time.Second), Payload: map[string]any{},
	}
	if _, err := repo.ReviewCustomer(ctx, customer.ID, domain.ReviewApprove, failedReviewEvent.Actor, failedReviewEvent); err == nil {
		t.Fatal("expected review audit insert to fail")
	}
	var status domain.CustomerStatus
	var reviewedBy *string
	if err := pool.QueryRow(ctx, "SELECT status, reviewed_by FROM customers WHERE id = $1", customer.ID).Scan(&status, &reviewedBy); err != nil {
		t.Fatal(err)
	}
	if status != domain.CustomerPendingApproval || reviewedBy != nil {
		t.Fatalf("review update was not rolled back: status=%s reviewed_by=%v", status, reviewedBy)
	}
}

func TestCreateTransactionPersistsTransactionAndAuditEvent(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	customer := testCustomer(now)
	customer.Status = domain.CustomerActive
	cleanupCustomer(t, pool, customer)
	repo := NewRepository(pool)
	createdEvent := domain.AuditEvent{
		ID: "094013e2-69a5-4636-8a24-bc40539f58b7", AggregateType: "customer", AggregateID: customer.ID,
		EventType: "customer.approved", Actor: "integration-test", OccurredAt: now, Payload: map[string]any{},
	}
	if err := repo.CreateCustomer(ctx, customer, createdEvent); err != nil {
		t.Fatal(err)
	}
	transaction := testTransaction(customer.ID, now)
	event := domain.AuditEvent{
		ID: "011ece8c-b950-4917-ae3c-e54e40a451b5", AggregateType: "transaction", AggregateID: transaction.ID,
		EventType: "transaction.ingested", Actor: transaction.IngestedBy, OccurredAt: now, Payload: map[string]any{},
	}
	alert := testAlert(transaction, now)
	alertEvent := domain.AuditEvent{
		ID: "78bc3330-a6c2-4531-aace-32c221333750", AggregateType: "alert", AggregateID: alert.ID,
		EventType: "alert.created", Actor: "transaction-monitoring-engine", OccurredAt: now, Payload: map[string]any{},
	}
	if _, _, _, err := repo.CreateTransaction(ctx, transaction, event, []domain.Alert{alert}, []domain.AuditEvent{alertEvent}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM transactions WHERE id = $1", transaction.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("transaction count=%d, want 1", count)
	}
	transactions, err := repo.ListCustomerTransactions(ctx, customer.ID, application.PageRequest{Limit: 2})
	if err != nil || len(transactions) != 1 || transactions[0].ID != transaction.ID {
		t.Fatalf("transactions=%+v err=%v", transactions, err)
	}
	replayedTransaction, replayedAlerts, replayed, err := repo.CreateTransaction(ctx, transaction, event, []domain.Alert{alert}, []domain.AuditEvent{alertEvent})
	if err != nil || !replayed || replayedTransaction.ID != transaction.ID || len(replayedAlerts) != 1 {
		t.Fatalf("replayed transaction=%+v alerts=%+v replayed=%v err=%v", replayedTransaction, replayedAlerts, replayed, err)
	}
	conflictingTransaction := transaction
	conflictingTransaction.AmountMinor++
	if _, _, _, err := repo.CreateTransaction(ctx, conflictingTransaction, event, nil, nil); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("idempotency conflict err=%v", err)
	}
	events, err := repo.ListAuditEvents(ctx, transaction.ID)
	if err != nil || len(events) != 1 || events[0].AggregateType != "transaction" {
		t.Fatalf("unexpected transaction events: %+v err=%v", events, err)
	}
	alerts, err := repo.ListAlerts(ctx, domain.AlertOpen)
	if err != nil || !containsAlert(alerts, alert.ID, domain.AlertOpen) {
		t.Fatalf("unexpected alerts: %+v err=%v", alerts, err)
	}
	alertPage, err := repo.ListAlertsPage(ctx, domain.AlertOpen, application.PageRequest{Limit: 100})
	if err != nil || !containsAlert(alertPage, alert.ID, domain.AlertOpen) {
		t.Fatalf("alert page missing test alert: page=%+v err=%v", alertPage, err)
	}
	invalidCloseEvent := domain.AuditEvent{
		ID: "d5748da6-1c0e-443e-80f9-f67cd7e8c862", AggregateType: "invalid", AggregateID: alert.ID,
		EventType: "alert.closed", Actor: "reviewer@example.test", OccurredAt: now.Add(time.Second), Payload: map[string]any{},
	}
	if _, err := repo.CloseAlert(ctx, alert.ID, invalidCloseEvent.Actor, "reviewed", invalidCloseEvent); err == nil {
		t.Fatal("expected alert closure audit insert to fail")
	}
	alerts, err = repo.ListAlerts(ctx, domain.AlertOpen)
	if err != nil || !containsAlert(alerts, alert.ID, domain.AlertOpen) {
		t.Fatalf("alert closure was not rolled back: alerts=%+v err=%v", alerts, err)
	}
	closeEvent := domain.AuditEvent{
		ID: "dd2ef1ea-cf55-4167-a251-5fcf13f4baf3", AggregateType: "alert", AggregateID: alert.ID,
		EventType: "alert.closed", Actor: "reviewer@example.test", OccurredAt: now.Add(time.Second), Payload: map[string]any{"reason": "reviewed"},
	}
	closed, err := repo.CloseAlert(ctx, alert.ID, closeEvent.Actor, "reviewed", closeEvent)
	if err != nil || closed.Status != domain.AlertClosed || closed.ClosedBy != closeEvent.Actor {
		t.Fatalf("closed alert=%+v err=%v", closed, err)
	}
}

func TestCreateTransactionRollsBackWhenAuditInsertFails(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	customer := testCustomer(now)
	customer.Status = domain.CustomerActive
	cleanupCustomer(t, pool, customer)
	repo := NewRepository(pool)
	createdEvent := domain.AuditEvent{
		ID: "67d42196-1534-4655-9831-9b12eff5b991", AggregateType: "customer", AggregateID: customer.ID,
		EventType: "customer.approved", Actor: "integration-test", OccurredAt: now, Payload: map[string]any{},
	}
	if err := repo.CreateCustomer(ctx, customer, createdEvent); err != nil {
		t.Fatal(err)
	}
	transaction := testTransaction(customer.ID, now)
	transactionEvent := domain.AuditEvent{
		ID: "e5fd3df4-1609-4990-8a3b-b6eb3382b84c", AggregateType: "transaction", AggregateID: transaction.ID,
		EventType: "transaction.ingested", Actor: transaction.IngestedBy, OccurredAt: now, Payload: map[string]any{},
	}
	alert := testAlert(transaction, now)
	invalidAlertEvent := domain.AuditEvent{
		ID: "0cbdba0c-18b2-432d-b4b6-c4e94278c92b", AggregateType: "invalid", AggregateID: alert.ID,
		EventType: "alert.created", Actor: "transaction-monitoring-engine", OccurredAt: now, Payload: map[string]any{},
	}
	if _, _, _, err := repo.CreateTransaction(ctx, transaction, transactionEvent, []domain.Alert{alert}, []domain.AuditEvent{invalidAlertEvent}); err == nil {
		t.Fatal("expected transaction audit insert to fail")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM transactions WHERE id = $1", transaction.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("transaction insert was not rolled back: count=%d", count)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alerts WHERE id = $1", alert.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("alert insert was not rolled back: count=%d", count)
	}
}

func TestCaseResolutionClosesAlertAtomically(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	customer := testCustomer(now)
	customer.Status = domain.CustomerActive
	cleanupCustomer(t, pool, customer)
	repo := NewRepository(pool)
	if err := repo.CreateCustomer(ctx, customer, domain.AuditEvent{ID: "10c65ac1-0ed3-4f74-b9fe-19f7371cbf53", AggregateType: "customer", AggregateID: customer.ID, EventType: "customer.approved", Actor: "test", OccurredAt: now, Payload: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	transaction := testTransaction(customer.ID, now)
	alert := testAlert(transaction, now)
	if _, _, _, err := repo.CreateTransaction(ctx, transaction, domain.AuditEvent{ID: "87b7520a-f834-49b0-849f-125470204af9", AggregateType: "transaction", AggregateID: transaction.ID, EventType: "transaction.ingested", Actor: "test", OccurredAt: now, Payload: map[string]any{}}, []domain.Alert{alert}, []domain.AuditEvent{{ID: "9dfb7efa-ad40-402e-a033-58f66db0625c", AggregateType: "alert", AggregateID: alert.ID, EventType: "alert.created", Actor: "engine", OccurredAt: now, Payload: map[string]any{}}}); err != nil {
		t.Fatal(err)
	}
	caseItem := domain.InvestigationCase{ID: "c6a0fb71-23fa-4495-8e50-39f5034a4d53", AlertID: alert.ID, Title: "Integration investigation", Priority: domain.CasePriorityHigh, Status: domain.CaseOpen, CreatedBy: "analyst", CreatedAt: now, UpdatedAt: now}
	created, err := repo.CreateCase(ctx, caseItem, domain.AuditEvent{ID: "70495d17-17f2-4ee2-8a34-6296e3c3976a", AggregateType: "case", AggregateID: caseItem.ID, EventType: "case.created", Actor: "analyst", OccurredAt: now, Payload: map[string]any{}})
	if err != nil || created.CustomerID != customer.ID {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	caseEvent := domain.AuditEvent{ID: "37d894df-0ea8-4649-862f-e981dcc8db13", AggregateType: "case", AggregateID: caseItem.ID, EventType: "case.resolved", Actor: "reviewer", OccurredAt: now.Add(time.Second), Payload: map[string]any{}}
	alertEvent := domain.AuditEvent{ID: "43bb4fc5-a879-46fd-b315-766972374aa3", AggregateType: "alert", EventType: "alert.closed", Actor: "reviewer", OccurredAt: caseEvent.OccurredAt, Payload: map[string]any{}}
	resolved, err := repo.ResolveCase(ctx, caseItem.ID, "explained", caseEvent.Actor, caseEvent, alertEvent)
	if err != nil || resolved.Status != domain.CaseResolved {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
	var alertStatus domain.AlertStatus
	if err := pool.QueryRow(ctx, "SELECT status FROM alerts WHERE id=$1", alert.ID).Scan(&alertStatus); err != nil || alertStatus != domain.AlertClosed {
		t.Fatalf("linked alert status=%s err=%v", alertStatus, err)
	}
}

func containsAlert(alerts []domain.Alert, id string, status domain.AlertStatus) bool {
	for _, alert := range alerts {
		if alert.ID == id && alert.Status == status {
			return true
		}
	}
	return false
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := migrations.Up(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	var migrationCount int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if migrationCount != 6 {
		t.Fatalf("applied migrations=%d, want 6", migrationCount)
	}
	return pool
}

func testCustomer(now time.Time) domain.Customer {
	return domain.Customer{
		ID:          "8dca1db5-63e1-4d26-9404-eaee2e98bf71",
		ExternalRef: "transaction-persistence-test",
		Type:        domain.CustomerCompany,
		LegalName:   "Persistence Test Ltd",
		CountryCode: "GB",
		RiskFactors: domain.RiskFactors{CountryRisk: domain.CountryRiskLow, SourceOfFundsVerified: true},
		RiskAssessment: domain.RiskAssessment{
			Rating: domain.RiskLow, DueDiligence: domain.DueDiligenceStandard,
			Reasons: []domain.RiskReason{}, AssessedAt: now, RuleVersion: "test",
		},
		Status:    domain.CustomerPendingApproval,
		CreatedBy: "maker@example.test",
		CreatedAt: now,
	}
}

func testTransaction(customerID string, now time.Time) domain.Transaction {
	return domain.Transaction{
		ID: "45c56ad7-c372-4b2f-bddf-caf14b0494de", IdempotencyKey: "transaction-integration-test-key", ExternalRef: "transaction-integration-test",
		CustomerID: customerID, Direction: domain.TransactionOutbound, AmountMinor: 125050,
		Currency: "GBP", CounterpartyCountry: "DE", OccurredAt: now, IngestedAt: now,
		IngestedBy: "payments-analyst@example.test",
	}
}

func testAlert(transaction domain.Transaction, now time.Time) domain.Alert {
	return domain.Alert{
		ID: "827f732f-6399-482f-af8b-5d2634251bb6", TransactionID: transaction.ID,
		CustomerID: transaction.CustomerID, RuleCode: "large_transaction",
		RuleVersion: domain.TransactionMonitoringRuleVersion, Severity: domain.AlertHigh,
		Status: domain.AlertOpen, ReasonCode: "amount_threshold_exceeded",
		Description: "Integration test alert", CreatedAt: now,
	}
}

func cleanupCustomer(t *testing.T, pool *pgxpool.Pool, customer domain.Customer) {
	t.Helper()
	_, _ = pool.Exec(context.Background(), "DELETE FROM audit_events WHERE aggregate_type = 'case' AND aggregate_id IN (SELECT id FROM investigation_cases WHERE customer_id = $1)", customer.ID)
	_, _ = pool.Exec(context.Background(), "DELETE FROM investigation_cases WHERE customer_id = $1", customer.ID)
	_, _ = pool.Exec(context.Background(), "DELETE FROM audit_events WHERE aggregate_type = 'alert' AND aggregate_id IN (SELECT id FROM alerts WHERE customer_id = $1)", customer.ID)
	_, _ = pool.Exec(context.Background(), "DELETE FROM alerts WHERE customer_id = $1", customer.ID)
	_, _ = pool.Exec(context.Background(), "DELETE FROM audit_events WHERE aggregate_type = 'transaction' AND aggregate_id IN (SELECT id FROM transactions WHERE customer_id = $1)", customer.ID)
	_, _ = pool.Exec(context.Background(), "DELETE FROM transactions WHERE customer_id = $1", customer.ID)
	_, _ = pool.Exec(context.Background(), "DELETE FROM audit_events WHERE aggregate_id = $1", customer.ID)
	_, _ = pool.Exec(context.Background(), "DELETE FROM customers WHERE id = $1 OR external_ref = $2", customer.ID, customer.ExternalRef)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_events WHERE aggregate_type = 'case' AND aggregate_id IN (SELECT id FROM investigation_cases WHERE customer_id = $1)", customer.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM investigation_cases WHERE customer_id = $1", customer.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_events WHERE aggregate_type = 'alert' AND aggregate_id IN (SELECT id FROM alerts WHERE customer_id = $1)", customer.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM alerts WHERE customer_id = $1", customer.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_events WHERE aggregate_type = 'transaction' AND aggregate_id IN (SELECT id FROM transactions WHERE customer_id = $1)", customer.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM transactions WHERE customer_id = $1", customer.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_events WHERE aggregate_id = $1", customer.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM customers WHERE id = $1", customer.ID)
	})
}
