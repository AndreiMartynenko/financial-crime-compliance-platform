package migrations

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed 000001_initial.up.sql
var initialSchema string

func Up(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, initialSchema); err != nil {
		return fmt.Errorf("apply initial database migration: %w", err)
	}
	return nil
}
