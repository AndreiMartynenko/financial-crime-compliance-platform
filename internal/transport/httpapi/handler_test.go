package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/infrastructure/memory"
)

func TestOnboardCustomer(t *testing.T) {
	t.Parallel()
	repo := memory.NewRepository()
	h := NewHandler(application.NewOnboardingService(repo), slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := []byte(`{"external_ref":"CRM-1001","type":"company","legal_name":"Example Payments Ltd","country_code":"gb","risk_factors":{"country_risk":"high","pep":true,"sanctions_potential_match":false,"high_risk_industry":false,"complex_ownership":true,"source_of_funds_verified":true}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/customers", bytes.NewReader(body))
	req.Header.Set("X-Actor-ID", "analyst@example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var customer domain.Customer
	if err := json.NewDecoder(rec.Body).Decode(&customer); err != nil {
		t.Fatal(err)
	}
	if customer.RiskAssessment.Rating != domain.RiskHigh || customer.RiskAssessment.DueDiligence != domain.DueDiligenceEnhanced {
		t.Fatalf("unexpected assessment: %+v", customer.RiskAssessment)
	}
	events, err := repo.ListAuditEvents(req.Context(), customer.ID)
	if err != nil || len(events) != 1 || events[0].Actor != "analyst@example.test" {
		t.Fatalf("unexpected audit events: %+v err=%v", events, err)
	}
}
