package handler

// ol_helper_test.go —— ol_ 前缀测试的共享辅助（仅新增，不改生产代码与既有测试）。
// 所有桩均为并发安全（-race）。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
)

// ---- 可编程假 S3 ----

// olResp 假 S3 单请求响应（status==0 视为未实现 → 405）。
type olResp struct {
	status  int
	headers map[string]string
	body    string
}

// olXML 返回任意 XML 成功响应。
func olXML(status int, body string) olResp {
	return olResp{status: status, headers: map[string]string{"Content-Type": "application/xml"}, body: body}
}

// olErr 返回 S3 风格错误 XML（SDK 可解析出错误码）。
func olErr(status int, code string) olResp {
	return olXML(status, fmt.Sprintf(`<?xml version="1.0"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, code))
}

// olPlain 返回无响应体的状态码（204/200 等）。
func olPlain(status int) olResp { return olResp{status: status} }

// olFake 起一个可编程假 S3：route 按请求给出响应；status==0 → 405。
func olFake(t *testing.T, route func(r *http.Request) olResp) *httptest.Server {
	t.Helper()
	return accStartFake(t, func(w http.ResponseWriter, r *http.Request) {
		resp := route(r)
		if resp.status == 0 {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		for k, v := range resp.headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(resp.status)
		_, _ = io.WriteString(w, resp.body)
	})
}

// olKeys 构造 n 个 key（k000000 格式，稳定排序）。
func olKeys(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("k%06d", i))
	}
	return out
}

// ---- 白盒工具 ----

// olClient 白盒取真实 S3 客户端（store.Get 返回未脱敏账号，SecretKey 可用）。
func olClient(t *testing.T, e *accEnv) *s3wrap.Client {
	t.Helper()
	acc, err := e.hnd.store.Get(e.acc.ID)
	if err != nil {
		t.Fatalf("store get: %v", err)
	}
	c, err := e.hnd.clients.get(acc)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c
}

// olCancelReq 构造已取消上下文的请求（白盒直调 handler，注入传输层错误；body 仍可读）。
func olCancelReq(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return req.WithContext(ctx)
}

// olWaitJobDone 轮询任务状态直到 done（上限 5s），返回终态 JSON。
func olWaitJobDone(t *testing.T, e *accEnv, jobID string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rr := e.accDoRec(http.MethodGet, "/api/migrate/jobs/"+jobID, "")
		if rr.Code != http.StatusOK {
			t.Fatalf("job status = %d body=%s", rr.Code, rr.Body.String())
		}
		var m map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &m)
		if b, _ := m["done"].(bool); b {
			return m
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s not done within 5s", jobID)
	return nil
}

// olExpectStatus 断言 recorder 状态码。
func olExpectStatus(t *testing.T, rr *httptest.ResponseRecorder, want int, what string) {
	t.Helper()
	if rr.Code != want {
		t.Fatalf("%s status = %d, want %d, body=%s", what, rr.Code, want, rr.Body.String())
	}
}
