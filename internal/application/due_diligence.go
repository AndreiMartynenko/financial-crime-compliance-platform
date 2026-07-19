package application

import (
	"context"
	"errors"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
	"strings"
	"time"
)

var ErrInvalidDueDiligence = errors.New("invalid due diligence data")

type DueDiligenceRepository interface {
	GetDueDiligence(context.Context, string) (domain.DueDiligenceDetails, error)
	UpsertCDDProfile(context.Context, domain.CDDProfile, domain.AuditEvent) (domain.CDDProfile, error)
	AddBeneficialOwner(context.Context, domain.BeneficialOwner, domain.AuditEvent) (domain.BeneficialOwner, error)
	AddKYCDocument(context.Context, domain.KYCDocument, domain.AuditEvent) (domain.KYCDocument, error)
	ReviewKYCDocument(context.Context, string, domain.DocumentStatus, string, domain.AuditEvent) (domain.KYCDocument, error)
}
type DueDiligenceService struct {
	repo DueDiligenceRepository
	now  func() time.Time
}

func NewDueDiligenceService(repo DueDiligenceRepository) *DueDiligenceService {
	return &DueDiligenceService{repo: repo, now: time.Now}
}
func (s *DueDiligenceService) Get(ctx context.Context, customerID string) (domain.DueDiligenceDetails, error) {
	return s.repo.GetDueDiligence(ctx, strings.TrimSpace(customerID))
}
func (s *DueDiligenceService) UpdateProfile(ctx context.Context, p domain.CDDProfile, actor string) (domain.CDDProfile, error) {
	p.CustomerID = strings.TrimSpace(p.CustomerID)
	p.SourceOfWealth = strings.TrimSpace(p.SourceOfWealth)
	p.BusinessPurpose = strings.TrimSpace(p.BusinessPurpose)
	p.Currency = strings.ToUpper(strings.TrimSpace(p.Currency))
	actor = strings.TrimSpace(actor)
	if p.CustomerID == "" || p.SourceOfWealth == "" || p.BusinessPurpose == "" || p.ExpectedMonthlyVolumeMinor < 0 || len(p.Currency) != 3 || actor == "" || (p.Status != domain.CDDIncomplete && p.Status != domain.CDDInReview && p.Status != domain.CDDComplete) {
		return p, ErrInvalidDueDiligence
	}
	p.UpdatedBy = actor
	p.UpdatedAt = s.now().UTC()
	event := domain.AuditEvent{ID: newID(), AggregateType: "customer", AggregateID: p.CustomerID, EventType: "cdd.profile_updated", Actor: actor, OccurredAt: p.UpdatedAt, Payload: map[string]any{"status": p.Status, "next_review_at": p.NextReviewAt}}
	return s.repo.UpsertCDDProfile(ctx, p, event)
}
func (s *DueDiligenceService) AddOwner(ctx context.Context, o domain.BeneficialOwner, actor string) (domain.BeneficialOwner, error) {
	o.CustomerID = strings.TrimSpace(o.CustomerID)
	o.FullName = strings.TrimSpace(o.FullName)
	o.CountryCode = strings.ToUpper(strings.TrimSpace(o.CountryCode))
	actor = strings.TrimSpace(actor)
	if o.CustomerID == "" || o.FullName == "" || o.OwnershipPercent < 1 || o.OwnershipPercent > 100 || len(o.CountryCode) != 2 || actor == "" {
		return o, ErrInvalidDueDiligence
	}
	o.ID = newID()
	o.CreatedBy = actor
	o.CreatedAt = s.now().UTC()
	event := domain.AuditEvent{ID: newID(), AggregateType: "customer", AggregateID: o.CustomerID, EventType: "cdd.beneficial_owner_added", Actor: actor, OccurredAt: o.CreatedAt, Payload: map[string]any{"owner_id": o.ID, "ownership_percent": o.OwnershipPercent, "pep": o.PEP}}
	return s.repo.AddBeneficialOwner(ctx, o, event)
}
func (s *DueDiligenceService) AddDocument(ctx context.Context, d domain.KYCDocument, actor string) (domain.KYCDocument, error) {
	d.CustomerID = strings.TrimSpace(d.CustomerID)
	d.Type = strings.TrimSpace(d.Type)
	d.Reference = strings.TrimSpace(d.Reference)
	actor = strings.TrimSpace(actor)
	if d.CustomerID == "" || d.Type == "" || d.Reference == "" || actor == "" {
		return d, ErrInvalidDueDiligence
	}
	d.ID = newID()
	d.Status = domain.DocumentPending
	d.CreatedBy = actor
	d.CreatedAt = s.now().UTC()
	event := domain.AuditEvent{ID: newID(), AggregateType: "customer", AggregateID: d.CustomerID, EventType: "cdd.document_added", Actor: actor, OccurredAt: d.CreatedAt, Payload: map[string]any{"document_id": d.ID, "type": d.Type}}
	return s.repo.AddKYCDocument(ctx, d, event)
}
func (s *DueDiligenceService) ReviewDocument(ctx context.Context, id string, status domain.DocumentStatus, actor string) (domain.KYCDocument, error) {
	id = strings.TrimSpace(id)
	actor = strings.TrimSpace(actor)
	if id == "" || actor == "" || (status != domain.DocumentVerified && status != domain.DocumentRejected) {
		return domain.KYCDocument{}, ErrInvalidDueDiligence
	}
	now := s.now().UTC()
	event := domain.AuditEvent{ID: newID(), AggregateType: "customer", EventType: "cdd.document_" + string(status), Actor: actor, OccurredAt: now, Payload: map[string]any{"document_id": id}}
	return s.repo.ReviewKYCDocument(ctx, id, status, actor, event)
}
