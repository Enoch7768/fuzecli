package api

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type requestLimitBucket struct {
	window time.Time
	count  int
}

type requestLimiter struct {
	mu      sync.Mutex
	buckets map[string]requestLimitBucket
}

func newRequestLimiter() *requestLimiter {
	return &requestLimiter{buckets: make(map[string]requestLimitBucket)}
}

func (l *requestLimiter) allow(key string, limit int, window time.Duration) bool {
	if limit <= 0 {
		return false
	}
	now := time.Now()
	start := now.Truncate(window)
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket := l.buckets[key]
	if bucket.window != start {
		bucket = requestLimitBucket{window: start}
	}
	if bucket.count >= limit {
		l.buckets[key] = bucket
		return false
	}
	bucket.count++
	l.buckets[key] = bucket
	if len(l.buckets) > 4096 {
		for k, value := range l.buckets {
			if value.window.Before(start) {
				delete(l.buckets, k)
			}
		}
	}
	return true
}

func requestClientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.Trim(strings.TrimSpace(r.RemoteAddr), "[]")
	}
	if host == "" {
		host = "unknown"
	}
	return host
}

func requestLimitForPath(path string, method string) (int, time.Duration) {
	if method == http.MethodPost && path == "/v1/chat" {
		return 12, time.Minute
	}
	if method == http.MethodPost && path == "/v1/upload" {
		return 20, time.Minute
	}
	if method == http.MethodPost && path == "/v1/file" {
		return 60, time.Minute
	}
	if path == "/v1/models" {
		return 30, time.Minute
	}
	return 120, time.Minute
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.limiter == nil {
			s.limiter = newRequestLimiter()
		}
		limit, window := requestLimitForPath(r.URL.Path, r.Method)
		key := requestClientKey(r) + "|" + r.URL.Path + "|" + r.Method
		if !s.limiter.allow(key, limit, window) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "request rate limit exceeded; retry later")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withChatSlot(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case s.chatSlots <- struct{}{}:
			defer func() { <-s.chatSlots }()
			next.ServeHTTP(w, r)
		default:
			w.Header().Set("Retry-After", "5")
			writeError(w, http.StatusServiceUnavailable, "too many concurrent chat requests")
		}
	})
}

func requestID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte("fallback-request-id"))
	}
	return hex.EncodeToString(b)
}

func requestContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", requestID())
		next.ServeHTTP(w, r)
	})
}
