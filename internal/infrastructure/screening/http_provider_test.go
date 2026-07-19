package screening

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPProviderRetriesAndDecodes(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
