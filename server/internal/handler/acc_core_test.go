package handler

// acc_core_test.go —— handler.go / middleware.go / routes.go / metrics.go 的补测。
// 覆盖：写响应辅助的错误分支、鉴权/CORS/安全头中间件分支、SPA 静态文件命中、
// expvar 指标闭包、Shutdown 等。

import (
	"errors"
	"expvar"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weilai1949/s3clinet/server/internal/model"
)

// TestAccWriteInternalErrBranches 白盒直测 writeInternalErr 的全部映射分支：
// 普通 500 / S3 已映射且用户消息为空（回退 publicMsg）/ 用户消息与 publicMsg 均为空 / AccessDenied→403。
func TestAccWriteInternalErrBranches(t *testing.T) {
	h := accNewHandler(t, &accStubStore{}, nil, "")
	cases := []struct {
		name     string
		err      error
		public   string
		wantCode int
		wantMsg  string
	}{
		{"plain error with public msg", errors.New("boom"), "failed to load accounts", 500, "failed to load accounts"},
		{"plain error without public msg", errors.New("boom"), "", 500, "internal server error"},
		// 注：UserMessage 对非 nil 错误恒返回非空（未知码→"storage operation failed"），
		// mapped 路径的 publicMsg 回退不可达——断言以实际映射结果为准。
		{"s3 mapped default user message", accCodeErr{code: "InvalidRange"}, "bucket operation failed", 416, "storage operation failed"},
		{"s3 mapped default with empty public", accCodeErr{code: "InvalidRange"}, "", 416, "storage operation failed"},
		{"access denied", accCodeErr{code: "AccessDenied"}, "any", 403, "access denied"},
		{"nil error 500", nil, "ops", 500, "ops"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			h.writeInternalErr(rr, c.err, c.public)
			if rr.Code != c.wantCode {
				t.Fatalf("status=%d want=%d body=%s", rr.Code, c.wantCode, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), c.wantMsg) {
				t.Fatalf("body=%s want contains %q", rr.Body.String(), c.wantMsg)
			}
		})
	}
}

// TestAccWriteJSONEncodeError 白盒：编码失败（不支持的类型）走 log.Error 分支。
func TestAccWriteJSONEncodeError(t *testing.T) {
	h := accNewHandler(t, &accStubStore{}, nil, "")
	rr := httptest.NewRecorder()
	h.writeJSON(rr, http.StatusOK, map[string]any{"bad": make(chan int)})
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200（header 先写，编码失败仅记日志）", rr.Code)
	}
	if rr.Body.String() != "" {
		t.Fatalf("编码失败不应写出 body, got %q", rr.Body.String())
	}
}

// TestAccSplitTokens 补测 token 串解析：空串 / 多值 / 空白与空段剔除。
func TestAccSplitTokens(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"a", []string{"a"}},
		{"a, b ,,c ", []string{"a", "b", "c"}},
		{",,", nil},
	}
	for _, c := range cases {
		got := splitTokens(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("splitTokens(%q)=%v want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("splitTokens(%q)=%v want %v", c.in, got, c.want)
			}
		}
	}
}

// TestAccDerefHelpers 补测 derefString/derefInt64/timeOrZero/boolOrFalse 的空/非空两分支。
func TestAccDerefHelpers(t *testing.T) {
	s := "v"
	if derefString(nil) != "" || derefString(&s) != "v" {
		t.Fatal("derefString 分支错误")
	}
	i := int64(7)
	if derefInt64(nil) != 0 || derefInt64(&i) != 7 {
		t.Fatal("derefInt64 分支错误")
	}
	now := time.Unix(1700000000, 0)
	if !timeOrZero(nil).IsZero() || !timeOrZero(&now).Equal(now) {
		t.Fatal("timeOrZero 分支错误")
	}
	b := true
	if boolOrFalse(nil) || !boolOrFalse(&b) {
		t.Fatal("boolOrFalse 分支错误")
	}
}

// TestAccShutdown 补测：有任务注册表时 Stop 被调；零值 Handler（nil 注册表）安全返回。
func TestAccShutdown(t *testing.T) {
	h := accNewHandler(t, &accStubStore{}, nil, "")
	h.Shutdown()                                   // migrateJobs != nil 分支
	(&Handler{log: accDiscardLogger()}).Shutdown() // nil 分支不 panic
}

// TestAccAccountClientAndBucketOr 白盒经路由黑盒请求覆盖 accountClient 预检与 bucketOr 兜底：
// 账号不存在→404；账号配置非法（缺 SecretKey）→400；无默认桶且未传 bucket→400。
func TestAccAccountClientAndBucketOr(t *testing.T) {
	srv := accStartFake(t, accBucketsFake(newAccFailInjector()))
	env := accNewEnv(t, srv.URL, "b")

	// 账号不存在 → accountClient 404 分支
	if rr := env.accDoRec("GET", "/api/accounts/acc-nope/buckets", ""); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown account=%d, want 404 body=%s", rr.Code, rr.Body.String())
	}

	// 账号缺 SecretKey → clients.get 失败 → 400 invalid account configuration
	//（必须建在 env 的 store：handler 只查自己持有的存储）
	bad, err := env.st.Create(&model.Account{Endpoint: srv.URL, AccessKey: "ak", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	if rr := env.accDoRec("GET", "/api/accounts/"+bad.ID+"/buckets", ""); rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid account=%d, want 400 body=%s", rr.Code, rr.Body.String())
	}

	// 无默认桶且未传 bucket → bucketOr 400（env.acc 有默认桶 b，需用无默认桶账号）
	nobucket, err := env.st.Create(&model.Account{Endpoint: srv.URL, AccessKey: "ak", SecretKey: "sk", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	if rr := env.accDoRec("GET", "/api/accounts/"+nobucket.ID+"/bucket-info", ""); rr.Code != http.StatusBadRequest {
		t.Fatalf("bucket required=%d, want 400 body=%s", rr.Code, rr.Body.String())
	}
}

// TestAccStatusRecorderDoubleWrite 白盒：statusRecorder 第二次 WriteHeader 被忽略。
func TestAccStatusRecorderDoubleWrite(t *testing.T) {
	rr := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: rr, status: 200}
	rec.WriteHeader(http.StatusCreated)
	rec.WriteHeader(http.StatusBadGateway) // 应被忽略
	if rec.status != http.StatusCreated || rr.Code != http.StatusCreated {
		t.Fatalf("status=%d underlying=%d, want 201/201", rec.status, rr.Code)
	}
	// Flush/Unwrap 透传
	if _, ok := rec.ResponseWriter.(interface{ Flush() }); ok {
		rec.Flush()
	}
	if rec.Unwrap() != rr {
		t.Fatal("Unwrap 应返回底层 ResponseWriter")
	}
}

// TestAccWithAuthOptionsBypass 白盒：配置 token 后 OPTIONS 预检绕过鉴权（交由 CORS 层）。
func TestAccWithAuthOptionsBypass(t *testing.T) {
	h := accNewHandler(t, &accStubStore{}, nil, "tok")
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	wrapped := h.withAuth(next)

	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, httptest.NewRequest(http.MethodOptions, "/api/accounts", nil))
	if !called {
		t.Fatal("OPTIONS 应绕过鉴权直达 next")
	}

	// 非 /api 路径同样绕过（静态资源）
	called = false
	wrapped.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/index.html", nil))
	if !called {
		t.Fatal("非 /api 路径应绕过鉴权")
	}

	// /api/health 与 /api/metrics 免鉴权
	for _, p := range []string{"/api/health", "/api/metrics"} {
		called = false
		wrapped.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, p, nil))
		if !called {
			t.Fatalf("%s 应免鉴权", p)
		}
	}

	// /api 根路径也要鉴权（无 token → 401）
	called = false
	rr = httptest.NewRecorder()
	wrapped.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api", nil))
	if called || rr.Code != http.StatusUnauthorized {
		t.Fatalf("/api 无 token 应 401, got %d called=%v", rr.Code, called)
	}
}

// TestAccCORSWildcardOrigin 白名单为 * 时：任意 Origin 放行且 ACAO=*（不带 Vary）。
func TestAccCORSWildcardOrigin(t *testing.T) {
	h := accNewHandler(t, &accStubStore{}, []string{"*"}, "")
	req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	req.Header.Set("Origin", "http://random.site")
	rr := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("wildcard origin=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("ACAO=%q want *", got)
	}
	if got := rr.Header().Get("Vary"); got != "" {
		t.Fatalf("wildcard 不应设置 Vary, got %q", got)
	}
}

// TestAccCORSTrustedDefaultOrigin 安全默认：localhost/127.0.0.1/tauri 协议免白名单放行。
func TestAccCORSTrustedDefaultOrigin(t *testing.T) {
	h := accNewHandler(t, &accStubStore{}, nil, "")
	for _, origin := range []string{"http://localhost:5173", "https://127.0.0.1:3000", "tauri://localhost"} {
		req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
		req.Header.Set("Origin", origin)
		rr := httptest.NewRecorder()
		h.Routes().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("origin %s=%d body=%s", origin, rr.Code, rr.Body.String())
		}
		if got := rr.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Fatalf("origin %s: ACAO=%q", origin, got)
		}
	}
	// 预检（OPTIONS）也放行 204
	req := httptest.NewRequest(http.MethodOptions, "/api/accounts", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rr := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("trusted preflight=%d, want 204", rr.Code)
	}
}

// TestAccIsTrustedDefaultOrigin 表驱动覆盖 isTrustedDefaultOrigin 各分支。
func TestAccIsTrustedDefaultOrigin(t *testing.T) {
	cases := []struct {
		origin string
		want   bool
	}{
		{"http://localhost:3000", true},
		{"https://tauri.localhost", true},
		{"tauri://localhost", true},    // 自定义协议
		{"ftp://localhost", false},     // 非 http(s) scheme
		{"http://[::1", false},         // URL 解析失败
		{"http://evil.example", false}, // 非可信主机
		{"", false},                    // 空 scheme
	}
	for _, c := range cases {
		if got := isTrustedDefaultOrigin(c.origin); got != c.want {
			t.Errorf("isTrustedDefaultOrigin(%q)=%v want %v", c.origin, got, c.want)
		}
	}
}

// TestAccSPAServesExistingFile 补测 routes.spaHandler：命中真实静态文件时直接由 FileServer 服务。
func TestAccSPAServesExistingFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("static-ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := accNewHandler(t, &accStubStore{}, nil, "")
	h.staticDir = dir
	req := httptest.NewRequest(http.MethodGet, "/hello.txt", nil)
	rr := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "static-ok") {
		t.Fatalf("static file=%d body=%s", rr.Code, rr.Body.String())
	}
	// 精确 /api 路径 → 404（不走 SPA 回退）
	rr2 := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, "/api", nil))
	if rr2.Code != http.StatusNotFound {
		t.Fatalf("/api=%d, want 404", rr2.Code)
	}
}

// TestAccMetricsExpvar 补测 metrics.go 的 expvar 闭包（读取触发 Func 求值）。
func TestAccMetricsExpvar(t *testing.T) {
	total := expvar.Get("s3c_http_requests_total")
	if total == nil {
		t.Fatal("s3c_http_requests_total 未发布")
	}
	if s := total.String(); s == "" {
		t.Fatal("total String() 为空")
	}
	up := expvar.Get("s3c_uptime_seconds")
	if up == nil {
		t.Fatal("s3c_uptime_seconds 未发布")
	}
	_ = up.String() // 触发 uptime 闭包
}

// TestAccLoggingRequestIDPassthrough 补测 withLogging：优先透传请求自带的 X-Request-ID。
func TestAccLoggingRequestIDPassthrough(t *testing.T) {
	h := accNewHandler(t, &accStubStore{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("X-Request-ID", "req-fixed-123")
	rr := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr, req)
	if got := rr.Header().Get("X-Request-ID"); got != "req-fixed-123" {
		t.Fatalf("X-Request-ID=%q, want 透传值", got)
	}
}
