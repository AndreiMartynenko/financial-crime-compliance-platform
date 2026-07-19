package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
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

func (r *Repository) CreateTransaction(ctx context.Context, transaction domain.Transaction, event domain.AuditEvent, alerts []domain.Alert, alertEvents []domain.AuditEvent) (stored domain.Transaction, storedAlerts []domain.Alert, replayed bool, err error) {
	if len(alerts) != len(alertEvents) {
		return domain.Transaction{}, nil, false, errors.New("each alert must have one audit event")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Transaction{}, nil, false, fmt.Errorf("begin ingest transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && rollbackErr != pgx.ErrTxClosed && err == nil {
			err = fmt.Errorf("rollback ingest transaction: %w", rollbackErr)
		}
	}()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", transaction.IdempotencyKey); err != nil {
		return domain.Transaction{}, nil, false, fmt.Errorf("lock idempotency key: %w", err)
	}
	existing, lookupErr := scanTransaction(tx.QueryRow(ctx, `
		SELECT id, idempotency_key, external_ref, customer_id, direction, amount_minor,
			currency, counterparty_country, occurred_at, ingested_at, ingested_by
		FROM transactions WHERE idempotency_key = $1`, transaction.IdempotencyKey))
	if lookupErr == nil {
		if !existing.SameIngestionPayload(transaction) {
			return domain.Transaction{}, nil, false, domain.ErrIdempotencyConflict
		}
		existingAlerts, err := listAlertsForTransaction(ctx, tx, existing.ID)
		if err != nil {
			return domain.Transaction{}, nil, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Transaction{}, nil, false, fmt.Errorf("commit idempotent replay: %w", err)
		}
		return existing, existingAlerts, true, nil
	}
	if !errors.Is(lookupErr, pgx.ErrNoRows) {
		return domain.Transaction{}, nil, false, fmt.Errorf("find idempotent transaction: %w", lookupErr)
	}

	result, err := tx.Exec(ctx, `
		INSERT INTO transactions (
			id, idempotency_key, external_ref, customer_id, direction, amount_minor, currency,
			counterparty_country, occurred_at, ingested_at, ingested_by
		)
		SELECT $1, $2, $3, id, $5, $6, $7, $8, $9, $10, $11
		FROM customers
		WHERE id = $4 AND status = 'active'`,
		transaction.ID, transaction.IdempotencyKey, transaction.ExternalRef, transaction.CustomerID, transaction.Direction,
		transaction.AmountMinor, transaction.Currency, transaction.CounterpartyCountry,
		transaction.OccurredAt, transaction.IngestedAt, transaction.IngestedBy)
	if err != nil {
		return domain.Transaction{}, nil, false, fmt.Errorf("insert transaction: %w", err)
	}
	if result.RowsAffected() == 0 {
		var status domain.CustomerStatus
		lookupErr := tx.QueryRow(ctx, "SELECT status FROM customers WHERE id = $1", transaction.CustomerID).Scan(&status)
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			return domain.Transaction{}, nil, false, domain.ErrCustomerNotFound
		}
		if lookupErr != nil {
			return domain.Transaction{}, nil, false, fmt.Errorf("inspect transaction customer: %w", lookupErr)
		}
		return domain.Transaction{}, nil, false, domain.ErrCustomerNotActive
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return domain.Transaction{}, nil, false, fmt.Errorf("marshal transaction audit payload: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_events (id, aggregate_type, aggregate_id, event_type, actor, occurred_at, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		event.ID, event.AggregateType, event.AggregateID, event.EventType, event.Actor, event.OccurredAt, payload)
	if err != nil {
		return domain.Transaction{}, nil, false, fmt.Errorf("insert transaction audit event: %w", err)
	}
	for index, alert := range alerts {
		_, err = tx.Exec(ctx, `
			INSERT INTO alerts (
				id, transaction_id, customer_id, rule_code, rule_version, severity,
				status, reason_code, description, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			alert.ID, alert.TransactionID, alert.CustomerID, alert.RuleCode, alert.RuleVersion,
			alert.Severity, alert.Status, alert.ReasonCode, alert.Description, alert.CreatedAt)
		if err != nil {
			return domain.Transaction{}, nil, false, fmt.Errorf("insert monitoring alert: %w", err)
		}
		alertEvent := alertEvents[index]
		alertPayload, marshalErr := json.Marshal(alertEvent.Payload)
		if marshalErr != nil {
			return domain.Transaction{}, nil, false, fmt.Errorf("marshal alert audit payload: %w", marshalErr)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO audit_events (id, aggregate_type, aggregate_id, event_type, actor, occurred_at, payload)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			alertEvent.ID, alertEvent.AggregateType, alertEvent.AggregateID, alertEvent.EventType,
			alertEvent.Actor, alertEvent.OccurredAt, alertPayload)
		if err != nil {
			return domain.Transaction{}, nil, false, fmt.Errorf("insert alert audit event: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Transaction{}, nil, false, fmt.Errorf("commit ingest transaction: %w", err)
	}
	return transaction, alerts, false, nil
}

func scanTransaction(row scanner) (domain.Transaction, error) {
	var transaction domain.Transaction
	err := row.Scan(
		&transaction.ID, &transaction.IdempotencyKey, &transaction.ExternalRef, &transaction.CustomerID,
		&transaction.Direction, &transaction.AmountMinor, &transaction.Currency,
		&transaction.CounterpartyCountry, &transaction.OccurredAt, &transaction.IngestedAt, &transaction.IngestedBy,
	)
	return transaction, err
}

func listAlertsForTransaction(ctx context.Context, tx pgx.Tx, transactionID string) ([]domain.Alert, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, transaction_id, customer_id, rule_code, rule_version, severity,
			status, reason_code, description, created_at, closed_at, closed_by, closure_reason
		FROM alerts WHERE transaction_id = $1 ORDER BY rule_code`, transactionID)
	if err != nil {
		return nil, fmt.Errorf("query replayed alerts: %w", err)
	}
	defer rows.Close()
	alerts := make([]domain.Alert, 0)
	for rows.Next() {
		alert, err := scanAlert(rows)
		if err != nil {
			return nil, fmt.Errorf("scan replayed alert: %w", err)
		}
		alerts = append(alerts, alert)
	}
	return alerts, rows.Err()
}

func (r *Repository) ListAlerts(ctx context.Context, status domain.AlertStatus) ([]domain.Alert, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, transaction_id, customer_id, rule_code, rule_version, severity,
			status, reason_code, description, created_at, closed_at, closed_by, closure_reason
		FROM alerts
		WHERE $1 = '' OR status = $1
		ORDER BY created_at DESC, id`, status)
	if err != nil {
		return nil, fmt.Errorf("query alerts: %w", err)
	}
	defer rows.Close()
	alerts := make([]domain.Alert, 0)
	for rows.Next() {
		alert, err := scanAlert(rows)
		if err != nil {
			return nil, fmt.Errorf("scan alert: %w", err)
		}
		alerts = append(alerts, alert)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alerts: %w", err)
	}
	return alerts, nil
}

func (r *Repository) CloseAlert(ctx context.Context, alertID, actor, reason string, event domain.AuditEvent) (alert domain.Alert, err error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Alert{}, fmt.Errorf("begin close alert: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && rollbackErr != pgx.ErrTxClosed && err == nil {
			err = fmt.Errorf("rollback close alert: %w", rollbackErr)
		}
	}()
	alert, err = scanAlert(tx.QueryRow(ctx, `
		UPDATE alerts
		SET status = 'closed', closed_at = $2, closed_by = $3, closure_reason = $4
		WHERE id = $1 AND status = 'open'
		RETURNING id, transaction_id, customer_id, rule_code, rule_version, severity,
			status, reason_code, description, created_at, closed_at, closed_by, closure_reason`,
		alertID, event.OccurredAt, actor, reason))
	if errors.Is(err, pgx.ErrNoRows) {
		var currentStatus domain.AlertStatus
		lookupErr := tx.QueryRow(ctx, "SELECT status FROM alerts WHERE id = $1", alertID).Scan(&currentStatus)
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			return domain.Alert{}, domain.ErrAlertNotFound
		}
		if lookupErr != nil {
			return domain.Alert{}, fmt.Errorf("inspect alert state: %w", lookupErr)
		}
		return domain.Alert{}, domain.ErrAlertConflict
	}
	if err != nil {
		return domain.Alert{}, fmt.Errorf("update alert: %w", err)
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return domain.Alert{}, fmt.Errorf("marshal alert closure audit payload: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_events (id, aggregate_type, aggregate_id, event_type, actor, occurred_at, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		event.ID, event.AggregateType, event.AggregateID, event.EventType, event.Actor, event.OccurredAt, payload)
	if err != nil {
		return domain.Alert{}, fmt.Errorf("insert alert closure audit event: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Alert{}, fmt.Errorf("commit close alert: %w", err)
	}
	return alert, nil
}

func scanAlert(row scanner) (domain.Alert, error) {
	var alert domain.Alert
	var closedBy, closureReason *string
	err := row.Scan(
		&alert.ID, &alert.TransactionID, &alert.CustomerID, &alert.RuleCode, &alert.RuleVersion,
		&alert.Severity, &alert.Status, &alert.ReasonCode, &alert.Description, &alert.CreatedAt,
		&alert.ClosedAt, &closedBy, &closureReason,
	)
	if err != nil {
		return domain.Alert{}, err
	}
	if closedBy != nil {
		alert.ClosedBy = *closedBy
	}
	if closureReason != nil {
		alert.ClosureReason = *closureReason
	}
	return alert, nil
}

type scanner interface {
	Scan(...any) error
}

func scanCustomer(row scanner) (domain.Customer, error) {
	var customer domain.Customer
	var riskFactors, riskReasons []byte
	var reviewedBy *string
	err := row.Scan(
		&customer.ID, &customer.ExternalRef, &customer.Type, &customer.LegalName, &customer.CountryCode,
		&riskFactors, &customer.RiskAssessment.Score, &customer.RiskAssessment.Rating,
		&customer.RiskAssessment.DueDiligence, &riskReasons, &customer.RiskAssessment.RuleVersion,
		&customer.RiskAssessment.AssessedAt, &customer.Status, &customer.CreatedBy,
		&reviewedBy, &customer.ReviewedAt, &customer.CreatedAt,
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
	if reviewedBy != nil {
		customer.ReviewedBy = *reviewedBy
	}
	return customer, nil
}

const customerSelect = `id, external_ref, customer_type, legal_name, country_code,
	risk_factors, risk_score, risk_rating, due_diligence, risk_reasons,
	risk_rule_version, risk_assessed_at, status, created_by, reviewed_by, reviewed_at, created_at`

func (r *Repository) GetCustomer(ctx context.Context, id string) (domain.Customer, error) {
	customer, err := scanCustomer(r.pool.QueryRow(ctx, "SELECT "+customerSelect+" FROM customers WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Customer{}, domain.ErrCustomerNotFound
	}
	if err != nil {
		return domain.Customer{}, fmt.Errorf("get customer: %w", err)
	}
	return customer, nil
}

func (r *Repository) ListCustomers(ctx context.Context, status domain.CustomerStatus, page application.PageRequest) ([]domain.Customer, error) {
	rows, err := r.pool.Query(ctx, "SELECT "+customerSelect+` FROM customers
		WHERE ($1 = '' OR status = $1) AND (NOT $2 OR (created_at, id) < ($3, $4::uuid))
		ORDER BY created_at DESC, id DESC LIMIT $5`, status, !page.CursorTime.IsZero(), page.CursorTime, nullableCursorID(page.CursorID), page.Limit)
	if err != nil {
		return nil, fmt.Errorf("list customers: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Customer, 0)
	for rows.Next() {
		item, err := scanCustomer(rows)
		if err != nil {
			return nil, fmt.Errorf("scan customer: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListCustomerTransactions(ctx context.Context, customerID string, page application.PageRequest) ([]domain.Transaction, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, idempotency_key, external_ref, customer_id, direction, amount_minor,
		currency, counterparty_country, occurred_at, ingested_at, ingested_by FROM transactions
		WHERE customer_id = $1 AND (NOT $2 OR (occurred_at, id) < ($3, $4::uuid))
		ORDER BY occurred_at DESC, id DESC LIMIT $5`, customerID, !page.CursorTime.IsZero(), page.CursorTime, nullableCursorID(page.CursorID), page.Limit)
	if err != nil {
		return nil, fmt.Errorf("list customer transactions: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Transaction, 0)
	for rows.Next() {
		item, err := scanTransaction(rows)
		if err != nil {
			return nil, fmt.Errorf("scan transaction: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListAuditEventsPage(ctx context.Context, aggregateID string, page application.PageRequest) ([]domain.AuditEvent, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, aggregate_type, aggregate_id, event_type, actor, occurred_at, payload FROM audit_events
		WHERE aggregate_id = $1 AND (NOT $2 OR (occurred_at, id) < ($3, $4::uuid))
		ORDER BY occurred_at DESC, id DESC LIMIT $5`, aggregateID, !page.CursorTime.IsZero(), page.CursorTime, nullableCursorID(page.CursorID), page.Limit)
	if err != nil {
		return nil, fmt.Errorf("list audit events page: %w", err)
	}
	defer rows.Close()
	items := make([]domain.AuditEvent, 0)
	for rows.Next() {
		var item domain.AuditEvent
		var payload []byte
		if err := rows.Scan(&item.ID, &item.AggregateType, &item.AggregateID, &item.EventType, &item.Actor, &item.OccurredAt, &payload); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &item.Payload); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListCustomerActivityPage(ctx context.Context, customerID string, page application.PageRequest) ([]domain.AuditEvent, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM customers WHERE id=$1)", customerID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, domain.ErrCustomerNotFound
	}
	rows, err := r.pool.Query(ctx, `SELECT id, aggregate_type, aggregate_id, event_type, actor, occurred_at, payload
		FROM audit_events WHERE (
			(aggregate_type='customer' AND aggregate_id=$1) OR
			(aggregate_type='transaction' AND aggregate_id IN (SELECT id FROM transactions WHERE customer_id=$1)) OR
			(aggregate_type='alert' AND aggregate_id IN (SELECT id FROM alerts WHERE customer_id=$1)) OR
			(aggregate_type='case' AND aggregate_id IN (SELECT id FROM investigation_cases WHERE customer_id=$1))
		) AND (NOT $2 OR (occurred_at,id)<($3,$4::uuid))
		ORDER BY occurred_at DESC,id DESC LIMIT $5`, customerID, !page.CursorTime.IsZero(), page.CursorTime, nullableCursorID(page.CursorID), page.Limit)
	if err != nil {
		return nil, fmt.Errorf("list customer activity: %w", err)
	}
	defer rows.Close()
	items := make([]domain.AuditEvent, 0)
	for rows.Next() {
		var item domain.AuditEvent
		var payload []byte
		if err := rows.Scan(&item.ID, &item.AggregateType, &item.AggregateID, &item.EventType, &item.Actor, &item.OccurredAt, &payload); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &item.Payload); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListAlertsPage(ctx context.Context, status domain.AlertStatus, page application.PageRequest) ([]domain.Alert, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, transaction_id, customer_id, rule_code, rule_version, severity,
		status, reason_code, description, created_at, closed_at, closed_by, closure_reason FROM alerts
		WHERE ($1 = '' OR status = $1) AND (NOT $2 OR (created_at, id) < ($3, $4::uuid))
		ORDER BY created_at DESC, id DESC LIMIT $5`, status, !page.CursorTime.IsZero(), page.CursorTime, nullableCursorID(page.CursorID), page.Limit)
	if err != nil {
		return nil, fmt.Errorf("list alerts page: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Alert, 0)
	for rows.Next() {
		item, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const caseSelect = `id, alert_id, customer_id, title, priority, status, assigned_to, resolution,
	created_by, created_at, updated_at, resolved_by, resolved_at`

func scanCase(row scanner) (domain.InvestigationCase, error) {
	var item domain.InvestigationCase
	var assignedTo, resolution, resolvedBy *string
	err := row.Scan(&item.ID, &item.AlertID, &item.CustomerID, &item.Title, &item.Priority, &item.Status,
		&assignedTo, &resolution, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt, &resolvedBy, &item.ResolvedAt)
	if assignedTo != nil {
		item.AssignedTo = *assignedTo
	}
	if resolution != nil {
		item.Resolution = *resolution
	}
	if resolvedBy != nil {
		item.ResolvedBy = *resolvedBy
	}
	return item, err
}

func insertAuditEvent(ctx context.Context, tx pgx.Tx, event domain.AuditEvent) error {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events (id, aggregate_type, aggregate_id, event_type, actor, occurred_at, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, event.ID, event.AggregateType, event.AggregateID, event.EventType, event.Actor, event.OccurredAt, payload)
	return err
}

func (r *Repository) CreateCase(ctx context.Context, item domain.InvestigationCase, event domain.AuditEvent) (stored domain.InvestigationCase, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return stored, fmt.Errorf("begin create case: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status domain.AlertStatus
	if err := tx.QueryRow(ctx, "SELECT status, customer_id FROM alerts WHERE id = $1 FOR UPDATE", item.AlertID).Scan(&status, &item.CustomerID); errors.Is(err, pgx.ErrNoRows) {
		return stored, domain.ErrAlertNotFound
	} else if err != nil {
		return stored, fmt.Errorf("find case alert: %w", err)
	}
	if status != domain.AlertOpen {
		return stored, domain.ErrAlertConflict
	}
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM investigation_cases WHERE alert_id = $1)", item.AlertID).Scan(&exists); err != nil {
		return stored, err
	}
	if exists {
		return stored, domain.ErrAlertHasCase
	}
	stored, err = scanCase(tx.QueryRow(ctx, `INSERT INTO investigation_cases
		(id, alert_id, customer_id, title, priority, status, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+caseSelect,
		item.ID, item.AlertID, item.CustomerID, item.Title, item.Priority, item.Status, item.CreatedBy, item.CreatedAt, item.UpdatedAt))
	if err != nil {
		return stored, fmt.Errorf("insert case: %w", err)
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return stored, fmt.Errorf("audit case creation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return stored, fmt.Errorf("commit create case: %w", err)
	}
	return stored, nil
}

func (r *Repository) GetCase(ctx context.Context, id string) (domain.InvestigationCase, error) {
	item, err := scanCase(r.pool.QueryRow(ctx, "SELECT "+caseSelect+" FROM investigation_cases WHERE id=$1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return item, domain.ErrCaseNotFound
	}
	return item, err
}

func (r *Repository) ListCasesPage(ctx context.Context, status domain.CaseStatus, page application.PageRequest) ([]domain.InvestigationCase, error) {
	rows, err := r.pool.Query(ctx, "SELECT "+caseSelect+` FROM investigation_cases
		WHERE ($1='' OR status=$1) AND (NOT $2 OR (updated_at,id)<($3,$4::uuid))
		ORDER BY updated_at DESC,id DESC LIMIT $5`, status, !page.CursorTime.IsZero(), page.CursorTime, nullableCursorID(page.CursorID), page.Limit)
	if err != nil {
		return nil, fmt.Errorf("list cases: %w", err)
	}
	defer rows.Close()
	items := make([]domain.InvestigationCase, 0)
	for rows.Next() {
		item, err := scanCase(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListCaseComments(ctx context.Context, caseID string) ([]domain.CaseComment, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,case_id,author,body,created_at FROM case_comments WHERE case_id=$1 ORDER BY created_at,id`, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.CaseComment, 0)
	for rows.Next() {
		var item domain.CaseComment
		if err := rows.Scan(&item.ID, &item.CaseID, &item.Author, &item.Body, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) AssignCase(ctx context.Context, id, assignee string, event domain.AuditEvent) (stored domain.InvestigationCase, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return stored, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stored, err = scanCase(tx.QueryRow(ctx, `UPDATE investigation_cases SET assigned_to=$2,status='in_progress',updated_at=$3
		WHERE id=$1 AND status<>'resolved' RETURNING `+caseSelect, id, assignee, event.OccurredAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return stored, r.caseMutationError(ctx, tx, id)
	}
	if err != nil {
		return stored, err
	}
	if err := insertAuditEvent(ctx, tx, event); err != nil {
		return stored, err
	}
	if err := tx.Commit(ctx); err != nil {
		return stored, err
	}
	return stored, nil
}

func (r *Repository) AddCaseComment(ctx context.Context, comment domain.CaseComment, event domain.AuditEvent) (stored domain.CaseComment, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return stored, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status domain.CaseStatus
	if err := tx.QueryRow(ctx, "SELECT status FROM investigation_cases WHERE id=$1 FOR UPDATE", comment.CaseID).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		return stored, domain.ErrCaseNotFound
	} else if err != nil {
		return stored, err
	}
	if status == domain.CaseResolved {
		return stored, domain.ErrCaseConflict
	}
	err = tx.QueryRow(ctx, `INSERT INTO case_comments(id,case_id,author,body,created_at) VALUES($1,$2,$3,$4,$5) RETURNING id,case_id,author,body,created_at`, comment.ID, comment.CaseID, comment.Author, comment.Body, comment.CreatedAt).Scan(&stored.ID, &stored.CaseID, &stored.Author, &stored.Body, &stored.CreatedAt)
	if err != nil {
		return stored, err
	}
	if _, err = tx.Exec(ctx, "UPDATE investigation_cases SET updated_at=$2 WHERE id=$1", comment.CaseID, comment.CreatedAt); err != nil {
		return stored, err
	}
	if err = insertAuditEvent(ctx, tx, event); err != nil {
		return stored, err
	}
	if err = tx.Commit(ctx); err != nil {
		return stored, err
	}
	return stored, nil
}

func (r *Repository) ResolveCase(ctx context.Context, id, resolution, actor string, caseEvent, alertEvent domain.AuditEvent) (stored domain.InvestigationCase, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return stored, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stored, err = scanCase(tx.QueryRow(ctx, `UPDATE investigation_cases SET status='resolved',resolution=$2,resolved_by=$3,resolved_at=$4,updated_at=$4 WHERE id=$1 AND status<>'resolved' RETURNING `+caseSelect, id, resolution, actor, caseEvent.OccurredAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return stored, r.caseMutationError(ctx, tx, id)
	}
	if err != nil {
		return stored, err
	}
	alertEvent.AggregateID = stored.AlertID
	result, err := tx.Exec(ctx, `UPDATE alerts SET status='closed',closed_at=$2,closed_by=$3,closure_reason=$4 WHERE id=$1 AND status='open'`, stored.AlertID, caseEvent.OccurredAt, actor, resolution)
	if err != nil {
		return stored, err
	}
	if result.RowsAffected() != 1 {
		return stored, domain.ErrAlertConflict
	}
	if err = insertAuditEvent(ctx, tx, caseEvent); err != nil {
		return stored, err
	}
	if err = insertAuditEvent(ctx, tx, alertEvent); err != nil {
		return stored, err
	}
	if err = tx.Commit(ctx); err != nil {
		return stored, err
	}
	return stored, nil
}

func (r *Repository) caseMutationError(ctx context.Context, tx pgx.Tx, id string) error {
	var status domain.CaseStatus
	if err := tx.QueryRow(ctx, "SELECT status FROM investigation_cases WHERE id=$1", id).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrCaseNotFound
	} else if err != nil {
		return err
	}
	return domain.ErrCaseConflict
}

func (r *Repository) GetDueDiligence(ctx context.Context, customerID string) (domain.DueDiligenceDetails, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM customers WHERE id=$1)", customerID).Scan(&exists); err != nil {
		return domain.DueDiligenceDetails{}, err
	}
	if !exists {
		return domain.DueDiligenceDetails{}, domain.ErrCustomerNotFound
	}
	result := domain.DueDiligenceDetails{Profile: domain.CDDProfile{CustomerID: customerID, Status: domain.CDDIncomplete}, BeneficialOwners: []domain.BeneficialOwner{}, Documents: []domain.KYCDocument{}}
	err := r.pool.QueryRow(ctx, `SELECT source_of_wealth,business_purpose,expected_monthly_volume_minor,currency,status,next_review_at,updated_by,updated_at FROM customer_due_diligence WHERE customer_id=$1`, customerID).Scan(&result.Profile.SourceOfWealth, &result.Profile.BusinessPurpose, &result.Profile.ExpectedMonthlyVolumeMinor, &result.Profile.Currency, &result.Profile.Status, &result.Profile.NextReviewAt, &result.Profile.UpdatedBy, &result.Profile.UpdatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	rows, err := r.pool.Query(ctx, `SELECT id,customer_id,full_name,ownership_percent,country_code,pep,created_by,created_at FROM beneficial_owners WHERE customer_id=$1 ORDER BY created_at,id`, customerID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var o domain.BeneficialOwner
		if err := rows.Scan(&o.ID, &o.CustomerID, &o.FullName, &o.OwnershipPercent, &o.CountryCode, &o.PEP, &o.CreatedBy, &o.CreatedAt); err != nil {
			rows.Close()
			return result, err
		}
		result.BeneficialOwners = append(result.BeneficialOwners, o)
	}
	rows.Close()
	rows, err = r.pool.Query(ctx, `SELECT id,customer_id,document_type,reference,status,expires_at,created_by,created_at,verified_by,verified_at FROM kyc_documents WHERE customer_id=$1 ORDER BY created_at,id`, customerID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		d, err := scanKYCDocument(rows)
		if err != nil {
			return result, err
		}
		result.Documents = append(result.Documents, d)
	}
	return result, rows.Err()
}
func scanKYCDocument(row scanner) (domain.KYCDocument, error) {
	var d domain.KYCDocument
	var verifiedBy *string
	err := row.Scan(&d.ID, &d.CustomerID, &d.Type, &d.Reference, &d.Status, &d.ExpiresAt, &d.CreatedBy, &d.CreatedAt, &verifiedBy, &d.VerifiedAt)
	if verifiedBy != nil {
		d.VerifiedBy = *verifiedBy
	}
	return d, err
}
func (r *Repository) UpsertCDDProfile(ctx context.Context, p domain.CDDProfile, event domain.AuditEvent) (stored domain.CDDProfile, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return stored, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `INSERT INTO customer_due_diligence(customer_id,source_of_wealth,business_purpose,expected_monthly_volume_minor,currency,status,next_review_at,updated_by,updated_at) SELECT id,$2,$3,$4,$5,$6,$7,$8,$9 FROM customers WHERE id=$1 ON CONFLICT(customer_id) DO UPDATE SET source_of_wealth=excluded.source_of_wealth,business_purpose=excluded.business_purpose,expected_monthly_volume_minor=excluded.expected_monthly_volume_minor,currency=excluded.currency,status=excluded.status,next_review_at=excluded.next_review_at,updated_by=excluded.updated_by,updated_at=excluded.updated_at RETURNING customer_id,source_of_wealth,business_purpose,expected_monthly_volume_minor,currency,status,next_review_at,updated_by,updated_at`, p.CustomerID, p.SourceOfWealth, p.BusinessPurpose, p.ExpectedMonthlyVolumeMinor, p.Currency, p.Status, p.NextReviewAt, p.UpdatedBy, p.UpdatedAt).Scan(&stored.CustomerID, &stored.SourceOfWealth, &stored.BusinessPurpose, &stored.ExpectedMonthlyVolumeMinor, &stored.Currency, &stored.Status, &stored.NextReviewAt, &stored.UpdatedBy, &stored.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return stored, domain.ErrCustomerNotFound
	}
	if err != nil {
		return stored, err
	}
	if err = insertAuditEvent(ctx, tx, event); err != nil {
		return stored, err
	}
	if err = tx.Commit(ctx); err != nil {
		return stored, err
	}
	return stored, nil
}
func (r *Repository) AddBeneficialOwner(ctx context.Context, o domain.BeneficialOwner, event domain.AuditEvent) (stored domain.BeneficialOwner, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return stored, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `INSERT INTO beneficial_owners(id,customer_id,full_name,ownership_percent,country_code,pep,created_by,created_at) SELECT $1,id,$3,$4,$5,$6,$7,$8 FROM customers WHERE id=$2 RETURNING id,customer_id,full_name,ownership_percent,country_code,pep,created_by,created_at`, o.ID, o.CustomerID, o.FullName, o.OwnershipPercent, o.CountryCode, o.PEP, o.CreatedBy, o.CreatedAt).Scan(&stored.ID, &stored.CustomerID, &stored.FullName, &stored.OwnershipPercent, &stored.CountryCode, &stored.PEP, &stored.CreatedBy, &stored.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return stored, domain.ErrCustomerNotFound
	}
	if err != nil {
		return stored, err
	}
	if err = insertAuditEvent(ctx, tx, event); err != nil {
		return stored, err
	}
	if err = tx.Commit(ctx); err != nil {
		return stored, err
	}
	return stored, nil
}
func (r *Repository) AddKYCDocument(ctx context.Context, d domain.KYCDocument, event domain.AuditEvent) (stored domain.KYCDocument, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return stored, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stored, err = scanKYCDocument(tx.QueryRow(ctx, `INSERT INTO kyc_documents(id,customer_id,document_type,reference,status,expires_at,created_by,created_at) SELECT $1,id,$3,$4,$5,$6,$7,$8 FROM customers WHERE id=$2 RETURNING id,customer_id,document_type,reference,status,expires_at,created_by,created_at,verified_by,verified_at`, d.ID, d.CustomerID, d.Type, d.Reference, d.Status, d.ExpiresAt, d.CreatedBy, d.CreatedAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return stored, domain.ErrCustomerNotFound
	}
	if err != nil {
		return stored, err
	}
	if err = insertAuditEvent(ctx, tx, event); err != nil {
		return stored, err
	}
	if err = tx.Commit(ctx); err != nil {
		return stored, err
	}
	return stored, nil
}
func (r *Repository) ReviewKYCDocument(ctx context.Context, id string, status domain.DocumentStatus, actor string, event domain.AuditEvent) (stored domain.KYCDocument, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return stored, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stored, err = scanKYCDocument(tx.QueryRow(ctx, `UPDATE kyc_documents SET status=$2,verified_by=$3,verified_at=$4 WHERE id=$1 AND status='pending' RETURNING id,customer_id,document_type,reference,status,expires_at,created_by,created_at,verified_by,verified_at`, id, status, actor, event.OccurredAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return stored, domain.ErrReviewConflict
	}
	if err != nil {
		return stored, err
	}
	event.AggregateID = stored.CustomerID
	if err = insertAuditEvent(ctx, tx, event); err != nil {
		return stored, err
	}
	if err = tx.Commit(ctx); err != nil {
		return stored, err
	}
	return stored, nil
}

func (r *Repository) SaveScreening(ctx context.Context, runs []domain.ScreeningRun, matches []domain.ScreeningMatch, notifications []domain.Notification, outbox []domain.OutboxMessage, events []domain.AuditEvent) (err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, run := range runs {
		if _, err = tx.Exec(ctx, `INSERT INTO screening_runs(id,customer_id,subject_type,subject_id,query_name,provider,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, run.ID, run.CustomerID, run.SubjectType, run.SubjectID, run.QueryName, run.Provider, run.CreatedBy, run.CreatedAt); err != nil {
			return err
		}
	}
	for _, m := range matches {
		if _, err = tx.Exec(ctx, `INSERT INTO screening_matches(id,run_id,customer_id,subject_type,subject_id,query_name,list_type,matched_name,score,reason,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, m.ID, m.RunID, m.CustomerID, m.SubjectType, m.SubjectID, m.QueryName, m.ListType, m.MatchedName, m.Score, m.Reason, m.Status, m.CreatedAt); err != nil {
			return err
		}
	}
	for _, n := range notifications {
		if _, err = tx.Exec(ctx, `INSERT INTO notifications(id,customer_id,match_id,type,title,message,read,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, n.ID, n.CustomerID, n.MatchID, n.Type, n.Title, n.Message, n.Read, n.CreatedAt); err != nil {
			return err
		}
	}
	for _, message := range outbox {
		payload, marshalErr := json.Marshal(message.Payload)
		if marshalErr != nil {
			return marshalErr
		}
		if _, err = tx.Exec(ctx, `INSERT INTO notification_outbox(id,notification_id,destination,payload,status,attempts,next_attempt_at,last_error,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, message.ID, message.NotificationID, message.Destination, payload, message.Status, message.Attempts, message.NextAttemptAt, message.LastError, message.CreatedAt); err != nil {
			return err
		}
	}
	for _, event := range events {
		if err = insertAuditEvent(ctx, tx, event); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func scanNotification(row scanner) (domain.Notification, error) {
	var n domain.Notification
	var readBy *string
	err := row.Scan(&n.ID, &n.CustomerID, &n.MatchID, &n.Type, &n.Title, &n.Message, &n.Read, &n.CreatedAt, &readBy, &n.ReadAt)
	if readBy != nil {
		n.ReadBy = *readBy
	}
	return n, err
}

const notificationSelect = `id,customer_id,match_id,type,title,message,read,created_at,read_by,read_at`

func (r *Repository) ListNotifications(ctx context.Context, limit int) ([]domain.Notification, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+notificationSelect+` FROM notifications ORDER BY read,created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Notification{}
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, n)
	}
	return items, rows.Err()
}
func (r *Repository) ReadNotification(ctx context.Context, id, actor string, at time.Time) (domain.Notification, error) {
	n, err := scanNotification(r.pool.QueryRow(ctx, `UPDATE notifications SET read=true,read_by=$2,read_at=$3 WHERE id=$1 RETURNING `+notificationSelect, id, actor, at))
	if errors.Is(err, pgx.ErrNoRows) {
		return n, domain.ErrNotificationNotFound
	}
	return n, err
}
func scanOutbox(row scanner) (domain.OutboxMessage, error) {
	var item domain.OutboxMessage
	var payload []byte
	err := row.Scan(&item.ID, &item.NotificationID, &item.Destination, &payload, &item.Status, &item.Attempts, &item.NextAttemptAt, &item.LastError, &item.LeaseOwner, &item.LeaseUntil, &item.CreatedAt, &item.DeliveredAt)
	if err == nil {
		err = json.Unmarshal(payload, &item.Payload)
	}
	return item, err
}

const outboxReturning = `o.id,o.notification_id,o.destination,o.payload,o.status,o.attempts,o.next_attempt_at,o.last_error,COALESCE(o.lease_owner,''),o.lease_until,o.created_at,o.delivered_at`

func (r *Repository) ClaimOutbox(ctx context.Context, now time.Time, limit int, owner string, leaseUntil time.Time) ([]domain.OutboxMessage, error) {
	rows, err := r.pool.Query(ctx, `WITH due AS (SELECT id FROM notification_outbox WHERE status='pending' AND next_attempt_at<=$1 AND (lease_until IS NULL OR lease_until<=$1) ORDER BY next_attempt_at FOR UPDATE SKIP LOCKED LIMIT $2) UPDATE notification_outbox o SET lease_owner=$3,lease_until=$4 FROM due WHERE o.id=due.id RETURNING `+outboxReturning, now, limit, owner, leaseUntil)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.OutboxMessage{}
	for rows.Next() {
		item, err := scanOutbox(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (r *Repository) CompleteOutbox(ctx context.Context, id, owner string, attemptedAt, next time.Time, lastError string) error {
	command, err := r.pool.Exec(ctx, `UPDATE notification_outbox SET status=CASE WHEN $5='' THEN 'delivered' ELSE 'pending' END,attempts=attempts+1,next_attempt_at=$4::timestamptz,last_error=$5,delivered_at=CASE WHEN $5='' THEN $3::timestamptz ELSE NULL END,lease_owner=NULL,lease_until=NULL WHERE id=$1 AND lease_owner=$2`, id, owner, attemptedAt, next, lastError)
	if err == nil && command.RowsAffected() == 0 {
		return domain.ErrNotificationNotFound
	}
	return err
}
func (r *Repository) CountPendingOutbox(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM notification_outbox WHERE status='pending'`).Scan(&count)
	return count, err
}
func scanScreeningMatch(row scanner) (domain.ScreeningMatch, error) {
	var m domain.ScreeningMatch
	var reviewedBy, reason *string
	err := row.Scan(&m.ID, &m.RunID, &m.CustomerID, &m.SubjectType, &m.SubjectID, &m.QueryName, &m.ListType, &m.MatchedName, &m.Score, &m.Reason, &m.Status, &m.CreatedAt, &reviewedBy, &m.ReviewedAt, &reason)
	if reviewedBy != nil {
		m.ReviewedBy = *reviewedBy
	}
	if reason != nil {
		m.DispositionReason = *reason
	}
	return m, err
}

const screeningMatchSelect = `id,run_id,customer_id,subject_type,subject_id,query_name,list_type,matched_name,score,reason,status,created_at,reviewed_by,reviewed_at,disposition_reason`

func (r *Repository) ListScreeningMatches(ctx context.Context, customerID string) ([]domain.ScreeningMatch, error) {
	rows, err := r.pool.Query(ctx, "SELECT "+screeningMatchSelect+` FROM screening_matches WHERE customer_id=$1 ORDER BY created_at DESC,id`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.ScreeningMatch{}
	for rows.Next() {
		m, err := scanScreeningMatch(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}
func (r *Repository) DispositionScreeningMatch(ctx context.Context, id string, status domain.ScreeningMatchStatus, reason, actor string, event domain.AuditEvent) (stored domain.ScreeningMatch, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return stored, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stored, err = scanScreeningMatch(tx.QueryRow(ctx, "UPDATE screening_matches SET status=$2,reviewed_by=$3,reviewed_at=$4,disposition_reason=$5 WHERE id=$1 AND status='potential' RETURNING "+screeningMatchSelect, id, status, actor, event.OccurredAt, reason))
	if errors.Is(err, pgx.ErrNoRows) {
		return stored, domain.ErrReviewConflict
	}
	if err != nil {
		return stored, err
	}
	event.AggregateID = stored.CustomerID
	if err = insertAuditEvent(ctx, tx, event); err != nil {
		return stored, err
	}
	if err = tx.Commit(ctx); err != nil {
		return stored, err
	}
	return stored, nil
}

func scanScreeningSchedule(row scanner) (domain.ScreeningSchedule, error) {
	var schedule domain.ScreeningSchedule
	err := row.Scan(&schedule.CustomerID, &schedule.Enabled, &schedule.IntervalHours, &schedule.NextRunAt, &schedule.LastRunAt, &schedule.LastError, &schedule.FailureCount, &schedule.LeaseOwner, &schedule.LeaseUntil, &schedule.UpdatedBy, &schedule.UpdatedAt)
	return schedule, err
}

const screeningScheduleSelect = `customer_id,enabled,interval_hours,next_run_at,last_run_at,last_error,failure_count,COALESCE(lease_owner,''),lease_until,updated_by,updated_at`
const screeningScheduleReturning = `s.customer_id,s.enabled,s.interval_hours,s.next_run_at,s.last_run_at,s.last_error,s.failure_count,COALESCE(s.lease_owner,''),s.lease_until,s.updated_by,s.updated_at`

func (r *Repository) UpsertScreeningSchedule(ctx context.Context, schedule domain.ScreeningSchedule, event domain.AuditEvent) (stored domain.ScreeningSchedule, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return stored, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stored, err = scanScreeningSchedule(tx.QueryRow(ctx, `INSERT INTO screening_schedules(customer_id,enabled,interval_hours,next_run_at,updated_by,updated_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(customer_id) DO UPDATE SET enabled=EXCLUDED.enabled,interval_hours=EXCLUDED.interval_hours,next_run_at=EXCLUDED.next_run_at,last_error='',failure_count=0,lease_owner=NULL,lease_until=NULL,updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at RETURNING `+screeningScheduleSelect, schedule.CustomerID, schedule.Enabled, schedule.IntervalHours, schedule.NextRunAt, schedule.UpdatedBy, schedule.UpdatedAt))
	if err != nil {
		return stored, err
	}
	if err = insertAuditEvent(ctx, tx, event); err != nil {
		return stored, err
	}
	if err = tx.Commit(ctx); err != nil {
		return stored, err
	}
	return stored, nil
}
func (r *Repository) GetScreeningSchedule(ctx context.Context, customerID string) (domain.ScreeningSchedule, error) {
	schedule, err := scanScreeningSchedule(r.pool.QueryRow(ctx, `SELECT `+screeningScheduleSelect+` FROM screening_schedules WHERE customer_id=$1`, customerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return schedule, domain.ErrScreeningScheduleNotFound
	}
	return schedule, err
}
func (r *Repository) ClaimDueScreeningSchedules(ctx context.Context, now time.Time, limit int, owner string, leaseUntil time.Time) ([]domain.ScreeningSchedule, error) {
	rows, err := r.pool.Query(ctx, `WITH due AS (SELECT customer_id FROM screening_schedules WHERE enabled AND next_run_at <= $1 AND (lease_until IS NULL OR lease_until <= $1) ORDER BY next_run_at FOR UPDATE SKIP LOCKED LIMIT $2) UPDATE screening_schedules s SET lease_owner=$3,lease_until=$4 FROM due WHERE s.customer_id=due.customer_id RETURNING `+screeningScheduleReturning, now, limit, owner, leaseUntil)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.ScreeningSchedule{}
	for rows.Next() {
		item, err := scanScreeningSchedule(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (r *Repository) CompleteScreeningSchedule(ctx context.Context, customerID, owner string, ranAt, nextRun time.Time, lastError string) error {
	command, err := r.pool.Exec(ctx, `UPDATE screening_schedules SET last_run_at=$3,last_error=$5,next_run_at=$4,updated_at=$3,failure_count=CASE WHEN $5='' THEN 0 ELSE failure_count+1 END,lease_owner=NULL,lease_until=NULL WHERE customer_id=$1 AND lease_owner=$2`, customerID, owner, ranAt, nextRun, lastError)
	if err == nil && command.RowsAffected() == 0 {
		return domain.ErrScreeningScheduleNotFound
	}
	return err
}

func nullableCursorID(id string) any {
	if id == "" {
		return nil
	}
	return id
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
