package domain

import "time"

type CDDStatus string

const (
	CDDIncomplete CDDStatus = "incomplete"
	CDDInReview   CDDStatus = "in_review"
	CDDComplete   CDDStatus = "complete"
)

type DocumentStatus string

const (
	DocumentPending  DocumentStatus = "pending"
	DocumentVerified DocumentStatus = "verified"
	DocumentRejected DocumentStatus = "rejected"
)

type CDDProfile struct {
	CustomerID                 string     `json:"customer_id"`
	SourceOfWealth             string     `json:"source_of_wealth"`
	BusinessPurpose            string     `json:"business_purpose"`
	ExpectedMonthlyVolumeMinor int64      `json:"expected_monthly_volume_minor"`
	Currency                   string     `json:"currency"`
	Status                     CDDStatus  `json:"status"`
	NextReviewAt               *time.Time `json:"next_review_at,omitempty"`
	UpdatedBy                  string     `json:"updated_by"`
	UpdatedAt                  time.Time  `json:"updated_at"`
}
type BeneficialOwner struct {
	ID               string    `json:"id"`
	CustomerID       string    `json:"customer_id"`
	FullName         string    `json:"full_name"`
	OwnershipPercent int       `json:"ownership_percent"`
	CountryCode      string    `json:"country_code"`
	PEP              bool      `json:"pep"`
	CreatedBy        string    `json:"created_by"`
	CreatedAt        time.Time `json:"created_at"`
}
type KYCDocument struct {
	ID         string         `json:"id"`
	CustomerID string         `json:"customer_id"`
	Type       string         `json:"type"`
	Reference  string         `json:"reference"`
	Status     DocumentStatus `json:"status"`
	ExpiresAt  *time.Time     `json:"expires_at,omitempty"`
	CreatedBy  string         `json:"created_by"`
	CreatedAt  time.Time      `json:"created_at"`
	VerifiedBy string         `json:"verified_by,omitempty"`
	VerifiedAt *time.Time     `json:"verified_at,omitempty"`
}
type DueDiligenceDetails struct {
	Profile          CDDProfile        `json:"profile"`
	BeneficialOwners []BeneficialOwner `json:"beneficial_owners"`
	Documents        []KYCDocument     `json:"documents"`
}
