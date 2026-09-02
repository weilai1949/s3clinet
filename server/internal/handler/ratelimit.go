package handler

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 简易令牌桶限速：每 IP 默认 120 req/min，突发 30。
// 仅保护写密集型 API 被刷；health 与静态资源不计入。
const (
	rateLimitPerMin = 120
	rateLimitBurst  = 30
)

type ipLimiter struct {
	mu   sync.Mutex
	byIP map[string]*tokenBucket
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

func newIPLimiter() *ipLimiter {
	return &ipLimiter{byIP: make(map[string]*tokenBucket)}
}

func (l *ipLimiter) allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.byIP[ip]
	if !ok {
		l.byIP[ip] = &tokenBucket{tokens: rateLimitBurst - 1, last: now}
		// 偶尔清理，避免 map 无限增长
		if len(l.byIP) > 10_000 {
			l.reapLocked(now)
		}
		return true
	}
	elapsed := now.Sub(b.last).Seconds()
	b.last = now
	b.tokens += elapsed * (float64(rateLimitPerMin) / 60.0)
	if b.tokens > rateLimitBurst {
		b.tokens = rateLimitBurst
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *ipLimiter) reapLocked(now time.Time) {
	for ip, b := range l.byIP {
		if now.Sub(b.last) > 10*time.Minute {
			delete(l.byIP, ip)
		}
	}
}

func clientIP(r *http.Request) string {
	// 仅信任直连 RemoteAddr（本服务通常置于 nginx 后；XFF 可伪造，生产应在边缘限速）。
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (h *Handler) withRateLimit(next http.Handler) http.Handler {
	if h.limiter == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/api/health" || p == "/api/metrics" || !strings.HasPrefix(p, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		ip := clientIP(r)
		if !h.limiter.allow(ip) {
			w.Header().Set("Retry-After", "5")
			h.writeErr(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}
