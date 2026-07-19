package domain

import "time"

const RiskRuleVersion = "customer-risk-v1"

type RiskEngine struct{}

func (RiskEngine) Assess(f RiskFactors, now time.Time) RiskAssessment {
	result := RiskAssessment{
		Reasons:     make([]RiskReason, 0, 6),
		AssessedAt:  now.UTC(),
		RuleVersion: RiskRuleVersion,
	}

	add := func(code string, points int, detail string) {
		result.Score += points
		result.Reasons = append(result.Reasons, RiskReason{Code: code, Points: points, Detail: detail})
	}

	switch f.CountryRisk {
	case CountryRiskHigh:
		add("HIGH_RISK_COUNTRY", 35, "Customer is associated with a high-risk country")
	case CountryRiskMedium:
		add("MEDIUM_RISK_COUNTRY", 15, "Customer is associated with a medium-risk country")
	}
	if f.PEP {
		add("PEP", 35, "Customer is a politically exposed person")
	}
	if f.SanctionsPotentialMatch {
		add("SANCTIONS_POTENTIAL_MATCH", 100, "Potential sanctions match requires human review")
	}
	if f.HighRiskIndustry {
		add("HIGH_RISK_INDUSTRY", 20, "Customer operates in a higher-risk industry")
	}
	if f.ComplexOwnership {
		add("COMPLEX_OWNERSHIP", 20, "Ownership structure requires additional verification")
	}
	if !f.SourceOfFundsVerified {
		add("SOURCE_OF_FUNDS_UNVERIFIED", 20, "Source of funds has not been verified")
	}

	switch {
	case f.SanctionsPotentialMatch:
		result.Rating = RiskHigh
		result.DueDiligence = DueDiligenceBlocked
	case result.Score >= 50:
		result.Rating = RiskHigh
		result.DueDiligence = DueDiligenceEnhanced
	case result.Score >= 20:
		result.Rating = RiskMedium
		result.DueDiligence = DueDiligenceStandard
	default:
		result.Rating = RiskLow
		result.DueDiligence = DueDiligenceStandard
	}

	return result
}
