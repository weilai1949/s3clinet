package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/weilai1949/s3clinet/server/internal/model"
	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
)

// newGatedFakeS3 返回一个「请求到达后阻塞等待放行」的假 S3（path-style），
// 每个到达的复制请求向 started 发信号，等待 release 放行后才返回成功。
// 用于确定性地断言：第 1 项完成时其进度必须已被回调（此时第 2 项仍在执行）。
func newGatedFakeS3(t *testing.T) (srv *httptest.Server, started chan struct{}, release chan struct{}) {
	t.Helper()
	started = make(chan struct{}, 8)
	release = make(chan struct{})
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.Header.Get("X-Amz-Copy-Source") == "" {
			http.Error(w, "unexpected", http.StatusMethodNotAllowed)
			return
		}
		started <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><CopyObjectResult><ETag>"e"</ETag></CopyObjectResult>`))
	}))
	t.Cleanup(srv.Close)
	return srv, started, release
}

func newTestClient(t *testing.T, endpoint string) *s3wrap.Client {
	t.Helper()
	c, err := s3wrap.New(&model.Account{
		Name: "t", Endpoint: endpoint, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c
}

// TestCopyKeysReportsProgressWhileRunning 行为测试：复制进行中（尚有项在途）就必须
// 回调 running 进度，而不是等 wg.Wait 全部完成后一次性灌出（回归：进度停 0/total）。
func TestCopyKeysReportsProgressWhileRunning(t *testing.T) {
	srv, started, release := newGatedFakeS3(t)
	client := newTestClient(t, srv.URL)

	pairs := [][2]string{{"a", "a"}, {"b", "b"}}
	pc := make(chan Progress, 16)

	done := make(chan BatchResult, 1)
	go func() {
		done <- CopyKeys(context.Background(), client, "src", "dst", pairs, 1, func(p Progress) { pc <- p })
	}()

	// 等第 1 项请求在途。
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("worker never started first copy")
	}
	// 放行第 1 项。
	release <- struct{}{}
	// 关键断言：此时第 2 项请求已进入在途（还没放行），第 1 项的进度必须先到达。
	<-started // 第 2 项已在途，阻塞在 release 上
	select {
	case p := <-pc:
		if p.Done != 1 || p.Total != 2 || p.Key != "a" || p.Status != "running" {
			t.Fatalf("first progress = %+v, want Done=1 Total=2 Key=a running", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("progress for item 1 not emitted while item 2 still in flight (progress fires only after wg.Wait)")
	}
	// 放行第 2 项，收尾断言：剩余事件中必含 done 终态（缓冲的 running 事件先到属正常）。
	release <- struct{}{}
	var out BatchResult
	select {
	case out = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("CopyKeys did not finish")
	}
	if out.OK != 2 || out.Failed != 0 {
		t.Fatalf("result = %+v", out)
	}
	var final *Progress
drain:
	for {
		select {
		case p := <-pc:
			if p.Status != "running" {
				pf := p
				final = &pf
			}
		default:
			break drain
		}
	}
	if final == nil || final.Status != "done" || final.Done != 2 {
		t.Fatalf("final progress = %+v, want done Done=2", final)
	}
}

// TestRunBatchProgressAndAggregate 直接以合成 do() 验证共享池的进度时序与汇总：
// 逐条回调 running 进度、失败聚合、终态 done/cancelled、FailKeys 截断。
func TestRunBatchProgressAndAggregate(t *testing.T) {
	items := []string{"k1", "k2", "k3", "k4", "k5"}
	var mu sync.Mutex
	var progress []Progress
	out := RunBatch(context.Background(), items, 2,
		func(k string) string { return k },
		func(_ context.Context, k string) error {
			if k == "k2" || k == "k4" {
				return context.DeadlineExceeded // 任意错误
			}
			return nil
		},
		func(p Progress) {
			mu.Lock()
			defer mu.Unlock()
			progress = append(progress, p)
		})
	mu.Lock()
	defer mu.Unlock()
	if out.OK != 3 || out.Failed != 2 {
		t.Fatalf("out = %+v", out)
	}
	if len(progress) != len(items)+1 {
		t.Fatalf("progress count = %d, want %d (per item + final)", len(progress), len(items)+1)
	}
	// 逐条递增：第 i 条 running 进度的 Done 必须为 i。
	for i, p := range progress[:len(items)] {
		if p.Done != i+1 || p.Status != "running" {
			t.Fatalf("progress[%d] = %+v, want Done=%d running", i, p, i+1)
		}
	}
	final := progress[len(progress)-1]
	if final.Status != "done" || final.Done != 5 || final.Failed != 2 {
		t.Fatalf("final progress = %+v", final)
	}
	// 失败键聚合与 LastError。
	if out.FailKeys[0] != "k2" || out.LastError == "" {
		t.Fatalf("fail aggregation broken: %+v", out)
	}
}

// TestRunBatchCancelled 终态在 ctx 取消时为 cancelled。
func TestRunBatchCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var got []Progress
	out := RunBatch(ctx, []string{"a"}, 1,
		func(k string) string { return k },
		func(context.Context, string) error { return nil },
		func(p Progress) { got = append(got, p) })
	if out.Failed != 1 {
		t.Fatalf("cancelled item should count as failed: %+v", out)
	}
	if got[len(got)-1].Status != "cancelled" {
		t.Fatalf("final status = %q, want cancelled", got[len(got)-1].Status)
	}
}
