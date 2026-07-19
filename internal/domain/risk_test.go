package domain

import (
	"testing"
	"time"
)

func TestRiskEngine(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		factors RiskFactors
		score   int
		rating  RiskRating
		dd      DueDiligence
	}{
		{"low risk", RiskFactors{CountryRisk: CountryRiskLow, SourceOfFundsVerified: true}, 0, RiskLow, DueDiligenceStandard},
		{"pep and high-risk country", RiskFactors{CountryRisk: CountryRiskHigh, PEP: true, SourceOfFundsVerified: true}, 70, RiskHigh, DueDiligenceEnhanced},
		{"potential sanctions match", RiskFactors{CountryRisk: CountryRiskLow, SanctionsPotentialMatch: true, SourceOfFundsVerified: true}, 100, RiskHigh, DueDiligenceBlocked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := (RiskEngine{}).Assess(tt.factors, now)
			if got.Score != tt.score || got.Rating != tt.rating || got.DueDiligence != tt.dd {
				t.Fatalf("got score=%d rating=%s dd=%s", got.Score, got.Rating, got.DueDiligence)
			}
			if got.RuleVersion != RiskRuleVersion {
				t.Fatalf("unexpected rule version %q", got.RuleVersion)
			}
		})
	}
}
