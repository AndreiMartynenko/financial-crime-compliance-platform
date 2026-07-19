package memory

import (
	"context"
	"errors"
	"sync"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
)

type Repository struct {
	mu           sync.RWMutex
	customers    map[string]domain.Customer
	transactions map[string]domain.Transaction
	alerts       map[string]domain.Alert
	idempotency  map[string]string
	events       map[string][]domain.AuditEvent
}

func NewRepository() *Repository {
	return &Repository{customers: make(map[string]domain.Customer), transactions: make(map[string]domain.Transaction), alerts: make(map[string]domain.Alert), idempotency: make(map[string]string), events: make(map[string][]domain.AuditEvent)}
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
