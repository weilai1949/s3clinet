package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// withSecurityHeaders 为所有响应设置基础安全头（含静态资源与 API）。
func (h *Handler) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// 纵深防御：脚本仅允许同源（杜绝注入脚本执行）；内联样式给 Vue 用；
		// connect 放行任意 http(s) 以支持多服务端后端地址（Tauri/远程）。
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; frame-src 'self' blob:; connect-src 'self' http: https:; object-src 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

// withLogging 记录每个请求的方法、路径、耗时与 request id；并累计 metrics。
func (h *Handler) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", reqID)
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		recordHTTPMetric(rec.status)
		attrs := []any{"method", r.Method, "path", r.URL.Path, "status", rec.status, "dur", time.Since(start).String(), "req", reqID}
		if r.URL.Path == "/api/health" || r.URL.Path == "/api/metrics" {
			h.log.Debug("http", attrs...)
			return
		}
		h.log.Info("http", attrs...)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

// WriteHeader 仅记录并透传第一次调用；后续多余调用忽略，
// 避免日志状态与实际响应状态不一致（Go 服务端本身也会忽略多余 WriteHeader）。
func (r *statusRecorder) WriteHeader(code int) {
	if r.written {
		return
	}
	r.written = true
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap 让 http.ResponseController / http.NewResponseController 穿透到真实连接，
// 否则 SetWriteDeadline 恒返回 ErrNotSupported 并被静默忽略。
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// corsAllowedOrigin 依据配置与安全默认值返回允许的 Origin。
// 返回 "" 表示不允许跨域（同源请求无需 CORS 头）。
func (h *Handler) corsAllowedOrigin(origin string) string {
	if origin == "" {
		return "" // 同源或非浏览器请求
	}
	// 显式白名单（S3C_CORS_ORIGINS）
	for _, o := range h.corsOrigins {
		if o == "*" {
			return "*"
		}
		if o == origin {
			return o
		}
	}
	// 安全默认：仅放行 localhost / 127.0.0.1 / tauri 自定义协议
	if isTrustedDefaultOrigin(origin) {
		return origin
	}
	return ""
}

func isTrustedDefaultOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "tauri" {
		return true
	}
	if scheme != "http" && scheme != "https" {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1", "tauri.localhost":
		return true
	}
	return false
}

func (h *Handler) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := h.corsAllowedOrigin(origin)
		if allowed != "" {
			w.Header().Set("Access-Control-Allow-Origin", allowed)
			if allowed != "*" {
				w.Header().Set("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			if allowed == "" {
				w.WriteHeader(http.StatusForbidden)
			} else {
				w.WriteHeader(http.StatusNoContent)
			}
			return
		}
		// 安全关键：跨域请求（带 Origin 且不在白名单）必须拒绝。
		// 仅给响应加 CORS 头无法阻止浏览器发送"简单请求"（text/plain 等无预检）
		// 执行状态变更接口，这里直接 403 阻断。
		if origin != "" && allowed == "" {
			w.Header().Set("Vary", "Origin")
			h.writeErr(w, http.StatusForbidden, "origin not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withMetricsGate 在未开启 S3C_EXPOSE_METRICS 时，对 /api/metrics 返回 404，
// 让外部看不出该端点是否存在（避免公网被 scrape 运行指标）。
func (h *Handler) withMetricsGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.exposeMetrics && r.URL.Path == "/api/metrics" {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withAuth 在配置了 S3C_TOKEN 时，对 /api/* 强制 Bearer 鉴权。
// 支持逗号分隔多 token（轮换）；跳过 OPTIONS 预检与 /api/health。
func (h *Handler) withAuth(next http.Handler) http.Handler {
	if len(h.tokens) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		p := r.URL.Path
		if p != "/api" && !strings.HasPrefix(p, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if p == "/api/health" || p == "/api/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		for _, t := range h.tokens {
			if secureCompare(auth, "Bearer "+t) {
				next.ServeHTTP(w, r)
				return
			}
		}
		h.writeErr(w, http.StatusUnauthorized, "unauthorized")
	})
}

// secureCompare 以常量时间比较两个字符串。
func secureCompare(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}
