package handler

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weilai1949/s3clinet/server/internal/store"
)

func newTestHandler(t *testing.T, cors []string, token string) http.Handler {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(st, logger, t.TempDir(), cors, token, "test").Routes()
}

func doJSON(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// listBucketXML 构造 ListObjectsV2 响应。
func listBucketXML(keys []string, truncated bool, token string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
	sb.WriteString("<IsTruncated>")
	if truncated {
		sb.WriteString("true")
	} else {
		sb.WriteString("false")
	}
	sb.WriteString("</IsTruncated>")
	if token != "" {
		sb.WriteString("<NextContinuationToken>" + token + "</NextContinuationToken>")
	}
	for _, k := range keys {
		sb.WriteString("<Contents><Key>" + k + "</Key><Size>1</Size><ETag>\"e\"</ETag><LastModified>2026-01-01T00:00:00.000Z</LastModified></Contents>")
	}
	sb.WriteString("</ListBucketResult>")
	return sb.String()
}

// lifecycleXML 构造 LifecycleConfiguration 响应。
func lifecycleXML(rules ...string) string {
	return `<?xml version="1.0"?><LifecycleConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">` +
		strings.Join(rules, "") + `</LifecycleConfiguration>`
}

func lifecycleRuleXML(id, prefix string, days int) string {
	return `<Rule><ID>` + id + `</ID><Prefix>` + prefix + `</Prefix><Status>Enabled</Status>` +
		`<Expiration><Days>` + fmt.Sprint(days) + `</Days></Expiration></Rule>`
}

// TestCORSRejectsForeignOrigin 补测：非白名单 Origin 的普通请求被 403 拒绝（不再放行）。
func TestCORSRejectsForeignOrigin(t *testing.T) {
	h := newTestHandler(t, []string{"http://myapp.example"}, "")

	// 白名单外 Origin → 403（无论方法）
	for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		req := httptest.NewRequest(m, "/api/accounts", strings.NewReader(`{}`))
		req.Header.Set("Origin", "http://evil.example")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("%s with evil origin = %d, want 403", m, rr.Code)
		}
	}
	// 白名单内 Origin → 放行（200/400 由业务逻辑决定，而非 403）
	req := httptest.NewRequest("GET", "/api/accounts", nil)
	req.Header.Set("Origin", "http://myapp.example")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code == http.StatusForbidden {
		t.Fatalf("allowed origin got 403")
	}
	// 无 Origin（同源/非浏览器）→ 放行
	if rr2 := doJSON(t, h, "GET", "/api/health", ""); rr2.Code != http.StatusOK {
		t.Fatalf("health without origin = %d, want 200", rr2.Code)
	}
}

// TestReadJSONRequiresJSONContentType 补测：非 application/json 请求体 → 400。
func TestReadJSONRequiresJSONContentType(t *testing.T) {
	h := newTestHandler(t, nil, "")
	req := httptest.NewRequest("POST", "/api/accounts", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Content-Type", "text/plain")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("text/plain body = %d, want 400", rr.Code)
	}
}

