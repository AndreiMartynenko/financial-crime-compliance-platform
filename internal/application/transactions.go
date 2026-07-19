package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
)

var (
	ErrInvalidTransaction = errors.New("invalid transaction")
	ErrInvalidAlertReview = errors.New("invalid alert review")
)

type TransactionRepository interface {
	CreateTransaction(context.Context, domain.Transaction, domain.AuditEvent, []domain.Alert, []domain.AuditEvent) (domain.Transaction, []domain.Alert, bool, error)
	ListAlerts(context.Context, domain.AlertStatus) ([]domain.Alert, error)
	CloseAlert(context.Context, string, string, string, domain.AuditEvent) (domain.Alert, error)
}

type IngestTransactionCommand struct {
	ExternalRef         string                      `json:"external_ref"`
	CustomerID          string                      `json:"customer_id"`
	Direction           domain.TransactionDirection `json:"direction"`
	AmountMinor         int64                       `json:"amount_minor"`
	Currency            string                      `json:"currency"`
	CounterpartyCountry string                      `json:"counterparty_country"`
	OccurredAt          time.Time                   `json:"occurred_at"`
	Actor               string                      `json:"-"`
	IdempotencyKey      string                      `json:"-"`
}

type TransactionService struct {
	repo TransactionRepository
	now  func() time.Time
}

type IngestTransactionResult struct {
	Transaction domain.Transaction `json:"transaction"`
	Alerts      []domain.Alert     `json:"alerts"`
	Replayed    bool               `json:"-"`
}

func NewTransactionService(repo TransactionRepository) *TransactionService {
	return &TransactionService{repo: repo, now: time.Now}
}

func (s *TransactionService) Ingest(ctx context.Context, cmd IngestTransactionCommand) (IngestTransactionResult, error) {
	cmd.ExternalRef = strings.TrimSpace(cmd.ExternalRef)
	cmd.CustomerID = strings.TrimSpace(cmd.CustomerID)
	cmd.Currency = strings.ToUpper(strings.TrimSpace(cmd.Currency))
	cmd.CounterpartyCountry = strings.ToUpper(strings.TrimSpace(cmd.CounterpartyCountry))
	cmd.Actor = strings.TrimSpace(cmd.Actor)
	cmd.IdempotencyKey = strings.TrimSpace(cmd.IdempotencyKey)
	if cmd.CustomerID == "" || cmd.AmountMinor <= 0 || len(cmd.Currency) != 3 || len(cmd.CounterpartyCountry) != 2 || cmd.OccurredAt.IsZero() || cmd.Actor == "" || cmd.IdempotencyKey == "" || len(cmd.IdempotencyKey) > 200 {
		return IngestTransactionResult{}, ErrInvalidTransaction
	}
	if cmd.Direction != domain.TransactionInbound && cmd.Direction != domain.TransactionOutbound {
		return IngestTransactionResult{}, ErrInvalidTransaction
	}

	now := s.now().UTC()
	transaction := domain.Transaction{
		ID: newID(), IdempotencyKey: cmd.IdempotencyKey, ExternalRef: cmd.ExternalRef, CustomerID: cmd.CustomerID,
		Direction: cmd.Direction, AmountMinor: cmd.AmountMinor, Currency: cmd.Currency,
		CounterpartyCountry: cmd.CounterpartyCountry, OccurredAt: cmd.OccurredAt.UTC().Truncate(time.Microsecond),
		IngestedAt: now, IngestedBy: cmd.Actor,
	}
	transactionEvent := domain.AuditEvent{
		ID: newID(), AggregateType: "transaction", AggregateID: transaction.ID,
		EventType: "transaction.ingested", Actor: cmd.Actor, OccurredAt: now,
		Payload: map[string]any{
			"customer_id": transaction.CustomerID, "amount_minor": transaction.AmountMinor,
			"currency": transaction.Currency, "direction": transaction.Direction,
		},
	}
	findings := (domain.TransactionMonitoringEngine{}).Evaluate(transaction)
	alerts := make([]domain.Alert, 0, len(findings))
	alertEvents := make([]domain.AuditEvent, 0, len(findings))
	for _, finding := range findings {
		alert := domain.Alert{
			ID: newID(), TransactionID: transaction.ID, CustomerID: transaction.CustomerID,
			RuleCode: finding.RuleCode, RuleVersion: finding.RuleVersion, Severity: finding.Severity,
			Status: domain.AlertOpen, ReasonCode: finding.ReasonCode, Description: finding.Description,
			CreatedAt: now,
		}
		alerts = append(alerts, alert)
		alertEvents = append(alertEvents, domain.AuditEvent{
			ID: newID(), AggregateType: "alert", AggregateID: alert.ID,
			EventType: "alert.created", Actor: "transaction-monitoring-engine", OccurredAt: now,
			Payload: map[string]any{
				"transaction_id": transaction.ID, "rule_code": alert.RuleCode,
				"rule_version": alert.RuleVersion, "reason_code": alert.ReasonCode,
			},
		})
	}
	storedTransaction, storedAlerts, replayed, err := s.repo.CreateTransaction(ctx, transaction, transactionEvent, alerts, alertEvents)
	if err != nil {
		return IngestTransactionResult{}, err
	}
	return IngestTransactionResult{Transaction: storedTransaction, Alerts: storedAlerts, Replayed: replayed}, nil
}

func (s *TransactionService) ListAlerts(ctx context.Context, status domain.AlertStatus) ([]domain.Alert, error) {
	if status != "" && status != domain.AlertOpen && status != domain.AlertClosed {
		return nil, ErrInvalidAlertReview
	}
	return s.repo.ListAlerts(ctx, status)
}

func (s *TransactionService) CloseAlert(ctx context.Context, alertID, actor, reason string) (domain.Alert, error) {
	alertID = strings.TrimSpace(alertID)
	actor = strings.TrimSpace(actor)
	reason = strings.TrimSpace(reason)
	if alertID == "" || actor == "" || reason == "" {
		return domain.Alert{}, ErrInvalidAlertReview
	}
	now := s.now().UTC()
	event := domain.AuditEvent{
		ID: newID(), AggregateType: "alert", AggregateID: alertID,
		EventType: "alert.closed", Actor: actor, OccurredAt: now,
		Payload: map[string]any{"reason": reason},
	}
	return s.repo.CloseAlert(ctx, alertID, actor, reason, event)
}
