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

func Up(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, initialSchema); err != nil {
		return fmt.Errorf("apply initial database migration: %w", err)
	}
	if _, err := pool.Exec(ctx, customerApprovalSchema); err != nil {
		return fmt.Errorf("apply customer approval migration: %w", err)
	}
	return nil
}
