package handler

// ol_proxy_test.go —— proxy.go 错误映射/内容类型/Range 补测（仅新增，不改生产代码）。

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOlProxy404AndValidation 代理接口：404 / 缺桶 / 非法 mode / 缺 key。
func TestOlProxy404AndValidation(t *testing.T) {
	// 账号不存在 → 404
	env := accNewEnv(t, "http://127.0.0.1:1", "b")
	rr := env.accDoRec("GET", "/api/accounts/nope/proxy?bucket=b&key=k", "")
	olExpectStatus(t, rr, http.StatusNotFound, "proxy 404")

	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	// 无默认桶 → 400
	nb := accNewEnv(t, srv.URL, "")
	rr = nb.accDoRec("GET", "/api/accounts/"+nb.acc.ID+"/proxy?key=k", "")
	olExpectStatus(t, rr, http.StatusBadRequest, "proxy no bucket")

	// 缺 key → 400
	e := accNewEnv(t, srv.URL, "b")
	rr = e.accDoRec("GET", "/api/accounts/"+e.acc.ID+"/proxy?bucket=b", "")
	olExpectStatus(t, rr, http.StatusBadRequest, "proxy no key")
	// 非法 mode → 400
	rr = e.accDoRec("GET", "/api/accounts/"+e.acc.ID+"/proxy?bucket=b&key=k&mode=bogus", "")
	olExpectStatus(t, rr, http.StatusBadRequest, "proxy bad mode")
}

// TestOlProxyTextErr text 模式拉取失败 → proxyErr 映射。
func TestOlProxyTextErr(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		return olErr(http.StatusForbidden, "AccessDenied")
	})
	env := accNewEnv(t, srv.URL, "b")
	rr := env.accDoRec("GET", "/api/accounts/"+env.acc.ID+"/proxy?bucket=b&key=k&mode=text", "")
	olExpectStatus(t, rr, http.StatusForbidden, "text err")
}

// TestOlProxyErrMap 代理错误映射：404 / 416。
func TestOlProxyErrMap(t *testing.T) {
	// NoSuchKey → 404
	srv := olFake(t, func(r *http.Request) olResp { return olErr(http.StatusNotFound, "NoSuchKey") })
	env := accNewEnv(t, srv.URL, "b")
	rr := env.accDoRec("GET", "/api/accounts/"+env.acc.ID+"/proxy?bucket=b&key=k&mode=download", "")
	olExpectStatus(t, rr, http.StatusNotFound, "proxy 404")

	// InvalidRange → 416
	srv2 := olFake(t, func(r *http.Request) olResp { return olErr(http.StatusForbidden, "InvalidRange") })
	env2 := accNewEnv(t, srv2.URL, "b")
	rr = env2.accDoRec("GET", "/api/accounts/"+env2.acc.ID+"/proxy?bucket=b&key=k&mode=inline", "")
	olExpectStatus(t, rr, http.StatusRequestedRangeNotSatisfiable, "proxy 416")
}

// TestOlProxyInlineNoContentType inline 模式缺 Content-Type → 回退 octet-stream。
func TestOlProxyInlineNoContentType(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env := accNewEnv(t, srv.URL, "b")
	rr := env.accDoRec("GET", "/api/accounts/"+env.acc.ID+"/proxy?bucket=b&key=k&mode=inline", "")
	olExpectStatus(t, rr, http.StatusOK, "inline no ct")
	if ct := rr.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want octet-stream", ct)
	}
	if disp := rr.Header().Get("Content-Disposition"); disp == "" {
		t.Fatal("Content-Disposition missing")
	}
}

// TestOlProxyRange Range 请求 → 206 + Content-Range。
func TestOlProxyRange(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		if r.Header.Get("Range") == "" {
			return olPlain(http.StatusOK)
		}
		return olResp{
			status: http.StatusPartialContent,
			headers: map[string]string{
				"Content-Type":   "text/plain",
				"Content-Range":  "bytes 0-2/9",
				"Content-Length": "3",
			},
			body: "abc",
		}
	})
	env := accNewEnv(t, srv.URL, "b")
	req := httptest.NewRequest("GET", "/api/accounts/"+env.acc.ID+"/proxy?bucket=b&key=k&mode=download", nil)
	req.Header.Set("Range", "bytes=0-2")
	rr := httptest.NewRecorder()
	env.h.ServeHTTP(rr, req)
	olExpectStatus(t, rr, http.StatusPartialContent, "range")
	if cr := rr.Header().Get("Content-Range"); cr != "bytes 0-2/9" {
		t.Fatalf("Content-Range = %q", cr)
	}
}

// TestOlInlineSafeCT 白盒表驱动：inline 安全内容类型白名单。
func TestOlInlineSafeCT(t *testing.T) {
	cases := []struct {
		ct   string
		want bool
	}{
		{"image/png", true},
		{"image/svg+xml", false},
		{"IMAGE/PNG; charset=x", true},
		{" audio/mpeg ", true},
		{"video/mp4", true},
		{"application/pdf", true},
		{"text/plain", true},
		{"text/html", false},
		{"application/octet-stream", false},
		{"", false},
	}
	for _, c := range cases {
		if got := inlineSafeCT(c.ct); got != c.want {
			t.Fatalf("inlineSafeCT(%q) = %v, want %v", c.ct, got, c.want)
		}
	}
}
