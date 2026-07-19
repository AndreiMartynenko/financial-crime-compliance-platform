package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
)

var ErrInvalidCustomer = errors.New("invalid customer")

type Repository interface {
	CreateCustomer(context.Context, domain.Customer, domain.AuditEvent) error
	ListAuditEvents(context.Context, string) ([]domain.AuditEvent, error)
}

type OnboardCustomerCommand struct {
	ExternalRef string              `json:"external_ref"`
	Type        domain.CustomerType `json:"type"`
	LegalName   string              `json:"legal_name"`
	CountryCode string              `json:"country_code"`
	RiskFactors domain.RiskFactors  `json:"risk_factors"`
	Actor       string              `json:"-"`
}

type OnboardingService struct {
	repo Repository
	now  func() time.Time
}

func NewOnboardingService(repo Repository) *OnboardingService {
	return &OnboardingService{repo: repo, now: time.Now}
}

func (s *OnboardingService) Onboard(ctx context.Context, cmd OnboardCustomerCommand) (domain.Customer, error) {
	cmd.LegalName = strings.TrimSpace(cmd.LegalName)
	cmd.CountryCode = strings.ToUpper(strings.TrimSpace(cmd.CountryCode))
	if cmd.LegalName == "" || len(cmd.CountryCode) != 2 || (cmd.Type != domain.CustomerIndividual && cmd.Type != domain.CustomerCompany) {
		return domain.Customer{}, ErrInvalidCustomer
	}
	if cmd.RiskFactors.CountryRisk != domain.CountryRiskLow && cmd.RiskFactors.CountryRisk != domain.CountryRiskMedium && cmd.RiskFactors.CountryRisk != domain.CountryRiskHigh {
		return domain.Customer{}, ErrInvalidCustomer
	}

	now := s.now().UTC()
	customer := domain.Customer{
		ID:             newID(),
		ExternalRef:    strings.TrimSpace(cmd.ExternalRef),
		Type:           cmd.Type,
		LegalName:      cmd.LegalName,
		CountryCode:    cmd.CountryCode,
		RiskFactors:    cmd.RiskFactors,
		RiskAssessment: (domain.RiskEngine{}).Assess(cmd.RiskFactors, now),
		CreatedAt:      now,
	}
	actor := strings.TrimSpace(cmd.Actor)
	if actor == "" {
		actor = "anonymous-api-user"
	}
	event := domain.AuditEvent{
		ID:          newID(),
		AggregateID: customer.ID,
		EventType:   "customer.onboarded",
		Actor:       actor,
		OccurredAt:  now,
		Payload: map[string]any{
			"risk_score":    customer.RiskAssessment.Score,
			"risk_rating":   customer.RiskAssessment.Rating,
			"due_diligence": customer.RiskAssessment.DueDiligence,
			"rule_version":  customer.RiskAssessment.RuleVersion,
		},
	}
	if err := s.repo.CreateCustomer(ctx, customer, event); err != nil {
		return domain.Customer{}, err
	}
	return customer, nil
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" + hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:])
}
