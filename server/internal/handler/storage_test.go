package handler

import (
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
	"github.com/weilai1949/s3clinet/server/internal/store"
)

// TestChangeStorageClass 用假 S3 验证：切换存储类型触发 CopyObject（携带 x-amz-storage-class）、
// 返回新版本号；非法 storageClass / 缺 key → 400。
func TestChangeStorageClass(t *testing.T) {
	var (
		mu         sync.Mutex
		copySource string
		sc         string
		copied     int
	)
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		mu.Lock()
		copySource = r.Header.Get("X-Amz-Copy-Source")
		sc = r.Header.Get("X-Amz-Storage-Class")
		copied++
		mu.Unlock()
		w.Header().Set("X-Amz-Version-Id", "new-1")
		io.WriteString(w, `<CopyObjectResult><LastModified>2026-09-01T00:00:00.000Z</LastModified><ETag>&quot;abc&quot;</ETag></CopyObjectResult>`)
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

	// 切换当前对象到 STANDARD_IA
	rr := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/storage-class", `{"key":"a.txt","storageClass":"STANDARD_IA"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("change status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Changed      string `json:"changed"`
		VersionID    string `json:"versionId"`
		StorageClass string `json:"storageClass"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body: %v", err)
	}
	mu.Lock()
	gotCopy, gotSC, gotN := copySource, sc, copied
	mu.Unlock()
	if gotSC != "STANDARD_IA" {
		t.Fatalf("x-amz-storage-class = %q, want STANDARD_IA", gotSC)
	}
	if gotCopy == "" || !strings.Contains(gotCopy, "a.txt") {
		t.Fatalf("copy source = %q, want to reference a.txt", gotCopy)
	}
	if gotN != 1 || resp.Changed != "a.txt" || resp.StorageClass != "STANDARD_IA" {
		t.Fatalf("resp = %+v, copies=%d", resp, gotN)
	}

	// 非法 storageClass → 400；缺 key → 400
	if rr2 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/storage-class", `{"key":"a.txt","storageClass":"BOGUS"}`); rr2.Code != http.StatusBadRequest {
		t.Fatalf("bad storageClass = %d, want 400", rr2.Code)
	}
	if rr3 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/storage-class", `{"storageClass":"STANDARD"}`); rr3.Code != http.StatusBadRequest {
		t.Fatalf("missing key = %d, want 400", rr3.Code)
	}
}

// TestRestoreDeleteMarker 用假 S3 验证：一键还原删除标记（DELETE + versionId）与参数校验。
func TestRestoreDeleteMarker(t *testing.T) {
	var (
		mu         sync.Mutex
		deletedVer string
		deletes    int
	)
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || !r.URL.Query().Has("versionId") {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		mu.Lock()
		deletedVer = r.URL.Query().Get("versionId")
		deletes++
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
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

	rr := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/delete-marker/restore", `{"key":"deleted.txt","versionId":"dv1"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("restore delete marker status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Restored  string `json:"restored"`
		VersionID string `json:"versionId"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body: %v", err)
	}
	mu.Lock()
	gotVer, gotN := deletedVer, deletes
	mu.Unlock()
	if gotVer != "dv1" || gotN != 1 || resp.Restored != "deleted.txt" || resp.VersionID != "dv1" {
		t.Fatalf("deletedVer=%q deletes=%d resp=%+v", gotVer, gotN, resp)
	}

	// 参数校验：缺 key / 缺 versionId → 400
	if rr2 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/delete-marker/restore", `{"versionId":"dv1"}`); rr2.Code != http.StatusBadRequest {
		t.Fatalf("missing key = %d, want 400", rr2.Code)
	}
	if rr3 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/delete-marker/restore", `{"key":"deleted.txt"}`); rr3.Code != http.StatusBadRequest {
		t.Fatalf("missing versionId = %d, want 400", rr3.Code)
	}
}

// TestPresignGetVersion 验证：预签名 GET 带上 versionId 时，URL 包含该版本（供版本比较拉取历史版本内容）。
// 预签名在本地计算，无需真实对端。
func TestPresignGetVersion(t *testing.T) {
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	rr := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/presign", `{"method":"get","key":"a.txt","versionId":"v1"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("presign status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body: %v", err)
	}
	if resp.URL == "" || !strings.Contains(resp.URL, "versionId") {
		t.Fatalf("presigned url = %q, want versionId in query", resp.URL)
	}
}
