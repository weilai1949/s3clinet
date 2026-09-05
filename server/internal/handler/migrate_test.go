package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/weilai1949/s3clinet/server/internal/model"
	"github.com/weilai1949/s3clinet/server/internal/store"
)

// TestMigrateStreamCopy 用两个假 S3 验证：跨 endpoint 迁移走 GetObject→PutObject
// 流式转发，且保留源对象的 Content-Type 与 x-amz-meta-* 元数据。
func TestMigrateStreamCopy(t *testing.T) {
	var (
		mu       sync.Mutex
		gotBody  []byte
		gotCT    string
		gotMeta  string
		putCalls int
	)
	srcS3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("x-amz-meta-owner", "alice")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "hello-stream")
	}))
	defer srcS3.Close()

	dstS3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		putCalls++
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read put body: %v", err)
			return
		}
		gotBody = append(gotBody, b...)
		gotCT = r.Header.Get("Content-Type")
		gotMeta = r.Header.Get("x-amz-meta-owner")
		w.Header().Set("ETag", `"e"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer dstS3.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	src, err := st.Create(&model.Account{
		Name: "src", Endpoint: srcS3.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "src-bucket", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	dst, err := st.Create(&model.Account{
		Name: "dst", Endpoint: dstS3.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "dst-bucket", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test", false).Routes()

	body := fmt.Sprintf(
		`{"sourceAccountId":%q,"sourceBucket":"src-bucket","sourceKeys":["dir/a.txt"],"targetAccountId":%q,"targetBucket":"dst-bucket","targetPrefix":"migrated/"}`,
		src.ID, dst.ID,
	)
	rr := doJSON(t, h, "POST", "/api/migrate", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("migrate status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Migrated  int    `json:"migrated"`
		Failed    int    `json:"failed"`
		LastError string `json:"lastError"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("migrate body: %v", err)
	}
	if resp.Migrated != 1 || resp.Failed != 0 {
		t.Fatalf("expected migrated=1 failed=0, got %+v (lastError=%q)", resp, resp.LastError)
	}

	mu.Lock()
	defer mu.Unlock()
	if putCalls != 1 {
		t.Fatalf("expected 1 PUT to target, got %d", putCalls)
	}
	if string(gotBody) != "hello-stream" {
		t.Fatalf("body = %q, want %q", gotBody, "hello-stream")
	}
	if gotCT != "text/plain" {
		t.Fatalf("Content-Type = %q, want text/plain", gotCT)
	}
	if gotMeta != "alice" {
		t.Fatalf("x-amz-meta-owner = %q, want alice", gotMeta)
	}
}
