package application

import (
	"context"
	"errors"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
	"strings"
	"time"
)

var ErrInvalidScreening = errors.New("invalid screening request")

type ScreeningCandidate struct {
	ListType domain.ScreeningListType
	Name     string
	Score    int
	Reason   string
}
type ScreeningProvider interface {
	Name() string
	Screen(context.Context, string) ([]ScreeningCandidate, error)
}
type ScreeningRepository interface {
	GetCustomer(context.Context, string) (domain.Customer, error)
	GetDueDiligence(context.Context, string) (domain.DueDiligenceDetails, error)
	SaveScreening(context.Context, []domain.ScreeningRun, []domain.ScreeningMatch, []domain.AuditEvent) error
	ListScreeningMatches(context.Context, string) ([]domain.ScreeningMatch, error)
	DispositionScreeningMatch(context.Context, string, domain.ScreeningMatchStatus, string, string, domain.AuditEvent) (domain.ScreeningMatch, error)
	UpsertScreeningSchedule(context.Context, domain.ScreeningSchedule, domain.AuditEvent) (domain.ScreeningSchedule, error)
	GetScreeningSchedule(context.Context, string) (domain.ScreeningSchedule, error)
	ListDueScreeningSchedules(context.Context, time.Time, int) ([]domain.ScreeningSchedule, error)
	CompleteScreeningSchedule(context.Context, string, time.Time, string) error
}

func (s *ScreeningService) ConfigureSchedule(ctx context.Context, customerID string, enabled bool, intervalHours int, actor string) (domain.ScreeningSchedule, error) {
	customerID, actor = strings.TrimSpace(customerID), strings.TrimSpace(actor)
	if customerID == "" || actor == "" || intervalHours < 1 || intervalHours > 8760 {
		return domain.ScreeningSchedule{}, ErrInvalidScreening
	}
	if _, err := s.repo.GetCustomer(ctx, customerID); err != nil {
		return domain.ScreeningSchedule{}, err
	}
	now := s.now().UTC()
	schedule := domain.ScreeningSchedule{CustomerID: customerID, Enabled: enabled, IntervalHours: intervalHours, NextRunAt: now.Add(time.Duration(intervalHours) * time.Hour), UpdatedBy: actor, UpdatedAt: now}
	event := domain.AuditEvent{ID: newID(), AggregateType: "customer", AggregateID: customerID, EventType: "screening.schedule_updated", Actor: actor, OccurredAt: now, Payload: map[string]any{"enabled": enabled, "interval_hours": intervalHours, "next_run_at": schedule.NextRunAt}}
	return s.repo.UpsertScreeningSchedule(ctx, schedule, event)
}

func (s *ScreeningService) GetSchedule(ctx context.Context, customerID string) (domain.ScreeningSchedule, error) {
	return s.repo.GetScreeningSchedule(ctx, strings.TrimSpace(customerID))
}

func (s *ScreeningService) RunDue(ctx context.Context, limit int) (int, error) {
	if limit < 1 {
		limit = 25
	}
	now := s.now().UTC()
	schedules, err := s.repo.ListDueScreeningSchedules(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	completed := 0
	for _, schedule := range schedules {
		_, runErr := s.ScreenCustomer(ctx, schedule.CustomerID, "ongoing-monitoring-worker")
		errorText := ""
		if runErr != nil {
			errorText = runErr.Error()
		}
		if err := s.repo.CompleteScreeningSchedule(ctx, schedule.CustomerID, now, errorText); err != nil {
			return completed, err
		}
		if runErr == nil {
			completed++
		}
	}
	return completed, nil
}

type ScreeningService struct {
	repo     ScreeningRepository
	provider ScreeningProvider
	now      func() time.Time
}

func NewScreeningService(repo ScreeningRepository, provider ScreeningProvider) *ScreeningService {
	return &ScreeningService{repo: repo, provider: provider, now: time.Now}
}
func (s *ScreeningService) ScreenCustomer(ctx context.Context, customerID, actor string) (domain.ScreeningResult, error) {
	customerID = strings.TrimSpace(customerID)
	actor = strings.TrimSpace(actor)
	if customerID == "" || actor == "" {
		return domain.ScreeningResult{}, ErrInvalidScreening
	}
	customer, err := s.repo.GetCustomer(ctx, customerID)
	if err != nil {
		return domain.ScreeningResult{}, err
	}
	cdd, err := s.repo.GetDueDiligence(ctx, customerID)
	if err != nil {
		return domain.ScreeningResult{}, err
	}
	type subject struct {
		kind     domain.ScreeningSubjectType
		id, name string
	}
	subjects := []subject{{domain.ScreeningCustomer, customer.ID, customer.LegalName}}
	for _, owner := range cdd.BeneficialOwners {
		subjects = append(subjects, subject{domain.ScreeningBeneficialOwner, owner.ID, owner.FullName})
	}
	now := s.now().UTC()
	result := domain.ScreeningResult{Runs: []domain.ScreeningRun{}, Matches: []domain.ScreeningMatch{}}
	events := []domain.AuditEvent{}
	for _, subject := range subjects {
		candidates, err := s.provider.Screen(ctx, subject.name)
		if err != nil {
			return result, err
		}
		run := domain.ScreeningRun{ID: newID(), CustomerID: customerID, SubjectType: subject.kind, SubjectID: subject.id, QueryName: subject.name, Provider: s.provider.Name(), CreatedBy: actor, CreatedAt: now}
		result.Runs = append(result.Runs, run)
		for _, candidate := range candidates {
			match := domain.ScreeningMatch{ID: newID(), RunID: run.ID, CustomerID: customerID, SubjectType: subject.kind, SubjectID: subject.id, QueryName: subject.name, ListType: candidate.ListType, MatchedName: candidate.Name, Score: candidate.Score, Reason: candidate.Reason, Status: domain.MatchPotential, CreatedAt: now}
			result.Matches = append(result.Matches, match)
		}
		events = append(events, domain.AuditEvent{ID: newID(), AggregateType: "customer", AggregateID: customerID, EventType: "screening.completed", Actor: actor, OccurredAt: now, Payload: map[string]any{"run_id": run.ID, "subject_type": subject.kind, "subject_id": subject.id, "match_count": len(candidates), "provider": s.provider.Name()}})
	}
	if err := s.repo.SaveScreening(ctx, result.Runs, result.Matches, events); err != nil {
		return domain.ScreeningResult{}, err
	}
	return result, nil
}
func (s *ScreeningService) List(ctx context.Context, customerID string) ([]domain.ScreeningMatch, error) {
	return s.repo.ListScreeningMatches(ctx, strings.TrimSpace(customerID))
}
func (s *ScreeningService) Disposition(ctx context.Context, id string, status domain.ScreeningMatchStatus, reason, actor string) (domain.ScreeningMatch, error) {
	id = strings.TrimSpace(id)
	reason = strings.TrimSpace(reason)
	actor = strings.TrimSpace(actor)
	if id == "" || reason == "" || actor == "" || (status != domain.MatchConfirmed && status != domain.MatchFalsePositive) {
		return domain.ScreeningMatch{}, ErrInvalidScreening
	}
	now := s.now().UTC()
	event := domain.AuditEvent{ID: newID(), AggregateType: "customer", EventType: "screening.match_" + string(status), Actor: actor, OccurredAt: now, Payload: map[string]any{"match_id": id, "reason": reason}}
	return s.repo.DispositionScreeningMatch(ctx, id, status, reason, actor, event)
}
