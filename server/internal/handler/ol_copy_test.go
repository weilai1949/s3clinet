package handler

// ol_copy_test.go —— copy.go 校验/失败聚合/异步取消补测（仅新增，不改生产代码）。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestOlCopyValidation 复制类接口：404 / 非法 JSON / 缺桶。
func TestOlCopyValidation(t *testing.T) {
	paths := func(id string) []struct{ name, method, path, body string } {
		return []struct{ name, method, path, body string }{
			{"copy-object", "POST", "/api/accounts/" + id + "/copy-object", `{"key":"a","newKey":"b"}`},
			{"copy-objects", "POST", "/api/accounts/" + id + "/copy-objects", `{"keys":["a"]}`},
			{"copy-objects-async", "POST", "/api/accounts/" + id + "/copy-objects/async", `{"keys":["a"]}`},
			{"copy-prefix", "POST", "/api/accounts/" + id + "/copy-prefix", `{"prefix":"p/","targetPrefix":"q/"}`},
			{"copy-prefix-async", "POST", "/api/accounts/" + id + "/copy-prefix/async", `{"prefix":"p/","targetPrefix":"q/"}`},
		}
	}
	// 404：账号不存在
	env := accNewEnv(t, "http://127.0.0.1:1", "b")
	for _, c := range paths("nope") {
		rr := env.accDoRec(c.method, c.path, c.body)
		olExpectStatus(t, rr, http.StatusNotFound, c.name)
	}
	// 非法 JSON
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env = accNewEnv(t, srv.URL, "b")
	for _, c := range paths(env.acc.ID) {
		rr := env.accDoRec(c.method, c.path, "{bad")
		olExpectStatus(t, rr, http.StatusBadRequest, c.name)
	}
	// 缺桶
	nb := accNewEnv(t, srv.URL, "")
	for _, c := range paths(nb.acc.ID) {
		rr := nb.accDoRec(c.method, c.path, c.body)
		olExpectStatus(t, rr, http.StatusBadRequest, c.name)
	}
}

// TestOlCopyObjectErr 复制失败 → 403。
func TestOlCopyObjectErr(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		if r.Header.Get("x-amz-copy-source") != "" {
			return olErr(http.StatusForbidden, "AccessDenied")
		}
		return olResp{}
	})
	env := accNewEnv(t, srv.URL, "b")
	rr := env.accDoRec("POST", "/api/accounts/"+env.acc.ID+"/copy-object", `{"key":"a","newKey":"b"}`)
	olExpectStatus(t, rr, http.StatusForbidden, "copy err")
}

// TestOlCopyManyBounds copyMany 超上限 → 400；空 key 被跳过。
func TestOlCopyManyBounds(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		if r.Header.Get("x-amz-copy-source") != "" {
			return olPlain(http.StatusOK)
		}
		return olResp{}
	})
	env := accNewEnv(t, srv.URL, "b")
	id := env.acc.ID

	// 超过 10000 keys → 400
	keys := make([]string, 10001)
	for i := range keys {
		keys[i] = fmt.Sprintf("k%d", i)
	}
	b, _ := json.Marshal(map[string]any{"bucket": "b", "keys": keys})
	rr := env.accDoRec("POST", "/api/accounts/"+id+"/copy-objects", string(b))
	olExpectStatus(t, rr, http.StatusBadRequest, "too many keys")

	// 空 key 跳过：["", "a.txt"] → 只复制 1 个
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/copy-objects", `{"keys":["","a.txt"]}`)
	olExpectStatus(t, rr, http.StatusOK, "skip empty key")
	if !strings.Contains(rr.Body.String(), `"copied":1`) {
		t.Fatalf("skip empty body: %s", rr.Body.String())
	}
}

// TestOlCopyManyMoveFailureAggregation 移动（copy+delete）失败聚合：lastError + failedKeys。
func TestOlCopyManyMoveFailureAggregation(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		src := r.Header.Get("x-amz-copy-source") // 形如 b%2Fkey
		if src != "" {
			if strings.Contains(src, "del-a") {
				return olErr(http.StatusForbidden, "AccessDenied") // 复制失败
			}
			return olPlain(http.StatusOK) // 其余复制成功
		}
		if strings.Contains(r.URL.Path, "del-b") {
			return olErr(http.StatusForbidden, "AccessDenied") // 复制后删源失败
		}
		return olPlain(http.StatusNoContent) // 删除成功
	})
	env := accNewEnv(t, srv.URL, "b")
	body := `{"bucket":"b","keys":["del-a","del-b","del-c"],"deleteSource":true,"targetPrefix":"mv/"}`
	rr := env.accDoRec("POST", "/api/accounts/"+env.acc.ID+"/copy-objects", body)
	olExpectStatus(t, rr, http.StatusOK, "move aggregate")
	var m map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &m)
	if int(m["failed"].(float64)) != 2 || int(m["copied"].(float64)) != 1 {
		t.Fatalf("result = %v", m)
	}
	if m["lastError"] == "" {
		t.Fatalf("lastError missing: %v", m)
	}
	fk, _ := m["failedKeys"].([]any)
	if len(fk) != 2 {
		t.Fatalf("failedKeys = %v", fk)
	}
}

// TestOlCopyManyAsyncMove 异步移动：成功完成与中途取消。
func TestOlCopyManyAsyncMove(t *testing.T) {
	// 成功：复制+删除全部成功
	srv := olFake(t, func(r *http.Request) olResp {
		if r.Header.Get("x-amz-copy-source") != "" {
			return olPlain(http.StatusOK)
		}
		return olPlain(http.StatusNoContent)
	})
	env := accNewEnv(t, srv.URL, "b")
	rr := env.accDoRec("POST", "/api/accounts/"+env.acc.ID+"/copy-objects/async",
		`{"keys":["a.txt"],"deleteSource":true,"targetPrefix":"mv/"}`)
	olExpectStatus(t, rr, http.StatusAccepted, "async move start")
	var start map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &start)
	done := olWaitJobDone(t, env, start["jobId"].(string))
	res, _ := done["result"].(map[string]any)
	if int(res["migrated"].(float64)) != 1 {
		t.Fatalf("migrated = %v", res["migrated"])
	}

	// 取消：删除被阻塞 → cancel → 状态 cancelled
	release := make(chan struct{})
	seen := make(chan struct{}, 1)
	srv2 := olFake(t, func(r *http.Request) olResp {
		if r.Header.Get("x-amz-copy-source") != "" {
			return olPlain(http.StatusOK)
		}
		seen <- struct{}{}
		<-release
		return olPlain(http.StatusNoContent)
	})
	env2 := accNewEnv(t, srv2.URL, "b")
	rr = env2.accDoRec("POST", "/api/accounts/"+env2.acc.ID+"/copy-objects/async",
		`{"keys":["a.txt"],"deleteSource":true}`)
	olExpectStatus(t, rr, http.StatusAccepted, "async move cancel start")
	start = nil
	_ = json.Unmarshal(rr.Body.Bytes(), &start)
	select {
	case <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("delete never reached fake S3")
	}
	env2.accDoRec("POST", "/api/migrate/jobs/"+start["jobId"].(string)+"/cancel", "")
	close(release)
	done = olWaitJobDone(t, env2, start["jobId"].(string))
	prog, _ := done["progress"].(map[string]any)
	if prog["status"] != "cancelled" {
		t.Fatalf("progress status = %v, want cancelled", prog["status"])
	}
}

// TestOlCopyPrefixValidation copy-prefix 前后端校验：空前缀 / 同桶前缀重叠 / 列举失败。
func TestOlCopyPrefixValidation(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		if r.URL.Query().Has("list-type") {
			return olErr(http.StatusForbidden, "AccessDenied")
		}
		if r.Header.Get("x-amz-copy-source") != "" {
			return olPlain(http.StatusOK)
		}
		return olResp{}
	})
	env := accNewEnv(t, srv.URL, "b")
	id := env.acc.ID

	// 空前缀 → 400
	rr := env.accDoRec("POST", "/api/accounts/"+id+"/copy-prefix", `{"bucket":"b"}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "no prefix")
	// 同桶前缀重叠 → 400
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/copy-prefix", `{"bucket":"b","prefix":"a/","targetPrefix":"a/sub"}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "overlap")
	// 列举失败 → 403
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/copy-prefix", `{"bucket":"b","prefix":"p/","targetPrefix":"q/"}`)
	olExpectStatus(t, rr, http.StatusForbidden, "list err")

	// 异步：空前缀 / 重叠 / 列举失败
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/copy-prefix/async", `{"bucket":"b"}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "async no prefix")
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/copy-prefix/async", `{"bucket":"b","prefix":"a/","targetPrefix":"a/sub"}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "async overlap")
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/copy-prefix/async", `{"bucket":"b","prefix":"p/","targetPrefix":"q/"}`)
	olExpectStatus(t, rr, http.StatusForbidden, "async list err")
}

// TestOlCopyPrefixAsyncSuccess 异步前缀复制成功。
func TestOlCopyPrefixAsyncSuccess(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		if r.URL.Query().Has("list-type") {
			return olXML(http.StatusOK, listBucketXML([]string{"p/1", "p/2"}, false, ""))
		}
		if r.Header.Get("x-amz-copy-source") != "" {
			return olPlain(http.StatusOK)
		}
		return olResp{}
	})
	env := accNewEnv(t, srv.URL, "b")
	rr := env.accDoRec("POST", "/api/accounts/"+env.acc.ID+"/copy-prefix/async",
		`{"bucket":"b","prefix":"p/","targetPrefix":"q/"}`)
	olExpectStatus(t, rr, http.StatusAccepted, "async copy start")
	var start map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &start)
	if start["truncated"] != false {
		t.Fatalf("truncated = %v", start["truncated"])
	}
	done := olWaitJobDone(t, env, start["jobId"].(string))
	res, _ := done["result"].(map[string]any)
	if int(res["migrated"].(float64)) != 2 {
		t.Fatalf("migrated = %v", res)
	}
}

// TestOlListPrefixKeysLimit 白盒验证 listPrefixKeys 达到 maxCopy 即截断返回。
func TestOlListPrefixKeysLimit(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		if r.URL.Query().Has("list-type") {
			return olXML(http.StatusOK, listBucketXML([]string{"p/1", "p/2", "p/3"}, true, "t"))
		}
		return olResp{}
	})
	env := accNewEnv(t, srv.URL, "b")
	client := olClient(t, env)
	keys, truncated, err := env.hnd.listPrefixKeys(t.Context(), client, "b", "p/", 2)
	if err != nil {
		t.Fatalf("listPrefixKeys: %v", err)
	}
	if !truncated || len(keys) != 2 {
		t.Fatalf("keys=%v truncated=%v, want 2 keys truncated", keys, truncated)
	}
}
