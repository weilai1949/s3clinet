package handler

// ol_headers_test.go —— headers.go 设置对象头补测（仅新增，不改生产代码）。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOlSetHeaders 设置对象头：成功 / 404 / 非法 JSON / 缺桶 / S3 失败。
func TestOlSetHeaders(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		if r.Header.Get("x-amz-copy-source") != "" {
			return olPlain(http.StatusOK) // CopyObject(REPLACE) 成功
		}
		return olResp{}
	})
	env := accNewEnv(t, srv.URL, "b")
	id := env.acc.ID

	// 成功
	rr := env.accDoRec("POST", "/api/accounts/"+id+"/set-headers",
		`{"bucket":"b","key":"k","contentType":"text/plain","metadata":{"owner":"alice"}}`)
	olExpectStatus(t, rr, http.StatusOK, "set-headers ok")
	if !strings.Contains(rr.Body.String(), `"updated":"k"`) {
		t.Fatalf("set-headers body: %s", rr.Body.String())
	}

	// 账号不存在 → 404
	rr = env.accDoRec("POST", "/api/accounts/nope/set-headers", `{"key":"k"}`)
	olExpectStatus(t, rr, http.StatusNotFound, "set-headers 404")

	// 非法 JSON → 400
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/set-headers", "{bad")
	olExpectStatus(t, rr, http.StatusBadRequest, "set-headers bad json")

	// 缺 key → 400
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/set-headers", `{"bucket":"b"}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "set-headers no key")

	// 缺桶（账号无默认桶）→ 400
	nb := accNewEnv(t, srv.URL, "")
	rr = nb.accDoRec("POST", "/api/accounts/"+nb.acc.ID+"/set-headers", `{"key":"k"}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "set-headers no bucket")

	// S3 复制失败 → 403
	failSrv := olFake(t, func(r *http.Request) olResp {
		if r.Header.Get("x-amz-copy-source") != "" {
			return olErr(http.StatusForbidden, "AccessDenied")
		}
		return olResp{}
	})
	fe := accNewEnv(t, failSrv.URL, "b")
	rr = fe.accDoRec("POST", "/api/accounts/"+fe.acc.ID+"/set-headers", `{"key":"k"}`)
	olExpectStatus(t, rr, http.StatusForbidden, "set-headers s3 err")

	// 传输层错误 → 500（白盒直调，取消的上下文）
	req := olCancelReq(t, "POST", "/x", `{"key":"k"}`)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	env.hnd.setHeaders(w, req)
	olExpectStatus(t, w, http.StatusInternalServerError, "set-headers ctx canceled")
}
