package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHeadersAndRateLimit(t *testing.T) {
	limiter := NewRateLimiter(0.0001, 1)
	handler := Headers(limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })))
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/v1/test", nil))
	if first.Code != 204 || first.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("first=%d headers=%v", first.Code, first.Header())
	}
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/v1/test", nil))
	if second.Code != 429 || second.Header().Get("Retry-After") != "1" {
		t.Fatalf("second=%d headers=%v", second.Code, second.Header())
	}
}
