package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
)

var ErrInvalidTransaction = errors.New("invalid transaction")

type TransactionRepository interface {
	CreateTransaction(context.Context, domain.Transaction, domain.AuditEvent) error
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
}

type TransactionService struct {
	repo TransactionRepository
	now  func() time.Time
}

func NewTransactionService(repo TransactionRepository) *TransactionService {
	return &TransactionService{repo: repo, now: time.Now}
}

func (s *TransactionService) Ingest(ctx context.Context, cmd IngestTransactionCommand) (domain.Transaction, error) {
	cmd.ExternalRef = strings.TrimSpace(cmd.ExternalRef)
	cmd.CustomerID = strings.TrimSpace(cmd.CustomerID)
	cmd.Currency = strings.ToUpper(strings.TrimSpace(cmd.Currency))
	cmd.CounterpartyCountry = strings.ToUpper(strings.TrimSpace(cmd.CounterpartyCountry))
	cmd.Actor = strings.TrimSpace(cmd.Actor)
	if cmd.CustomerID == "" || cmd.AmountMinor <= 0 || len(cmd.Currency) != 3 || len(cmd.CounterpartyCountry) != 2 || cmd.OccurredAt.IsZero() || cmd.Actor == "" {
		return domain.Transaction{}, ErrInvalidTransaction
	}
	if cmd.Direction != domain.TransactionInbound && cmd.Direction != domain.TransactionOutbound {
		return domain.Transaction{}, ErrInvalidTransaction
	}

	now := s.now().UTC()
	transaction := domain.Transaction{
		ID: newID(), ExternalRef: cmd.ExternalRef, CustomerID: cmd.CustomerID,
		Direction: cmd.Direction, AmountMinor: cmd.AmountMinor, Currency: cmd.Currency,
		CounterpartyCountry: cmd.CounterpartyCountry, OccurredAt: cmd.OccurredAt.UTC(),
		IngestedAt: now, IngestedBy: cmd.Actor,
	}
	event := domain.AuditEvent{
		ID: newID(), AggregateType: "transaction", AggregateID: transaction.ID,
		EventType: "transaction.ingested", Actor: cmd.Actor, OccurredAt: now,
		Payload: map[string]any{
			"customer_id": transaction.CustomerID, "amount_minor": transaction.AmountMinor,
			"currency": transaction.Currency, "direction": transaction.Direction,
		},
	}
	if err := s.repo.CreateTransaction(ctx, transaction, event); err != nil {
		return domain.Transaction{}, err
	}
	return transaction, nil
}
