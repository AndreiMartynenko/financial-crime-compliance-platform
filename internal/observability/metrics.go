package observability

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type httpMetric struct {
	Count    uint64
	Duration float64
}
type Registry struct {
	mu                  sync.RWMutex
	http                map[string]httpMetric
	workerRuns          uint64
	workerErrors        uint64
	jobsCompleted       uint64
	deliveryRuns        uint64
	deliveryErrors      uint64
	deliveriesCompleted uint64
	outboxPending       int
	metricsToken        string
}

func (r *Registry) ObserveDelivery(completed, pending int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deliveryRuns++
	r.deliveriesCompleted += uint64(completed)
	r.outboxPending = pending
	if err != nil {
		r.deliveryErrors++
	}
}

func NewRegistry() *Registry { return &Registry{http: map[string]httpMetric{}} }
func (r *Registry) SetMetricsToken(token string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metricsToken = strings.TrimSpace(token)
}

func (r *Registry) ObserveWorker(completed int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workerRuns++
	r.jobsCompleted += uint64(completed)
	if err != nil {
		r.workerErrors++
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (r *Registry) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestID := strings.TrimSpace(request.Header.Get("X-Request-ID"))
		if requestID == "" || len(requestID) > 128 {
			requestID = newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		started := time.Now()
		wrapped := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(wrapped, request)
		route := request.Pattern
		if route == "" {
			route = "unmatched"
		}
		status := wrapped.status
		if status == 0 {
			status = 200
		}
		key := request.Method + "\x00" + route + "\x00" + strconv.Itoa(status)
		r.mu.Lock()
		metric := r.http[key]
		metric.Count++
		metric.Duration += time.Since(started).Seconds()
		r.http[key] = metric
		r.mu.Unlock()
	})
}

func (r *Registry) Handler(w http.ResponseWriter, request *http.Request) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.metricsToken != "" {
		provided := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(r.metricsToken)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintln(w, "# HELP fccp_http_requests_total Total HTTP requests.")
	fmt.Fprintln(w, "# TYPE fccp_http_requests_total counter")
	keys := make([]string, 0, len(r.http))
	for key := range r.http {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts := strings.Split(key, "\x00")
		metric := r.http[key]
		labels := fmt.Sprintf("method=%q,route=%q,status=%q", parts[0], parts[1], parts[2])
		fmt.Fprintf(w, "fccp_http_requests_total{%s} %d\n", labels, metric.Count)
		fmt.Fprintf(w, "fccp_http_request_duration_seconds_sum{%s} %f\n", labels, metric.Duration)
		fmt.Fprintf(w, "fccp_http_request_duration_seconds_count{%s} %d\n", labels, metric.Count)
	}
	fmt.Fprintln(w, "# HELP fccp_screening_worker_runs_total Screening worker polling runs.")
	fmt.Fprintln(w, "# TYPE fccp_screening_worker_runs_total counter")
	fmt.Fprintf(w, "fccp_screening_worker_runs_total %d\n", r.workerRuns)
	fmt.Fprintln(w, "# HELP fccp_screening_worker_errors_total Screening worker runs with failures.")
	fmt.Fprintln(w, "# TYPE fccp_screening_worker_errors_total counter")
	fmt.Fprintf(w, "fccp_screening_worker_errors_total %d\n", r.workerErrors)
	fmt.Fprintln(w, "# HELP fccp_screening_jobs_completed_total Successfully completed recurring screening jobs.")
	fmt.Fprintln(w, "# TYPE fccp_screening_jobs_completed_total counter")
	fmt.Fprintf(w, "fccp_screening_jobs_completed_total %d\n", r.jobsCompleted)
	fmt.Fprintln(w, "# HELP fccp_notification_delivery_runs_total Notification delivery worker runs.")
	fmt.Fprintln(w, "# TYPE fccp_notification_delivery_runs_total counter")
	fmt.Fprintf(w, "fccp_notification_delivery_runs_total %d\n", r.deliveryRuns)
	fmt.Fprintln(w, "# HELP fccp_notification_delivery_errors_total Notification delivery worker failures.")
	fmt.Fprintln(w, "# TYPE fccp_notification_delivery_errors_total counter")
	fmt.Fprintf(w, "fccp_notification_delivery_errors_total %d\n", r.deliveryErrors)
	fmt.Fprintln(w, "# HELP fccp_notification_deliveries_completed_total Successfully delivered webhook notifications.")
	fmt.Fprintln(w, "# TYPE fccp_notification_deliveries_completed_total counter")
	fmt.Fprintf(w, "fccp_notification_deliveries_completed_total %d\n", r.deliveriesCompleted)
	fmt.Fprintln(w, "# HELP fccp_notification_outbox_pending Pending notification outbox messages.")
	fmt.Fprintln(w, "# TYPE fccp_notification_outbox_pending gauge")
	fmt.Fprintf(w, "fccp_notification_outbox_pending %d\n", r.outboxPending)
}

func newRequestID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(value)
}
