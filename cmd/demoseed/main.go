package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
	postgresrepo "github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/infrastructure/postgres"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/infrastructure/screening"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

const confirmation = "seed"

var demoRefs = []string{"demo-pending-001", "demo-orion-001", "demo-nadia-001"}

func main() {
	if os.Getenv("CONFIRM_DEMO_SEED") != confirmation {
		log.Fatalf("refusing to write demo data: set CONFIRM_DEMO_SEED=%s", confirmation)
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("connect to PostgreSQL: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ping PostgreSQL: %v", err)
	}
	if err := migrations.Up(ctx, pool); err != nil {
		log.Fatalf("apply migrations: %v", err)
	}

	state, err := demoState(ctx, pool)
	if err != nil {
		log.Fatalf("inspect demo data: %v", err)
	}
	if state == len(demoRefs) {
		fmt.Println("Demo dataset already present; no changes made.")
		return
	}
	if state != 0 {
		log.Fatalf("partial demo dataset found (%d/%d customers); use a fresh database instead of overwriting data", state, len(demoRefs))
	}

	repo := postgresrepo.NewRepository(pool)
	if err := seed(ctx, repo); err != nil {
		log.Fatalf("seed demo data: %v", err)
	}
	fmt.Println("Demo dataset created: 3 customers, an approval, CDD evidence, monitoring alerts, a case, screening matches, notifications, and audit events.")
}

func demoState(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var count int
	err := pool.QueryRow(ctx, "SELECT count(*) FROM customers WHERE external_ref = ANY($1)", demoRefs).Scan(&count)
	return count, err
}

type demoRepository interface {
	application.Repository
	application.DueDiligenceRepository
	application.TransactionRepository
	application.CaseRepository
	application.ScreeningRepository
}

func seed(ctx context.Context, repo demoRepository) error {
	onboarding := application.NewOnboardingService(repo)
	dueDiligence := application.NewDueDiligenceService(repo)
	transactions := application.NewTransactionService(repo)
	cases := application.NewCaseService(repo)
	screeningService := application.NewScreeningService(repo, screening.DemoProvider{})

	_, err := onboarding.Onboard(ctx, application.OnboardCustomerCommand{
		ExternalRef: "demo-pending-001", Type: domain.CustomerCompany, LegalName: "Harbour Design Studio",
		CountryCode: "GB", Actor: "demo-analyst",
		RiskFactors: domain.RiskFactors{CountryRisk: domain.CountryRiskLow, SourceOfFundsVerified: true},
	})
	if err != nil {
		return fmt.Errorf("create pending customer: %w", err)
	}

	orion, err := onboarding.Onboard(ctx, application.OnboardCustomerCommand{
		ExternalRef: "demo-orion-001", Type: domain.CustomerCompany, LegalName: "Orion Trading Company",
		CountryCode: "GB", Actor: "demo-analyst",
		RiskFactors: domain.RiskFactors{CountryRisk: domain.CountryRiskMedium, ComplexOwnership: true, SourceOfFundsVerified: true},
	})
	if err != nil {
		return fmt.Errorf("create Orion customer: %w", err)
	}
	if _, err := onboarding.Review(ctx, orion.ID, domain.ReviewApprove, "demo-reviewer", "Demo maker-checker approval"); err != nil {
		return fmt.Errorf("approve Orion customer: %w", err)
	}
	nextReview := time.Now().UTC().AddDate(1, 0, 0)
	if _, err := dueDiligence.UpdateProfile(ctx, domain.CDDProfile{
		CustomerID: orion.ID, SourceOfWealth: "Documented wholesale trading revenue",
		BusinessPurpose: "Cross-border wholesale settlement", ExpectedMonthlyVolumeMinor: 25_000_000,
		Currency: "GBP", Status: domain.CDDComplete, NextReviewAt: &nextReview,
	}, "demo-analyst"); err != nil {
		return fmt.Errorf("create Orion CDD profile: %w", err)
	}
	if _, err := dueDiligence.AddOwner(ctx, domain.BeneficialOwner{
		CustomerID: orion.ID, FullName: "Elena Morris", OwnershipPercent: 75, CountryCode: "GB",
	}, "demo-analyst"); err != nil {
		return fmt.Errorf("add Orion beneficial owner: %w", err)
	}
	document, err := dueDiligence.AddDocument(ctx, domain.KYCDocument{
		CustomerID: orion.ID, Type: "company_registry_extract", Reference: "DEMO-ORION-REG-2026",
	}, "demo-analyst")
	if err != nil {
		return fmt.Errorf("add Orion document: %w", err)
	}
	if _, err := dueDiligence.ReviewDocument(ctx, document.ID, domain.DocumentVerified, "demo-reviewer"); err != nil {
		return fmt.Errorf("verify Orion document: %w", err)
	}
	ingestion, err := transactions.Ingest(ctx, application.IngestTransactionCommand{
		ExternalRef: "demo-payment-001", CustomerID: orion.ID, Direction: domain.TransactionOutbound,
		AmountMinor: 2_500_000, Currency: "GBP", CounterpartyCountry: "IR",
		OccurredAt: time.Now().UTC().Add(-2 * time.Hour), Actor: "demo-analyst", IdempotencyKey: "demo-payment-001",
	})
	if err != nil {
		return fmt.Errorf("ingest Orion transaction: %w", err)
	}
	if len(ingestion.Alerts) == 0 {
		return errors.New("demo transaction unexpectedly created no alerts")
	}
	item, err := cases.Create(ctx, ingestion.Alerts[0].ID, "Review Orion high-value cross-border payment", domain.CasePriorityHigh, "demo-analyst")
	if err != nil {
		return fmt.Errorf("create Orion investigation case: %w", err)
	}
	if _, err := cases.Assign(ctx, item.ID, "demo-analyst", "demo-reviewer"); err != nil {
		return fmt.Errorf("assign Orion case: %w", err)
	}
	if _, err := cases.Comment(ctx, item.ID, "Escalated for source-of-funds and counterparty verification.", "demo-analyst"); err != nil {
		return fmt.Errorf("comment on Orion case: %w", err)
	}
	if _, err := screeningService.ScreenCustomer(ctx, orion.ID, "demo-analyst"); err != nil {
		return fmt.Errorf("screen Orion customer: %w", err)
	}
	if _, err := screeningService.ConfigureSchedule(ctx, orion.ID, true, 24, "demo-reviewer"); err != nil {
		return fmt.Errorf("schedule Orion screening: %w", err)
	}

	nadia, err := onboarding.Onboard(ctx, application.OnboardCustomerCommand{
		ExternalRef: "demo-nadia-001", Type: domain.CustomerIndividual, LegalName: "Nadia Karim",
		CountryCode: "AE", Actor: "demo-analyst",
		RiskFactors: domain.RiskFactors{CountryRisk: domain.CountryRiskMedium, PEP: true, SourceOfFundsVerified: true},
	})
	if err != nil {
		return fmt.Errorf("create Nadia customer: %w", err)
	}
	if _, err := onboarding.Review(ctx, nadia.ID, domain.ReviewApprove, "demo-reviewer", "Demo enhanced-due-diligence approval"); err != nil {
		return fmt.Errorf("approve Nadia customer: %w", err)
	}
	if _, err := dueDiligence.UpdateProfile(ctx, domain.CDDProfile{
		CustomerID: nadia.ID, SourceOfWealth: "Declared professional income",
		BusinessPurpose: "Personal investment account", ExpectedMonthlyVolumeMinor: 500_000,
		Currency: "GBP", Status: domain.CDDInReview, NextReviewAt: &nextReview,
	}, "demo-analyst"); err != nil {
		return fmt.Errorf("create Nadia CDD profile: %w", err)
	}
	if _, err := screeningService.ScreenCustomer(ctx, nadia.ID, "demo-analyst"); err != nil {
		return fmt.Errorf("screen Nadia customer: %w", err)
	}
	return nil
}
