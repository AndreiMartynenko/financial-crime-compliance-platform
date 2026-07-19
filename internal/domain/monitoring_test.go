package domain

import "testing"

func TestTransactionMonitoringEngine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		transaction Transaction
		rules       []string
	}{
		{name: "no findings", transaction: Transaction{AmountMinor: 50_000, CounterpartyCountry: "GB"}},
		{name: "large transaction", transaction: Transaction{AmountMinor: 1_000_000, CounterpartyCountry: "GB"}, rules: []string{"large_transaction"}},
		{name: "two findings", transaction: Transaction{AmountMinor: 2_000_000, CounterpartyCountry: "IR"}, rules: []string{"large_transaction", "high_risk_counterparty_country"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := (TransactionMonitoringEngine{}).Evaluate(tt.transaction)
			if len(findings) != len(tt.rules) {
				t.Fatalf("findings=%+v", findings)
			}
			for index, rule := range tt.rules {
				if findings[index].RuleCode != rule || findings[index].RuleVersion != TransactionMonitoringRuleVersion {
					t.Fatalf("finding=%+v", findings[index])
				}
			}
		})
	}
}
