package observability

import (
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
}
