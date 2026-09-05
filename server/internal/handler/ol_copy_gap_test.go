package handler

// ol_copy_gap_test.go —— copy.go 剩余分支：failedKeys 截断 / 异步校验与空 key 跳过 / 异步取消。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestOlCopyManyFailKeysAll 201 个 key 全部删源失败 → 响应 200，failed=201，failedKeys 全部回传。
// （之前的截断逻辑是 copy.go 内的死代码，已删除——service.RunBatch 内部不再裁剪。）
func TestOlCopyManyFailKeysAll(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		if r.Header.Get("x-amz-copy-source") != "" {
			return olPlain(http.StatusOK) // 复制全部成功
		}
		return olErr(http.StatusForbidden, "AccessDenied") // 删源全部失败
	})
	env := accNewEnv(t, srv.URL, "b")
	keys := make([]string, 201)
	for i := range keys {
		keys[i] = fmt.Sprintf("k%03d", i)
	}
	b, _ := json.Marshal(map[string]any{"bucket": "b", "keys": keys, "deleteSource": true})
	rr := env.accDoRec("POST", "/api/accounts/"+env.acc.ID+"/copy-objects", string(b))
	olExpectStatus(t, rr, http.StatusOK, "fail keys all")
	var m map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &m)
	if int(m["failed"].(float64)) != 201 {
		t.Fatalf("failed = %v", m["failed"])
	}
	if fk, _ := m["failedKeys"].([]any); len(fk) != 201 {
		t.Fatalf("failedKeys len = %d, want 201 (无截断)", len(fk))
	}
}

// TestOlCopyManyAsyncValidation 异步批量复制：缺 keys / 超上限 / 空 key 跳过。
func TestOlCopyManyAsyncValidation(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		if r.Header.Get("x-amz-copy-source") != "" {
			return olPlain(http.StatusOK)
		}
		return olPlain(http.StatusNoContent)
	})
	env := accNewEnv(t, srv.URL, "b")
	id := env.acc.ID

	// 缺 keys → 400
	rr := env.accDoRec("POST", "/api/accounts/"+id+"/copy-objects/async", `{"bucket":"b"}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "no keys")

	// 超过上限 → 400
	keys := make([]string, 10001)
	for i := range keys {
		keys[i] = fmt.Sprintf("k%d", i)
	}
	b, _ := json.Marshal(map[string]any{"bucket": "b", "keys": keys})
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/copy-objects/async", string(b))
	olExpectStatus(t, rr, http.StatusBadRequest, "too many keys")

	// 空 key 被跳过：仅复制 1 个
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/copy-objects/async", `{"keys":["","a.txt"]}`)
	olExpectStatus(t, rr, http.StatusAccepted, "skip empty key")
	var start map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &start)
	if int(start["total"].(float64)) != 1 {
		t.Fatalf("total = %v, want 1", start["total"])
	}
	done := olWaitJobDone(t, env, start["jobId"].(string))
	res, _ := done["result"].(map[string]any)
	if int(res["migrated"].(float64)) != 1 {
		t.Fatalf("migrated = %v", res)
	}
}

// TestOlCopyPrefixAsyncCancel 异步前缀复制被取消：复制阻塞 → cancel → 状态 cancelled。
func TestOlCopyPrefixAsyncCancel(t *testing.T) {
	release := make(chan struct{})
	seen := make(chan struct{}, 1)
	srv := olFake(t, func(r *http.Request) olResp {
		if r.URL.Query().Has("list-type") {
			return olXML(http.StatusOK, listBucketXML([]string{"p/1", "p/2"}, false, ""))
		}
		if r.Header.Get("x-amz-copy-source") != "" {
			seen <- struct{}{}
			<-release
			return olPlain(http.StatusOK)
		}
		return olResp{}
	})
	env := accNewEnv(t, srv.URL, "b")
	rr := env.accDoRec("POST", "/api/accounts/"+env.acc.ID+"/copy-prefix/async",
		`{"bucket":"b","prefix":"p/","targetPrefix":"q/"}`)
	olExpectStatus(t, rr, http.StatusAccepted, "async copy cancel start")
	var start map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &start)
	select {
	case <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("copy never reached fake S3")
	}
	env.accDoRec("POST", "/api/migrate/jobs/"+start["jobId"].(string)+"/cancel", "")
	close(release)
	done := olWaitJobDone(t, env, start["jobId"].(string))
	prog, _ := done["progress"].(map[string]any)
	if prog["status"] != "cancelled" {
		t.Fatalf("progress status = %v, want cancelled", prog["status"])
	}
}
