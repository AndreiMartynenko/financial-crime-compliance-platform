package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
)

type Handler struct {
	service *application.OnboardingService
	logger  *slog.Logger
}

func NewHandler(service *application.OnboardingService, logger *slog.Logger) http.Handler {
	h := &Handler{service: service, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("POST /v1/customers", h.onboardCustomer)
	return requestLogging(logger, mux)
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
	cmd.Actor = strings.TrimSpace(r.Header.Get("X-Actor-ID"))
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
