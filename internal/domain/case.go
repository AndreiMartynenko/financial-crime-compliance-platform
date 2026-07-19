package domain

import (
	"errors"
	"time"
)

var (
	ErrCaseNotFound = errors.New("case not found")
	ErrCaseConflict = errors.New("case is already resolved")
	ErrAlertHasCase = errors.New("alert already has a case")
)

type CasePriority string

const (
	CasePriorityLow    CasePriority = "low"
	CasePriorityMedium CasePriority = "medium"
	CasePriorityHigh   CasePriority = "high"
)

type CaseStatus string

const (
	CaseOpen       CaseStatus = "open"
	CaseInProgress CaseStatus = "in_progress"
	CaseResolved   CaseStatus = "resolved"
)

type InvestigationCase struct {
	ID         string       `json:"id"`
	AlertID    string       `json:"alert_id"`
	CustomerID string       `json:"customer_id"`
	Title      string       `json:"title"`
	Priority   CasePriority `json:"priority"`
	Status     CaseStatus   `json:"status"`
	AssignedTo string       `json:"assigned_to,omitempty"`
	Resolution string       `json:"resolution,omitempty"`
	CreatedBy  string       `json:"created_by"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
	ResolvedBy string       `json:"resolved_by,omitempty"`
	ResolvedAt *time.Time   `json:"resolved_at,omitempty"`
}

type CaseComment struct {
	ID        string    `json:"id"`
	CaseID    string    `json:"case_id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type CaseDetails struct {
	Case     InvestigationCase `json:"case"`
	Comments []CaseComment     `json:"comments"`
	Timeline []AuditEvent      `json:"timeline"`
}
