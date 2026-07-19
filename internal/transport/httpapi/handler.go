package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/auth"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
)

type Handler struct {
	service *application.OnboardingService
	logger  *slog.Logger
}

func NewHandler(service *application.OnboardingService, logger *slog.Logger, authenticator *auth.Authenticator) http.Handler {
	h := &Handler{service: service, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.Handle("POST /v1/customers", authenticate(authenticator, requireRoles(h.onboardCustomer, auth.RoleAnalyst, auth.RoleAdmin)))
	mux.Handle("POST /v1/customers/{customer_id}/approve", authenticate(authenticator, requireRoles(h.reviewCustomer(domain.ReviewApprove), auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("POST /v1/customers/{customer_id}/reject", authenticate(authenticator, requireRoles(h.reviewCustomer(domain.ReviewReject), auth.RoleReviewer, auth.RoleAdmin)))
	return requestLogging(logger, mux)
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
