package domain

import "time"

type CustomerType string

const (
	CustomerIndividual CustomerType = "individual"
	CustomerCompany    CustomerType = "company"
)

type CountryRisk string

const (
	CountryRiskLow    CountryRisk = "low"
	CountryRiskMedium CountryRisk = "medium"
	CountryRiskHigh   CountryRisk = "high"
)

type RiskRating string

const (
	RiskLow    RiskRating = "low"
	RiskMedium RiskRating = "medium"
	RiskHigh   RiskRating = "high"
)

type DueDiligence string

const (
	DueDiligenceStandard DueDiligence = "standard"
	DueDiligenceEnhanced DueDiligence = "enhanced"
	DueDiligenceBlocked  DueDiligence = "blocked_pending_review"
)

type RiskFactors struct {
	CountryRisk             CountryRisk `json:"country_risk"`
	PEP                     bool        `json:"pep"`
	SanctionsPotentialMatch bool        `json:"sanctions_potential_match"`
	HighRiskIndustry        bool        `json:"high_risk_industry"`
	ComplexOwnership        bool        `json:"complex_ownership"`
	SourceOfFundsVerified   bool        `json:"source_of_funds_verified"`
}

type RiskReason struct {
	Code   string `json:"code"`
	Points int    `json:"points"`
	Detail string `json:"detail"`
}

type RiskAssessment struct {
	Score        int          `json:"score"`
	Rating       RiskRating   `json:"rating"`
	DueDiligence DueDiligence `json:"due_diligence"`
	Reasons      []RiskReason `json:"reasons"`
	AssessedAt   time.Time    `json:"assessed_at"`
	RuleVersion  string       `json:"rule_version"`
}

type Customer struct {
	ID             string         `json:"id"`
	ExternalRef    string         `json:"external_ref"`
	Type           CustomerType   `json:"type"`
	LegalName      string         `json:"legal_name"`
	CountryCode    string         `json:"country_code"`
	RiskFactors    RiskFactors    `json:"risk_factors"`
	RiskAssessment RiskAssessment `json:"risk_assessment"`
	CreatedAt      time.Time      `json:"created_at"`
}

type AuditEvent struct {
	ID          string         `json:"id"`
	AggregateID string         `json:"aggregate_id"`
	EventType   string         `json:"event_type"`
	Actor       string         `json:"actor"`
	OccurredAt  time.Time      `json:"occurred_at"`
	Payload     map[string]any `json:"payload"`
}
