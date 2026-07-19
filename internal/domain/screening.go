package domain

import "time"

type ScreeningSubjectType string

const (
	ScreeningCustomer        ScreeningSubjectType = "customer"
	ScreeningBeneficialOwner ScreeningSubjectType = "beneficial_owner"
)

type ScreeningListType string

const (
	ScreeningSanctions    ScreeningListType = "sanctions"
	ScreeningPEP          ScreeningListType = "pep"
	ScreeningAdverseMedia ScreeningListType = "adverse_media"
)

type ScreeningMatchStatus string

const (
	MatchPotential     ScreeningMatchStatus = "potential"
	MatchConfirmed     ScreeningMatchStatus = "confirmed"
	MatchFalsePositive ScreeningMatchStatus = "false_positive"
)

type ScreeningRun struct {
	ID          string               `json:"id"`
	CustomerID  string               `json:"customer_id"`
	SubjectType ScreeningSubjectType `json:"subject_type"`
	SubjectID   string               `json:"subject_id"`
	QueryName   string               `json:"query_name"`
	Provider    string               `json:"provider"`
	CreatedBy   string               `json:"created_by"`
	CreatedAt   time.Time            `json:"created_at"`
}
type ScreeningMatch struct {
	ID                string               `json:"id"`
	RunID             string               `json:"run_id"`
	CustomerID        string               `json:"customer_id"`
	SubjectType       ScreeningSubjectType `json:"subject_type"`
	SubjectID         string               `json:"subject_id"`
	QueryName         string               `json:"query_name"`
	ListType          ScreeningListType    `json:"list_type"`
	MatchedName       string               `json:"matched_name"`
	Score             int                  `json:"score"`
	Reason            string               `json:"reason"`
	Status            ScreeningMatchStatus `json:"status"`
	CreatedAt         time.Time            `json:"created_at"`
	ReviewedBy        string               `json:"reviewed_by,omitempty"`
	ReviewedAt        *time.Time           `json:"reviewed_at,omitempty"`
	DispositionReason string               `json:"disposition_reason,omitempty"`
}
type ScreeningResult struct {
	Runs    []ScreeningRun   `json:"runs"`
	Matches []ScreeningMatch `json:"matches"`
}
