package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
