package observability

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsMiddlewareRecordsRouteAndRequestID(t *testing.T) {
	registry := NewRegistry()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })
	request := httptest.NewRequest(http.MethodGet, "/items/123", nil)
	response := httptest.NewRecorder()
	registry.Middleware(mux).ServeHTTP(response, request)
	if response.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing request id")
	}
	metrics := httptest.NewRecorder()
	registry.Handler(metrics, request)
	if body := metrics.Body.String(); !strings.Contains(body, `fccp_http_requests_total{method="GET",route="GET /items/{id}",status="201"} 1`) {
		t.Fatalf("metrics=%s", body)
	}
	if body := metrics.Body.String(); !strings.Contains(body, `fccp_http_request_duration_seconds_bucket{method="GET",route="GET /items/{id}",status="201",le="+Inf"} 1`) {
		t.Fatalf("histogram=%s", body)
	}
}
func TestDeliveryMetrics(t *testing.T) {
	registry := NewRegistry()
	registry.ObserveDelivery(2, 3, fmt.Errorf("failed"))
	response := httptest.NewRecorder()
	registry.Handler(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := response.Body.String()
	for _, expected := range []string{"fccp_notification_deliveries_completed_total 2", "fccp_notification_delivery_errors_total 1", "fccp_notification_outbox_pending 3"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in %s", expected, body)
		}
	}
}
func TestMetricsToken(t *testing.T) {
	registry := NewRegistry()
	registry.SetMetricsToken("secret")
	denied := httptest.NewRecorder()
	registry.Handler(denied, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("denied=%d", denied.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer secret")
	allowed := httptest.NewRecorder()
	registry.Handler(allowed, request)
	if allowed.Code != http.StatusOK {
		t.Fatalf("allowed=%d", allowed.Code)
	}
}
