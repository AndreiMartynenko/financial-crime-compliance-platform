package security

import (
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"
)

type bucket struct {
	tokens  float64
	updated time.Time
}
type RateLimiter struct {
	mu          sync.Mutex
	clients     map[string]bucket
	rate, burst float64
	now         func() time.Time
	lastPrune   time.Time
}

func NewRateLimiter(rate float64, burst int) *RateLimiter {
	return &RateLimiter{clients: map[string]bucket{}, rate: rate, burst: float64(burst), now: time.Now}
}
func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if !l.allow(host) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "rate_limited", "message": "Request rate limit exceeded"}})
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (l *RateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if l.lastPrune.IsZero() || now.Sub(l.lastPrune) > time.Minute {
		for id, item := range l.clients {
			if now.Sub(item.updated) > 10*time.Minute {
				delete(l.clients, id)
			}
		}
		l.lastPrune = now
	}
	item, ok := l.clients[key]
	if !ok {
		item = bucket{tokens: l.burst, updated: now}
	}
	item.tokens += now.Sub(item.updated).Seconds() * l.rate
	if item.tokens > l.burst {
		item.tokens = l.burst
	}
	item.updated = now
	if item.tokens < 1 {
		l.clients[key] = item
		return false
	}
	item.tokens--
	l.clients[key] = item
	return true
}

func Headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		next.ServeHTTP(w, r)
	})
}
