package postgres

import (
	"context"
	"encoding/json"
	"errors"
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
			risk_reasons, risk_rule_version, risk_assessed_at, status, created_by, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		customer.ID, customer.ExternalRef, customer.Type, customer.LegalName, customer.CountryCode,
		riskFactors, customer.RiskAssessment.Score, customer.RiskAssessment.Rating,
		customer.RiskAssessment.DueDiligence, riskReasons, customer.RiskAssessment.RuleVersion,
		customer.RiskAssessment.AssessedAt, customer.Status, customer.CreatedBy, customer.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert customer: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO audit_events (id, aggregate_type, aggregate_id, event_type, actor, occurred_at, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		event.ID, event.AggregateType, event.AggregateID, event.EventType, event.Actor, event.OccurredAt, payload,
	)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit create customer transaction: %w", err)
	}
	return nil
}

func (r *Repository) ReviewCustomer(ctx context.Context, customerID string, decision domain.ReviewDecision, actor string, event domain.AuditEvent) (customer domain.Customer, err error) {
	if decision != domain.ReviewApprove && decision != domain.ReviewReject {
		return domain.Customer{}, fmt.Errorf("invalid review decision %q", decision)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Customer{}, fmt.Errorf("begin review customer transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && rollbackErr != pgx.ErrTxClosed && err == nil {
			err = fmt.Errorf("rollback review customer transaction: %w", rollbackErr)
		}
	}()

	status := domain.CustomerActive
	if decision == domain.ReviewReject {
		status = domain.CustomerRejected
	}
	row := tx.QueryRow(ctx, `
		UPDATE customers
		SET status = $2, reviewed_by = $3, reviewed_at = $4
		WHERE id = $1 AND status = 'pending_approval' AND created_by <> $3
		RETURNING id, external_ref, customer_type, legal_name, country_code,
			risk_factors, risk_score, risk_rating, due_diligence, risk_reasons,
			risk_rule_version, risk_assessed_at, status, created_by, reviewed_by, reviewed_at, created_at`,
		customerID, status, actor, event.OccurredAt)
	customer, err = scanCustomer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		var currentStatus domain.CustomerStatus
		var createdBy string
		lookupErr := tx.QueryRow(ctx, "SELECT status, created_by FROM customers WHERE id = $1", customerID).Scan(&currentStatus, &createdBy)
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			return domain.Customer{}, domain.ErrCustomerNotFound
		}
		if lookupErr != nil {
			return domain.Customer{}, fmt.Errorf("inspect customer review state: %w", lookupErr)
		}
		if createdBy == actor {
			return domain.Customer{}, domain.ErrMakerCannotReview
		}
		return domain.Customer{}, domain.ErrReviewConflict
	}
	if err != nil {
		return domain.Customer{}, fmt.Errorf("update customer review: %w", err)
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return domain.Customer{}, fmt.Errorf("marshal review audit payload: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_events (id, aggregate_type, aggregate_id, event_type, actor, occurred_at, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		event.ID, event.AggregateType, event.AggregateID, event.EventType, event.Actor, event.OccurredAt, payload)
	if err != nil {
		return domain.Customer{}, fmt.Errorf("insert review audit event: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Customer{}, fmt.Errorf("commit review customer transaction: %w", err)
	}
	return customer, nil
}

func (r *Repository) CreateTransaction(ctx context.Context, transaction domain.Transaction, event domain.AuditEvent) (err error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin ingest transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && rollbackErr != pgx.ErrTxClosed && err == nil {
			err = fmt.Errorf("rollback ingest transaction: %w", rollbackErr)
		}
	}()

	result, err := tx.Exec(ctx, `
		INSERT INTO transactions (
			id, external_ref, customer_id, direction, amount_minor, currency,
			counterparty_country, occurred_at, ingested_at, ingested_by
		)
		SELECT $1, $2, id, $4, $5, $6, $7, $8, $9, $10
		FROM customers
		WHERE id = $3 AND status = 'active'`,
		transaction.ID, transaction.ExternalRef, transaction.CustomerID, transaction.Direction,
		transaction.AmountMinor, transaction.Currency, transaction.CounterpartyCountry,
		transaction.OccurredAt, transaction.IngestedAt, transaction.IngestedBy)
	if err != nil {
		return fmt.Errorf("insert transaction: %w", err)
	}
	if result.RowsAffected() == 0 {
		var status domain.CustomerStatus
		lookupErr := tx.QueryRow(ctx, "SELECT status FROM customers WHERE id = $1", transaction.CustomerID).Scan(&status)
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			return domain.ErrCustomerNotFound
		}
		if lookupErr != nil {
			return fmt.Errorf("inspect transaction customer: %w", lookupErr)
		}
		return domain.ErrCustomerNotActive
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("marshal transaction audit payload: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_events (id, aggregate_type, aggregate_id, event_type, actor, occurred_at, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		event.ID, event.AggregateType, event.AggregateID, event.EventType, event.Actor, event.OccurredAt, payload)
	if err != nil {
		return fmt.Errorf("insert transaction audit event: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit ingest transaction: %w", err)
	}
	return nil
}

type scanner interface {
	Scan(...any) error
}

func scanCustomer(row scanner) (domain.Customer, error) {
	var customer domain.Customer
	var riskFactors, riskReasons []byte
	err := row.Scan(
		&customer.ID, &customer.ExternalRef, &customer.Type, &customer.LegalName, &customer.CountryCode,
		&riskFactors, &customer.RiskAssessment.Score, &customer.RiskAssessment.Rating,
		&customer.RiskAssessment.DueDiligence, &riskReasons, &customer.RiskAssessment.RuleVersion,
		&customer.RiskAssessment.AssessedAt, &customer.Status, &customer.CreatedBy,
		&customer.ReviewedBy, &customer.ReviewedAt, &customer.CreatedAt,
	)
	if err != nil {
		return domain.Customer{}, err
	}
	if err := json.Unmarshal(riskFactors, &customer.RiskFactors); err != nil {
		return domain.Customer{}, fmt.Errorf("decode customer risk factors: %w", err)
	}
	if err := json.Unmarshal(riskReasons, &customer.RiskAssessment.Reasons); err != nil {
		return domain.Customer{}, fmt.Errorf("decode customer risk reasons: %w", err)
	}
	return customer, nil
}

func (r *Repository) ListAuditEvents(ctx context.Context, customerID string) ([]domain.AuditEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, aggregate_type, aggregate_id, event_type, actor, occurred_at, payload
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
		if err := rows.Scan(&event.ID, &event.AggregateType, &event.AggregateID, &event.EventType, &event.Actor, &event.OccurredAt, &payload); err != nil {
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
