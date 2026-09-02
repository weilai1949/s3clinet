package handler

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/weilai1949/s3clinet/server/internal/model"
	"github.com/weilai1949/s3clinet/server/internal/service"
	"github.com/weilai1949/s3clinet/server/internal/store"
)

// TestHeadObject 用假 S3 验证 HeadObject 详情返回与 404 语义。
func TestHeadObject(t *testing.T) {
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if strings.Contains(r.URL.Path, "missing") {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `<?xml version="1.0"?><Error><Code>NoSuchKey</Code><Message>no such key</Message></Error>`)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("ETag", `"abc123"`)
		w.Header().Set("Content-Length", "5")
		w.Header().Set("Last-Modified", "Mon, 01 Jan 2026 00:00:00 GMT")
		w.Header().Set("x-amz-meta-owner", "alice")
		w.Header().Set("x-amz-storage-class", "STANDARD_IA")
		w.WriteHeader(http.StatusOK)
	}))
	defer s3fake.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	rr := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/head?bucket=b&key=hello.txt", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("head status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatalf("head body: %v", err)
	}
	if m["contentType"] != "text/plain" || m["etag"] != `"abc123"` || m["size"] != float64(5) {
		t.Fatalf("unexpected head fields: %+v", m)
	}
	if m["storageClass"] != "STANDARD_IA" {
		t.Fatalf("storageClass = %v, want STANDARD_IA", m["storageClass"])
	}
	if md, ok := m["metadata"].(map[string]any); !ok || md["owner"] != "alice" {
		t.Fatalf("metadata = %v, want owner=alice", m["metadata"])
	}

	// 对象不存在 → 404
	rr2 := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/head?bucket=b&key=missing.txt", "")
	if rr2.Code != http.StatusNotFound {
		t.Fatalf("head missing status = %d, want 404, body=%s", rr2.Code, rr2.Body.String())
	}
	// key 缺失 → 400
	if rr3 := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/head?bucket=b", ""); rr3.Code != http.StatusBadRequest {
		t.Fatalf("head without key = %d, want 400", rr3.Code)
	}
}

// TestMkdirObject 用假 S3 验证：PUT 空对象创建 key 以 / 结尾的"文件夹"。
func TestMkdirObject(t *testing.T) {
	var gotPath string
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 {
			t.Errorf("mkdir body should be empty, got %q", body)
		}
		w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer s3fake.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	// key 不带斜杠 → 自动补全为目录形式
	rr := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/mkdir", `{"key":"images"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("mkdir status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if gotPath != "/b/images/" {
		t.Fatalf("mkdir path = %q, want /b/images/", gotPath)
	}
	var resp struct {
		Created string `json:"created"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Created != "images/" {
		t.Fatalf("created = %q, want images/", resp.Created)
	}
	// key 缺失 → 400
	if rr2 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/mkdir", `{}`); rr2.Code != http.StatusBadRequest {
		t.Fatalf("mkdir without key = %d, want 400", rr2.Code)
	}
}

// TestRenameObject 用假 S3 验证：先 CopyObject 成功后才 DeleteObject 源。
func TestRenameObject(t *testing.T) {
	var calls []string
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut: // CopyObject（同桶）与 mkdir 一样走 PUT，用 Copy-Source 区分
			if src := r.Header.Get("X-Amz-Copy-Source"); src != "" {
				calls = append(calls, "copy:"+src)
				if strings.Contains(src, "bad") {
					w.WriteHeader(http.StatusNotFound)
					io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code></Error>`)
					return
				}
				io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><CopyObjectResult><ETag>"e"</ETag></CopyObjectResult>`)
				return
			}
			w.WriteHeader(http.StatusMethodNotAllowed)
		case http.MethodDelete:
			calls = append(calls, "delete:"+r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer s3fake.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	// 正常重命名：copy 先于 delete，响应新 key
	rr := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/rename", `{"key":"old.txt","newKey":"dir/new.txt"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("rename status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if len(calls) != 2 || !strings.HasPrefix(calls[0], "copy:") || !strings.HasPrefix(calls[1], "delete:") {
		t.Fatalf("calls = %v, want copy then delete", calls)
	}
	var resp struct {
		Renamed string `json:"renamed"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Renamed != "dir/new.txt" {
		t.Fatalf("renamed = %q, want dir/new.txt", resp.Renamed)
	}

	// 复制失败 → 404（对象不存在，经 s3HTTPStatus 映射）且不删除源
	calls = nil
	rr2 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/rename", `{"key":"bad.txt","newKey":"x.txt"}`)
	if rr2.Code != http.StatusNotFound {
		t.Fatalf("rename fail status = %d, want 404", rr2.Code)
	}
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "copy:") {
		t.Fatalf("calls = %v, want only copy attempt", calls)
	}

	// 同桶内新 key 与旧 key 相同 → 400
	if rr3 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/rename", `{"key":"a.txt","newKey":"a.txt"}`); rr3.Code != http.StatusBadRequest {
		t.Fatalf("rename same key = %d, want 400", rr3.Code)
	}

	// 跨桶同名移动 → 允许（copy 到目标桶 + 删除源）
	calls = nil
	rr4 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/rename", `{"key":"a.txt","newBucket":"b2","newKey":"a.txt"}`)
	if rr4.Code != http.StatusOK {
		t.Fatalf("cross-bucket same-key rename = %d, want 200, body=%s", rr4.Code, rr4.Body.String())
	}
	if len(calls) != 2 || !strings.HasPrefix(calls[0], "copy:") || !strings.HasPrefix(calls[1], "delete:") {
		t.Fatalf("calls = %v, want copy then delete", calls)
	}
}

// TestCopyObject 用假 S3 验证：复制单对象到目标桶/键且不删除源。
func TestCopyObject(t *testing.T) {
	var (
		mu          sync.Mutex
		gotSrc      string
		deleteCalls int
	)
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			mu.Lock()
			gotSrc = r.Header.Get("X-Amz-Copy-Source")
			mu.Unlock()
			io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><CopyObjectResult><ETag>"e"</ETag></CopyObjectResult>`)
		case http.MethodDelete:
			mu.Lock()
			deleteCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer s3fake.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	// 复制（同桶，新 key）
	rr := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/copy-object", `{"key":"a.txt","newKey":"archive/a.txt"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("copy-object status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Copied string `json:"copied"`
		Bucket string `json:"bucket"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body: %v", err)
	}
	if resp.Copied != "archive/a.txt" || resp.Bucket != "b" {
		t.Fatalf("copied=%q bucket=%q, want archive/a.txt b", resp.Copied, resp.Bucket)
	}
	mu.Lock()
	src, dels := gotSrc, deleteCalls
	mu.Unlock()
	if src == "" {
		t.Fatalf("expected Copy-Source header, got empty")
	}
	if dels != 0 {
		t.Fatalf("copy-object must not delete source, got %d delete calls", dels)
	}

	// 跨桶复制
	rr2 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/copy-object", `{"key":"a.txt","newBucket":"b2","newKey":"a.txt"}`)
	if rr2.Code != http.StatusOK {
		t.Fatalf("cross-bucket copy-object status = %d, body=%s", rr2.Code, rr2.Body.String())
	}

	// 同桶同 key → 400
	if rr3 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/copy-object", `{"key":"a.txt","newKey":"a.txt"}`); rr3.Code != http.StatusBadRequest {
		t.Fatalf("copy to self = %d, want 400", rr3.Code)
	}
	// key 缺失 → 400
	if rr4 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/copy-object", `{"key":"a.txt"}`); rr4.Code != http.StatusBadRequest {
		t.Fatalf("copy without newKey = %d, want 400", rr4.Code)
	}
}

// TestCopyMany 用假 S3 验证：批量复制/移动所选文件（目标前缀 + 保留文件名，deleteSource 删除源，部分失败）。
func TestCopyMany(t *testing.T) {
	var (
		mu       sync.Mutex
		copySrcs []string
		putPaths []string
		delPaths []string
	)
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			mu.Lock()
			copySrcs = append(copySrcs, r.Header.Get("X-Amz-Copy-Source"))
			putPaths = append(putPaths, r.URL.Path)
			mu.Unlock()
			if strings.Contains(r.Header.Get("X-Amz-Copy-Source"), "bad") {
				w.WriteHeader(http.StatusNotFound)
				io.WriteString(w, `<?xml version="1.0"?><Error><Code>NoSuchKey</Code></Error>`)
				return
			}
			io.WriteString(w, `<?xml version="1.0"?><CopyObjectResult><ETag>"e"</ETag></CopyObjectResult>`)
		case http.MethodDelete:
			mu.Lock()
			delPaths = append(delPaths, r.URL.Path)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer s3fake.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	// 复制模式：目标 = targetPrefix + basename；bad.txt 失败但不中断其余
	rr := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/copy-objects",
		`{"keys":["dir/a.txt","dir/b.txt","dir/bad.txt"],"targetPrefix":"archive/"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("copy-objects status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Copied     int      `json:"copied"`
		Failed     int      `json:"failed"`
		FailedKeys []string `json:"failedKeys"`
		LastError  string   `json:"lastError"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body: %v", err)
	}
	if resp.Copied != 2 || resp.Failed != 1 || resp.LastError == "" {
		t.Fatalf("copied=%d failed=%d lastError=%q, want 2/1/nonempty", resp.Copied, resp.Failed, resp.LastError)
	}
	if len(resp.FailedKeys) != 1 || resp.FailedKeys[0] != "dir/bad.txt" {
		t.Fatalf("failedKeys = %v, want [dir/bad.txt]", resp.FailedKeys)
	}
	mu.Lock()
	if len(putPaths) != 3 {
		t.Fatalf("putPaths = %v, want 3 copy attempts", putPaths)
	}
	// 目标路径含 targetPrefix + basename（path-style：/b/archive/a.txt 等）
	if !containsStr(putPaths, "/b/archive/a.txt") || !containsStr(putPaths, "/b/archive/b.txt") || !containsStr(putPaths, "/b/archive/bad.txt") {
		t.Fatalf("putPaths = %v, want /b/archive/{a,b,bad}.txt", putPaths)
	}
	if len(delPaths) != 0 {
		t.Fatalf("delPaths = %v, want no delete in copy mode", delPaths)
	}
	mu.Unlock()

	// 移动模式：复制成功后删除源（跨桶，保留文件名）
	copySrcs, putPaths, delPaths = nil, nil, nil
	rr2 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/copy-objects",
		`{"keys":["dir/a.txt"],"targetBucket":"b2","targetPrefix":"dest/","deleteSource":true}`)
	if rr2.Code != http.StatusOK {
		t.Fatalf("move status = %d, body=%s", rr2.Code, rr2.Body.String())
	}
	var resp2 struct {
		Copied int `json:"copied"`
		Failed int `json:"failed"`
	}
	if err := json.Unmarshal(rr2.Body.Bytes(), &resp2); err != nil || resp2.Copied != 1 || resp2.Failed != 0 {
		t.Fatalf("move resp = %+v, err=%v, want copied=1 failed=0", resp2, err)
	}
	mu.Lock()
	if !containsStr(putPaths, "/b2/dest/a.txt") {
		t.Fatalf("putPaths = %v, want /b2/dest/a.txt", putPaths)
	}
	if !containsStr(delPaths, "/b/dir/a.txt") {
		t.Fatalf("delPaths = %v, want source delete /b/dir/a.txt", delPaths)
	}
	mu.Unlock()

	// keys 缺失 → 400
	if rr3 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/copy-objects", `{}`); rr3.Code != http.StatusBadRequest {
		t.Fatalf("copy-objects without keys = %d, want 400", rr3.Code)
	}
}

// TestDeletePrefix 用假 S3 验证递归删除：分页列出 + 批量删除，返回删除数。
func TestDeletePrefix(t *testing.T) {
	page1 := []string{"dir/a.txt", "dir/b.txt"}
	page2 := []string{"dir/sub/c.txt"}
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
			// ListObjectsV2：第一页截断，第二页结束
			if r.URL.Query().Get("continuation-token") == "tok2" {
				io.WriteString(w, listBucketXML(page2, false, ""))
			} else {
				io.WriteString(w, listBucketXML(page1, true, "tok2"))
			}
		case r.Method == http.MethodPost && (r.URL.Query().Has("x-id") || r.URL.Query().Has("delete")):
			// DeleteObjects：返回与请求等量的 Deleted 条目
			body, _ := io.ReadAll(r.Body)
			n := strings.Count(string(body), "<Key>")
			var sb strings.Builder
			sb.WriteString(`<?xml version="1.0"?><DeleteResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
			for i := 0; i < n; i++ {
				sb.WriteString("<Deleted><Key>x</Key></Deleted>")
			}
			sb.WriteString("</DeleteResult>")
			io.WriteString(w, sb.String())
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer s3fake.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	rr := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/delete-prefix", `{"prefix":"dir/"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete-prefix status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Deleted   int  `json:"deleted"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body: %v", err)
	}
	if resp.Deleted != 3 || resp.Truncated {
		t.Fatalf("deleted = %d truncated=%v, want 3 false", resp.Deleted, resp.Truncated)
	}
	// 空前缀 → 400（防止误删全桶）
	if rr2 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/delete-prefix", `{"prefix":""}`); rr2.Code != http.StatusBadRequest {
		t.Fatalf("delete-prefix empty prefix = %d, want 400", rr2.Code)
	}
}

// TestCopyManyAsyncAndDeletePrefixAsync 验证批量复制/删除前缀异步任务复用 migrate job SSE。
func TestCopyManyAsyncAndDeletePrefixAsync(t *testing.T) {
	s3copy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.Header.Get("X-Amz-Copy-Source") != "" {
			io.WriteString(w, `<?xml version="1.0"?><CopyObjectResult><ETag>"e"</ETag></CopyObjectResult>`)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer s3copy.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "fake", Endpoint: s3copy.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	rr := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/copy-objects/async",
		`{"keys":["a.txt","b.txt"],"targetPrefix":"out/"}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("copy async = %d body=%s", rr.Code, rr.Body.String())
	}
	var start struct {
		JobID string `json:"jobId"`
		Total int    `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &start); err != nil || start.JobID == "" || start.Total != 2 {
		t.Fatalf("start=%+v err=%v", start, err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/migrate/jobs/"+start.JobID+"/events", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	sc := bufio.NewScanner(w.Body)
	var last service.JobProgress
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") {
			_ = json.Unmarshal([]byte(line[6:]), &last)
		}
	}
	if last.Status != "done" || last.Migrated != 2 {
		t.Fatalf("copy progress=%+v", last)
	}

	// delete-prefix/async
	s3del := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
			io.WriteString(w, listBucketXML([]string{"dir/a.txt", "dir/b.txt"}, false, ""))
		case r.Method == http.MethodPost && r.URL.Query().Has("delete"):
			io.WriteString(w, `<?xml version="1.0"?><DeleteResult><Deleted><Key>dir/a.txt</Key></Deleted><Deleted><Key>dir/b.txt</Key></Deleted></DeleteResult>`)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer s3del.Close()
	acc2, err := st.Create(&model.Account{
		Name: "del", Endpoint: s3del.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create2: %v", err)
	}
	rr2 := doJSON(t, h, "POST", "/api/accounts/"+acc2.ID+"/delete-prefix/async", `{"prefix":"dir/"}`)
	if rr2.Code != http.StatusAccepted {
		t.Fatalf("delete async = %d body=%s", rr2.Code, rr2.Body.String())
	}
	var dstart struct {
		JobID string `json:"jobId"`
		Total int    `json:"total"`
	}
	if err := json.Unmarshal(rr2.Body.Bytes(), &dstart); err != nil || dstart.Total != 2 {
		t.Fatalf("dstart=%+v err=%v", dstart, err)
	}
	req2 := httptest.NewRequest(http.MethodGet, "/api/migrate/jobs/"+dstart.JobID+"/events", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	sc2 := bufio.NewScanner(w2.Body)
	var dlast service.JobProgress
	for sc2.Scan() {
		line := sc2.Text()
		if strings.HasPrefix(line, "data: ") {
			_ = json.Unmarshal([]byte(line[6:]), &dlast)
		}
	}
	if dlast.Status != "done" || dlast.Migrated != 2 {
		t.Fatalf("delete progress=%+v", dlast)
	}
}

// TestCopyPrefix 用假 S3 验证递归复制：分页列出 + 逐 key CopyObject，目标 key 前缀拼接正确。
func TestCopyPrefix(t *testing.T) {
	page1 := []string{"src/a.txt", "src/b.txt"}
	page2 := []string{"src/sub/c.txt"}
	var copySources []string
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
			if r.URL.Query().Get("continuation-token") == "tok2" {
				io.WriteString(w, listBucketXML(page2, false, ""))
			} else {
				io.WriteString(w, listBucketXML(page1, true, "tok2"))
			}
		case r.Method == http.MethodPut && r.Header.Get("X-Amz-Copy-Source") != "":
			copySources = append(copySources, r.Header.Get("X-Amz-Copy-Source"))
			io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><CopyObjectResult><ETag>"e"</ETag></CopyObjectResult>`)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer s3fake.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	rr := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/copy-prefix",
		`{"prefix":"src/","targetPrefix":"dst/"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("copy-prefix status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Copied int `json:"copied"`
		Failed int `json:"failed"`
		Total  int `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body: %v", err)
	}
	if resp.Copied != 3 || resp.Failed != 0 || resp.Total != 3 {
		t.Fatalf("copied=%d failed=%d total=%d, want 3/0/3", resp.Copied, resp.Failed, resp.Total)
	}
	// 同桶目标前缀与源重叠 → 400（防无限复制）
	if rr2 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/copy-prefix",
		`{"prefix":"src/","targetPrefix":"src/archive/"}`); rr2.Code != http.StatusBadRequest {
		t.Fatalf("overlap copy = %d, want 400, body=%s", rr2.Code, rr2.Body.String())
	}
	// 跨桶同前缀不重叠 → 允许（不同桶无循环风险）
	if rr3 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/copy-prefix",
		`{"prefix":"src/","targetBucket":"b2","targetPrefix":"src/"}`); rr3.Code != http.StatusOK {
		t.Fatalf("cross-bucket same prefix = %d, want 200, body=%s", rr3.Code, rr3.Body.String())
	}
}

// TestDownloadZip 用假 S3 验证：多个对象流式打包为 ZIP，失败对象写入清单。
func TestDownloadZip(t *testing.T) {
	// key -> 内容；bad.txt 返回 404
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch {
		case strings.Contains(r.URL.Path, "bad.txt"):
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `<?xml version="1.0"?><Error><Code>NoSuchKey</Code></Error>`)
		case strings.Contains(r.URL.Path, "hello.txt"):
			w.Header().Set("Content-Type", "text/plain")
			io.WriteString(w, "hello")
		case strings.Contains(r.URL.Path, "dir/nested.txt"):
			w.Header().Set("Content-Type", "text/plain")
			io.WriteString(w, "nested!")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer s3fake.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	rr := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/download-zip",
		`{"keys":["hello.txt","dir/nested.txt","bad.txt"]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("zip status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("Content-Type = %q, want application/zip", ct)
	}
	zr, err := zip.NewReader(bytes.NewReader(rr.Body.Bytes()), int64(rr.Body.Len()))
	if err != nil {
		t.Fatalf("parse zip: %v", err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = string(b)
	}
	if got["hello.txt"] != "hello" || got["dir/nested.txt"] != "nested!" {
		t.Fatalf("zip contents = %v", got)
	}
	if !strings.Contains(got["_下载失败清单.txt"], "bad.txt") {
		t.Fatalf("fail list missing bad.txt: %v", got)
	}
	// keys 缺失 → 400
	if rr2 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/download-zip", `{}`); rr2.Code != http.StatusBadRequest {
		t.Fatalf("zip without keys = %d, want 400", rr2.Code)
	}
}

// TestProxyDownload 用假 S3 验证代理下载：attachment 头、Content-Type 透传、Range 转发。
func TestProxyDownload(t *testing.T) {
	var gotRange string
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		gotRange = r.Header.Get("Range")
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Length", "5")
		io.WriteString(w, "a,b,c")
	}))
	defer s3fake.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	req := httptest.NewRequest("GET", "/api/accounts/"+acc.ID+"/proxy?bucket=b&key=report.csv&mode=download", nil)
	req.Header.Set("Range", "bytes=0-99")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("proxy status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if gotRange != "bytes=0-99" {
		t.Fatalf("Range forwarded = %q, want bytes=0-99", gotRange)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/csv" {
		t.Fatalf("Content-Type = %q, want text/csv", ct)
	}
	if cd := rr.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
		t.Fatalf("Content-Disposition = %q, want attachment", cd)
	}
	if rr.Body.String() != "a,b,c" {
		t.Fatalf("body = %q", rr.Body.String())
	}
	// key 缺失 → 400；无效 mode → 400
	if rr2 := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/proxy?bucket=b", ""); rr2.Code != http.StatusBadRequest {
		t.Fatalf("proxy without key = %d, want 400", rr2.Code)
	}
	if rr3 := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/proxy?bucket=b&key=x&mode=bad", ""); rr3.Code != http.StatusBadRequest {
		t.Fatalf("proxy bad mode = %d, want 400", rr3.Code)
	}
	// 对象不存在 → 404
	s3fake2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `<?xml version="1.0"?><Error><Code>NoSuchKey</Code></Error>`)
	}))
	defer s3fake2.Close()
	st2, _ := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	acc2, _ := st2.Create(&model.Account{
		Name: "fake", Endpoint: s3fake2.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	h2 := New(st2, logger, t.TempDir(), nil, "", "test").Routes()
	if rr4 := doJSON(t, h2, "GET", "/api/accounts/"+acc2.ID+"/proxy?bucket=b&key=missing", ""); rr4.Code != http.StatusNotFound {
		t.Fatalf("proxy missing = %d, want 404", rr4.Code)
	}
}

// TestProxyInline 验证 inline 模式透传 Content-Disposition: inline。
func TestProxyInline(t *testing.T) {
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		io.WriteString(w, "PNGDATA")
	}))
	defer s3fake.Close()
	st, _ := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	acc, _ := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()
	rr := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/proxy?bucket=b&key=a.png&mode=inline", "")
	if cd := rr.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "inline;") {
		t.Fatalf("Content-Disposition = %q, want inline", cd)
	}
	if rr.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", rr.Header().Get("Content-Type"))
	}
}

// TestProxyTextTruncate 验证文本预览：强制 text/plain + nosniff，超限截断并标记。
func TestProxyTextTruncate(t *testing.T) {
	big := strings.Repeat("x", 4096)
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html") // 源是 HTML，代理必须降级为纯文本
		io.WriteString(w, big)
	}))
	defer s3fake.Close()
	st, _ := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	acc, _ := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	rr := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/proxy?bucket=b&key=page.html&mode=text&maxBytes=1024", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("text status = %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/plain", ct)
	}
	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing nosniff")
	}
	if rr.Header().Get("X-Preview-Truncated") != "1" {
		t.Fatalf("missing X-Preview-Truncated")
	}
	if len(rr.Body.Bytes()) != 1024 {
		t.Fatalf("body len = %d, want 1024", len(rr.Body.Bytes()))
	}

	// 小文件不截断
	rr2 := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/proxy?bucket=b&key=small.txt&mode=text", "")
	if rr2.Header().Get("X-Preview-Truncated") != "" {
		t.Fatalf("small file should not be truncated")
	}
	if len(rr2.Body.Bytes()) != 4096 {
		t.Fatalf("small body len = %d, want 4096", len(rr2.Body.Bytes()))
	}
}

// TestSanitizeFilename 验证 Content-Disposition 文件名清洗。
func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"a.txt":         "a.txt",
		`../"evil".txt`: "..__evil_.txt",
		"a/b.txt":       "a_b.txt",
		"":              "download",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSetHeaders 用假 S3 验证设置对象 HTTP 头：metadata-directive=REPLACE + Content-Type/元数据。
func TestSetHeaders(t *testing.T) {
	var (
		gotDirective string
		gotCT        string
		gotMeta      string
	)
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		gotDirective = r.Header.Get("X-Amz-Metadata-Directive")
		gotCT = r.Header.Get("Content-Type")
		gotMeta = r.Header.Get("X-Amz-Meta-Owner")
		io.WriteString(w, `<?xml version="1.0"?><CopyObjectResult><ETag>"e"</ETag></CopyObjectResult>`)
	}))
	defer s3fake.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	rr := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/set-headers",
		`{"key":"a.txt","contentType":"text/markdown","metadata":{"owner":"alice"}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("set-headers status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if gotDirective != "REPLACE" {
		t.Fatalf("directive = %q, want REPLACE", gotDirective)
	}
	if gotCT != "text/markdown" {
		t.Fatalf("content-type = %q, want text/markdown", gotCT)
	}
	if gotMeta != "alice" {
		t.Fatalf("meta = %q, want alice", gotMeta)
	}
	// key 缺失 → 400
	if rr2 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/set-headers", `{}`); rr2.Code != http.StatusBadRequest {
		t.Fatalf("set-headers without key = %d, want 400", rr2.Code)
	}
}

// TestListObjectsAndDeleteObjects 补测：主浏览端点（ListObjectsV2）与批量删除（DeleteObjects）。
func TestListObjectsAndDeleteObjects(t *testing.T) {
	var (
		mu        sync.Mutex
		delBodies []string
	)
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Query().Has("delete") {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			delBodies = append(delBodies, string(b))
			mu.Unlock()
			io.WriteString(w, `<DeleteResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Deleted><Key>a.txt</Key></Deleted><Deleted><Key>b.txt</Key></Deleted></DeleteResult>`)
			return
		}
		if r.Method == http.MethodGet && r.URL.Query().Has("list-type") {
			// 含 StorageClass 的 ListObjectsV2 响应
			io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>b</Name><Prefix></Prefix><KeyCount>2</KeyCount><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated><Contents><Key>a.txt</Key><Size>3</Size><ETag>&quot;e1&quot;</ETag><StorageClass>STANDARD</StorageClass><LastModified>2026-01-01T00:00:00.000Z</LastModified></Contents><Contents><Key>dir/c.txt</Key><Size>4</Size><ETag>&quot;e2&quot;</ETag><StorageClass>STANDARD_IA</StorageClass><LastModified>2026-01-02T00:00:00.000Z</LastModified></Contents></ListBucketResult>`)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer s3fake.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	// 列出对象：objects + storageClass + commonPrefixes
	rr := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/objects?bucket=b&prefix=&delimiter=/", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Objects []struct {
			Key          string `json:"key"`
			Size         int64  `json:"size"`
			StorageClass string `json:"storageClass"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("list body: %v", err)
	}
	if len(resp.Objects) != 2 || resp.Objects[0].Key != "a.txt" || resp.Objects[0].StorageClass != "STANDARD" {
		t.Fatalf("objects = %+v", resp.Objects)
	}
	if resp.Objects[1].StorageClass != "STANDARD_IA" {
		t.Fatalf("objects[1].storageClass = %q, want STANDARD_IA", resp.Objects[1].StorageClass)
	}

	// 批量删除：POST ?delete（XML body）
	rr2 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/delete", `{"bucket":"b","keys":["a.txt","b.txt"]}`)
	if rr2.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rr2.Code, rr2.Body.String())
	}
	var del struct {
		Deleted int `json:"deleted"`
	}
	_ = json.Unmarshal(rr2.Body.Bytes(), &del)
	mu.Lock()
	gotBodies := delBodies
	mu.Unlock()
	if del.Deleted != 2 || len(gotBodies) != 1 || !strings.Contains(gotBodies[0], "<Key>a.txt</Key>") {
		t.Fatalf("deleted=%d bodies=%v", del.Deleted, gotBodies)
	}
	// 缺 keys → 400
	if rr3 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/delete", `{"bucket":"b","keys":[]}`); rr3.Code != http.StatusBadRequest {
		t.Fatalf("delete empty keys=%d, want 400", rr3.Code)
	}
}

