package handler

// ol_multipart_test.go —— multipart.go 各阶段失败/校验补测（仅新增，不改生产代码）。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOlMultipart404NoBucket 分段上传接口：404 / 缺桶。
func TestOlMultipart404NoBucket(t *testing.T) {
	paths := func(id string) []struct{ name, method, path, body string } {
		return []struct{ name, method, path, body string }{
			{"init", "POST", "/api/accounts/" + id + "/multipart/init", `{"key":"k"}`},
			{"part", "POST", "/api/accounts/" + id + "/multipart/part", `{"key":"k","uploadId":"u","partNumber":1}`},
			{"complete", "POST", "/api/accounts/" + id + "/multipart/complete", `{"key":"k","uploadId":"u","parts":[{"partNumber":1,"etag":"e"}]}`},
			{"abort", "POST", "/api/accounts/" + id + "/multipart/abort", `{"key":"k","uploadId":"u"}`},
		}
	}
	// 404：账号不存在
	env := accNewEnv(t, "http://127.0.0.1:1", "b")
	for _, c := range paths("nope") {
		rr := env.accDoRec(c.method, c.path, c.body)
		olExpectStatus(t, rr, http.StatusNotFound, c.name)
	}
	// 缺桶
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	nb := accNewEnv(t, srv.URL, "")
	for _, c := range paths(nb.acc.ID) {
		rr := nb.accDoRec(c.method, c.path, c.body)
		olExpectStatus(t, rr, http.StatusBadRequest, c.name)
	}
}

// TestOlMultipartBadJSON 请求体非法 → 400。
func TestOlMultipartBadJSON(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env := accNewEnv(t, srv.URL, "b")
	id := env.acc.ID
	cases := []struct{ name, method, path string }{
		{"init", "POST", "/api/accounts/" + id + "/multipart/init"},
		{"part", "POST", "/api/accounts/" + id + "/multipart/part"},
		{"complete", "POST", "/api/accounts/" + id + "/multipart/complete"},
		{"abort", "POST", "/api/accounts/" + id + "/multipart/abort"},
	}
	for _, c := range cases {
		rr := env.accDoRec(c.method, c.path, "{bad")
		olExpectStatus(t, rr, http.StatusBadRequest, c.name)
	}
}

// TestOlMultipartValidation 各阶段必填字段与分段校验。
func TestOlMultipartValidation(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env := accNewEnv(t, srv.URL, "b")
	id := env.acc.ID

	// init 缺 key → 400
	rr := env.accDoRec("POST", "/api/accounts/"+id+"/multipart/init", `{"bucket":"b"}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "init no key")
	// part 缺 uploadId → 400
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/multipart/part", `{"key":"k"}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "part no uploadId")
	// complete 缺 parts → 400
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/multipart/complete", `{"key":"k","uploadId":"u"}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "complete no parts")
	// complete 分段缺 etag → 400
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/multipart/complete", `{"key":"k","uploadId":"u","parts":[{"partNumber":0,"etag":"e"}]}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "complete bad part")
	// complete 重复段号 → 400
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/multipart/complete",
		`{"key":"k","uploadId":"u","parts":[{"partNumber":1,"etag":"e"},{"partNumber":1,"etag":"f"}]}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "complete dup part")
	// abort 缺 uploadId → 400
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/multipart/abort", `{"key":"k"}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "abort no uploadId")
}

// TestOlMultipartS3Errors 各阶段 S3 调用失败 → 内部错误映射；part 过期时间钳制。
func TestOlMultipartS3Errors(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		q := r.URL.Query()
		switch {
		case q.Has("uploads"):
			return olErr(http.StatusForbidden, "AccessDenied") // init 失败
		case q.Has("uploadId"):
			return olErr(http.StatusForbidden, "AccessDenied") // complete / abort 失败
		}
		return olResp{}
	})
	env := accNewEnv(t, srv.URL, "b")
	id := env.acc.ID

	// init 失败 → 403
	rr := env.accDoRec("POST", "/api/accounts/"+id+"/multipart/init", `{"key":"k"}`)
	olExpectStatus(t, rr, http.StatusForbidden, "init err")

	// complete 失败 → 403
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/multipart/complete",
		`{"key":"k","uploadId":"u","parts":[{"partNumber":1,"etag":"e"}]}`)
	olExpectStatus(t, rr, http.StatusForbidden, "complete err")

	// abort 失败 → 403
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/multipart/abort", `{"key":"k","uploadId":"u"}`)
	olExpectStatus(t, rr, http.StatusForbidden, "abort err")
}

// TestOlMultipartPartClamp part 预签名成功且过期时间钳制到 24h。
func TestOlMultipartPartClamp(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env := accNewEnv(t, srv.URL, "b")
	rr := env.accDoRec("POST", "/api/accounts/"+env.acc.ID+"/multipart/part",
		`{"key":"k","uploadId":"u","partNumber":1,"expiresIn":100000}`)
	olExpectStatus(t, rr, http.StatusOK, "part clamp")
	var m map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &m)
	if m["expiresIn"] != float64(86400) {
		t.Fatalf("expiresIn = %v, want 86400", m["expiresIn"])
	}
	if u, _ := m["url"].(string); u == "" {
		t.Fatal("missing presigned url")
	}
}

// TestOlMultipartInitCtxErr init 在上下文取消时返回内部错误（白盒）。
func TestOlMultipartInitCtxErr(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env := accNewEnv(t, srv.URL, "b")
	req := olCancelReq(t, "POST", "/x", `{"key":"k"}`)
	req.SetPathValue("id", env.acc.ID)
	w := httptest.NewRecorder()
	env.hnd.multipartInit(w, req)
	olExpectStatus(t, w, http.StatusInternalServerError, "init ctx canceled")
}
