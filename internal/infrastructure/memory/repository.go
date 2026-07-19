package memory

import (
	"context"
	"sync"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
)

type Repository struct {
	mu        sync.RWMutex
	customers map[string]domain.Customer
	events    map[string][]domain.AuditEvent
}

func NewRepository() *Repository {
	return &Repository{customers: make(map[string]domain.Customer), events: make(map[string][]domain.AuditEvent)}
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
