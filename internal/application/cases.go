package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
)

var ErrInvalidCase = errors.New("invalid case data")

type CaseRepository interface {
	CreateCase(context.Context, domain.InvestigationCase, domain.AuditEvent) (domain.InvestigationCase, error)
	GetCase(context.Context, string) (domain.InvestigationCase, error)
	ListCaseComments(context.Context, string) ([]domain.CaseComment, error)
	ListAuditEvents(context.Context, string) ([]domain.AuditEvent, error)
	AssignCase(context.Context, string, string, domain.AuditEvent) (domain.InvestigationCase, error)
	AddCaseComment(context.Context, domain.CaseComment, domain.AuditEvent) (domain.CaseComment, error)
	ResolveCase(context.Context, string, string, string, domain.AuditEvent, domain.AuditEvent) (domain.InvestigationCase, error)
}

type CaseService struct {
	repo CaseRepository
	now  func() time.Time
}

func NewCaseService(repo CaseRepository) *CaseService { return &CaseService{repo: repo, now: time.Now} }

func validCasePriority(priority domain.CasePriority) bool {
	return priority == domain.CasePriorityLow || priority == domain.CasePriorityMedium || priority == domain.CasePriorityHigh
}

func (s *CaseService) Create(ctx context.Context, alertID, title string, priority domain.CasePriority, actor string) (domain.InvestigationCase, error) {
	alertID, title, actor = strings.TrimSpace(alertID), strings.TrimSpace(title), strings.TrimSpace(actor)
	if alertID == "" || title == "" || len(title) > 200 || actor == "" || !validCasePriority(priority) {
		return domain.InvestigationCase{}, ErrInvalidCase
	}
	now := s.now().UTC()
	item := domain.InvestigationCase{ID: newID(), AlertID: alertID, Title: title, Priority: priority, Status: domain.CaseOpen, CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
	event := domain.AuditEvent{ID: newID(), AggregateType: "case", AggregateID: item.ID, EventType: "case.created", Actor: actor, OccurredAt: now, Payload: map[string]any{"alert_id": alertID, "priority": priority}}
	return s.repo.CreateCase(ctx, item, event)
}

func (s *CaseService) Details(ctx context.Context, caseID string) (domain.CaseDetails, error) {
	item, err := s.repo.GetCase(ctx, strings.TrimSpace(caseID))
	if err != nil {
		return domain.CaseDetails{}, err
	}
	comments, err := s.repo.ListCaseComments(ctx, item.ID)
	if err != nil {
		return domain.CaseDetails{}, err
	}
	timeline, err := s.repo.ListAuditEvents(ctx, item.ID)
	return domain.CaseDetails{Case: item, Comments: comments, Timeline: timeline}, err
}

func (s *CaseService) Assign(ctx context.Context, caseID, assignee, actor string) (domain.InvestigationCase, error) {
	caseID, assignee, actor = strings.TrimSpace(caseID), strings.TrimSpace(assignee), strings.TrimSpace(actor)
	if caseID == "" || assignee == "" || len(assignee) > 200 || actor == "" {
		return domain.InvestigationCase{}, ErrInvalidCase
	}
	now := s.now().UTC()
	event := domain.AuditEvent{ID: newID(), AggregateType: "case", AggregateID: caseID, EventType: "case.assigned", Actor: actor, OccurredAt: now, Payload: map[string]any{"assigned_to": assignee}}
	return s.repo.AssignCase(ctx, caseID, assignee, event)
}

func (s *CaseService) Comment(ctx context.Context, caseID, body, actor string) (domain.CaseComment, error) {
	caseID, body, actor = strings.TrimSpace(caseID), strings.TrimSpace(body), strings.TrimSpace(actor)
	if caseID == "" || body == "" || len(body) > 4000 || actor == "" {
		return domain.CaseComment{}, ErrInvalidCase
	}
	now := s.now().UTC()
	comment := domain.CaseComment{ID: newID(), CaseID: caseID, Author: actor, Body: body, CreatedAt: now}
	event := domain.AuditEvent{ID: newID(), AggregateType: "case", AggregateID: caseID, EventType: "case.comment_added", Actor: actor, OccurredAt: now, Payload: map[string]any{"comment_id": comment.ID}}
	return s.repo.AddCaseComment(ctx, comment, event)
}

func (s *CaseService) Resolve(ctx context.Context, caseID, resolution, actor string) (domain.InvestigationCase, error) {
	caseID, resolution, actor = strings.TrimSpace(caseID), strings.TrimSpace(resolution), strings.TrimSpace(actor)
	if caseID == "" || resolution == "" || len(resolution) > 4000 || actor == "" {
		return domain.InvestigationCase{}, ErrInvalidCase
	}
	now := s.now().UTC()
	caseEvent := domain.AuditEvent{ID: newID(), AggregateType: "case", AggregateID: caseID, EventType: "case.resolved", Actor: actor, OccurredAt: now, Payload: map[string]any{"resolution": resolution}}
	alertEvent := domain.AuditEvent{ID: newID(), AggregateType: "alert", EventType: "alert.closed", Actor: actor, OccurredAt: now, Payload: map[string]any{"reason": resolution, "source": "case_resolution"}}
	return s.repo.ResolveCase(ctx, caseID, resolution, actor, caseEvent, alertEvent)
}
