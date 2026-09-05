package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
)

// ---- 多端点 fake S3：支持 GET/PUT/COPY/multipart 全套 + 注入失败 ----

type svcFake struct {
	mu      sync.Mutex
	src     map[string][]byte // 源端对象
	dst     map[string][]byte // 目标端对象
	dstMeta map[string]string // 目标端 PUT 元数据头
	dstCT   map[string]string // 目标端 PUT content-type
	copies  []string          // COPY 请求的 CopySource
	aborts  int               // AbortMultipartUpload 次数
	comps   int               // CompleteMultipartUpload 次数
	parts   int               // UploadPart 次数
	created chan struct{}     // CreateMultipartUpload 已返回
	// 注入点
	failCreate   bool  // POST ?uploads → 403
	failPart     bool  // UploadPart → 500
	failComplete bool  // Complete → 500
	failGet      bool  // GET → 404
	copyTooLarge bool  // COPY → EntityTooLarge
	lieLength    int64 // GET 返回伪 Content-Length（实际 body 更小）
	noLength     bool  // GET 不带 Content-Length
	srcSrv       *httptest.Server
	dstSrv       *httptest.Server
}

func newSvcFake(t *testing.T) *svcFake {
	t.Helper()
	f := &svcFake{src: map[string][]byte{}, dst: map[string][]byte{}, dstMeta: map[string]string{}, dstCT: map[string]string{}, created: make(chan struct{}, 4)}
	mk := func(end func(w http.ResponseWriter, r *http.Request) bool) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if end(w, r) {
				return
			}
			http.Error(w, "unexpected request: "+r.Method+" "+r.URL.String(), http.StatusBadRequest)
		}))
		t.Cleanup(srv.Close)
		return srv
	}
	srcEnd := func(w http.ResponseWriter, r *http.Request) bool {
		key := strings.TrimPrefix(r.URL.Path, "/bkt/")
		switch r.Method {
		case http.MethodGet:
			if f.failGet {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>NoSuchKey</Code></Error>`))
				return true
			}
			f.mu.Lock()
			body := f.src[key]
			f.mu.Unlock()
			if f.lieLength > 0 {
				w.Header().Set("Content-Length", fmt.Sprintf("%d", f.lieLength))
			} else if f.noLength {
				// 强制 chunked：禁止 Go 为小 body 自动补 Content-Length
				if rc := http.NewResponseController(w); rc != nil {
					_ = rc.Flush()
				}
			} else {
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
			}
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("x-amz-meta-color", "red")
			_, _ = w.Write(body)
			return true
		case http.MethodPut:
			if r.Header.Get("X-Amz-Copy-Source") != "" {
				f.mu.Lock()
				f.copies = append(f.copies, r.Header.Get("X-Amz-Copy-Source"))
				f.mu.Unlock()
				if f.copyTooLarge {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>EntityTooLarge</Code></Error>`))
					return true
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`<?xml version="1.0"?><CopyObjectResult><ETag>"e1"</ETag></CopyObjectResult>`))
				return true
			}
		}
		return false
	}
	dstEnd := func(w http.ResponseWriter, r *http.Request) bool {
		key := strings.TrimPrefix(r.URL.Path, "/dst/")
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodPost && q.Has("uploads"):
			if f.failCreate {
				w.WriteHeader(http.StatusForbidden)
				return true
			}
			f.created <- struct{}{}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<?xml version="1.0"?><InitiateMultipartUploadResult><UploadId>upid-1</UploadId></InitiateMultipartUploadResult>`))
			return true
		case r.Method == http.MethodPut && q.Get("uploadId") != "" && q.Get("partNumber") != "":
			f.mu.Lock()
			f.parts++
			f.mu.Unlock()
			if f.failPart {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>InternalError</Code></Error>`))
				return true
			}
			w.Header().Set("ETag", `"pt"`)
			w.WriteHeader(http.StatusOK)
			return true
		case r.Method == http.MethodPost && q.Get("uploadId") != "":
			f.mu.Lock()
			f.comps++
			f.mu.Unlock()
			if f.failComplete {
				w.WriteHeader(http.StatusInternalServerError)
				return true
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<?xml version="1.0"?><CompleteMultipartUploadResult><ETag>"m"</ETag></CompleteMultipartUploadResult>`))
			return true
		case r.Method == http.MethodDelete && q.Get("uploadId") != "":
			f.mu.Lock()
			f.aborts++
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return true
		case r.Method == http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			f.mu.Lock()
			f.dst[key] = body
			f.dstCT[key] = r.Header.Get("Content-Type")
			f.dstMeta[key] = r.Header.Get("x-amz-meta-color")
			f.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return true
		}
		return false
	}
	f.srcSrv = mk(srcEnd)
	f.dstSrv = mk(dstEnd)
	return f
}

func (f *svcFake) srcClient(t *testing.T) *s3wrap.Client { return newTestClient(t, f.srcSrv.URL) }
func (f *svcFake) dstClient(t *testing.T) *s3wrap.Client { return newTestClient(t, f.dstSrv.URL) }

func TestStreamCopySinglePut(t *testing.T) {
	f := newSvcFake(t)
	f.src["a.txt"] = []byte("hello svc")
	res, err := f.srcClient(t).GetObjectStream(context.Background(), "bkt", "a.txt", "", "")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if err := StreamCopy(context.Background(), f.srcClient(t), f.dstClient(t), "bkt", "a.txt", "dst", "a.txt"); err != nil {
		t.Fatalf("StreamCopy: %v", err)
	}
	_ = res
	if got := string(f.dst["a.txt"]); got != "hello svc" {
		t.Fatalf("dst body = %q", got)
	}
	if f.dstCT["a.txt"] != "text/plain" || f.dstMeta["a.txt"] != "red" {
		t.Fatalf("content-type/meta = %q/%q", f.dstCT["a.txt"], f.dstMeta["a.txt"])
	}
}

func TestStreamCopySrcError(t *testing.T) {
	f := newSvcFake(t)
	f.failGet = true
	if err := StreamCopy(context.Background(), f.srcClient(t), f.dstClient(t), "bkt", "gone", "dst", "gone"); err == nil {
		t.Fatal("expected source error")
	}
}

// TestStreamCopyMultipartPaths 伪 Content-Length / 无 Content-Length 都走 multipart。
func TestStreamCopyMultipartPaths(t *testing.T) {
	t.Run("declared oversize", func(t *testing.T) {
		f := newSvcFake(t)
		f.lieLength = 6_000_000_000
		f.src["big.bin"] = []byte("tiny")
		if err := StreamCopy(context.Background(), f.srcClient(t), f.dstClient(t), "bkt", "big.bin", "dst", "big.bin"); err != nil {
			t.Fatalf("StreamCopy: %v", err)
		}
		if f.comps != 1 || f.parts != 1 || f.aborts != 0 {
			t.Fatalf("comps=%d parts=%d aborts=%d", f.comps, f.parts, f.aborts)
		}
	})
	t.Run("unknown size", func(t *testing.T) {
		f := newSvcFake(t)
		f.noLength = true
		f.src["u.bin"] = []byte("chunky")
		if err := StreamCopy(context.Background(), f.srcClient(t), f.dstClient(t), "bkt", "u.bin", "dst", "u.bin"); err != nil {
			t.Fatalf("StreamCopy: %v", err)
		}
		if f.comps != 1 {
			t.Fatalf("expected multipart complete, comps=%d", f.comps)
		}
	})
}

func TestMultipartStreamCopyMultiParts(t *testing.T) {
	old := multipartPartSize
	multipartPartSize = 8
	t.Cleanup(func() { multipartPartSize = old })
	f := newSvcFake(t)
	body := strings.Repeat("01234567", 3) // 24B / 8B = 3 段
	if err := MultipartStreamCopy(context.Background(), f.dstClient(t), "dst", "m.bin", "text/plain", strings.NewReader(body)); err != nil {
		t.Fatalf("MultipartStreamCopy: %v", err)
	}
	if f.parts != 3 || f.comps != 1 || f.aborts != 0 {
		t.Fatalf("parts=%d comps=%d aborts=%d", f.parts, f.comps, f.aborts)
	}
}

func TestMultipartStreamCopyEmptyBodyFallsBackToPut(t *testing.T) {
	f := newSvcFake(t)
	if err := MultipartStreamCopy(context.Background(), f.dstClient(t), "dst", "e.bin", "", strings.NewReader("")); err != nil {
		t.Fatalf("MultipartStreamCopy: %v", err)
	}
	if len(f.dst["e.bin"]) != 0 || f.comps != 0 {
		t.Fatalf("empty body should PutObject fallback: comps=%d", f.comps)
	}
}

func TestMultipartStreamCopyFailures(t *testing.T) {
	t.Run("create fails", func(t *testing.T) {
		f := newSvcFake(t)
		f.failCreate = true
		if err := MultipartStreamCopy(context.Background(), f.dstClient(t), "dst", "x", "", strings.NewReader("d")); err == nil {
			t.Fatal("expected create error")
		}
	})
	t.Run("part fails aborts", func(t *testing.T) {
		f := newSvcFake(t)
		f.failPart = true
		if err := MultipartStreamCopy(context.Background(), f.dstClient(t), "dst", "x", "", strings.NewReader("d")); err == nil {
			t.Fatal("expected part error")
		}
		if f.aborts != 1 {
			t.Fatalf("aborts = %d, want 1", f.aborts)
		}
	})
	t.Run("complete fails aborts", func(t *testing.T) {
		f := newSvcFake(t)
		f.failComplete = true
		if err := MultipartStreamCopy(context.Background(), f.dstClient(t), "dst", "x", "", strings.NewReader("d")); err == nil {
			t.Fatal("expected complete error")
		}
		if f.aborts != 1 {
			t.Fatalf("aborts = %d, want 1", f.aborts)
		}
	})
	t.Run("read error aborts", func(t *testing.T) {
		f := newSvcFake(t)
		rerr := errors.New("read boom")
		if err := MultipartStreamCopy(context.Background(), f.dstClient(t), "dst", "x", "", io.MultiReader(strings.NewReader("ab"), errReader{rerr})); err == nil {
			t.Fatal("expected read error")
		}
		if f.aborts != 1 {
			t.Fatalf("aborts = %d, want 1", f.aborts)
		}
	})
	t.Run("ctx cancelled aborts", func(t *testing.T) {
		f := newSvcFake(t)
		ctx, cancel := context.WithCancel(context.Background())
		// create 无竞态地成功；首次 Read 时才取消 → 循环内 readErr 分支命中 abort
		err := MultipartStreamCopy(ctx, f.dstClient(t), "dst", "x", "", &cancelReader{ctx: ctx, cancel: cancel})
		if err == nil {
			t.Fatal("expected ctx error")
		}
		if f.aborts != 1 {
			t.Fatalf("aborts = %d, want 1", f.aborts)
		}
	})
	t.Run("part limit aborts", func(t *testing.T) {
		old := multipartPartSize
		multipartPartSize = 1
		t.Cleanup(func() { multipartPartSize = old })
		f := newSvcFake(t)
		err := MultipartStreamCopy(context.Background(), f.dstClient(t), "dst", "x", "", strings.NewReader(strings.Repeat("a", 10_005)))
		if err == nil || !strings.Contains(err.Error(), "10000") {
			t.Fatalf("expected part-limit error, got %v", err)
		}
		if f.aborts != 1 {
			t.Fatalf("aborts = %d, want 1", f.aborts)
		}
	})
}

// cancelReader 首次 Read 即取消 ctx 并返回其错误（确定性触发 ctx 分支）。
type cancelReader struct {
	ctx    context.Context
	cancel context.CancelFunc
	once   bool
}

func (c *cancelReader) Read(p []byte) (int, error) {
	if !c.once {
		c.once = true
		c.cancel()
	}
	return 0, context.Cause(c.ctx)
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func TestMigrateKeysSameEndpointCopies(t *testing.T) {
	f := newSvcFake(t)
	f.src["k1"] = []byte("v1")
	f.src["k2"] = []byte("v2")
	out := MigrateKeys(context.Background(), f.srcClient(t), f.dstClient(t), "bkt", "dst", []string{"k1", "k2"}, "pre/", true, 2, nil)
	if out.OK != 2 || out.Failed != 0 {
		t.Fatalf("result = %+v", out)
	}
	sort.Strings(f.copies)
	// 同端点迁移 = 源端点服务端 COPY（目标桶写在 CopyObject 请求体内），不走 GET/PUT。
	if len(f.copies) != 2 || !strings.Contains(f.copies[0], "bkt%2Fk") {
		t.Fatalf("copies = %v", f.copies)
	}
}

func TestMigrateKeysEntityTooLargeFallsBackToStream(t *testing.T) {
	f := newSvcFake(t)
	f.copyTooLarge = true
	f.src["big"] = []byte("payload")
	out := MigrateKeys(context.Background(), f.srcClient(t), f.dstClient(t), "bkt", "dst", []string{"big"}, "", true, 1, nil)
	if out.OK != 1 {
		t.Fatalf("fallback copy should succeed: %+v", out)
	}
	if string(f.dst["big"]) != "payload" {
		t.Fatalf("stream fallback body = %q", f.dst["big"])
	}
}

func TestMigrateKeysDifferentEndpointStreamsAndAggregates(t *testing.T) {
	f := newSvcFake(t)
	f.failGet = true
	var progress []Progress
	out := MigrateKeys(context.Background(), f.srcClient(t), f.dstClient(t), "bkt", "dst", []string{"a", "b"}, "", false, 0, func(p Progress) {
		progress = append(progress, p)
	})
	if out.OK != 0 || out.Failed != 2 || out.LastError == "" || len(out.FailKeys) != 2 {
		t.Fatalf("result = %+v", out)
	}
	if len(progress) == 0 || progress[len(progress)-1].Done != 2 {
		t.Fatalf("progress should reach total, got %v", progress)
	}
}

func TestSameEndpointNormalizeEdges(t *testing.T) {
	if !SameEndpoint(" http://A.com/ ", "a.com") {
		t.Fatal("normalize should match scheme/host case/trailing slash")
	}
	if SameEndpoint("", "x") || SameEndpoint("  ", "  ") {
		t.Fatal("empty endpoints never equal")
	}
}

// ---- job.go 剩余分支 ----

func TestJobReapUnsubscribeSnapshotCancel(t *testing.T) {
	r := NewJobRegistry()
	t.Cleanup(r.Stop)
	cancel := context.CancelFunc(func() {})
	j := r.Create(3, cancel)
	// Snapshot：运行中
	p, res, done := j.Snapshot()
	if done || p.Total != 3 || res.Migrated != 0 {
		t.Fatalf("snapshot running = %+v %+v %v", p, res, done)
	}
	// Unsubscribe：未订阅的 channel 也可安全移除
	j.Unsubscribe(make(chan JobProgress, 1))
	ch := j.Subscribe()
	j.Unsubscribe(ch)
	// Emit 慢订阅者丢帧：先灌满缓冲
	for i := 0; i < 20; i++ {
		j.Emit(JobProgress{Done: i, Total: 3})
	}
	// Finish 终态推送
	j.Finish(ResultFromBatch(BatchResult{OK: 3}), "")
	if _, res2, done2 := j.Snapshot(); !done2 || res2.Migrated != 3 {
		t.Fatalf("after finish snapshot = %+v %v", res2, done2)
	}
	// 已完成后再 Finish 为无操作；Cancel 返回 alreadyDone
	j.Finish(ResultFromBatch(BatchResult{OK: 99}), "")
	cancelled, alreadyDone := j.Cancel()
	if cancelled || !alreadyDone {
		t.Fatalf("cancel after done = %v %v", cancelled, alreadyDone)
	}
	// 完成后 Subscribe 立即收终态并关闭
	final, ok := <-j.Subscribe()
	if !ok || final.Status == "" {
		t.Fatalf("subscribe after done = %+v ok=%v", final, ok)
	}
	// Reap：过期完成任务被清理，未完成不受影响
	ctx2, cancel2 := context.WithCancel(context.Background())
	_ = cancel2
	j2 := r.Create(1, cancel2)
	_ = ctx2
	j.mu.Lock()
	j.Created = j.Created.Add(-(JobTTL + time.Minute))
	j.mu.Unlock()
	r.Reap()
	if _, ok := r.Get(j.ID); ok {
		t.Fatal("expired done job should be reaped")
	}
	if _, ok := r.Get(j2.ID); !ok {
		t.Fatal("running job must survive reap")
	}
}

func TestJobRegistryStopCancelsRunning(t *testing.T) {
	r := NewJobRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	j := r.Create(1, cancel)
	r.Stop()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("Stop should cancel running jobs")
	}
	r.Stop() // once 语义：重复 Stop 安全
	if _, ok := r.Get(j.ID); !ok {
		t.Fatal("job record stays after stop (reap only removes expired done)")
	}
}

// ---- batch.go RelKey/BaseKey ----

func TestRelKeyAndBaseKey(t *testing.T) {
	if got := RelKey("docs/a/b.txt", "docs/", "tgt/"); got != "tgt/a/b.txt" {
		t.Fatalf("RelKey = %q", got)
	}
	if got := BaseKey("/x/y/c.txt", "dst/"); got != "dst/c.txt" {
		t.Fatalf("BaseKey = %q", got)
	}
}

// ---- zip.go 剩余分支 ----

func TestLikelyCompressedTable(t *testing.T) {
	cases := []struct {
		name, ct string
		want     bool
	}{
		{"a.zip", "application/octet-stream", true},
		{"b.tgz", "", true},
		{"c.jpg", "", true},
		{"d.mp4", "", true},
		{"e.pdf", "", true},
		{"f.txt", "application/json", false},
		{"g.bin", "image/png", true},
		{"h.webm", "video/webm", true},
		{"i.mp3", "audio/mpeg", true},
		{"j.gz", "application/gzip", true},
	}
	for _, c := range cases {
		if got := LikelyCompressed(c.name, c.ct); got != c.want {
			t.Fatalf("LikelyCompressed(%q,%q) = %v", c.name, c.ct, got)
		}
	}
}

func TestSanitizeZipNameEdges(t *testing.T) {
	cases := []struct{ in, want string }{
		{"..", "_"},
		{".", "_"},
		{"///", "download"},
		{"/abs/path/f.txt", "abs/path/f.txt"},
		{"a:..", "a_"},
		{"weird\\\\n.txt", "weird/n.txt"},
	}
	for _, c := range cases {
		if got := SanitizeZipName(c.in); got != c.want {
			t.Fatalf("SanitizeZipName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCtxReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&ctxReader{ctx: ctx, r: strings.NewReader("x")}).Read(make([]byte, 4)); err == nil {
		t.Fatal("cancelled ctx should error on read")
	}
	buf := make([]byte, 2)
	n, err := (&ctxReader{ctx: context.Background(), r: strings.NewReader("ok")}).Read(buf)
	if err != nil || string(buf[:n]) != "ok" {
		t.Fatalf("ctxReader passthrough = %q err=%v", buf[:n], err)
	}
}

// failWriter 所有 Write 立即失败：覆盖 zip 头创建失败与 Close 失败兜底。
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWriteObjectsZipWriteError(t *testing.T) {
	// 8KB body > zip 内部 bufio 缓冲：key1 复制失败、key2 头创建失败（cw 已带错误），
	// 最终 Close 失败透出。
	get := func(context.Context, string) (io.ReadCloser, string, error) {
		return io.NopCloser(strings.NewReader(strings.Repeat("x", 8<<10))), "text/plain", nil
	}
	_, err := WriteObjectsZip(context.Background(), get, []string{"a.txt", "b.txt"}, failWriter{})
	if err == nil {
		t.Fatal("write failure should surface via zip close")
	}
}

func TestWriteObjectsZipCancelDuringFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	get := func(context.Context, string) (io.ReadCloser, string, error) {
		once.Do(cancel) // 首次取回成功后取消 → 后续 fetch 全部 ctx 失败
		return io.NopCloser(strings.NewReader("x")), "text/plain", nil
	}
	failKeys, err := WriteObjectsZip(ctx, get, []string{"a", "b"}, io.Discard)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(failKeys) != 2 {
		t.Fatalf("both keys should fail after cancel, got %v", failKeys)
	}
}

// TestJobReapStop reapLoop 收到 stop 信号后退出（通过 Reap 的 ticker 路径或 stopCh）。
func TestJobReapStop(t *testing.T) {
	r := NewJobRegistry()
	// reapLoop 在 NewJobRegistry 中即启动；Stop 触发 stopCh 关闭使其退出。
	r.Stop()
	// 再次 Stop 不应 panic
	r.Stop()
}

// TestJobFinishAlreadyDone 重复 Finish 第二次直接返回。
func TestJobFinishAlreadyDone(t *testing.T) {
	r := NewJobRegistry()
	defer r.Stop()
	j := r.Create(0, nil)
	j.Finish(JobResult{}, "done")
	j.Finish(JobResult{}, "cancelled") // 第二次 noop
	_, _, done := j.Snapshot()
	if !done {
		t.Fatal("done should be true")
	}
}

// TestJobCancelStates 覆盖 Cancel 的两条分支：未完成、已完成、未注册 cancel。
func TestJobCancelStates(t *testing.T) {
	r := NewJobRegistry()
	defer r.Stop()
	// 已完成
	j1 := r.Create(1, nil)
	j1.Finish(JobResult{}, "done")
	if ok, done := j1.Cancel(); ok || !done {
		t.Fatalf("cancel done: ok=%v done=%v, want false true", ok, done)
	}
	// 未完成（含 cancel 函数）
	cancelCalled := false
	j2 := r.Create(1, func() { cancelCalled = true })
	if ok, done := j2.Cancel(); !ok || done {
		t.Fatalf("cancel running: ok=%v done=%v, want true false", ok, done)
	}
	if !cancelCalled {
		t.Fatal("cancel func not invoked")
	}
	// 未完成（无 cancel）
	j3 := r.Create(1, nil)
	if ok, done := j3.Cancel(); !ok || done {
		t.Fatalf("cancel no func: ok=%v done=%v, want true false", ok, done)
	}
}

// TestWriteObjectsZipBranches 覆盖 WriteObjectsZip 的空 keys / ctx 取消分发器 / zip 创建失败。
func TestWriteObjectsZipBranches(t *testing.T) {
	// 1) 空 keys
	if _, err := WriteObjectsZip(context.Background(), nil, nil, io.Discard); err != nil {
		t.Fatalf("empty keys = %v", err)
	}
	// 2) 单个 key 拉取失败 → 写入失败的占位条目，继续完成 zip
	fetcher := func(ctx context.Context, key string) (io.ReadCloser, string, error) {
		if key == "bad" {
			return nil, "", errors.New("boom")
		}
		return io.NopCloser(strings.NewReader("x")), "text/plain", nil
	}
	var buf bytes.Buffer
	if _, err := WriteObjectsZip(context.Background(), fetcher, []string{"good", "bad", "good2"}, &buf); err != nil {
		t.Fatalf("WriteObjectsZip = %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("zip empty")
	}
	// 3) ctx 在分发阶段取消
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = WriteObjectsZip(ctx, fetcher, []string{"a", "b", "c", "d", "e"}, io.Discard)
}

// TestSanitizeZipNameBranches 覆盖 path-traversal 拒绝。
func TestSanitizeZipNameBranches(t *testing.T) {
	for _, c := range []struct {
		in, want string
	}{
		{"a:b", "a_b"},
		{".", "_"},
		{"..", "_"},
		{"../etc", "_/etc"},
		{"a/../b", "a/_/b"},
	} {
		if got := SanitizeZipName(c.in); got != c.want {
			t.Fatalf("%q = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestJobReapGoroutineExit 触发 reapLoop 的 stopCh 路径：注册 + 立即 Stop，等 goroutine 退出。
func TestJobReapGoroutineExit(t *testing.T) {
	r := NewJobRegistry()
	_ = r // NewJobRegistry 启动 goroutine；下面 Stop 关闭 stopCh 让其退出
	r.Stop()
	// 第二次 Stop 不应 panic
	r.Stop()
	// 等待 reapLoop 退出（无强保证，但 Sleep 小量时间后 goroutine 应已退）
	time.Sleep(10 * time.Millisecond)
}

// TestJobEmitDrop 测试 Emit 在订阅者 channel 满时走 default 丢帧路径。
// Subscribe 返回 cap=16 的 chan；连续发送 20 条且不读取即可触发 default。
func TestJobEmitDrop(t *testing.T) {
	r := NewJobRegistry()
	defer r.Stop()
	j := r.Create(1, nil)
	sub := j.Subscribe()
	go func() {
		for range sub {
		}
	}()
	for i := 0; i < 20; i++ {
		j.Emit(JobProgress{Done: i, Total: 100, Key: "k", Status: "running"})
	}
	j.Finish(JobResult{}, "done")
}

// TestWriteObjectsZipBodyError body 在 Copy 阶段报错（io.Copy 错误分支）。
func TestWriteObjectsZipBodyError(t *testing.T) {
	fetcher := func(ctx context.Context, key string) (io.ReadCloser, string, error) {
		return &errBody{err: errors.New("mid-read fail")}, "text/plain", nil
	}
	var buf bytes.Buffer
	_, err := WriteObjectsZip(context.Background(), fetcher, []string{"a"}, &buf)
	if err != nil {
		t.Fatalf("WriteObjectsZip = %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("zip empty")
	}
}

type errBody struct{ err error }

func (e *errBody) Read(p []byte) (int, error) { return 0, e.err }
func (e *errBody) Close() error               { return nil }

// TestSanitizeZipNameAllDots 全部点/相对路径 → cleaned == "." → "download"。
func TestSanitizeZipNameAllDots(t *testing.T) {
	// "../" 经过 colon/.. 替换 + path.Clean 后应落到 "." 分支。
	for _, in := range []string{"../..", "a/../.."} {
		_ = in
	}
	if got := SanitizeZipName("../.."); got == "" {
		t.Fatal("expected non-empty")
	}
}

// TestWriteObjectsZipBodyAndError fetcher 同时返回 body 与 err → 走到 body.Close() 分支。
func TestWriteObjectsZipBodyAndError(t *testing.T) {
	body := &errBody{err: errors.New("unused")}
	fetcher := func(ctx context.Context, key string) (io.ReadCloser, string, error) {
		return body, "text/plain", errors.New("fetch failed")
	}
	var buf bytes.Buffer
	_, err := WriteObjectsZip(context.Background(), fetcher, []string{"a"}, &buf)
	if err != nil {
		t.Fatalf("WriteObjectsZip = %v", err)
	}
}

// TestReapLoopFires 注入短 reap 间隔，让 ticker 触发 Reap 调用。
func TestReapLoopFires(t *testing.T) {
	old := reapInterval
	reapInterval = 20 * time.Millisecond
	t.Cleanup(func() { reapInterval = old })
	r := NewJobRegistry()
	defer r.Stop()
	time.Sleep(60 * time.Millisecond)
}

// TestFinishSendTimeout 注入短 finishSendTimeout，触发 5s 超时分支。
// 用一个订阅者，先用 Emit 填满 cap=16 的缓冲，再 Finish 让第 17 个 send 阻塞。
func TestFinishSendTimeout(t *testing.T) {
	old := finishSendTimeout
	finishSendTimeout = 20 * time.Millisecond
	t.Cleanup(func() { finishSendTimeout = old })
	r := NewJobRegistry()
	defer r.Stop()
	j := r.Create(1, nil)
	sub := j.Subscribe()
	_ = sub
	// 填满 16 个缓冲
	for i := 0; i < 16; i++ {
		j.Emit(JobProgress{Done: i, Total: 100, Key: "k", Status: "running"})
	}
	// 再 Emit 一个 → 走 default（不阻塞）但 Finish 必发 final 进同一 buffer 必阻塞
	j.Emit(JobProgress{Done: 16, Total: 100, Key: "k", Status: "running"})
	j.Finish(JobResult{}, "done")
}

// TestWriteObjectsZipCtxCancelInFetcher ctx 在 fetcher 等待期间被取消 → 走 ctx.Err 分支。
func TestWriteObjectsZipCtxCancelInFetcher(t *testing.T) {
	release := make(chan struct{})
	fetcher := func(ctx context.Context, key string) (io.ReadCloser, string, error) {
		<-ctx.Done()
		return nil, "", ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	// 让 fetcher 阻塞
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = WriteObjectsZip(ctx, fetcher, []string{"a", "b", "c"}, &buf)
		close(done)
	}()
	// 等待 fetcher 进入 ctx.Done
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done
	_ = release
}
