package handler

// ol_trash_test.go —— trash.go 错误分支/边界补测（仅新增，不改生产代码）。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOlTrashValidation 回收站接口：404 / 缺桶 / 非法请求体 / 缺 key。
func TestOlTrashValidation(t *testing.T) {
	// 不存在的账号 → 404
	env := accNewEnv(t, "http://127.0.0.1:1", "b")
	rr := env.accDoRec("GET", "/api/accounts/nope/trash?bucket=b", "")
	olExpectStatus(t, rr, http.StatusNotFound, "trash 404")
	rr = env.accDoRec("POST", "/api/accounts/nope/trash/purge", `{"key":"k"}`)
	olExpectStatus(t, rr, http.StatusNotFound, "purge 404")

	// 账号无默认桶 → 400
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	nb := accNewEnv(t, srv.URL, "")
	rr = nb.accDoRec("GET", "/api/accounts/"+nb.acc.ID+"/trash", "")
	olExpectStatus(t, rr, http.StatusBadRequest, "trash no bucket")
	rr = nb.accDoRec("POST", "/api/accounts/"+nb.acc.ID+"/trash/purge", `{"key":"k"}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "purge no bucket")

	// 非法 JSON / 缺 key → 400
	e := accNewEnv(t, srv.URL, "b")
	id := e.acc.ID
	rr = e.accDoRec("POST", "/api/accounts/"+id+"/trash/purge", "{bad")
	olExpectStatus(t, rr, http.StatusBadRequest, "purge bad json")
	rr = e.accDoRec("POST", "/api/accounts/"+id+"/trash/purge", `{"bucket":"b"}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "purge no key")
}

// TestOlTrashList 回收站列表：maxKeys 解析与列举失败。
func TestOlTrashList(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		if r.URL.Query().Has("versions") {
			if r.URL.Query().Get("prefix") == "err" {
				return olErr(http.StatusForbidden, "AccessDenied")
			}
			return olXML(http.StatusOK, olVersionsXML("k", true))
		}
		return olResp{}
	})
	env := accNewEnv(t, srv.URL, "b")
	id := env.acc.ID

	// maxKeys=5 传入（解析分支生效）→ 200
	rr := env.accDoRec("GET", "/api/accounts/"+id+"/trash?bucket=b&maxKeys=5", "")
	olExpectStatus(t, rr, http.StatusOK, "trash maxKeys ok")
	if rr.Body.String() == "" || !strings.Contains(rr.Body.String(), `"isTruncated":false`) {
		t.Fatalf("trash body: %s", rr.Body.String())
	}
	// maxKeys 非法 → 默认 1000（仍然 200）
	rr = env.accDoRec("GET", "/api/accounts/"+id+"/trash?bucket=b&maxKeys=abc", "")
	olExpectStatus(t, rr, http.StatusOK, "trash bad maxKeys")
	// 列举失败 → 403
	rr = env.accDoRec("GET", "/api/accounts/"+id+"/trash?bucket=b&prefix=err", "")
	olExpectStatus(t, rr, http.StatusForbidden, "trash list err")
}

// TestOlTrashPurge 彻底清除：API 错误 → 409；成功 → 200 + 删除数。
func TestOlTrashPurge(t *testing.T) {
	var failList bool
	srv := olFake(t, func(r *http.Request) olResp {
		q := r.URL.Query()
		switch {
		case q.Has("versions"):
			if failList {
				return olErr(http.StatusForbidden, "AccessDenied")
			}
			return olXML(http.StatusOK, olVersionsXML("k", true))
		case q.Has("delete"):
			return olXML(http.StatusOK, `<?xml version="1.0"?><DeleteResult/>`)
		}
		return olResp{}
	})
	env := accNewEnv(t, srv.URL, "b")
	id := env.acc.ID

	// S3 API 错误 → 409 冲突 + 错误摘要
	failList = true
	rr := env.accDoRec("POST", "/api/accounts/"+id+"/trash/purge", `{"bucket":"b","key":"k"}`)
	olExpectStatus(t, rr, http.StatusConflict, "purge api error")
	failList = false

	// 成功：列出该 key 的 1 版本 + 1 删除标记并全部删除
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/trash/purge", `{"bucket":"b","key":"k"}`)
	olExpectStatus(t, rr, http.StatusOK, "purge ok")
	if !strings.Contains(rr.Body.String(), `"deleted":2`) {
		t.Fatalf("purge body: %s", rr.Body.String())
	}
}

// TestOlTrashPurgeTransportErr 传输层错误（非 API 错误）→ 500 通用映射。
func TestOlTrashPurgeTransportErr(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env := accNewEnv(t, srv.URL, "b")
	req := olCancelReq(t, "POST", "/x", `{"bucket":"b","key":"k"}`)
	req.SetPathValue("id", env.acc.ID)
	w := httptest.NewRecorder()
	env.hnd.purgeTrashObject(w, req)
	olExpectStatus(t, w, http.StatusInternalServerError, "purge transport err")
}
