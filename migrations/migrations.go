package migrations

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed 000001_initial.up.sql
var initialSchema string

//go:embed 000002_customer_approval.up.sql
var customerApprovalSchema string

//go:embed 000003_transaction_ingestion.up.sql
var transactionIngestionSchema string

//go:embed 000004_monitoring_alerts.up.sql
var monitoringAlertsSchema string

//go:embed 000005_transaction_idempotency.up.sql
var transactionIdempotencySchema string

//go:embed 000006_case_management.up.sql
var caseManagementSchema string

//go:embed 000007_kyc_cdd.up.sql
var kycCDDSchema string

//go:embed 000008_screening.up.sql
var screeningSchema string

//go:embed 000009_ongoing_monitoring.up.sql
var ongoingMonitoringSchema string

//go:embed 000010_screening_job_leases.up.sql
var screeningJobLeasesSchema string

//go:embed 000011_notifications.up.sql
var notificationsSchema string

//go:embed 000012_notification_outbox.up.sql
var notificationOutboxSchema string

func Up(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migrations: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(7281942501)"); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	migrationList := []struct {
		version int
		name    string
		sql     string
	}{
		{1, "initial", initialSchema},
		{2, "customer_approval", customerApprovalSchema},
		{3, "transaction_ingestion", transactionIngestionSchema},
		{4, "monitoring_alerts", monitoringAlertsSchema},
		{5, "transaction_idempotency", transactionIdempotencySchema},
		{6, "case_management", caseManagementSchema},
		{7, "kyc_cdd", kycCDDSchema},
		{8, "screening", screeningSchema},
		{9, "ongoing_monitoring", ongoingMonitoringSchema},
		{10, "screening_job_leases", screeningJobLeasesSchema},
		{11, "notifications", notificationsSchema},
		{12, "notification_outbox", notificationOutboxSchema},
	}
	for _, migration := range migrationList {
		var applied bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", migration.version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %d: %w", migration.version, err)
		}
		if applied {
			continue
		}
		if _, err := tx.Exec(ctx, migration.sql); err != nil {
			return fmt.Errorf("apply migration %d (%s): %w", migration.version, migration.name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", migration.version, migration.name); err != nil {
			return fmt.Errorf("record migration %d: %w", migration.version, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}
