package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

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
	if err := repo.CreateTransaction(ctx, transaction, event); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM transactions WHERE id = $1", transaction.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("transaction count=%d, want 1", count)
	}
	events, err := repo.ListAuditEvents(ctx, transaction.ID)
	if err != nil || len(events) != 1 || events[0].AggregateType != "transaction" {
		t.Fatalf("unexpected transaction events: %+v err=%v", events, err)
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
	invalidEvent := domain.AuditEvent{
		ID: "e5fd3df4-1609-4990-8a3b-b6eb3382b84c", AggregateType: "invalid", AggregateID: transaction.ID,
		EventType: "transaction.ingested", Actor: transaction.IngestedBy, OccurredAt: now, Payload: map[string]any{},
	}
	if err := repo.CreateTransaction(ctx, transaction, invalidEvent); err == nil {
		t.Fatal("expected transaction audit insert to fail")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM transactions WHERE id = $1", transaction.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("transaction insert was not rolled back: count=%d", count)
	}
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
		ID: "45c56ad7-c372-4b2f-bddf-caf14b0494de", ExternalRef: "transaction-integration-test",
		CustomerID: customerID, Direction: domain.TransactionOutbound, AmountMinor: 125050,
		Currency: "GBP", CounterpartyCountry: "DE", OccurredAt: now, IngestedAt: now,
		IngestedBy: "payments-analyst@example.test",
	}
}

func cleanupCustomer(t *testing.T, pool *pgxpool.Pool, customer domain.Customer) {
	t.Helper()
	_, _ = pool.Exec(context.Background(), "DELETE FROM audit_events WHERE aggregate_type = 'transaction' AND aggregate_id IN (SELECT id FROM transactions WHERE customer_id = $1)", customer.ID)
	_, _ = pool.Exec(context.Background(), "DELETE FROM transactions WHERE customer_id = $1", customer.ID)
	_, _ = pool.Exec(context.Background(), "DELETE FROM audit_events WHERE aggregate_id = $1", customer.ID)
	_, _ = pool.Exec(context.Background(), "DELETE FROM customers WHERE id = $1 OR external_ref = $2", customer.ID, customer.ExternalRef)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_events WHERE aggregate_type = 'transaction' AND aggregate_id IN (SELECT id FROM transactions WHERE customer_id = $1)", customer.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM transactions WHERE customer_id = $1", customer.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_events WHERE aggregate_id = $1", customer.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM customers WHERE id = $1", customer.ID)
	})
}
