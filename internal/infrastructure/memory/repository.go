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
	events       map[string][]domain.AuditEvent
}

func NewRepository() *Repository {
	return &Repository{customers: make(map[string]domain.Customer), transactions: make(map[string]domain.Transaction), events: make(map[string][]domain.AuditEvent)}
}

func (r *Repository) CreateTransaction(_ context.Context, transaction domain.Transaction, event domain.AuditEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	customer, ok := r.customers[transaction.CustomerID]
	if !ok {
		return domain.ErrCustomerNotFound
	}
	if customer.Status != domain.CustomerActive {
		return domain.ErrCustomerNotActive
	}
	r.transactions[transaction.ID] = transaction
	r.events[transaction.ID] = append(r.events[transaction.ID], event)
	return nil
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
