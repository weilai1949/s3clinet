package handler

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weilai1949/s3clinet/server/internal/store"
)

func TestMetricsEndpoint(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()
	// 触发一次请求以累计计数
	rr0 := httptest.NewRecorder()
	h.ServeHTTP(rr0, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/metrics", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{"s3c_http_requests_total", "s3c_uptime_seconds", "s3c_build_info"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in %s", want, body)
		}
	}
	if rr.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing X-Request-ID")
	}
}
