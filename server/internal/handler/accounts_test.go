package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weilai1949/s3clinet/server/internal/model"
	"github.com/weilai1949/s3clinet/server/internal/store"
)

func TestHeathAPI(t *testing.T) {
	h := newTestHandler(t, nil, "")
	rr := doJSON(t, h, "GET", "/api/health", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("health status = %d", rr.Code)
	}
	var m map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatalf("health body: %v", err)
	}
	if m["status"] != "ok" {
		t.Fatalf("health status field = %v", m["status"])
	}
}

func TestHealthStoreUnavailable(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(&failPingStore{}, logger, t.TempDir(), nil, "", "test").Routes()
	rr := doJSON(t, h, "GET", "/api/health", "")
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("health status = %d, want 503", rr.Code)
	}
	var m map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatalf("health body: %v", err)
	}
	if m["status"] != "error" {
		t.Fatalf("status = %v, want error (no degraded)", m["status"])
	}
}

// failPingStore 仅用于健康检查：Ping 失败，其余方法不调用。
type failPingStore struct{}

func (failPingStore) List() []*model.Account                        { return nil }
func (failPingStore) Get(string) (*model.Account, error)            { return nil, store.ErrNotFound }
func (failPingStore) Create(*model.Account) (*model.Account, error) { return nil, nil }
func (failPingStore) Update(string, *model.Account) (*model.Account, error) {
	return nil, store.ErrNotFound
}
func (failPingStore) Delete(string) error { return store.ErrNotFound }
func (failPingStore) Close() error        { return nil }
func (failPingStore) Ping() error         { return os.ErrInvalid }

func TestAccountCRUD(t *testing.T) {
	h := newTestHandler(t, nil, "")
	body := `{"name":"minio","endpoint":"http://localhost:9000","region":"us-east-1","accessKey":"ak","secretKey":"sk","bucket":"b","pathStyle":true}`
	rr := doJSON(t, h, "POST", "/api/accounts", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var a map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &a)
	if a["secretKey"] != "******" {
		t.Fatalf("expected masked secret, got %v", a["secretKey"])
	}
	// 必填校验
	bad := doJSON(t, h, "POST", "/api/accounts", `{"name":"x"}`)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid create, got %d", bad.Code)
	}
}

func TestAuthEnforced(t *testing.T) {
	h := newTestHandler(t, nil, "s3cret")
	// 未带 token -> 401
	rr := doJSON(t, h, "GET", "/api/accounts", "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rr.Code)
	}
	// 带 token -> 200
	req := httptest.NewRequest("GET", "/api/accounts", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req)
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected 200 with token, got %d", rr2.Code)
	}
	// 错误 token（含长度不同的）-> 401
	for _, bad := range []string{"Bearer wrong", "Bearer " + strings.Repeat("x", 100), ""} {
		req := httptest.NewRequest("GET", "/api/accounts", nil)
		if bad != "" {
			req.Header.Set("Authorization", bad)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for bad auth %q, got %d", bad, rr.Code)
		}
	}
	// 健康检查无需鉴权
	if rr := doJSON(t, h, "GET", "/api/health", ""); rr.Code != http.StatusOK {
		t.Fatalf("health should bypass auth, got %d", rr.Code)
	}
}

func TestAuthMultiToken(t *testing.T) {
	h := newTestHandler(t, nil, "tokA, tokB")
	for _, tok := range []string{"tokA", "tokB"} {
		req := httptest.NewRequest("GET", "/api/accounts", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("token %s: status=%d", tok, rr.Code)
		}
	}
	req := httptest.NewRequest("GET", "/api/accounts", nil)
	req.Header.Set("Authorization", "Bearer tokC")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token should 401, got %d", rr.Code)
	}
}

func TestSecureCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"abc", "abc", true},
		{"abc", "abd", false},
		{"abc", "abcd", false}, // 长度不同也返回 false
		{"", "", true},
		{"Bearer tok", "Bearer tok", true},
	}
	for _, c := range cases {
		if got := secureCompare(c.a, c.b); got != c.want {
			t.Errorf("secureCompare(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestHealthReportsVersion(t *testing.T) {
	h := newTestHandler(t, nil, "")
	rr := doJSON(t, h, "GET", "/api/health", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("health status = %d", rr.Code)
	}
	var m map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatalf("health body: %v", err)
	}
	if m["version"] != "test" {
		t.Fatalf("version = %v, want test", m["version"])
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := newTestHandler(t, nil, "")
	rr := doJSON(t, h, "GET", "/api/health", "")
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := rr.Header().Get(k); got != want {
			t.Errorf("API header %s = %q, want %q", k, got, want)
		}
	}

	// 静态资源（SPA fallback）同样携带安全头
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>spa</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h2 := New(st, logger, dir, nil, "", "test").Routes()
	req := httptest.NewRequest("GET", "/some/route", nil)
	rr2 := httptest.NewRecorder()
	h2.ServeHTTP(rr2, req)
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := rr2.Header().Get(k); got != want {
			t.Errorf("static header %s = %q, want %q", k, got, want)
		}
	}
}

func TestSPAFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>spa</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, dir, nil, "", "test").Routes()

	// 未命中的页面路由（无扩展名）→ 回退 index.html
	req := httptest.NewRequest("GET", "/some/route", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "spa") {
		t.Fatalf("SPA fallback expected index.html, got %d %q", rr.Code, rr.Body.String())
	}

	// 未命中的静态资源（带扩展名）→ 404，不能把 HTML 当 JS 返回
	req2 := httptest.NewRequest("GET", "/assets/missing.js", nil)
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusNotFound {
		t.Fatalf("missing asset should 404, got %d", rr2.Code)
	}

	// 未知 /api 路径 → 404，不走 SPA
	if rr3 := doJSON(t, h, "GET", "/api/unknown", ""); rr3.Code != http.StatusNotFound {
		t.Fatalf("unknown /api path should 404, got %d", rr3.Code)
	}
}

func TestAuthOnlyForAPI(t *testing.T) {
	h := newTestHandler(t, nil, "s3cret")
	// 非 /api 前缀的路径不受鉴权影响（静态目录为空会 404，但绝不应 401）
	req := httptest.NewRequest("GET", "/apiary", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code == http.StatusUnauthorized {
		t.Fatal("non-/api path should not require auth")
	}
}

func TestReadJSONRejectsTrailingData(t *testing.T) {
	h := newTestHandler(t, nil, "")
	body := `{"name":"x","endpoint":"e","accessKey":"a","secretKey":"s"} {"extra":1}`
	if rr := doJSON(t, h, "POST", "/api/accounts", body); rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for trailing data, got %d", rr.Code)
	}
}

func TestCORSPolicy(t *testing.T) {
	h := newTestHandler(t, []string{"http://myapp.example"}, "")
	// 白名单内
	req := httptest.NewRequest("OPTIONS", "/api/accounts", nil)
	req.Header.Set("Origin", "http://myapp.example")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("preflight allowed origin should 204, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "http://myapp.example" {
		t.Fatalf("expected ACAO=%q, got %q", "http://myapp.example", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	// 白名单外
	req2 := httptest.NewRequest("OPTIONS", "/api/accounts", nil)
	req2.Header.Set("Origin", "http://evil.example")
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusForbidden {
		t.Fatalf("preflight disallowed origin should 403, got %d", rr2.Code)
	}
}

// TestAccountRUD 补测：账号 GET/PUT/DELETE 与连通性测试（store + handler 全链路）。
func TestAccountRUD(t *testing.T) {
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer s3fake.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	// GET /api/accounts/{id}：返回脱敏密钥
	rr := doJSON(t, h, "GET", "/api/accounts/"+acc.ID, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rr.Code, rr.Body.String())
	}
	var a model.Account
	if err := json.Unmarshal(rr.Body.Bytes(), &a); err != nil {
		t.Fatalf("get body: %v", err)
	}
	if a.SecretKey != "******" {
		t.Fatalf("secret=%q, want masked", a.SecretKey)
	}

	// PUT：更新名称
	rr2 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID,
		`{"name":"renamed","endpoint":"`+s3fake.URL+`","region":"us-east-1","accessKey":"ak","secretKey":"new-sk"}`)
	if rr2.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", rr2.Code, rr2.Body.String())
	}
	var up model.Account
	_ = json.Unmarshal(rr2.Body.Bytes(), &up)
	if up.Name != "renamed" || up.SecretKey != "******" {
		t.Fatalf("updated=%+v", up)
	}

	// 连通性测试（带 bucket 参数）
	rr3 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/test?bucket=b", "")
	if rr3.Code != http.StatusOK {
		t.Fatalf("test status=%d body=%s", rr3.Code, rr3.Body.String())
	}
	var tc struct {
		OK     bool   `json:"ok"`
		Bucket string `json:"bucket"`
	}
	_ = json.Unmarshal(rr3.Body.Bytes(), &tc)
	if !tc.OK || tc.Bucket != "b" {
		t.Fatalf("test resp=%+v", tc)
	}

	// DELETE
	rr4 := doJSON(t, h, "DELETE", "/api/accounts/"+acc.ID, "")
	if rr4.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rr4.Code, rr4.Body.String())
	}
	// 删除后 GET → 404
	if rr5 := doJSON(t, h, "GET", "/api/accounts/"+acc.ID, ""); rr5.Code != http.StatusNotFound {
		t.Fatalf("get after delete=%d, want 404", rr5.Code)
	}
}

// TestPreviewBuckets 补测：用表单凭证临时列出桶（preview-buckets，不落库）。
func TestPreviewBuckets(t *testing.T) {
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && !r.URL.Query().Has("list-type") {
			io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>o</ID></Owner><Buckets><Bucket><Name>pb1</Name><CreationDate>2026-01-01T00:00:00.000Z</CreationDate></Bucket></Buckets></ListAllMyBucketsResult>`)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer s3fake.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	body := `{"name":"tmp","endpoint":"` + s3fake.URL + `","region":"us-east-1","accessKey":"ak","secretKey":"sk","pathStyle":true}`
	rr := doJSON(t, h, "POST", "/api/accounts/preview-buckets", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Buckets []struct {
			Name string `json:"name"`
		} `json:"buckets"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body: %v", err)
	}
	if len(resp.Buckets) != 1 || resp.Buckets[0].Name != "pb1" {
		t.Fatalf("buckets = %+v", resp.Buckets)
	}
	// 缺字段 → 400（不落库）
	if rr2 := doJSON(t, h, "POST", "/api/accounts/preview-buckets", `{"name":"x"}`); rr2.Code != http.StatusBadRequest {
		t.Fatalf("preview missing fields = %d, want 400", rr2.Code)
	}
}
