package httpapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/application"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/auth"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/domain"
	"github.com/AndreiMartynenko/financial-crime-compliance-platform/internal/infrastructure/memory"
)

func TestOnboardCustomer(t *testing.T) {
	t.Parallel()
	repo := memory.NewRepository()
	h := testHandler(t, repo)
	body := []byte(`{"external_ref":"CRM-1001","type":"company","legal_name":"Example Payments Ltd","country_code":"gb","risk_factors":{"country_risk":"high","pep":true,"sanctions_potential_match":false,"high_risk_industry":false,"complex_ownership":true,"source_of_funds_verified":true}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/customers", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+signedToken("analyst@example.test", auth.RoleAnalyst))
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
	if customer.Status != domain.CustomerPendingApproval || customer.CreatedBy != "analyst@example.test" {
		t.Fatalf("unexpected approval state: %+v", customer)
	}
	events, err := repo.ListAuditEvents(req.Context(), customer.ID)
	if err != nil || len(events) != 1 || events[0].Actor != "analyst@example.test" {
		t.Fatalf("unexpected audit events: %+v err=%v", events, err)
	}
}

func TestReviewerApprovesCustomer(t *testing.T) {
	t.Parallel()
	repo := memory.NewRepository()
	h := testHandler(t, repo)
	body := []byte(`{"type":"company","legal_name":"Approval Test Ltd","country_code":"GB","risk_factors":{"country_risk":"low","source_of_funds_verified":true}}`)
	createRequest := httptest.NewRequest(http.MethodPost, "/v1/customers", bytes.NewReader(body))
	createRequest.Header.Set("Authorization", "Bearer "+signedToken("maker@example.test", auth.RoleAnalyst))
	createResponse := httptest.NewRecorder()
	h.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	var created domain.Customer
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	reviewRequest := httptest.NewRequest(http.MethodPost, "/v1/customers/"+created.ID+"/approve", bytes.NewReader([]byte(`{"reason":"KYC evidence verified"}`)))
	reviewRequest.Header.Set("Authorization", "Bearer "+signedToken("checker@example.test", auth.RoleReviewer))
	reviewResponse := httptest.NewRecorder()
	h.ServeHTTP(reviewResponse, reviewRequest)
	if reviewResponse.Code != http.StatusOK {
		t.Fatalf("review status=%d body=%s", reviewResponse.Code, reviewResponse.Body.String())
	}
	var reviewed domain.Customer
	if err := json.NewDecoder(reviewResponse.Body).Decode(&reviewed); err != nil {
		t.Fatal(err)
	}
	if reviewed.Status != domain.CustomerActive || reviewed.ReviewedBy != "checker@example.test" || reviewed.ReviewedAt == nil {
		t.Fatalf("unexpected reviewed customer: %+v", reviewed)
	}
	events, err := repo.ListAuditEvents(reviewRequest.Context(), created.ID)
	if err != nil || len(events) != 2 || events[1].EventType != "customer.approved" {
		t.Fatalf("unexpected audit events: %+v err=%v", events, err)
	}
}

func TestMakerCannotApproveOwnCustomer(t *testing.T) {
	t.Parallel()
	repo := memory.NewRepository()
	h := testHandler(t, repo)
	body := []byte(`{"type":"company","legal_name":"Self Review Ltd","country_code":"GB","risk_factors":{"country_risk":"low","source_of_funds_verified":true}}`)
	createRequest := httptest.NewRequest(http.MethodPost, "/v1/customers", bytes.NewReader(body))
	createRequest.Header.Set("Authorization", "Bearer "+signedToken("admin@example.test", auth.RoleAdmin))
	createResponse := httptest.NewRecorder()
	h.ServeHTTP(createResponse, createRequest)
	var created domain.Customer
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	reviewRequest := httptest.NewRequest(http.MethodPost, "/v1/customers/"+created.ID+"/approve", nil)
	reviewRequest.Header.Set("Authorization", "Bearer "+signedToken("admin@example.test", auth.RoleAdmin))
	reviewResponse := httptest.NewRecorder()
	h.ServeHTTP(reviewResponse, reviewRequest)
	if reviewResponse.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", reviewResponse.Code, reviewResponse.Body.String())
	}
}

func TestIngestTransactionForActiveCustomer(t *testing.T) {
	t.Parallel()
	repo := memory.NewRepository()
	h := testHandler(t, repo)
	customerBody := []byte(`{"type":"company","legal_name":"Payments Customer Ltd","country_code":"GB","risk_factors":{"country_risk":"low","source_of_funds_verified":true}}`)
	createRequest := httptest.NewRequest(http.MethodPost, "/v1/customers", bytes.NewReader(customerBody))
	createRequest.Header.Set("Authorization", "Bearer "+signedToken("maker@example.test", auth.RoleAnalyst))
	createResponse := httptest.NewRecorder()
	h.ServeHTTP(createResponse, createRequest)
	var customer domain.Customer
	if err := json.NewDecoder(createResponse.Body).Decode(&customer); err != nil {
		t.Fatal(err)
	}
	reviewRequest := httptest.NewRequest(http.MethodPost, "/v1/customers/"+customer.ID+"/approve", nil)
	reviewRequest.Header.Set("Authorization", "Bearer "+signedToken("checker@example.test", auth.RoleReviewer))
	reviewResponse := httptest.NewRecorder()
	h.ServeHTTP(reviewResponse, reviewRequest)
	if reviewResponse.Code != http.StatusOK {
		t.Fatalf("review status=%d body=%s", reviewResponse.Code, reviewResponse.Body.String())
	}

	transactionBody := []byte(fmt.Sprintf(`{"external_ref":"PAY-1001","customer_id":%q,"direction":"outbound","amount_minor":125050,"currency":"gbp","counterparty_country":"de","occurred_at":"2026-07-19T12:00:00Z"}`, customer.ID))
	ingestRequest := httptest.NewRequest(http.MethodPost, "/v1/transactions", bytes.NewReader(transactionBody))
	ingestRequest.Header.Set("Authorization", "Bearer "+signedToken("payments-analyst@example.test", auth.RoleAnalyst))
	ingestResponse := httptest.NewRecorder()
	h.ServeHTTP(ingestResponse, ingestRequest)
	if ingestResponse.Code != http.StatusCreated {
		t.Fatalf("ingest status=%d body=%s", ingestResponse.Code, ingestResponse.Body.String())
	}
	var transaction domain.Transaction
	if err := json.NewDecoder(ingestResponse.Body).Decode(&transaction); err != nil {
		t.Fatal(err)
	}
	if transaction.Currency != "GBP" || transaction.CounterpartyCountry != "DE" || transaction.AmountMinor != 125050 {
		t.Fatalf("unexpected transaction: %+v", transaction)
	}
	events, err := repo.ListAuditEvents(ingestRequest.Context(), transaction.ID)
	if err != nil || len(events) != 1 || events[0].EventType != "transaction.ingested" || events[0].AggregateType != "transaction" {
		t.Fatalf("unexpected transaction events: %+v err=%v", events, err)
	}
}

func TestCannotIngestTransactionForPendingCustomer(t *testing.T) {
	t.Parallel()
	repo := memory.NewRepository()
	h := testHandler(t, repo)
	customerBody := []byte(`{"type":"company","legal_name":"Pending Customer Ltd","country_code":"GB","risk_factors":{"country_risk":"low","source_of_funds_verified":true}}`)
	createRequest := httptest.NewRequest(http.MethodPost, "/v1/customers", bytes.NewReader(customerBody))
	createRequest.Header.Set("Authorization", "Bearer "+signedToken("maker@example.test", auth.RoleAnalyst))
	createResponse := httptest.NewRecorder()
	h.ServeHTTP(createResponse, createRequest)
	var customer domain.Customer
	if err := json.NewDecoder(createResponse.Body).Decode(&customer); err != nil {
		t.Fatal(err)
	}
	transactionBody := []byte(fmt.Sprintf(`{"customer_id":%q,"direction":"inbound","amount_minor":100,"currency":"GBP","counterparty_country":"GB","occurred_at":"2026-07-19T12:00:00Z"}`, customer.ID))
	ingestRequest := httptest.NewRequest(http.MethodPost, "/v1/transactions", bytes.NewReader(transactionBody))
	ingestRequest.Header.Set("Authorization", "Bearer "+signedToken("analyst@example.test", auth.RoleAnalyst))
	ingestResponse := httptest.NewRecorder()
	h.ServeHTTP(ingestResponse, ingestRequest)
	if ingestResponse.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", ingestResponse.Code, ingestResponse.Body.String())
	}
}

func TestOnboardCustomerRequiresAuthentication(t *testing.T) {
	t.Parallel()
	h := testHandler(t, memory.NewRepository())
	req := httptest.NewRequest(http.MethodPost, "/v1/customers", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestReviewerCannotOnboardCustomer(t *testing.T) {
	t.Parallel()
	h := testHandler(t, memory.NewRepository())
	req := httptest.NewRequest(http.MethodPost, "/v1/customers", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer "+signedToken("reviewer@example.test", auth.RoleReviewer))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

const handlerTestSecret = "handler-test-secret-with-at-least-32-chars"

func testHandler(t *testing.T, repo *memory.Repository) http.Handler {
	t.Helper()
	authenticator, err := auth.NewAuthenticator(handlerTestSecret, "fccp-test")
	if err != nil {
		t.Fatal(err)
	}
	return NewHandler(application.NewOnboardingService(repo), application.NewTransactionService(repo), slog.New(slog.NewTextHandler(io.Discard, nil)), authenticator)
}

func signedToken(subject string, role auth.Role) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(map[string]any{
		"sub": subject, "role": role, "iss": "fccp-test", "exp": time.Now().Add(time.Hour).Unix(),
	})
	body := base64.RawURLEncoding.EncodeToString(payload)
	unsigned := header + "." + body
	mac := hmac.New(sha256.New, []byte(handlerTestSecret))
	_, _ = mac.Write([]byte(unsigned))
	return fmt.Sprintf("%s.%s", unsigned, base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
}
