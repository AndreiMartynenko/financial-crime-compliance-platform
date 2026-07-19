package domain

import (
	"errors"
	"time"
)

var (
	ErrTransactionNotFound = errors.New("transaction not found")
	ErrCustomerNotActive   = errors.New("customer is not active")
)

type TransactionDirection string

const (
	TransactionInbound  TransactionDirection = "inbound"
	TransactionOutbound TransactionDirection = "outbound"
)

type Transaction struct {
	ID                  string               `json:"id"`
	ExternalRef         string               `json:"external_ref"`
	CustomerID          string               `json:"customer_id"`
	Direction           TransactionDirection `json:"direction"`
	AmountMinor         int64                `json:"amount_minor"`
	Currency            string               `json:"currency"`
	CounterpartyCountry string               `json:"counterparty_country"`
	OccurredAt          time.Time            `json:"occurred_at"`
	IngestedAt          time.Time            `json:"ingested_at"`
	IngestedBy          string               `json:"ingested_by"`
}
