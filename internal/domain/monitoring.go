package domain

import (
	"errors"
	"fmt"
	"time"
)

const TransactionMonitoringRuleVersion = "transaction-monitoring-v1"

var (
	ErrAlertNotFound = errors.New("alert not found")
	ErrAlertConflict = errors.New("alert is not open")
)

type AlertSeverity string

const (
	AlertMedium AlertSeverity = "medium"
	AlertHigh   AlertSeverity = "high"
)

type AlertStatus string

const (
	AlertOpen   AlertStatus = "open"
	AlertClosed AlertStatus = "closed"
)

type MonitoringFinding struct {
	RuleCode    string
	RuleVersion string
	Severity    AlertSeverity
	ReasonCode  string
	Description string
}

type Alert struct {
	ID            string        `json:"id"`
	TransactionID string        `json:"transaction_id"`
	CustomerID    string        `json:"customer_id"`
	RuleCode      string        `json:"rule_code"`
	RuleVersion   string        `json:"rule_version"`
	Severity      AlertSeverity `json:"severity"`
	Status        AlertStatus   `json:"status"`
	ReasonCode    string        `json:"reason_code"`
	Description   string        `json:"description"`
	CreatedAt     time.Time     `json:"created_at"`
	ClosedAt      *time.Time    `json:"closed_at,omitempty"`
	ClosedBy      string        `json:"closed_by,omitempty"`
	ClosureReason string        `json:"closure_reason,omitempty"`
}

type TransactionMonitoringEngine struct{}

func (TransactionMonitoringEngine) Evaluate(transaction Transaction) []MonitoringFinding {
	findings := make([]MonitoringFinding, 0, 2)
	if transaction.AmountMinor >= 1_000_000 {
		findings = append(findings, MonitoringFinding{
			RuleCode: "large_transaction", RuleVersion: TransactionMonitoringRuleVersion,
			Severity: AlertHigh, ReasonCode: "amount_threshold_exceeded",
			Description: fmt.Sprintf("Transaction amount %d minor units meets or exceeds the 1000000 threshold", transaction.AmountMinor),
		})
	}
	if highRiskCounterpartyCountries[transaction.CounterpartyCountry] {
		findings = append(findings, MonitoringFinding{
			RuleCode: "high_risk_counterparty_country", RuleVersion: TransactionMonitoringRuleVersion,
			Severity: AlertHigh, ReasonCode: "counterparty_country_high_risk",
			Description: fmt.Sprintf("Counterparty country %s is configured as high risk", transaction.CounterpartyCountry),
		})
	}
	return findings
}

var highRiskCounterpartyCountries = map[string]bool{
	"IR": true,
	"KP": true,
	"SY": true,
}
