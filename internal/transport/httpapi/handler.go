package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/auth"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/observability"
)

type Handler struct {
	service             *application.OnboardingService
	transactionService  *application.TransactionService
	queryService        *application.QueryService
	caseService         *application.CaseService
	dueDiligenceService *application.DueDiligenceService
	screeningService    *application.ScreeningService
	logger              *slog.Logger
	readiness           HealthChecker
	metrics             *observability.Registry
}

type HealthChecker interface {
	Ping(context.Context) error
}

func NewHandler(service *application.OnboardingService, transactionService *application.TransactionService, queryService *application.QueryService, caseService *application.CaseService, dueDiligenceService *application.DueDiligenceService, screeningService *application.ScreeningService, logger *slog.Logger, authenticator *auth.Authenticator, health HealthChecker, metrics *observability.Registry) http.Handler {
	h := &Handler{service: service, transactionService: transactionService, queryService: queryService, caseService: caseService, dueDiligenceService: dueDiligenceService, screeningService: screeningService, logger: logger, readiness: health, metrics: metrics}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("GET /readyz", h.ready)
	mux.HandleFunc("GET /metrics", metrics.Handler)
	mux.Handle("POST /v1/customers", authenticate(authenticator, requireRoles(h.onboardCustomer, auth.RoleAnalyst, auth.RoleAdmin)))
	mux.Handle("GET /v1/customers", authenticate(authenticator, requireRoles(h.listCustomers, auth.RoleAnalyst, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("GET /v1/customers/{customer_id}", authenticate(authenticator, requireRoles(h.getCustomer, auth.RoleAnalyst, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("GET /v1/customers/{customer_id}/transactions", authenticate(authenticator, requireRoles(h.listCustomerTransactions, auth.RoleAnalyst, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("GET /v1/customers/{customer_id}/audit-events", authenticate(authenticator, requireRoles(h.listAuditEvents, auth.RoleAnalyst, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("GET /v1/customers/{customer_id}/activity", authenticate(authenticator, requireRoles(h.listCustomerActivity, auth.RoleAnalyst, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("POST /v1/customers/{customer_id}/approve", authenticate(authenticator, requireRoles(h.reviewCustomer(domain.ReviewApprove), auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("POST /v1/customers/{customer_id}/reject", authenticate(authenticator, requireRoles(h.reviewCustomer(domain.ReviewReject), auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("POST /v1/transactions", authenticate(authenticator, requireRoles(h.ingestTransaction, auth.RoleAnalyst, auth.RoleAdmin)))
	mux.Handle("GET /v1/alerts", authenticate(authenticator, requireRoles(h.listAlerts, auth.RoleAnalyst, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("POST /v1/alerts/{alert_id}/close", authenticate(authenticator, requireRoles(h.closeAlert, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("GET /v1/cases", authenticate(authenticator, requireRoles(h.listCases, auth.RoleAnalyst, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("POST /v1/cases", authenticate(authenticator, requireRoles(h.createCase, auth.RoleAnalyst, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("GET /v1/cases/{case_id}", authenticate(authenticator, requireRoles(h.getCase, auth.RoleAnalyst, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("POST /v1/cases/{case_id}/assign", authenticate(authenticator, requireRoles(h.assignCase, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("POST /v1/cases/{case_id}/comments", authenticate(authenticator, requireRoles(h.commentCase, auth.RoleAnalyst, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("POST /v1/cases/{case_id}/resolve", authenticate(authenticator, requireRoles(h.resolveCase, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("GET /v1/customers/{customer_id}/due-diligence", authenticate(authenticator, requireRoles(h.getDueDiligence, auth.RoleAnalyst, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("PUT /v1/customers/{customer_id}/due-diligence", authenticate(authenticator, requireRoles(h.updateDueDiligence, auth.RoleAnalyst, auth.RoleAdmin)))
	mux.Handle("POST /v1/customers/{customer_id}/beneficial-owners", authenticate(authenticator, requireRoles(h.addBeneficialOwner, auth.RoleAnalyst, auth.RoleAdmin)))
	mux.Handle("POST /v1/customers/{customer_id}/kyc-documents", authenticate(authenticator, requireRoles(h.addKYCDocument, auth.RoleAnalyst, auth.RoleAdmin)))
	mux.Handle("POST /v1/kyc-documents/{document_id}/review", authenticate(authenticator, requireRoles(h.reviewKYCDocument, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("POST /v1/customers/{customer_id}/screenings", authenticate(authenticator, requireRoles(h.screenCustomer, auth.RoleAnalyst, auth.RoleAdmin)))
	mux.Handle("GET /v1/customers/{customer_id}/screening-matches", authenticate(authenticator, requireRoles(h.listScreeningMatches, auth.RoleAnalyst, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("POST /v1/screening-matches/{match_id}/disposition", authenticate(authenticator, requireRoles(h.dispositionScreeningMatch, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("GET /v1/customers/{customer_id}/screening-schedule", authenticate(authenticator, requireRoles(h.getScreeningSchedule, auth.RoleAnalyst, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("PUT /v1/customers/{customer_id}/screening-schedule", authenticate(authenticator, requireRoles(h.updateScreeningSchedule, auth.RoleAnalyst, auth.RoleAdmin)))
	mux.Handle("GET /v1/notifications", authenticate(authenticator, requireRoles(h.listNotifications, auth.RoleAnalyst, auth.RoleReviewer, auth.RoleAdmin)))
	mux.Handle("POST /v1/notifications/{notification_id}/read", authenticate(authenticator, requireRoles(h.readNotification, auth.RoleAnalyst, auth.RoleReviewer, auth.RoleAdmin)))
	return metrics.Middleware(requestLogging(logger, mux))
}
func (h *Handler) listNotifications(w http.ResponseWriter, r *http.Request) {
	items, err := h.screeningService.ListNotifications(r.Context(), 100)
	if err != nil {
		h.readError(w, "list notifications", err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (h *Handler) readNotification(w http.ResponseWriter, r *http.Request) {
	item, err := h.screeningService.ReadNotification(r.Context(), r.PathValue("notification_id"), principalSubject(r))
	if errors.Is(err, domain.ErrNotificationNotFound) {
		writeError(w, 404, "notification_not_found", "Notification was not found")
		return
	}
	if err != nil {
		h.readError(w, "read notification", err)
		return
	}
	writeJSON(w, 200, item)
}

type screeningScheduleRequest struct {
	Enabled       bool `json:"enabled"`
	IntervalHours int  `json:"interval_hours"`
}

func (h *Handler) getScreeningSchedule(w http.ResponseWriter, r *http.Request) {
	result, err := h.screeningService.GetSchedule(r.Context(), r.PathValue("customer_id"))
	if errors.Is(err, domain.ErrScreeningScheduleNotFound) {
		writeError(w, 404, "schedule_not_found", "Ongoing screening is not configured")
		return
	}
	if err != nil {
		h.readError(w, "get screening schedule", err)
		return
	}
	writeJSON(w, 200, result)
}
func (h *Handler) updateScreeningSchedule(w http.ResponseWriter, r *http.Request) {
	var request screeningScheduleRequest
	if decodeJSON(w, r, &request) != nil {
		writeError(w, 400, "invalid_json", "Request body is not valid")
		return
	}
	result, err := h.screeningService.ConfigureSchedule(r.Context(), r.PathValue("customer_id"), request.Enabled, request.IntervalHours, principalSubject(r))
	if errors.Is(err, application.ErrInvalidScreening) {
		writeError(w, 422, "invalid_schedule", "Interval must be between 1 and 8760 hours")
		return
	}
	if errors.Is(err, domain.ErrCustomerNotFound) {
		writeError(w, 404, "customer_not_found", "Customer was not found")
		return
	}
	if err != nil {
		h.readError(w, "update screening schedule", err)
		return
	}
	writeJSON(w, 200, result)
}

type screeningDispositionRequest struct {
	Status domain.ScreeningMatchStatus `json:"status"`
	Reason string                      `json:"reason"`
}

func (h *Handler) screenCustomer(w http.ResponseWriter, r *http.Request) {
	result, err := h.screeningService.ScreenCustomer(r.Context(), r.PathValue("customer_id"), principalSubject(r))
	if errors.Is(err, domain.ErrCustomerNotFound) {
		writeError(w, 404, "customer_not_found", "Customer was not found")
		return
	}
	if err != nil {
		h.readError(w, "screen customer", err)
		return
	}
	writeJSON(w, 201, result)
}
func (h *Handler) listScreeningMatches(w http.ResponseWriter, r *http.Request) {
	result, err := h.screeningService.List(r.Context(), r.PathValue("customer_id"))
	if err != nil {
		h.readError(w, "list screening matches", err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": result})
}
func (h *Handler) dispositionScreeningMatch(w http.ResponseWriter, r *http.Request) {
	var request screeningDispositionRequest
	if decodeJSON(w, r, &request) != nil {
		writeError(w, 400, "invalid_json", "Request body is not valid")
		return
	}
	result, err := h.screeningService.Disposition(r.Context(), r.PathValue("match_id"), request.Status, request.Reason, principalSubject(r))
	if errors.Is(err, application.ErrInvalidScreening) {
		writeError(w, 422, "invalid_disposition", "Disposition failed validation")
		return
	}
	if errors.Is(err, domain.ErrReviewConflict) {
		writeError(w, 409, "review_conflict", "Match is not pending review")
		return
	}
	if err != nil {
		h.readError(w, "disposition screening match", err)
		return
	}
	writeJSON(w, 200, result)
}

type documentReviewRequest struct {
	Status domain.DocumentStatus `json:"status"`
}

func (h *Handler) getDueDiligence(w http.ResponseWriter, r *http.Request) {
	result, err := h.dueDiligenceService.Get(r.Context(), r.PathValue("customer_id"))
	h.writeCDDResult(w, result, err, 200)
}
func (h *Handler) updateDueDiligence(w http.ResponseWriter, r *http.Request) {
	var request domain.CDDProfile
	if decodeJSON(w, r, &request) != nil {
		writeError(w, 400, "invalid_json", "Request body is not valid")
		return
	}
	request.CustomerID = r.PathValue("customer_id")
	result, err := h.dueDiligenceService.UpdateProfile(r.Context(), request, principalSubject(r))
	if errors.Is(err, application.ErrInvalidDueDiligence) {
		writeError(w, 422, "invalid_due_diligence", "CDD profile failed validation")
		return
	}
	if errors.Is(err, domain.ErrCustomerNotFound) {
		writeError(w, 404, "customer_not_found", "Customer was not found")
		return
	}
	if err != nil {
		h.readError(w, "update due diligence", err)
		return
	}
	writeJSON(w, 200, result)
}
func (h *Handler) addBeneficialOwner(w http.ResponseWriter, r *http.Request) {
	var request domain.BeneficialOwner
	if decodeJSON(w, r, &request) != nil {
		writeError(w, 400, "invalid_json", "Request body is not valid")
		return
	}
	request.CustomerID = r.PathValue("customer_id")
	result, err := h.dueDiligenceService.AddOwner(r.Context(), request, principalSubject(r))
	if errors.Is(err, application.ErrInvalidDueDiligence) {
		writeError(w, 422, "invalid_beneficial_owner", "Beneficial owner failed validation")
		return
	}
	if err != nil {
		h.readError(w, "add beneficial owner", err)
		return
	}
	writeJSON(w, 201, result)
}
func (h *Handler) addKYCDocument(w http.ResponseWriter, r *http.Request) {
	var request domain.KYCDocument
	if decodeJSON(w, r, &request) != nil {
		writeError(w, 400, "invalid_json", "Request body is not valid")
		return
	}
	request.CustomerID = r.PathValue("customer_id")
	result, err := h.dueDiligenceService.AddDocument(r.Context(), request, principalSubject(r))
	if errors.Is(err, application.ErrInvalidDueDiligence) {
		writeError(w, 422, "invalid_document", "KYC document failed validation")
		return
	}
	if err != nil {
		h.readError(w, "add KYC document", err)
		return
	}
	writeJSON(w, 201, result)
}
func (h *Handler) reviewKYCDocument(w http.ResponseWriter, r *http.Request) {
	var request documentReviewRequest
	if decodeJSON(w, r, &request) != nil {
		writeError(w, 400, "invalid_json", "Request body is not valid")
		return
	}
	result, err := h.dueDiligenceService.ReviewDocument(r.Context(), r.PathValue("document_id"), request.Status, principalSubject(r))
	if errors.Is(err, application.ErrInvalidDueDiligence) {
		writeError(w, 422, "invalid_review", "Document review failed validation")
		return
	}
	if errors.Is(err, domain.ErrReviewConflict) {
		writeError(w, 409, "review_conflict", "Document is not pending review")
		return
	}
	if err != nil {
		h.readError(w, "review KYC document", err)
		return
	}
	writeJSON(w, 200, result)
}
func (h *Handler) writeCDDResult(w http.ResponseWriter, result domain.DueDiligenceDetails, err error, status int) {
	if errors.Is(err, domain.ErrCustomerNotFound) {
		writeError(w, 404, "customer_not_found", "Customer was not found")
		return
	}
	if err != nil {
		h.readError(w, "get due diligence", err)
		return
	}
	writeJSON(w, status, result)
}

type caseRequest struct {
	AlertID  string              `json:"alert_id"`
	Title    string              `json:"title"`
	Priority domain.CasePriority `json:"priority"`
}
type caseTextRequest struct {
	Assignee   string `json:"assignee"`
	Body       string `json:"body"`
	Resolution string `json:"resolution"`
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(destination)
}
func principalSubject(r *http.Request) string {
	principal, _ := auth.PrincipalFromContext(r.Context())
	return principal.Subject
}

func (h *Handler) listCases(w http.ResponseWriter, r *http.Request) {
	page, err := pageRequest(r)
	if err != nil {
		writeError(w, 422, "invalid_page", "Pagination parameters are invalid")
		return
	}
	result, err := h.queryService.ListCases(r.Context(), domain.CaseStatus(r.URL.Query().Get("status")), page)
	if errors.Is(err, application.ErrInvalidPage) {
		writeError(w, 422, "invalid_filter", "Case status is invalid")
		return
	}
	if err != nil {
		h.readError(w, "list cases", err)
		return
	}
	writeJSON(w, 200, result)
}
func (h *Handler) getCase(w http.ResponseWriter, r *http.Request) {
	result, err := h.caseService.Details(r.Context(), r.PathValue("case_id"))
	if errors.Is(err, domain.ErrCaseNotFound) {
		writeError(w, 404, "case_not_found", "Case was not found")
		return
	}
	if err != nil {
		h.readError(w, "get case", err)
		return
	}
	writeJSON(w, 200, result)
}
func (h *Handler) createCase(w http.ResponseWriter, r *http.Request) {
	var request caseRequest
	if decodeJSON(w, r, &request) != nil {
		writeError(w, 400, "invalid_json", "Request body is not valid")
		return
	}
	result, err := h.caseService.Create(r.Context(), request.AlertID, request.Title, request.Priority, principalSubject(r))
	switch {
	case errors.Is(err, application.ErrInvalidCase):
		writeError(w, 422, "invalid_case", "Case data failed validation")
	case errors.Is(err, domain.ErrAlertNotFound):
		writeError(w, 404, "alert_not_found", "Alert was not found")
	case errors.Is(err, domain.ErrAlertConflict), errors.Is(err, domain.ErrAlertHasCase):
		writeError(w, 409, "case_conflict", "Alert cannot be added to a new case")
	case err != nil:
		h.readError(w, "create case", err)
	default:
		writeJSON(w, 201, result)
	}
}
func (h *Handler) assignCase(w http.ResponseWriter, r *http.Request) {
	var request caseTextRequest
	if decodeJSON(w, r, &request) != nil {
		writeError(w, 400, "invalid_json", "Request body is not valid")
		return
	}
	result, err := h.caseService.Assign(r.Context(), r.PathValue("case_id"), request.Assignee, principalSubject(r))
	h.writeCaseMutation(w, result, err)
}
func (h *Handler) commentCase(w http.ResponseWriter, r *http.Request) {
	var request caseTextRequest
	if decodeJSON(w, r, &request) != nil {
		writeError(w, 400, "invalid_json", "Request body is not valid")
		return
	}
	result, err := h.caseService.Comment(r.Context(), r.PathValue("case_id"), request.Body, principalSubject(r))
	switch {
	case errors.Is(err, application.ErrInvalidCase):
		writeError(w, 422, "invalid_case", "Comment is required")
	case errors.Is(err, domain.ErrCaseNotFound):
		writeError(w, 404, "case_not_found", "Case was not found")
	case errors.Is(err, domain.ErrCaseConflict):
		writeError(w, 409, "case_conflict", "Resolved cases cannot be changed")
	case err != nil:
		h.readError(w, "comment case", err)
	default:
		writeJSON(w, 201, result)
	}
}
func (h *Handler) resolveCase(w http.ResponseWriter, r *http.Request) {
	var request caseTextRequest
	if decodeJSON(w, r, &request) != nil {
		writeError(w, 400, "invalid_json", "Request body is not valid")
		return
	}
	result, err := h.caseService.Resolve(r.Context(), r.PathValue("case_id"), request.Resolution, principalSubject(r))
	h.writeCaseMutation(w, result, err)
}
func (h *Handler) writeCaseMutation(w http.ResponseWriter, result domain.InvestigationCase, err error) {
	switch {
	case errors.Is(err, application.ErrInvalidCase):
		writeError(w, 422, "invalid_case", "Case update failed validation")
	case errors.Is(err, domain.ErrCaseNotFound):
		writeError(w, 404, "case_not_found", "Case was not found")
	case errors.Is(err, domain.ErrCaseConflict), errors.Is(err, domain.ErrAlertConflict):
		writeError(w, 409, "case_conflict", "Resolved cases cannot be changed")
	case err != nil:
		h.readError(w, "update case", err)
	default:
		writeJSON(w, 200, result)
	}
}

func pageRequest(r *http.Request) (application.PageRequest, error) {
	size := 0
	if value := r.URL.Query().Get("page_size"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return application.PageRequest{}, application.ErrInvalidPage
		}
		size = parsed
	}
	return application.NewPageRequest(size, r.URL.Query().Get("page_token"))
}

func (h *Handler) listCustomers(w http.ResponseWriter, r *http.Request) {
	page, err := pageRequest(r)
	if err != nil {
		writeError(w, 422, "invalid_page", "Pagination parameters are invalid")
		return
	}
	result, err := h.queryService.ListCustomers(r.Context(), domain.CustomerStatus(r.URL.Query().Get("status")), page)
	if err != nil {
		if errors.Is(err, application.ErrInvalidPage) {
			writeError(w, 422, "invalid_filter", "Customer status is invalid")
			return
		}
		h.readError(w, "list customers", err)
		return
	}
	writeJSON(w, 200, result)
}
func (h *Handler) getCustomer(w http.ResponseWriter, r *http.Request) {
	customer, err := h.queryService.GetCustomer(r.Context(), r.PathValue("customer_id"))
	if errors.Is(err, domain.ErrCustomerNotFound) {
		writeError(w, 404, "customer_not_found", "Customer was not found")
		return
	}
	if err != nil {
		h.readError(w, "get customer", err)
		return
	}
	writeJSON(w, 200, customer)
}
func (h *Handler) listCustomerTransactions(w http.ResponseWriter, r *http.Request) {
	page, err := pageRequest(r)
	if err != nil {
		writeError(w, 422, "invalid_page", "Pagination parameters are invalid")
		return
	}
	result, err := h.queryService.ListTransactions(r.Context(), r.PathValue("customer_id"), page)
	if err != nil {
		h.readError(w, "list transactions", err)
		return
	}
	writeJSON(w, 200, result)
}
func (h *Handler) listAuditEvents(w http.ResponseWriter, r *http.Request) {
	page, err := pageRequest(r)
	if err != nil {
		writeError(w, 422, "invalid_page", "Pagination parameters are invalid")
		return
	}
	result, err := h.queryService.ListAuditEvents(r.Context(), r.PathValue("customer_id"), page)
	if err != nil {
		h.readError(w, "list audit events", err)
		return
	}
	writeJSON(w, 200, result)
}
func (h *Handler) listCustomerActivity(w http.ResponseWriter, r *http.Request) {
	page, err := pageRequest(r)
	if err != nil {
		writeError(w, 422, "invalid_page", "Pagination parameters are invalid")
		return
	}
	result, err := h.queryService.ListCustomerActivity(r.Context(), r.PathValue("customer_id"), page)
	if errors.Is(err, domain.ErrCustomerNotFound) {
		writeError(w, 404, "customer_not_found", "Customer was not found")
		return
	}
	if err != nil {
		h.readError(w, "list customer activity", err)
		return
	}
	writeJSON(w, 200, result)
}
func (h *Handler) readError(w http.ResponseWriter, operation string, err error) {
	h.logger.Error(operation, "error", err)
	writeError(w, 500, "internal_error", "Data could not be loaded")
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
	page, err := pageRequest(r)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_page", "Pagination parameters are invalid")
		return
	}
	alerts, err := h.queryService.ListAlerts(r.Context(), domain.AlertStatus(r.URL.Query().Get("status")), page)
	if errors.Is(err, application.ErrInvalidPage) {
		writeError(w, http.StatusUnprocessableEntity, "invalid_alert_filter", "Alert status filter is not valid")
		return
	}
	if err != nil {
		h.logger.Error("list alerts", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Alerts could not be listed")
		return
	}
	writeJSON(w, http.StatusOK, alerts)
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
		started := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("http request", "request_id", w.Header().Get("X-Request-ID"), "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
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
