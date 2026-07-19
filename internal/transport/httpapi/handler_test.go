package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
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

	transactionBody := []byte(fmt.Sprintf(`{"external_ref":"PAY-1001","customer_id":%q,"direction":"outbound","amount_minor":2000000,"currency":"gbp","counterparty_country":"ir","occurred_at":"2026-07-19T12:00:00Z"}`, customer.ID))
	ingestRequest := httptest.NewRequest(http.MethodPost, "/v1/transactions", bytes.NewReader(transactionBody))
	ingestRequest.Header.Set("Authorization", "Bearer "+signedToken("payments-analyst@example.test", auth.RoleAnalyst))
	ingestRequest.Header.Set("Idempotency-Key", "payment-PAY-1001")
	ingestResponse := httptest.NewRecorder()
	h.ServeHTTP(ingestResponse, ingestRequest)
	if ingestResponse.Code != http.StatusCreated {
		t.Fatalf("ingest status=%d body=%s", ingestResponse.Code, ingestResponse.Body.String())
	}
	var result application.IngestTransactionResult
	if err := json.NewDecoder(ingestResponse.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	transaction := result.Transaction
	if transaction.Currency != "GBP" || transaction.CounterpartyCountry != "IR" || transaction.AmountMinor != 2000000 {
		t.Fatalf("unexpected transaction: %+v", transaction)
	}
	if len(result.Alerts) != 2 || result.Alerts[0].RuleVersion != domain.TransactionMonitoringRuleVersion {
		t.Fatalf("unexpected alerts: %+v", result.Alerts)
	}
	events, err := repo.ListAuditEvents(ingestRequest.Context(), transaction.ID)
	if err != nil || len(events) != 1 || events[0].EventType != "transaction.ingested" || events[0].AggregateType != "transaction" {
		t.Fatalf("unexpected transaction events: %+v err=%v", events, err)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/v1/alerts?status=open", nil)
	listRequest.Header.Set("Authorization", "Bearer "+signedToken("analyst@example.test", auth.RoleAnalyst))
	listResponse := httptest.NewRecorder()
	h.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	var listed struct {
		Alerts []domain.Alert `json:"items"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&listed); err != nil || len(listed.Alerts) != 2 {
		t.Fatalf("listed alerts=%+v err=%v", listed.Alerts, err)
	}

	alert := result.Alerts[0]
	closeRequest := httptest.NewRequest(http.MethodPost, "/v1/alerts/"+alert.ID+"/close", bytes.NewReader([]byte(`{"reason":"Reviewed and explained by customer activity"}`)))
	closeRequest.Header.Set("Authorization", "Bearer "+signedToken("reviewer@example.test", auth.RoleReviewer))
	closeResponse := httptest.NewRecorder()
	h.ServeHTTP(closeResponse, closeRequest)
	if closeResponse.Code != http.StatusOK {
		t.Fatalf("close status=%d body=%s", closeResponse.Code, closeResponse.Body.String())
	}
	var closed domain.Alert
	if err := json.NewDecoder(closeResponse.Body).Decode(&closed); err != nil || closed.Status != domain.AlertClosed || closed.ClosedBy != "reviewer@example.test" {
		t.Fatalf("closed alert=%+v err=%v", closed, err)
	}
	alertEvents, err := repo.ListAuditEvents(closeRequest.Context(), alert.ID)
	if err != nil || len(alertEvents) != 2 || alertEvents[1].EventType != "alert.closed" {
		t.Fatalf("unexpected alert events: %+v err=%v", alertEvents, err)
	}

	replayRequest := httptest.NewRequest(http.MethodPost, "/v1/transactions", bytes.NewReader(transactionBody))
	replayRequest.Header.Set("Authorization", "Bearer "+signedToken("payments-analyst@example.test", auth.RoleAnalyst))
	replayRequest.Header.Set("Idempotency-Key", "payment-PAY-1001")
	replayResponse := httptest.NewRecorder()
	h.ServeHTTP(replayResponse, replayRequest)
	if replayResponse.Code != http.StatusOK || replayResponse.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay status=%d headers=%v body=%s", replayResponse.Code, replayResponse.Header(), replayResponse.Body.String())
	}
	var replayed application.IngestTransactionResult
	if err := json.NewDecoder(replayResponse.Body).Decode(&replayed); err != nil || replayed.Transaction.ID != transaction.ID || len(replayed.Alerts) != 2 {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}

	conflictingBody := []byte(fmt.Sprintf(`{"external_ref":"PAY-1001","customer_id":%q,"direction":"outbound","amount_minor":3000000,"currency":"GBP","counterparty_country":"IR","occurred_at":"2026-07-19T12:00:00Z"}`, customer.ID))
	conflictRequest := httptest.NewRequest(http.MethodPost, "/v1/transactions", bytes.NewReader(conflictingBody))
	conflictRequest.Header.Set("Authorization", "Bearer "+signedToken("payments-analyst@example.test", auth.RoleAnalyst))
	conflictRequest.Header.Set("Idempotency-Key", "payment-PAY-1001")
	conflictResponse := httptest.NewRecorder()
	h.ServeHTTP(conflictResponse, conflictRequest)
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", conflictResponse.Code, conflictResponse.Body.String())
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
	ingestRequest.Header.Set("Idempotency-Key", "pending-customer-payment")
	ingestResponse := httptest.NewRecorder()
	h.ServeHTTP(ingestResponse, ingestRequest)
	if ingestResponse.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", ingestResponse.Code, ingestResponse.Body.String())
	}
}

func TestListCustomersUsesCursorPagination(t *testing.T) {
	t.Parallel()
	repo := memory.NewRepository()
	h := testHandler(t, repo)
	for _, name := range []string{"First Customer Ltd", "Second Customer Ltd"} {
		body := []byte(fmt.Sprintf(`{"type":"company","legal_name":%q,"country_code":"GB","risk_factors":{"country_risk":"low","source_of_funds_verified":true}}`, name))
		req := httptest.NewRequest(http.MethodPost, "/v1/customers", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+signedToken("maker@example.test", auth.RoleAnalyst))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	firstRequest := httptest.NewRequest(http.MethodGet, "/v1/customers?status=pending_approval&page_size=1", nil)
	firstRequest.Header.Set("Authorization", "Bearer "+signedToken("reviewer@example.test", auth.RoleReviewer))
	firstResponse := httptest.NewRecorder()
	h.ServeHTTP(firstResponse, firstRequest)
	var first application.Page[domain.Customer]
	if err := json.NewDecoder(firstResponse.Body).Decode(&first); err != nil || len(first.Items) != 1 || first.NextPageToken == "" {
		t.Fatalf("first page=%+v err=%v", first, err)
	}
	secondRequest := httptest.NewRequest(http.MethodGet, "/v1/customers?page_size=1&page_token="+first.NextPageToken, nil)
	secondRequest.Header.Set("Authorization", "Bearer "+signedToken("reviewer@example.test", auth.RoleReviewer))
	secondResponse := httptest.NewRecorder()
	h.ServeHTTP(secondResponse, secondRequest)
	var second application.Page[domain.Customer]
	if err := json.NewDecoder(secondResponse.Body).Decode(&second); err != nil || len(second.Items) != 1 || second.Items[0].ID == first.Items[0].ID {
		t.Fatalf("second page=%+v err=%v", second, err)
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

func TestReadinessFailsWhenDatabaseIsUnavailable(t *testing.T) {
	t.Parallel()
	repo := memory.NewRepository()
	authenticator, err := auth.NewAuthenticator(handlerTestSecret, "fccp-test")
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(
		application.NewOnboardingService(repo), application.NewTransactionService(repo), application.NewQueryService(repo),
		slog.New(slog.NewTextHandler(io.Discard, nil)), authenticator,
		healthCheckerFunc(func(context.Context) error { return errors.New("database unavailable") }),
	)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
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
	return NewHandler(application.NewOnboardingService(repo), application.NewTransactionService(repo), application.NewQueryService(repo), slog.New(slog.NewTextHandler(io.Discard, nil)), authenticator, healthCheckerFunc(func(context.Context) error { return nil }))
}

type healthCheckerFunc func(context.Context) error

func (check healthCheckerFunc) Ping(ctx context.Context) error {
	return check(ctx)
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
