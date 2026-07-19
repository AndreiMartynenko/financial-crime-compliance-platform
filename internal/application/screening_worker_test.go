package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/infrastructure/memory"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/infrastructure/screening"
)

func TestScreeningWorkerRunsDueSchedule(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	customer, err := application.NewOnboardingService(repo).Onboard(ctx, application.OnboardCustomerCommand{Type: domain.CustomerIndividual, LegalName: "Viktor Petrov", CountryCode: "GB", RiskFactors: domain.RiskFactors{CountryRisk: domain.CountryRiskLow, SourceOfFundsVerified: true}, Actor: "analyst"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	schedule := domain.ScreeningSchedule{CustomerID: customer.ID, Enabled: true, IntervalHours: 24, NextRunAt: now.Add(-time.Minute), UpdatedBy: "analyst", UpdatedAt: now}
	if _, err := repo.UpsertScreeningSchedule(ctx, schedule, domain.AuditEvent{ID: "schedule-event", AggregateType: "customer", AggregateID: customer.ID, EventType: "screening.schedule_updated", Actor: "analyst", OccurredAt: now, Payload: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	service := application.NewScreeningService(repo, screening.DemoProvider{})
	count, err := service.RunDue(ctx, 10)
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	matches, err := service.List(ctx, customer.ID)
	if err != nil || len(matches) != 1 {
		t.Fatalf("matches=%+v err=%v", matches, err)
	}
	completed, err := service.GetSchedule(ctx, customer.ID)
	if err != nil || completed.LastRunAt == nil || !completed.NextRunAt.After(*completed.LastRunAt) {
		t.Fatalf("schedule=%+v err=%v", completed, err)
	}
}
