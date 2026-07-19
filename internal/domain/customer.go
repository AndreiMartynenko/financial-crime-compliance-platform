package domain

import (
	"errors"
	"time"
)

var (
	ErrCustomerNotFound  = errors.New("customer not found")
	ErrReviewConflict    = errors.New("customer is not pending approval")
	ErrMakerCannotReview = errors.New("maker cannot review own customer")
)

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

type CustomerStatus string

const (
	CustomerPendingApproval CustomerStatus = "pending_approval"
	CustomerActive          CustomerStatus = "active"
	CustomerRejected        CustomerStatus = "rejected"
)

type ReviewDecision string

const (
	ReviewApprove ReviewDecision = "approve"
	ReviewReject  ReviewDecision = "reject"
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
	Status         CustomerStatus `json:"status"`
	CreatedBy      string         `json:"created_by"`
	ReviewedBy     string         `json:"reviewed_by,omitempty"`
	ReviewedAt     *time.Time     `json:"reviewed_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

type AuditEvent struct {
	ID            string         `json:"id"`
	AggregateType string         `json:"aggregate_type"`
	AggregateID   string         `json:"aggregate_id"`
	EventType     string         `json:"event_type"`
	Actor         string         `json:"actor"`
	OccurredAt    time.Time      `json:"occurred_at"`
	Payload       map[string]any `json:"payload"`
}
