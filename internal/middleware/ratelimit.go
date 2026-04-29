package middleware

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"insighta-backend/internal/models"
)

type bucket struct {
	tokens   float64
	lastSeen time.Time
	mu       sync.Mutex
}

var (
	buckets   = make(map[string]*bucket)
	bucketsMu sync.Mutex
)

func getBucket(key string) *bucket {
	bucketsMu.Lock()
	defer bucketsMu.Unlock()
	b, ok := buckets[key]
	if !ok {
		b = &bucket{tokens: 0, lastSeen: time.Now()}
		buckets[key] = b
	}
	return b
}

// refill adds tokens proportional to elapsed time.
func (b *bucket) refill(ratePerMin float64) {
	now := time.Now()
	elapsed := now.Sub(b.lastSeen).Seconds()
	b.lastSeen = now
	b.tokens += elapsed * (ratePerMin / 60.0)
	if b.tokens > ratePerMin {
		b.tokens = ratePerMin
	}
}

// RateLimit returns a middleware that allows maxPerMin requests per minute
// keyed by the provided keyFn (e.g. IP or user ID).
func RateLimit(maxPerMin float64, keyFn func(r *http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFn(r)
			b := getBucket(key)
			b.mu.Lock()
			b.refill(maxPerMin)
			if b.tokens < 1 {
				b.mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(models.APIResponse{Status: "error", Message: "rate limit exceeded"})
				return
			}
			b.tokens--
			b.mu.Unlock()
			next.ServeHTTP(w, r)
		})
	}
}

// IPKey extracts the client IP for rate-limiting unauthenticated routes.
func IPKey(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}

// UserKey uses the user ID from context if available, falls back to IP.
func UserKey(r *http.Request) string {
	if u, ok := r.Context().Value(models.ContextKeyUser).(*models.User); ok && u != nil {
		return u.ID
	}
	return IPKey(r)
}
