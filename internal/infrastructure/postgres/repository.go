package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) CreateCustomer(ctx context.Context, customer domain.Customer, event domain.AuditEvent) (err error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin create customer transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && rollbackErr != pgx.ErrTxClosed && err == nil {
			err = fmt.Errorf("rollback create customer transaction: %w", rollbackErr)
		}
	}()

	riskFactors, err := json.Marshal(customer.RiskFactors)
	if err != nil {
		return fmt.Errorf("marshal risk factors: %w", err)
	}
	riskReasons, err := json.Marshal(customer.RiskAssessment.Reasons)
	if err != nil {
		return fmt.Errorf("marshal risk reasons: %w", err)
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("marshal audit payload: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO customers (
			id, external_ref, customer_type, legal_name, country_code,
			risk_factors, risk_score, risk_rating, due_diligence,
			risk_reasons, risk_rule_version, risk_assessed_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		customer.ID, customer.ExternalRef, customer.Type, customer.LegalName, customer.CountryCode,
		riskFactors, customer.RiskAssessment.Score, customer.RiskAssessment.Rating,
		customer.RiskAssessment.DueDiligence, riskReasons, customer.RiskAssessment.RuleVersion,
		customer.RiskAssessment.AssessedAt, customer.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert customer: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO audit_events (id, aggregate_id, event_type, actor, occurred_at, payload)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		event.ID, event.AggregateID, event.EventType, event.Actor, event.OccurredAt, payload,
	)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit create customer transaction: %w", err)
	}
	return nil
}

func (r *Repository) ListAuditEvents(ctx context.Context, customerID string) ([]domain.AuditEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, aggregate_id, event_type, actor, occurred_at, payload
		FROM audit_events
		WHERE aggregate_id = $1
		ORDER BY occurred_at, id`, customerID)
	if err != nil {
		return nil, fmt.Errorf("query audit events: %w", err)
	}
	defer rows.Close()

	events := make([]domain.AuditEvent, 0)
	for rows.Next() {
		var event domain.AuditEvent
		var payload []byte
		if err := rows.Scan(&event.ID, &event.AggregateID, &event.EventType, &event.Actor, &event.OccurredAt, &payload); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		if err := json.Unmarshal(payload, &event.Payload); err != nil {
			return nil, fmt.Errorf("decode audit event payload: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}
	return events, nil
}
