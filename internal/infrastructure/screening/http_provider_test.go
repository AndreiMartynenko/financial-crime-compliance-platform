package screening

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPProviderRetriesAndDecodes(t *testing.T) {
	var calls atomic.Int32
	var idempotencyKey atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("X-Correlation-ID") == "" || r.Header.Get("Idempotency-Key") == "" {
			t.Error("missing provider request headers")
		}
		if previous := idempotencyKey.Load(); previous != nil && previous.(string) != r.Header.Get("Idempotency-Key") {
			t.Error("retry changed idempotency key")
		}
		idempotencyKey.Store(r.Header.Get("Idempotency-Key"))
		if calls.Add(1) == 1 {
			http.Error(w, "temporary", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"list_type":"pep","name":"Nadia Karim","score":98,"reason":"provider match"}]}`))
	}))
	defer server.Close()
	provider := NewHTTPProvider(server.URL, "secret", time.Second, 1, time.Minute)
	items, err := provider.Screen(context.Background(), "Nadia Karim")
	if err != nil || len(items) != 1 || items[0].Score != 98 || calls.Load() != 2 {
		t.Fatalf("items=%+v calls=%d err=%v", items, calls.Load(), err)
	}
}

func TestConfiguredHTTPProviderRequiresHTTPS(t *testing.T) {
	if _, err := NewConfiguredHTTPProvider(HTTPProviderConfig{Endpoint: "http://provider.example", Timeout: time.Second}); err == nil {
		t.Fatal("expected HTTPS validation error")
	}
	provider, err := NewConfiguredHTTPProvider(HTTPProviderConfig{Endpoint: "http://provider.example", Timeout: time.Second, AllowInsecureHTTP: true, Name: "test-provider"})
	if err != nil || provider.Name() != "test-provider" {
		t.Fatalf("provider=%+v err=%v", provider, err)
	}
}

func TestHTTPProviderRejectsInvalidContractResponse(t *testing.T) {
	tests := []string{
		`{"candidates":[{"list_type":"unknown","name":"Name","score":90,"reason":"match"}]}`,
		`{"candidates":[{"list_type":"pep","name":"","score":90,"reason":"match"}]}`,
		`{"candidates":[],"unexpected":true}`,
		`{"candidates":[]} {"trailing":true}`,
	}
	for _, response := range tests {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(response))
		}))
		provider := NewHTTPProvider(server.URL, "", time.Second, 0, time.Minute)
		if _, err := provider.Screen(context.Background(), "Test Name"); err == nil {
			server.Close()
			t.Fatalf("response accepted: %s", response)
		}
		server.Close()
	}
}

func TestHTTPProviderRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[],"padding":"` + strings.Repeat("x", maxProviderResponseBytes) + `"}`))
	}))
	defer server.Close()
	if _, err := NewHTTPProvider(server.URL, "", time.Second, 0, time.Minute).Screen(context.Background(), "Test Name"); err == nil || !strings.Contains(err.Error(), "exceeds 1 MiB") {
		t.Fatalf("err=%v", err)
	}
}
func TestHTTPProviderCircuitBreaker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "down", 500) }))
	defer server.Close()
	provider := NewHTTPProvider(server.URL, "", time.Second, 0, time.Minute)
	for i := 0; i < 3; i++ {
		_, _ = provider.Screen(context.Background(), "name")
	}
	if _, err := provider.Screen(context.Background(), "name"); err != ErrCircuitOpen {
		t.Fatalf("err=%v", err)
	}
}

func TestHTTPProviderDoesNotFollowRedirects(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[]}`))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	if _, err := NewHTTPProvider(redirect.URL, "", time.Second, 0, time.Minute).Screen(context.Background(), "name"); err == nil {
		t.Fatal("expected redirect rejection")
	}
	if targetCalls.Load() != 0 {
		t.Fatalf("redirect target calls=%d", targetCalls.Load())
	}
}
