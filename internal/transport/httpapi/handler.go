package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/auth"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
)

type Handler struct {
	service            *application.OnboardingService
	transactionService *application.TransactionService
	logger             *slog.Logger
	readiness          HealthChecker
}

type HealthChecker interface {
	Ping(context.Context) error
}

func NewHandler(service *application.OnboardingService, transactionService *application.TransactionService, logger *slog.Logger, authenticator *auth.Authenticator, health HealthChecker) http.Handler {
	h := &Handler{service: service, transactionService: transactionService, logger: logger, readiness: health}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("GET /readyz", h.ready)
	mux.Handle("POST /v1/customers", authenticate(authenticator, requireRoles(h.onboardCustomer, auth.RoleAnalyst, auth.RoleAdmin)))
	mux.Handle("POST /v1/customers/{customer_id}/approve", authenticate(authenticator, requireRoles(h.reviewCustomer(domain.ReviewApprove), auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("POST /v1/customers/{customer_id}/reject", authenticate(authenticator, requireRoles(h.reviewCustomer(domain.ReviewReject), auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("POST /v1/transactions", authenticate(authenticator, requireRoles(h.ingestTransaction, auth.RoleAnalyst, auth.RoleAdmin)))
	mux.Handle("GET /v1/alerts", authenticate(authenticator, requireRoles(h.listAlerts, auth.RoleAnalyst, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("POST /v1/alerts/{alert_id}/close", authenticate(authenticator, requireRoles(h.closeAlert, auth.RoleReviewer, auth.RoleAdmin)))
	return requestLogging(logger, mux)
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := h.readiness.Ping(ctx); err != nil {
		h.logger.Warn("readiness check failed", "error", err)
		writeError(w, http.StatusServiceUnavailable, "not_ready", "Database is not available")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) listAlerts(w http.ResponseWriter, r *http.Request) {
	alerts, err := h.transactionService.ListAlerts(r.Context(), domain.AlertStatus(r.URL.Query().Get("status")))
	if errors.Is(err, application.ErrInvalidAlertReview) {
		writeError(w, http.StatusUnprocessableEntity, "invalid_alert_filter", "Alert status filter is not valid")
		return
	}
	if err != nil {
		h.logger.Error("list alerts", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Alerts could not be listed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"alerts": alerts})
}

func (h *Handler) closeAlert(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var request reviewRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Request body is not valid")
		return
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "A valid bearer token is required")
		return
	}
	alert, err := h.transactionService.CloseAlert(r.Context(), r.PathValue("alert_id"), principal.Subject, request.Reason)
	switch {
	case errors.Is(err, application.ErrInvalidAlertReview):
		writeError(w, http.StatusUnprocessableEntity, "invalid_alert_review", "A closure reason is required")
		return
	case errors.Is(err, domain.ErrAlertNotFound):
		writeError(w, http.StatusNotFound, "alert_not_found", "Alert was not found")
		return
	case errors.Is(err, domain.ErrAlertConflict):
		writeError(w, http.StatusConflict, "alert_conflict", "Alert is not open")
		return
	case err != nil:
		h.logger.Error("close alert", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Alert could not be closed")
		return
	}
	writeJSON(w, http.StatusOK, alert)
}

func (h *Handler) ingestTransaction(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	var cmd application.IngestTransactionCommand
	if err := decoder.Decode(&cmd); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Request body is not valid")
		return
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "A valid bearer token is required")
		return
	}
	cmd.Actor = principal.Subject
	cmd.IdempotencyKey = r.Header.Get("Idempotency-Key")
	result, err := h.transactionService.Ingest(r.Context(), cmd)
	switch {
	case errors.Is(err, application.ErrInvalidTransaction):
		writeError(w, http.StatusUnprocessableEntity, "invalid_transaction", "Transaction data failed validation")
		return
	case errors.Is(err, domain.ErrCustomerNotFound):
		writeError(w, http.StatusNotFound, "customer_not_found", "Customer was not found")
		return
	case errors.Is(err, domain.ErrCustomerNotActive):
		writeError(w, http.StatusConflict, "customer_not_active", "Transactions can only be ingested for active customers")
		return
	case errors.Is(err, domain.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "Idempotency-Key was already used with a different request")
		return
	case err != nil:
		h.logger.Error("ingest transaction", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Transaction could not be ingested")
		return
	}
	if result.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
		writeJSON(w, http.StatusOK, result)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

type reviewRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) reviewCustomer(decision domain.ReviewDecision) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var request reviewRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid_json", "Request body is not valid")
			return
		}
		principal, ok := auth.PrincipalFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "A valid bearer token is required")
			return
		}
		customer, err := h.service.Review(r.Context(), r.PathValue("customer_id"), decision, principal.Subject, request.Reason)
		switch {
		case errors.Is(err, domain.ErrCustomerNotFound):
			writeError(w, http.StatusNotFound, "customer_not_found", "Customer was not found")
			return
		case errors.Is(err, domain.ErrMakerCannotReview):
			writeError(w, http.StatusConflict, "maker_checker_violation", "The customer creator cannot review their own submission")
			return
		case errors.Is(err, domain.ErrReviewConflict):
			writeError(w, http.StatusConflict, "review_conflict", "Customer is not pending approval")
			return
		case err != nil:
			h.logger.Error("review customer", "decision", decision, "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "Customer review could not be completed")
			return
		}
		writeJSON(w, http.StatusOK, customer)
	}
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) onboardCustomer(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	var cmd application.OnboardCustomerCommand
	if err := decoder.Decode(&cmd); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Request body is not valid")
		return
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "A valid bearer token is required")
		return
	}
	cmd.Actor = principal.Subject
	customer, err := h.service.Onboard(r.Context(), cmd)
	if errors.Is(err, application.ErrInvalidCustomer) {
		writeError(w, http.StatusUnprocessableEntity, "invalid_customer", "Customer data failed validation")
		return
	}
	if err != nil {
		h.logger.Error("onboard customer", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Customer could not be created")
		return
	}
	writeJSON(w, http.StatusCreated, customer)
}

func authenticate(authenticator *auth.Authenticator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticator.Authenticate(r.Header.Get("Authorization"))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "A valid bearer token is required")
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
	})
}

func requireRoles(next http.HandlerFunc, roles ...auth.Role) http.Handler {
	allowed := make(map[auth.Role]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.PrincipalFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "A valid bearer token is required")
			return
		}
		if _, ok := allowed[principal.Role]; !ok {
			writeError(w, http.StatusForbidden, "forbidden", "The authenticated role cannot perform this action")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info("http request", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
