package handler

// ol_objects_test.go —— objects.go 错误分支/边界补测（仅新增，不改生产代码）。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestOlObjectsHandlers404 各对象接口对不存在账号统一返回 404。
func TestOlObjectsHandlers404(t *testing.T) {
	env := accNewEnv(t, "http://127.0.0.1:1", "b")
	cases := []struct{ name, method, path, body string }{
		{"list", "GET", "/api/accounts/nope/objects?bucket=b", ""},
		{"head", "GET", "/api/accounts/nope/head?bucket=b&key=k", ""},
		{"storage-class", "POST", "/api/accounts/nope/storage-class", `{"key":"k","storageClass":"STANDARD"}`},
		{"mkdir", "POST", "/api/accounts/nope/mkdir", `{"key":"d"}`},
		{"rename", "POST", "/api/accounts/nope/rename", `{"key":"a","newKey":"b"}`},
		{"delete", "POST", "/api/accounts/nope/delete", `{"keys":["a"]}`},
		{"delete-prefix", "POST", "/api/accounts/nope/delete-prefix", `{"prefix":"p/"}`},
		{"delete-prefix-async", "POST", "/api/accounts/nope/delete-prefix/async", `{"prefix":"p/"}`},
		{"presign", "POST", "/api/accounts/nope/presign", `{"key":"k"}`},
	}
	for _, c := range cases {
		rr := env.accDoRec(c.method, c.path, c.body)
		olExpectStatus(t, rr, http.StatusNotFound, c.name)
	}
}

// TestOlObjectsHandlersBadJSON 各对象接口请求体非法 → 400。
func TestOlObjectsHandlersBadJSON(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env := accNewEnv(t, srv.URL, "b")
	cases := []struct{ name, method, path string }{
		{"storage-class", "POST", "/api/accounts/" + env.acc.ID + "/storage-class"},
		{"mkdir", "POST", "/api/accounts/" + env.acc.ID + "/mkdir"},
		{"rename", "POST", "/api/accounts/" + env.acc.ID + "/rename"},
		{"delete", "POST", "/api/accounts/" + env.acc.ID + "/delete"},
		{"delete-prefix", "POST", "/api/accounts/" + env.acc.ID + "/delete-prefix"},
		{"delete-prefix-async", "POST", "/api/accounts/" + env.acc.ID + "/delete-prefix/async"},
		{"presign", "POST", "/api/accounts/" + env.acc.ID + "/presign"},
	}
	for _, c := range cases {
		rr := env.accDoRec(c.method, c.path, "{bad json")
		olExpectStatus(t, rr, http.StatusBadRequest, c.name)
	}
}

// TestOlObjectsHandlersNoBucket 账号无默认桶且请求未带桶 → 400。
func TestOlObjectsHandlersNoBucket(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env := accNewEnv(t, srv.URL, "")
	id := env.acc.ID
	cases := []struct{ name, method, path, body string }{
		{"list", "GET", "/api/accounts/" + id + "/objects", ""},
		{"head", "GET", "/api/accounts/" + id + "/head?key=k", ""},
		{"storage-class", "POST", "/api/accounts/" + id + "/storage-class", `{"key":"k","storageClass":"STANDARD"}`},
		{"mkdir", "POST", "/api/accounts/" + id + "/mkdir", `{"key":"d"}`},
		{"rename", "POST", "/api/accounts/" + id + "/rename", `{"key":"a","newKey":"b"}`},
		{"delete", "POST", "/api/accounts/" + id + "/delete", `{"keys":["a"]}`},
		{"delete-prefix", "POST", "/api/accounts/" + id + "/delete-prefix", `{"prefix":"p/"}`},
		{"delete-prefix-async", "POST", "/api/accounts/" + id + "/delete-prefix/async", `{"prefix":"p/"}`},
		{"presign", "POST", "/api/accounts/" + id + "/presign", `{"key":"k"}`},
	}
	for _, c := range cases {
		rr := env.accDoRec(c.method, c.path, c.body)
		olExpectStatus(t, rr, http.StatusBadRequest, c.name)
	}
}

// TestOlObjectsHandlersFieldErrors 关键字段校验：rename 缺 newKey、deletePrefix 缺 prefix → 400。
func TestOlObjectsHandlersFieldErrors(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env := accNewEnv(t, srv.URL, "b")
	id := env.acc.ID
	cases := []struct{ name, method, path, body string }{
		{"rename no newKey", "POST", "/api/accounts/" + id + "/rename", `{"key":"a"}`},
		{"delete-prefix no prefix", "POST", "/api/accounts/" + id + "/delete-prefix", `{"prefix":""}`},
		{"delete-prefix-async no prefix", "POST", "/api/accounts/" + id + "/delete-prefix/async", `{"prefix":""}`},
		{"presign no key", "POST", "/api/accounts/" + id + "/presign", `{"method":"get"}`},
	}
	for _, c := range cases {
		rr := env.accDoRec(c.method, c.path, c.body)
		olExpectStatus(t, rr, http.StatusBadRequest, c.name)
	}
}

// TestOlObjectsS3Errors 假 S3 注入错误：列表/详情/存储类型/建目录/改名/删除/前缀删除。
func TestOlObjectsS3Errors(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodGet && q.Has("list-type"):
			if q.Get("prefix") == "err" {
				return olErr(http.StatusForbidden, "AccessDenied")
			}
			return olXML(http.StatusOK, listBucketXML([]string{"ok1", "ok2"}, false, ""))
		case r.Method == http.MethodHead:
			return olErr(http.StatusForbidden, "AccessDenied") // HEAD 无响应体，SDK 按状态码生成通用错误
		case r.Method == http.MethodPut && r.Header.Get("x-amz-copy-source") == "":
			return olErr(http.StatusForbidden, "AccessDenied") // PutObject（建目录）失败
		case r.Method == http.MethodPut && r.Header.Get("x-amz-copy-source") != "":
			if r.Header.Get("x-amz-storage-class") != "" {
				return olErr(http.StatusBadRequest, "InvalidStorageClass") // 切换存储类型被拒
			}
			return olPlain(http.StatusOK) // rename 先复制成功
		case r.Method == http.MethodDelete:
			return olErr(http.StatusForbidden, "AccessDenied")
		case r.Method == http.MethodPost && q.Has("delete"):
			return olErr(http.StatusForbidden, "AccessDenied")
		}
		return olResp{}
	})
	env := accNewEnv(t, srv.URL, "b")
	id := env.acc.ID

	// 列表失败 → 内部错误映射（403）
	rr := env.accDoRec("GET", "/api/accounts/"+id+"/objects?bucket=b&prefix=err&maxKeys=2000", "")
	olExpectStatus(t, rr, http.StatusForbidden, "list err")

	// head 失败（非 404）→ 500 通用内部错误
	rr = env.accDoRec("GET", "/api/accounts/"+id+"/head?bucket=b&key=k", "")
	olExpectStatus(t, rr, http.StatusInternalServerError, "head err")

	// 切换存储类型被 S3 拒绝 → 400
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/storage-class", `{"key":"k","storageClass":"GLACIER"}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "storage-class err")

	// 非法存储类型 → 400
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/storage-class", `{"key":"k","storageClass":"NOPE"}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "storage-class invalid")

	// 建目录失败 → 403
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/mkdir", `{"key":"d"}`)
	olExpectStatus(t, rr, http.StatusForbidden, "mkdir err")

	// 改名：复制失败 → 403
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/rename", `{"key":"a","newKey":"b"}`)
	olExpectStatus(t, rr, http.StatusForbidden, "rename copy err")

	// 改名：复制成功但删除源失败 → 403（AccessDenied 映射）
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/rename", `{"key":"okd","newKey":"okd2"}`)
	olExpectStatus(t, rr, http.StatusForbidden, "rename delete err")

	// 批量删除失败 → 403
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/delete", `{"keys":["x"]}`)
	olExpectStatus(t, rr, http.StatusForbidden, "delete err")

	// delete-keys 为空 → 400
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/delete", `{"keys":[]}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "delete empty keys")

	// 前缀删除：列举失败 → 403
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/delete-prefix", `{"prefix":"err"}`)
	olExpectStatus(t, rr, http.StatusForbidden, "delete-prefix list err")

	// 前缀删除：批量删除失败 → 403
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/delete-prefix", `{"prefix":"derr"}`)
	olExpectStatus(t, rr, http.StatusForbidden, "delete-prefix delete err")

	// 异步前缀删除：列举失败 → 403
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/delete-prefix/async", `{"prefix":"err"}`)
	olExpectStatus(t, rr, http.StatusForbidden, "delete-prefix-async list err")

	// 列表成功（maxKeys 超限被钳制到 1000）
	rr = env.accDoRec("GET", "/api/accounts/"+id+"/objects?bucket=b&prefix=ok&maxKeys=2000", "")
	olExpectStatus(t, rr, http.StatusOK, "list ok")
	var resp struct {
		Objects []map[string]any `json:"objects"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Objects) != 2 {
		t.Fatalf("list objects = %d, want 2", len(resp.Objects))
	}
}

// TestOlStorageClassUnmappedErr 传输层错误（取消的上下文）→ 500 通用映射分支。
func TestOlStorageClassUnmappedErr(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env := accNewEnv(t, srv.URL, "b")
	req := olCancelReq(t, "POST", "/x", `{"key":"k","storageClass":"STANDARD"}`)
	req.SetPathValue("id", env.acc.ID)
	w := httptest.NewRecorder()
	env.hnd.changeStorageClass(w, req)
	olExpectStatus(t, w, http.StatusInternalServerError, "storage-class ctx canceled")
}

// TestOlDeletePrefixAsyncSuccess 异步前缀删除成功：完成时 migrated=2。
func TestOlDeletePrefixAsyncSuccess(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodGet && q.Has("list-type"):
			return olXML(http.StatusOK, listBucketXML([]string{"p/1", "p/2"}, false, ""))
		case r.Method == http.MethodPost && q.Has("delete"):
			return olXML(http.StatusOK, `<?xml version="1.0"?><DeleteResult/>`)
		}
		return olResp{}
	})
	env := accNewEnv(t, srv.URL, "b")
	rr := env.accDoRec("POST", "/api/accounts/"+env.acc.ID+"/delete-prefix/async", `{"prefix":"p/"}`)
	olExpectStatus(t, rr, http.StatusAccepted, "async start")
	var start map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &start)
	jobID, _ := start["jobId"].(string)
	if jobID == "" {
		t.Fatalf("missing jobId: %s", rr.Body.String())
	}
	done := olWaitJobDone(t, env, jobID)
	res, _ := done["result"].(map[string]any)
	if int(res["migrated"].(float64)) != 2 {
		t.Fatalf("migrated = %v, want 2", res["migrated"])
	}
}

// TestOlDeletePrefixAsyncCancel 异步前缀删除被取消：删除阻塞 → cancel → 状态 cancelled 且记录失败 key。
func TestOlDeletePrefixAsyncCancel(t *testing.T) {
	release := make(chan struct{})
	seenDelete := make(chan struct{}, 1)
	srv := olFake(t, func(r *http.Request) olResp {
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodGet && q.Has("list-type"):
			return olXML(http.StatusOK, listBucketXML(olKeys(1001), true, "t"))
		case r.Method == http.MethodPost && q.Has("delete"):
			seenDelete <- struct{}{}
			<-release // 阻塞直到测试放行
			return olXML(http.StatusOK, `<?xml version="1.0"?><DeleteResult/>`)
		}
		return olResp{}
	})
	env := accNewEnv(t, srv.URL, "b")
	rr := env.accDoRec("POST", "/api/accounts/"+env.acc.ID+"/delete-prefix/async", `{"prefix":"p/"}`)
	olExpectStatus(t, rr, http.StatusAccepted, "async start")
	var start map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &start)
	jobID, _ := start["jobId"].(string)
	select {
	case <-seenDelete:
	case <-time.After(5 * time.Second):
		t.Fatal("delete never reached fake S3")
	}
	env.accDoRec("POST", "/api/migrate/jobs/"+jobID+"/cancel", "")
	close(release)
	done := olWaitJobDone(t, env, jobID)
	prog, _ := done["progress"].(map[string]any)
	if prog["status"] != "cancelled" {
		t.Fatalf("progress status = %v, want cancelled", prog["status"])
	}
	res, _ := done["result"].(map[string]any)
	if int(res["failed"].(float64)) < 1 {
		t.Fatalf("cancelled result = %v, want failed >= 1", res)
	}
	if keys, _ := res["failedKeys"].([]any); len(keys) < 1 {
		t.Fatalf("failedKeys missing: %v", res)
	}
}

// TestOlPresignModes 预签名各方法成功/默认值/钳制/非法方法。
func TestOlPresignModes(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env := accNewEnv(t, srv.URL, "b")
	id := env.acc.ID

	cases := []struct {
		name    string
		body    string
		want    int
		method  string
		expires float64
	}{
		{"default put", `{"key":"k"}`, http.StatusOK, "put", 3600},
		{"get", `{"key":"k","method":"get"}`, http.StatusOK, "get", 3600},
		{"post", `{"key":"k","method":"post"}`, http.StatusOK, "post", 3600},
		{"put clamp", `{"key":"k","method":"put","expiresIn":100000}`, http.StatusOK, "put", 86400},
	}
	for _, c := range cases {
		rr := env.accDoRec("POST", "/api/accounts/"+id+"/presign", c.body)
		olExpectStatus(t, rr, c.want, c.name)
		var m map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &m)
		if m["method"] != c.method {
			t.Fatalf("%s method = %v, want %s", c.name, m["method"], c.method)
		}
		if m["expiresIn"] != c.expires {
			t.Fatalf("%s expiresIn = %v, want %v", c.name, m["expiresIn"], c.expires)
		}
		if m["url"] == nil && m["fields"] == nil {
			t.Fatalf("%s missing url/fields", c.name)
		}
	}

	// 非法方法 → 400
	rr := env.accDoRec("POST", "/api/accounts/"+id+"/presign", `{"key":"k","method":"bogus"}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "invalid method")
	// 缺 key → 400
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/presign", `{}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "no key")
}

// TestOlRunDeletePrefixTruncate 白盒验证 runDeletePrefix 达到上限即截断（跨页 guard）。
func TestOlRunDeletePrefixTruncateExact(t *testing.T) {
	// 100 页 ×1000 key，每页声明 truncated → 第 101 轮触发上限 guard
	pages := make([]string, 0, 100)
	for i := 0; i < 100; i++ {
		pages = append(pages, listBucketXML(olKeys(1000), true, "t"))
	}
	srv := olListPagesFake(t, pages)
	env := accNewEnv(t, srv.URL, "b")
	client := olClient(t, env)
	deleted, truncated, err := runDeletePrefix(t.Context(), client, "b", "p/")
	if err != nil {
		t.Fatalf("runDeletePrefix: %v", err)
	}
	if deleted != 100_000 || !truncated {
		t.Fatalf("deleted=%d truncated=%v, want 100000 true", deleted, truncated)
	}
}

// TestOlRunDeletePrefixCrossLimit 白盒验证单页跨越上限时裁剪 keys 并截断。
func TestOlRunDeletePrefixCrossLimit(t *testing.T) {
	// 99 页 ×1000 + 1 页 ×1500 → 最后一页裁剪到 1000 并截断
	pages := make([]string, 0, 100)
	for i := 0; i < 99; i++ {
		pages = append(pages, listBucketXML(olKeys(1000), true, "t"))
	}
	pages = append(pages, listBucketXML(olKeys(1500), false, ""))
	srv := olListPagesFake(t, pages)
	env := accNewEnv(t, srv.URL, "b")
	client := olClient(t, env)
	deleted, truncated, err := runDeletePrefix(t.Context(), client, "b", "p/")
	if err != nil {
		t.Fatalf("runDeletePrefix: %v", err)
	}
	if deleted != 100_000 || !truncated {
		t.Fatalf("deleted=%d truncated=%v, want 100000 true", deleted, truncated)
	}
}

// olListPagesFake 假 S3：按调用次序返回预置 list 页（越界 → 错误）；delete 一律成功。
func olListPagesFake(t *testing.T, pages []string) *httptest.Server {
	var mu sync.Mutex
	calls := 0
	return olFake(t, func(r *http.Request) olResp {
		switch {
		case r.Method == http.MethodGet && r.URL.Query().Has("list-type"):
			mu.Lock()
			i := calls
			calls++
			mu.Unlock()
			if i < len(pages) {
				return olXML(http.StatusOK, pages[i])
			}
			return olErr(http.StatusForbidden, "AccessDenied")
		case r.Method == http.MethodPost && r.URL.Query().Has("delete"):
			return olXML(http.StatusOK, `<?xml version="1.0"?><DeleteResult/>`)
		}
		return olResp{}
	})
}

// olListPagesFake 假 S3：按调用次序返回预置 list 页（越界 → 错误）；delete 一律成功。
