package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTraceMiddlewareExtractsTraceParent(t *testing.T) {
	shutdown, err := InitTracing(context.Background(), "", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(context.Background())
	var got string
	handler := TraceMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got, _ = TraceIDs(r.Context()); w.WriteHeader(204) }))
	request := httptest.NewRequest(http.MethodGet, "/test", nil)
	request.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id=%s", got)
	}
}
