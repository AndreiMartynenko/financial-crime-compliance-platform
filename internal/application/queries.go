package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
)

var ErrInvalidPage = errors.New("invalid page parameters")

type Page[T any] struct {
	Items         []T    `json:"items"`
	NextPageToken string `json:"next_page_token,omitempty"`
}

type PageRequest struct {
	Limit      int
	CursorTime time.Time
	CursorID   string
}

type ReadRepository interface {
	GetCustomer(context.Context, string) (domain.Customer, error)
	ListCustomers(context.Context, domain.CustomerStatus, PageRequest) ([]domain.Customer, error)
	ListCustomerTransactions(context.Context, string, PageRequest) ([]domain.Transaction, error)
	ListAuditEventsPage(context.Context, string, PageRequest) ([]domain.AuditEvent, error)
	ListAlertsPage(context.Context, domain.AlertStatus, PageRequest) ([]domain.Alert, error)
}

type QueryService struct{ repo ReadRepository }

func NewQueryService(repo ReadRepository) *QueryService { return &QueryService{repo: repo} }

func NewPageRequest(size int, token string) (PageRequest, error) {
	if size == 0 {
		size = 25
	}
	if size < 1 || size > 100 {
		return PageRequest{}, ErrInvalidPage
	}
	request := PageRequest{Limit: size + 1}
	if token == "" {
		return request, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return PageRequest{}, ErrInvalidPage
	}
	var cursor struct {
		Time time.Time `json:"time"`
		ID   string    `json:"id"`
	}
	if json.Unmarshal(data, &cursor) != nil || cursor.Time.IsZero() || cursor.ID == "" {
		return PageRequest{}, ErrInvalidPage
	}
	request.CursorTime, request.CursorID = cursor.Time, cursor.ID
	return request, nil
}

func page[T any](items []T, requestedLimit int, cursor func(T) (time.Time, string)) Page[T] {
	result := Page[T]{Items: items}
	if len(items) < requestedLimit {
		return result
	}
	result.Items = items[:requestedLimit-1]
	timestamp, id := cursor(result.Items[len(result.Items)-1])
	data, _ := json.Marshal(map[string]any{"time": timestamp, "id": id})
	result.NextPageToken = base64.RawURLEncoding.EncodeToString(data)
	return result
}

func (s *QueryService) GetCustomer(ctx context.Context, id string) (domain.Customer, error) {
	if id == "" {
		return domain.Customer{}, domain.ErrCustomerNotFound
	}
	return s.repo.GetCustomer(ctx, id)
}

func (s *QueryService) ListCustomers(ctx context.Context, status domain.CustomerStatus, request PageRequest) (Page[domain.Customer], error) {
	if status != "" && status != domain.CustomerPendingApproval && status != domain.CustomerActive && status != domain.CustomerRejected {
		return Page[domain.Customer]{}, ErrInvalidPage
	}
	items, err := s.repo.ListCustomers(ctx, status, request)
	return page(items, request.Limit, func(v domain.Customer) (time.Time, string) { return v.CreatedAt, v.ID }), err
}

func (s *QueryService) ListTransactions(ctx context.Context, customerID string, request PageRequest) (Page[domain.Transaction], error) {
	items, err := s.repo.ListCustomerTransactions(ctx, customerID, request)
	return page(items, request.Limit, func(v domain.Transaction) (time.Time, string) { return v.OccurredAt, v.ID }), err
}

func (s *QueryService) ListAuditEvents(ctx context.Context, aggregateID string, request PageRequest) (Page[domain.AuditEvent], error) {
	items, err := s.repo.ListAuditEventsPage(ctx, aggregateID, request)
	return page(items, request.Limit, func(v domain.AuditEvent) (time.Time, string) { return v.OccurredAt, v.ID }), err
}

func (s *QueryService) ListAlerts(ctx context.Context, status domain.AlertStatus, request PageRequest) (Page[domain.Alert], error) {
	if status != "" && status != domain.AlertOpen && status != domain.AlertClosed {
		return Page[domain.Alert]{}, ErrInvalidPage
	}
	items, err := s.repo.ListAlertsPage(ctx, status, request)
	return page(items, request.Limit, func(v domain.Alert) (time.Time, string) { return v.CreatedAt, v.ID }), err
}
