package memory

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
)

type Repository struct {
	mu           sync.RWMutex
	customers    map[string]domain.Customer
	transactions map[string]domain.Transaction
	alerts       map[string]domain.Alert
	idempotency  map[string]string
	events       map[string][]domain.AuditEvent
	cases        map[string]domain.InvestigationCase
	caseComments map[string][]domain.CaseComment
}

func (r *Repository) GetCustomer(_ context.Context, id string) (domain.Customer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	customer, ok := r.customers[id]
	if !ok {
		return domain.Customer{}, domain.ErrCustomerNotFound
	}
	return customer, nil
}

func (r *Repository) ListCustomers(_ context.Context, status domain.CustomerStatus, page application.PageRequest) ([]domain.Customer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]domain.Customer, 0)
	for _, v := range r.customers {
		if (status == "" || v.Status == status) && before(v.CreatedAt, v.ID, page) {
			items = append(items, v)
		}
	}
	sort.Slice(items, func(i, j int) bool { return newer(items[i].CreatedAt, items[i].ID, items[j].CreatedAt, items[j].ID) })
	return limit(items, page.Limit), nil
}

func (r *Repository) ListCustomerTransactions(_ context.Context, customerID string, page application.PageRequest) ([]domain.Transaction, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]domain.Transaction, 0)
	for _, v := range r.transactions {
		if v.CustomerID == customerID && before(v.OccurredAt, v.ID, page) {
			items = append(items, v)
		}
	}
	sort.Slice(items, func(i, j int) bool { return newer(items[i].OccurredAt, items[i].ID, items[j].OccurredAt, items[j].ID) })
	return limit(items, page.Limit), nil
}

func (r *Repository) ListAuditEventsPage(_ context.Context, aggregateID string, page application.PageRequest) ([]domain.AuditEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]domain.AuditEvent, 0)
	for _, v := range r.events[aggregateID] {
		if before(v.OccurredAt, v.ID, page) {
			items = append(items, v)
		}
	}
	sort.Slice(items, func(i, j int) bool { return newer(items[i].OccurredAt, items[i].ID, items[j].OccurredAt, items[j].ID) })
	return limit(items, page.Limit), nil
}

func (r *Repository) ListCustomerActivityPage(_ context.Context, customerID string, page application.PageRequest) ([]domain.AuditEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.customers[customerID]; !ok {
		return nil, domain.ErrCustomerNotFound
	}
	related := map[string]bool{customerID: true}
	for _, transaction := range r.transactions {
		if transaction.CustomerID == customerID {
			related[transaction.ID] = true
		}
	}
	for _, alert := range r.alerts {
		if alert.CustomerID == customerID {
			related[alert.ID] = true
		}
	}
	for _, item := range r.cases {
		if item.CustomerID == customerID {
			related[item.ID] = true
		}
	}
	items := make([]domain.AuditEvent, 0)
	for aggregateID := range related {
		for _, event := range r.events[aggregateID] {
			if before(event.OccurredAt, event.ID, page) {
				items = append(items, event)
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return newer(items[i].OccurredAt, items[i].ID, items[j].OccurredAt, items[j].ID) })
	return limit(items, page.Limit), nil
}

func (r *Repository) ListAlertsPage(_ context.Context, status domain.AlertStatus, page application.PageRequest) ([]domain.Alert, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]domain.Alert, 0)
	for _, v := range r.alerts {
		if (status == "" || v.Status == status) && before(v.CreatedAt, v.ID, page) {
			items = append(items, v)
		}
	}
	sort.Slice(items, func(i, j int) bool { return newer(items[i].CreatedAt, items[i].ID, items[j].CreatedAt, items[j].ID) })
	return limit(items, page.Limit), nil
}

func (r *Repository) ListCasesPage(_ context.Context, status domain.CaseStatus, page application.PageRequest) ([]domain.InvestigationCase, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]domain.InvestigationCase, 0)
	for _, v := range r.cases {
		if (status == "" || v.Status == status) && before(v.UpdatedAt, v.ID, page) {
			items = append(items, v)
		}
	}
	sort.Slice(items, func(i, j int) bool { return newer(items[i].UpdatedAt, items[i].ID, items[j].UpdatedAt, items[j].ID) })
	return limit(items, page.Limit), nil
}

func before(timestamp time.Time, id string, page application.PageRequest) bool {
	return page.CursorTime.IsZero() || timestamp.Before(page.CursorTime) || (timestamp.Equal(page.CursorTime) && id < page.CursorID)
}
func newer(a time.Time, aID string, b time.Time, bID string) bool {
	return a.After(b) || (a.Equal(b) && aID > bID)
}
func limit[T any](items []T, size int) []T {
	if len(items) > size {
		return items[:size]
	}
	return items
}

func NewRepository() *Repository {
	return &Repository{customers: make(map[string]domain.Customer), transactions: make(map[string]domain.Transaction), alerts: make(map[string]domain.Alert), idempotency: make(map[string]string), events: make(map[string][]domain.AuditEvent), cases: make(map[string]domain.InvestigationCase), caseComments: make(map[string][]domain.CaseComment)}
}

func (r *Repository) CreateCase(_ context.Context, item domain.InvestigationCase, event domain.AuditEvent) (domain.InvestigationCase, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	alert, ok := r.alerts[item.AlertID]
	if !ok {
		return item, domain.ErrAlertNotFound
	}
	if alert.Status != domain.AlertOpen {
		return item, domain.ErrAlertConflict
	}
	for _, existing := range r.cases {
		if existing.AlertID == item.AlertID {
			return item, domain.ErrAlertHasCase
		}
	}
	item.CustomerID = alert.CustomerID
	r.cases[item.ID] = item
	r.events[item.ID] = append(r.events[item.ID], event)
	return item, nil
}
func (r *Repository) GetCase(_ context.Context, id string) (domain.InvestigationCase, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.cases[id]
	if !ok {
		return item, domain.ErrCaseNotFound
	}
	return item, nil
}
func (r *Repository) ListCaseComments(_ context.Context, id string) ([]domain.CaseComment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]domain.CaseComment(nil), r.caseComments[id]...), nil
}
func (r *Repository) AssignCase(_ context.Context, id, assignee string, event domain.AuditEvent) (domain.InvestigationCase, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.cases[id]
	if !ok {
		return item, domain.ErrCaseNotFound
	}
	if item.Status == domain.CaseResolved {
		return item, domain.ErrCaseConflict
	}
	item.AssignedTo = assignee
	item.Status = domain.CaseInProgress
	item.UpdatedAt = event.OccurredAt
	r.cases[id] = item
	r.events[id] = append(r.events[id], event)
	return item, nil
}
func (r *Repository) AddCaseComment(_ context.Context, comment domain.CaseComment, event domain.AuditEvent) (domain.CaseComment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.cases[comment.CaseID]
	if !ok {
		return comment, domain.ErrCaseNotFound
	}
	if item.Status == domain.CaseResolved {
		return comment, domain.ErrCaseConflict
	}
	item.UpdatedAt = comment.CreatedAt
	r.cases[item.ID] = item
	r.caseComments[item.ID] = append(r.caseComments[item.ID], comment)
	r.events[item.ID] = append(r.events[item.ID], event)
	return comment, nil
}
func (r *Repository) ResolveCase(_ context.Context, id, resolution, actor string, caseEvent, alertEvent domain.AuditEvent) (domain.InvestigationCase, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.cases[id]
	if !ok {
		return item, domain.ErrCaseNotFound
	}
	if item.Status == domain.CaseResolved {
		return item, domain.ErrCaseConflict
	}
	alert := r.alerts[item.AlertID]
	if alert.Status != domain.AlertOpen {
		return item, domain.ErrAlertConflict
	}
	item.Status = domain.CaseResolved
	item.Resolution = resolution
	item.ResolvedBy = actor
	item.ResolvedAt = &caseEvent.OccurredAt
	item.UpdatedAt = caseEvent.OccurredAt
	alert.Status = domain.AlertClosed
	alert.ClosedAt = &caseEvent.OccurredAt
	alert.ClosedBy = actor
	alert.ClosureReason = resolution
	alertEvent.AggregateID = alert.ID
	r.cases[id] = item
	r.alerts[alert.ID] = alert
	r.events[id] = append(r.events[id], caseEvent)
	r.events[alert.ID] = append(r.events[alert.ID], alertEvent)
	return item, nil
}

func (r *Repository) CreateTransaction(_ context.Context, transaction domain.Transaction, event domain.AuditEvent, alerts []domain.Alert, alertEvents []domain.AuditEvent) (domain.Transaction, []domain.Alert, bool, error) {
	if len(alerts) != len(alertEvents) {
		return domain.Transaction{}, nil, false, errors.New("each alert must have one audit event")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if transactionID, ok := r.idempotency[transaction.IdempotencyKey]; ok {
		stored := r.transactions[transactionID]
		if !stored.SameIngestionPayload(transaction) {
			return domain.Transaction{}, nil, false, domain.ErrIdempotencyConflict
		}
		storedAlerts := make([]domain.Alert, 0)
		for _, alert := range r.alerts {
			if alert.TransactionID == stored.ID {
				storedAlerts = append(storedAlerts, alert)
			}
		}
		return stored, storedAlerts, true, nil
	}
	customer, ok := r.customers[transaction.CustomerID]
	if !ok {
		return domain.Transaction{}, nil, false, domain.ErrCustomerNotFound
	}
	if customer.Status != domain.CustomerActive {
		return domain.Transaction{}, nil, false, domain.ErrCustomerNotActive
	}
	r.transactions[transaction.ID] = transaction
	r.idempotency[transaction.IdempotencyKey] = transaction.ID
	r.events[transaction.ID] = append(r.events[transaction.ID], event)
	for index, alert := range alerts {
		r.alerts[alert.ID] = alert
		r.events[alert.ID] = append(r.events[alert.ID], alertEvents[index])
	}
	return transaction, append([]domain.Alert(nil), alerts...), false, nil
}

func (r *Repository) ListAlerts(_ context.Context, status domain.AlertStatus) ([]domain.Alert, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	alerts := make([]domain.Alert, 0, len(r.alerts))
	for _, alert := range r.alerts {
		if status == "" || alert.Status == status {
			alerts = append(alerts, alert)
		}
	}
	return alerts, nil
}

func (r *Repository) CloseAlert(_ context.Context, alertID, actor, reason string, event domain.AuditEvent) (domain.Alert, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	alert, ok := r.alerts[alertID]
	if !ok {
		return domain.Alert{}, domain.ErrAlertNotFound
	}
	if alert.Status != domain.AlertOpen {
		return domain.Alert{}, domain.ErrAlertConflict
	}
	alert.Status = domain.AlertClosed
	closedAt := event.OccurredAt
	alert.ClosedAt = &closedAt
	alert.ClosedBy = actor
	alert.ClosureReason = reason
	r.alerts[alertID] = alert
	r.events[alertID] = append(r.events[alertID], event)
	return alert, nil
}

func (r *Repository) CreateCustomer(_ context.Context, customer domain.Customer, event domain.AuditEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.customers[customer.ID] = customer
	r.events[customer.ID] = append(r.events[customer.ID], event)
	return nil
}

func (r *Repository) ListAuditEvents(_ context.Context, customerID string) ([]domain.AuditEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	events := r.events[customerID]
	return append([]domain.AuditEvent(nil), events...), nil
}

func (r *Repository) ReviewCustomer(_ context.Context, customerID string, decision domain.ReviewDecision, actor string, event domain.AuditEvent) (domain.Customer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	customer, ok := r.customers[customerID]
	if !ok {
		return domain.Customer{}, domain.ErrCustomerNotFound
	}
	if customer.CreatedBy == actor {
		return domain.Customer{}, domain.ErrMakerCannotReview
	}
	if customer.Status != domain.CustomerPendingApproval {
		return domain.Customer{}, domain.ErrReviewConflict
	}
	if decision == domain.ReviewApprove {
		customer.Status = domain.CustomerActive
	} else if decision == domain.ReviewReject {
		customer.Status = domain.CustomerRejected
	} else {
		return domain.Customer{}, errors.New("invalid review decision")
	}
	customer.ReviewedBy = actor
	reviewedAt := event.OccurredAt
	customer.ReviewedAt = &reviewedAt
	r.customers[customerID] = customer
	r.events[customerID] = append(r.events[customerID], event)
	return customer, nil
}
