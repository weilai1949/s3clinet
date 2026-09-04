package handler

// ol_migrate_async_test.go —— migrate_async.go 校验/取消/SSE 分支补测。

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weilai1949/s3clinet/server/internal/service"
)

// TestOlMigrateAsyncValidation 异步迁移：非法请求体 → 400。
func TestOlMigrateAsyncValidation(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env := accNewEnv(t, srv.URL, "b")
	rr := doJSON(t, env.h, "POST", "/api/migrate/async", "{bad")
	olExpectStatus(t, rr, http.StatusBadRequest, "bad json")
	rr = doJSON(t, env.h, "POST", "/api/migrate/async", `{"sourceAccountId":"s","targetAccountId":"d","sourceKeys":[]}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "no keys")
}

// TestOlMigrateAsyncCancel 异步迁移中途取消：源端拉取阻塞 → cancel → 状态 cancelled 且带 lastError。
func TestOlMigrateAsyncCancel(t *testing.T) {
	release := make(chan struct{})
	seenGet := make(chan struct{}, 1)
	srcSrv := olFake(t, func(r *http.Request) olResp {
		if r.Method == http.MethodGet {
			seenGet <- struct{}{}
			<-release // 阻塞直到测试放行
			return olResp{status: http.StatusOK, headers: map[string]string{"Content-Type": "text/plain"}, body: "data"}
		}
		return olResp{}
	})
	dstSrv := olFake(t, func(r *http.Request) olResp {
		if r.Method == http.MethodPut {
			return olPlain(http.StatusOK)
		}
		return olResp{}
	})
	env := accNewEnv(t, srcSrv.URL, "b")
	dst := olCreateAccount(t, env, "dst", dstSrv.URL, "ak", "db")
	body := `{"sourceAccountId":"` + env.acc.ID + `","sourceBucket":"b","sourceKeys":["a.txt"],"targetAccountId":"` + dst.ID + `","targetBucket":"db"}`
	rr := doJSON(t, env.h, "POST", "/api/migrate/async", body)
	olExpectStatus(t, rr, http.StatusAccepted, "async start")
	var start map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &start)
	jobID, _ := start["jobId"].(string)
	select {
	case <-seenGet:
	case <-time.After(5 * time.Second):
		t.Fatal("get never reached fake S3")
	}
	env.accDoRec("POST", "/api/migrate/jobs/"+jobID+"/cancel", "")
	close(release)
	done := olWaitJobDone(t, env, jobID)
	prog, _ := done["progress"].(map[string]any)
	if prog["status"] != "cancelled" {
		t.Fatalf("progress status = %v, want cancelled", prog["status"])
	}
	res, _ := done["result"].(map[string]any)
	if res["lastError"] == "" {
		t.Fatalf("lastError missing: %v", res)
	}
}

// TestOlMigrateJobCancelAndStatus 任务取消/轮询：未知 ID、运行中取消、已完成取消、终态查询。
func TestOlMigrateJobCancelAndStatus(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env := accNewEnv(t, srv.URL, "b")

	// 未知 job → 404
	rr := env.accDoRec("POST", "/api/migrate/jobs/ghost/cancel", "")
	olExpectStatus(t, rr, http.StatusNotFound, "cancel unknown")
	rr = env.accDoRec("GET", "/api/migrate/jobs/ghost", "")
	olExpectStatus(t, rr, http.StatusNotFound, "status unknown")

	// 运行中取消 → cancelled:true
	jctx, jcancel := context.WithCancel(context.Background())
	defer jcancel()
	_ = jctx // ctx 仅用于派生 cancel
	job := env.hnd.migrateJobs.Create(1, jcancel)
	rr = env.accDoRec("POST", "/api/migrate/jobs/"+job.ID+"/cancel", "")
	olExpectStatus(t, rr, http.StatusOK, "cancel running")
	var m map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &m)
	if m["cancelled"] != true {
		t.Fatalf("cancel running = %v", m)
	}

	// 已完成后再取消 → done:true
	job.Finish(migrateJobResult(), "done")
	rr = env.accDoRec("POST", "/api/migrate/jobs/"+job.ID+"/cancel", "")
	olExpectStatus(t, rr, http.StatusOK, "cancel done")
	_ = json.Unmarshal(rr.Body.Bytes(), &m)
	if m["done"] != true {
		t.Fatalf("cancel done = %v", m)
	}

	// 终态查询：带 result（lastError/failedKeys）
	rr = env.accDoRec("GET", "/api/migrate/jobs/"+job.ID, "")
	olExpectStatus(t, rr, http.StatusOK, "status done")
	_ = json.Unmarshal(rr.Body.Bytes(), &m)
	res, _ := m["result"].(map[string]any)
	if res == nil || res["lastError"] == "" || res["failedKeys"] == nil {
		t.Fatalf("status result missing: %v", m)
	}
}

// migrateJobResult 带失败信息的终态结果。
func migrateJobResult() service.JobResult {
	return service.JobResult{Failed: 1, LastError: "boom", FailKeys: []string{"k"}}
}

// TestOlMigrateEventsValidation SSE 订阅：404 / 非流式 writer / 写失败。
func TestOlMigrateEventsValidation(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env := accNewEnv(t, srv.URL, "b")

	// 未知 job → 404
	req := httptest.NewRequest("GET", "/x", nil)
	req.SetPathValue("id", "ghost")
	rr := httptest.NewRecorder()
	env.hnd.migrateJobEvents(rr, req)
	olExpectStatus(t, rr, http.StatusNotFound, "events unknown")

	// 非流式 writer（无 Flusher）→ 500
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = ctx // ctx 仅用于派生 cancel
	job := env.hnd.migrateJobs.Create(1, cancel)
	defer job.Finish(migrateJobResult(), "done")
	req = httptest.NewRequest("GET", "/x", nil)
	req.SetPathValue("id", job.ID)
	rr = httptest.NewRecorder()
	env.hnd.migrateJobEvents(olNoFlushW{rr}, req)
	olExpectStatus(t, rr, http.StatusInternalServerError, "events no flusher")

	// writer 写失败 → 提前返回
	rr = httptest.NewRecorder()
	env.hnd.migrateJobEvents(olErrW{rr}, req)
}

// olNoFlushW 不实现 Flusher 的 ResponseWriter 包装。
type olNoFlushW struct{ w http.ResponseWriter }

func (o olNoFlushW) Header() http.Header         { return o.w.Header() }
func (o olNoFlushW) Write(b []byte) (int, error) { return o.w.Write(b) }
func (o olNoFlushW) WriteHeader(code int)        { o.w.WriteHeader(code) }

// olErrW 每次 Write 都失败的 ResponseWriter。
type olErrW struct{ w http.ResponseWriter }

func (o olErrW) Header() http.Header       { return o.w.Header() }
func (o olErrW) Write([]byte) (int, error) { return 0, context.Canceled }
func (o olErrW) WriteHeader(int)           {}
func (o olErrW) Flush()                    {}

// TestOlMigrateEventsHeartbeatAndDone SSE 心跳（15s ticker）与客户端断开感知（真实 HTTP 服务）。
func TestOlMigrateEventsHeartbeatAndDone(t *testing.T) {
	release := make(chan struct{})
	seenGet := make(chan struct{}, 1)
	srcSrv := olFake(t, func(r *http.Request) olResp {
		if r.Method == http.MethodGet {
			seenGet <- struct{}{}
			<-release
			return olResp{status: http.StatusOK, headers: map[string]string{"Content-Type": "text/plain"}, body: "data"}
		}
		return olResp{}
	})
	dstSrv := olFake(t, func(r *http.Request) olResp {
		if r.Method == http.MethodPut {
			return olPlain(http.StatusOK)
		}
		return olResp{}
	})
	env := accNewEnv(t, srcSrv.URL, "b")
	dst := olCreateAccount(t, env, "dst", dstSrv.URL, "ak", "db")

	// 启动阻塞中的异步迁移
	body := `{"sourceAccountId":"` + env.acc.ID + `","sourceBucket":"b","sourceKeys":["a.txt"],"targetAccountId":"` + dst.ID + `","targetBucket":"db"}`
	rr := doJSON(t, env.h, "POST", "/api/migrate/async", body)
	var start map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &start)
	jobID := start["jobId"].(string)
	select {
	case <-seenGet:
	case <-time.After(5 * time.Second):
		t.Fatal("get never reached fake S3")
	}

	// 真实 HTTP 服务上订阅 SSE
	sseSrv := httptest.NewServer(env.h)
	t.Cleanup(sseSrv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, sseSrv.URL+"/api/migrate/jobs/"+jobID+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("sse request: %v", err)
	}
	defer resp.Body.Close()

	sawData, sawPing := false, false
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") {
			sawData = true
		}
		if line == "event: ping" {
			sawPing = true
			break // 心跳已确认，主动断开（服务端感知 r.Context().Done）
		}
	}
	if !sawData || !sawPing {
		t.Fatalf("sse saw data=%v ping=%v", sawData, sawPing)
	}
	cancel()

	// 放行源端读取，任务完成
	close(release)
	olWaitJobDone(t, env, jobID)
}

// olFailAtW 第 n 次 Write 开始失败的 ResponseWriter（实现 Flusher，用于逐段覆盖 writeSSE 错误分支）。
type olFailAtW struct {
	rr *httptest.ResponseRecorder
	at int
	mu sync.Mutex
	n  int
}

func (o *olFailAtW) Header() http.Header { return o.rr.Header() }

func (o *olFailAtW) Write(b []byte) (int, error) {
	o.mu.Lock()
	o.n++
	n := o.n
	o.mu.Unlock()
	if n >= o.at {
		return 0, context.Canceled
	}
	return o.rr.Write(b)
}

func (o *olFailAtW) WriteHeader(code int) { o.rr.WriteHeader(code) }
func (o *olFailAtW) Flush()               {}

// writes 已执行的 Write 次数。
func (o *olFailAtW) writes() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.n
}

// TestOlMigrateEventsWriteErrStages 逐段覆盖 writeSSE 的 data/payload/tail 写失败分支。
func TestOlMigrateEventsWriteErrStages(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env := accNewEnv(t, srv.URL, "b")
	// 初始事件共 3 次写：data 头 / 负载 / 尾换行；第 2、3 次 Writer 失败 → 对应错误分支
	for _, at := range []int{2, 3} {
		ctx, cancel := context.WithCancel(context.Background())
		_ = ctx // ctx 仅用于派生 cancel
		job := env.hnd.migrateJobs.Create(1, cancel)
		w := &olFailAtW{rr: httptest.NewRecorder(), at: at}
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.SetPathValue("id", job.ID)
		env.hnd.migrateJobEvents(w, req)
		if w.writes() < at {
			t.Fatalf("at=%d writes=%d", at, w.writes())
		}
		job.Finish(migrateJobResult(), "done")
		cancel()
	}
}

// TestOlMigrateEventsPingWriteErr 心跳写失败：阻塞 ~15s 直到 ticker 触发，ping 的 event 前缀写失败 → 返回。
func TestOlMigrateEventsPingWriteErr(t *testing.T) {
	release := make(chan struct{})
	seenGet := make(chan struct{}, 1)
	srcSrv := olFake(t, func(r *http.Request) olResp {
		if r.Method == http.MethodGet {
			seenGet <- struct{}{}
			<-release
			return olResp{status: http.StatusOK, headers: map[string]string{"Content-Type": "text/plain"}, body: "data"}
		}
		return olResp{}
	})
	dstSrv := olFake(t, func(r *http.Request) olResp {
		if r.Method == http.MethodPut {
			return olPlain(http.StatusOK)
		}
		return olResp{}
	})
	env := accNewEnv(t, srcSrv.URL, "b")
	dst := olCreateAccount(t, env, "dst", dstSrv.URL, "ak", "db")
	body := `{"sourceAccountId":"` + env.acc.ID + `","sourceBucket":"b","sourceKeys":["a.txt"],"targetAccountId":"` + dst.ID + `","targetBucket":"db"}`
	rr := doJSON(t, env.h, "POST", "/api/migrate/async", body)
	var start map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &start)
	jobID := start["jobId"].(string)
	select {
	case <-seenGet:
	case <-time.After(5 * time.Second):
		t.Fatal("get never reached fake S3")
	}

	// 第 4 次写（ping 的 event 前缀）失败：阻塞 ~15s 直到 ticker 触发
	w := &olFailAtW{rr: httptest.NewRecorder(), at: 4}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.SetPathValue("id", jobID)
	env.hnd.migrateJobEvents(w, req)
	if w.writes() < 4 {
		t.Fatalf("writes = %d, want >= 4 (ping 未触发)", w.writes())
	}
	close(release)
	olWaitJobDone(t, env, jobID)
}
