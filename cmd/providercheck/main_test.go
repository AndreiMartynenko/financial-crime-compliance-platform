package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunChecksProviderContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/screen" || request.Header.Get("Idempotency-Key") == "" {
			t.Errorf("request=%s idempotency=%q", request.URL.Path, request.Header.Get("Idempotency-Key"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"list_type":"sanctions","name":"Synthetic Match","score":90,"reason":"contract fixture"}]}`))
	}))
	defer server.Close()
	t.Setenv("SCREENING_PROVIDER_URL", server.URL)
	t.Setenv("SCREENING_PROVIDER_CHECK_NAME", "Synthetic Subject")
	t.Setenv("SCREENING_PROVIDER_ALLOW_HTTP", "true")
	if err := run(); err != nil {
		t.Fatal(err)
	}
}
