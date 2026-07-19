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
		ID: "760267e3-7c95-4c91-a6fa-1ffcd7a19831", AggregateID: customer.ID,
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
}

func TestCreateCustomerRollsBackWhenAuditInsertFails(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	now := time.Now().UTC()
	customer := testCustomer(now)
	event := domain.AuditEvent{
		ID:          "e94cc5b8-aa03-43e9-ae26-771ac22f22a5",
		AggregateID: "a4278ce4-f13b-4865-916d-34d80b507d38", // Does not match an existing customer.
		EventType:   "customer.onboarded",
		Actor:       "integration-test",
		OccurredAt:  now,
		Payload:     map[string]any{},
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
		CreatedAt: now,
	}
}

func cleanupCustomer(t *testing.T, pool *pgxpool.Pool, customer domain.Customer) {
	t.Helper()
	_, _ = pool.Exec(context.Background(), "DELETE FROM audit_events WHERE aggregate_id = $1", customer.ID)
	_, _ = pool.Exec(context.Background(), "DELETE FROM customers WHERE id = $1 OR external_ref = $2", customer.ID, customer.ExternalRef)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_events WHERE aggregate_id = $1", customer.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM customers WHERE id = $1", customer.ID)
	})
}
