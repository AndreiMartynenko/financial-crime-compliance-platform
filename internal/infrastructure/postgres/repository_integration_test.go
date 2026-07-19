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
	activity, err := repo.ListCustomerActivityPage(ctx, customer.ID, application.PageRequest{Limit: 100})
	if err != nil || len(activity) != 6 {
		t.Fatalf("customer activity=%+v err=%v", activity, err)
	}
	types := map[string]bool{}
	for _, event := range activity {
		types[event.AggregateType] = true
	}
	for _, kind := range []string{"customer", "transaction", "alert", "case"} {
		if !types[kind] {
			t.Fatalf("activity missing %s", kind)
		}
	}
}

func TestDueDiligencePersistsAndAuditsDocumentReview(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	customer := testCustomer(now)
	cleanupCustomer(t, pool, customer)
	repo := NewRepository(pool)
	if err := repo.CreateCustomer(ctx, customer, domain.AuditEvent{ID: "75ed2dde-c66c-4170-9822-a5010834aba9", AggregateType: "customer", AggregateID: customer.ID, EventType: "customer.onboarded", Actor: "maker", OccurredAt: now, Payload: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	profile := domain.CDDProfile{CustomerID: customer.ID, SourceOfWealth: "Business income", BusinessPurpose: "International trade", ExpectedMonthlyVolumeMinor: 10000000, Currency: "GBP", Status: domain.CDDInReview, UpdatedBy: "analyst", UpdatedAt: now}
	if _, err := repo.UpsertCDDProfile(ctx, profile, domain.AuditEvent{ID: "c0962846-8ef1-4183-999a-57d9e25c97de", AggregateType: "customer", AggregateID: customer.ID, EventType: "cdd.profile_updated", Actor: "analyst", OccurredAt: now, Payload: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	owner := domain.BeneficialOwner{ID: "45360514-140c-444c-b426-e4aa26149555", CustomerID: customer.ID, FullName: "Ada Owner", OwnershipPercent: 75, CountryCode: "GB", CreatedBy: "analyst", CreatedAt: now}
	if _, err := repo.AddBeneficialOwner(ctx, owner, domain.AuditEvent{ID: "66c2141f-47e6-408d-aa53-50aaf75accf4", AggregateType: "customer", AggregateID: customer.ID, EventType: "cdd.beneficial_owner_added", Actor: "analyst", OccurredAt: now, Payload: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	document := domain.KYCDocument{ID: "6bd3cd49-f852-4893-a355-32dc5826ff65", CustomerID: customer.ID, Type: "certificate_of_incorporation", Reference: "DOC-001", Status: domain.DocumentPending, CreatedBy: "analyst", CreatedAt: now}
	if _, err := repo.AddKYCDocument(ctx, document, domain.AuditEvent{ID: "144740d0-7425-48c5-8df8-e09014a31c28", AggregateType: "customer", AggregateID: customer.ID, EventType: "cdd.document_added", Actor: "analyst", OccurredAt: now, Payload: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	reviewEvent := domain.AuditEvent{ID: "2c5ffc82-b7ca-45aa-9514-23b98ae37884", AggregateType: "customer", EventType: "cdd.document_verified", Actor: "reviewer", OccurredAt: now.Add(time.Second), Payload: map[string]any{}}
	verified, err := repo.ReviewKYCDocument(ctx, document.ID, domain.DocumentVerified, "reviewer", reviewEvent)
	if err != nil || verified.Status != domain.DocumentVerified {
		t.Fatalf("verified=%+v err=%v", verified, err)
	}
	details, err := repo.GetDueDiligence(ctx, customer.ID)
	if err != nil || len(details.BeneficialOwners) != 1 || len(details.Documents) != 1 || details.Profile.Status != domain.CDDInReview {
		t.Fatalf("details=%+v err=%v", details, err)
	}
	events, err := repo.ListAuditEvents(ctx, customer.ID)
	if err != nil || len(events) != 5 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestScreeningPersistsAndDisposesMatchAtomically(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	customer := testCustomer(now)
	cleanupCustomer(t, pool, customer)
	repo := NewRepository(pool)
	if err := repo.CreateCustomer(ctx, customer, domain.AuditEvent{ID: "45dc3331-2f74-4d94-962a-79c746c76214", AggregateType: "customer", AggregateID: customer.ID, EventType: "customer.onboarded", Actor: "maker", OccurredAt: now, Payload: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	run := domain.ScreeningRun{ID: "9131e714-5eba-478b-b84e-50fb95f6a572", CustomerID: customer.ID, SubjectType: domain.ScreeningCustomer, SubjectID: customer.ID, QueryName: customer.LegalName, Provider: "integration-provider", CreatedBy: "analyst", CreatedAt: now}
	match := domain.ScreeningMatch{ID: "384c015e-5855-42d1-a745-f545489d8c6b", RunID: run.ID, CustomerID: customer.ID, SubjectType: domain.ScreeningCustomer, SubjectID: customer.ID, QueryName: customer.LegalName, ListType: domain.ScreeningSanctions, MatchedName: customer.LegalName, Score: 100, Reason: "exact match", Status: domain.MatchPotential, CreatedAt: now}
	createdEvent := domain.AuditEvent{ID: "d0b199d7-61c9-42a8-b6dc-2b3c9710ae30", AggregateType: "customer", AggregateID: customer.ID, EventType: "screening.completed", Actor: "analyst", OccurredAt: now, Payload: map[string]any{"matches": float64(1)}}
	notification := domain.Notification{ID: "1c4b78b8-5f66-46da-b80c-c5cfbe29f2c1", CustomerID: customer.ID, MatchID: match.ID, Type: "screening_match", Title: "Potential sanctions match", Message: "Review match", CreatedAt: now}
	outbox := domain.OutboxMessage{ID: "c34554d0-6546-49ea-8afc-87aad87a7f74", NotificationID: notification.ID, Destination: "http://webhook.test", Payload: map[string]any{"notification_id": notification.ID}, Status: "pending", NextAttemptAt: now, CreatedAt: now}
	if err := repo.SaveScreening(ctx, []domain.ScreeningRun{run}, []domain.ScreeningMatch{match}, []domain.Notification{notification}, []domain.OutboxMessage{outbox}, []domain.AuditEvent{createdEvent}); err != nil {
		t.Fatal(err)
	}
	notifications, err := repo.ListNotifications(ctx, 10)
	if err != nil || len(notifications) < 1 {
		t.Fatalf("notifications=%+v err=%v", notifications, err)
	}
	read, err := repo.ReadNotification(ctx, notification.ID, "reviewer", now.Add(time.Second))
	if err != nil || !read.Read || read.ReadBy != "reviewer" {
		t.Fatalf("notification=%+v err=%v", read, err)
	}
	claimed, err := repo.ClaimOutbox(ctx, now.Add(time.Second), 10, "delivery-worker", now.Add(time.Minute))
	if err != nil || len(claimed) < 1 {
		t.Fatalf("outbox=%+v err=%v", claimed, err)
	}
	if err := repo.CompleteOutbox(ctx, outbox.ID, "delivery-worker", now.Add(time.Second), now.Add(time.Second), ""); err != nil {
		t.Fatal(err)
	}
	var outboxStatus string
	if err := pool.QueryRow(ctx, "SELECT status FROM notification_outbox WHERE id=$1", outbox.ID).Scan(&outboxStatus); err != nil || outboxStatus != "delivered" {
		t.Fatalf("outbox status=%s err=%v", outboxStatus, err)
	}
	matches, err := repo.ListScreeningMatches(ctx, customer.ID)
	if err != nil || len(matches) != 1 || matches[0].Status != domain.MatchPotential {
		t.Fatalf("matches=%+v err=%v", matches, err)
	}
	dispositionEvent := domain.AuditEvent{ID: "10735423-c465-46cc-af19-3a06644066bf", AggregateType: "customer", EventType: "screening.match_dispositioned", Actor: "reviewer", OccurredAt: now.Add(time.Second), Payload: map[string]any{}}
	disposed, err := repo.DispositionScreeningMatch(ctx, match.ID, domain.MatchFalsePositive, "identity differs", "reviewer", dispositionEvent)
	if err != nil || disposed.Status != domain.MatchFalsePositive || disposed.ReviewedBy != "reviewer" {
		t.Fatalf("disposed=%+v err=%v", disposed, err)
	}
	events, err := repo.ListAuditEvents(ctx, customer.ID)
	if err != nil || len(events) != 3 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	schedule := domain.ScreeningSchedule{CustomerID: customer.ID, Enabled: true, IntervalHours: 24, NextRunAt: now.Add(24 * time.Hour), UpdatedBy: "analyst", UpdatedAt: now}
	scheduleEvent := domain.AuditEvent{ID: "e44dd2f1-6432-411e-a668-a1cf909cf047", AggregateType: "customer", AggregateID: customer.ID, EventType: "screening.schedule_updated", Actor: "analyst", OccurredAt: now, Payload: map[string]any{}}
	storedSchedule, err := repo.UpsertScreeningSchedule(ctx, schedule, scheduleEvent)
	if err != nil || !storedSchedule.Enabled || storedSchedule.IntervalHours != 24 {
		t.Fatalf("schedule=%+v err=%v", storedSchedule, err)
	}
	claimAt := now.Add(25 * time.Hour)
	due, err := repo.ClaimDueScreeningSchedules(ctx, claimAt, 10, "worker-a", claimAt.Add(5*time.Minute))
	if err != nil || len(due) != 1 {
		t.Fatalf("due=%+v err=%v", due, err)
	}
	secondClaim, err := repo.ClaimDueScreeningSchedules(ctx, claimAt, 10, "worker-b", claimAt.Add(5*time.Minute))
	if err != nil || len(secondClaim) != 0 {
		t.Fatalf("leased schedule was claimed twice: %+v err=%v", secondClaim, err)
	}
	recoveredAt := claimAt.Add(6 * time.Minute)
	recovered, err := repo.ClaimDueScreeningSchedules(ctx, recoveredAt, 10, "worker-b", recoveredAt.Add(5*time.Minute))
	if err != nil || len(recovered) != 1 {
		t.Fatalf("expired lease was not recovered: %+v err=%v", recovered, err)
	}
	if err := repo.CompleteScreeningSchedule(ctx, customer.ID, "worker-b", recoveredAt, recoveredAt.Add(24*time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	completedSchedule, err := repo.GetScreeningSchedule(ctx, customer.ID)
	if err != nil || completedSchedule.LastRunAt == nil || !completedSchedule.NextRunAt.After(*completedSchedule.LastRunAt) {
		t.Fatalf("completed schedule=%+v err=%v", completedSchedule, err)
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
	if migrationCount != 12 {
		t.Fatalf("applied migrations=%d, want 12", migrationCount)
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
