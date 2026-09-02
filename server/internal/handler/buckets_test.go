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

// TestBucketInfoVersioning 用假 S3 验证：桶属性（区域/创建时间/版本状态）与版本控制开关。
func TestBucketInfoVersioning(t *testing.T) {
	var (
		mu       sync.Mutex
		putBody  string
		putCalls int
	)
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Has("location"):
			io.WriteString(w, `<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">cn-north-1</LocationConstraint>`)
		case q.Has("versioning") && r.Method == http.MethodGet:
			io.WriteString(w, `<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>`)
		case q.Has("versioning") && r.Method == http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			putBody = string(b)
			putCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/":
			io.WriteString(w, `<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>owner</ID></Owner><Buckets><Bucket><Name>b</Name><CreationDate>2026-09-01T00:00:00.000Z</CreationDate></Bucket></Buckets></ListAllMyBucketsResult>`)
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

	rr := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/bucket-info?bucket=b", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("bucket-info status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var info struct {
		Bucket     string `json:"bucket"`
		Region     string `json:"region"`
		CreatedAt  string `json:"createdAt"`
		Versioning string `json:"versioning"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &info); err != nil {
		t.Fatalf("body: %v", err)
	}
	if info.Bucket != "b" || info.Region != "cn-north-1" || info.Versioning != "Enabled" || info.CreatedAt == "" {
		t.Fatalf("info = %+v", info)
	}

	// 暂停版本控制
	rr2 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/bucket-versioning", `{"status":"Suspended"}`)
	if rr2.Code != http.StatusOK {
		t.Fatalf("put versioning status = %d, body=%s", rr2.Code, rr2.Body.String())
	}
	mu.Lock()
	if !strings.Contains(putBody, "<Status>Suspended</Status>") || putCalls != 1 {
		t.Fatalf("putBody=%s calls=%d", putBody, putCalls)
	}
	mu.Unlock()

	// 非法状态 → 400
	if rr3 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/bucket-versioning", `{"status":"Bogus"}`); rr3.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d, want 400", rr3.Code)
	}
}

// TestBucketCRUD 用假 S3 验证创建/删除桶与命名校验。
func TestBucketCRUD(t *testing.T) {
	var (
		createdPath string
		createdACL  string
		deletedPath string
	)
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			createdPath = r.URL.Path
			createdACL = r.Header.Get("X-Amz-Acl")
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			deletedPath = r.URL.Path
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
		Name: "fake", Endpoint: s3fake.URL, Region: "cn-north-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	// 创建（带权限与地域）
	rr := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/bucket", `{"name":"new-bucket","acl":"public-read"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("create bucket status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if createdPath != "/new-bucket" {
		t.Fatalf("created path = %q, want /new-bucket", createdPath)
	}
	if createdACL != "public-read" {
		t.Fatalf("created acl = %q, want public-read", createdACL)
	}
	// 非法名称 → 400
	if rr2 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/bucket", `{"name":"Bad_Name"}`); rr2.Code != http.StatusBadRequest {
		t.Fatalf("bad name = %d, want 400", rr2.Code)
	}
	// 非法 acl → 400
	if rr3 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/bucket", `{"name":"ok-bucket","acl":"hack"}`); rr3.Code != http.StatusBadRequest {
		t.Fatalf("bad acl = %d, want 400", rr3.Code)
	}
	// 删除
	rr4 := doJSON(t, h, "DELETE", "/api/accounts/"+acc.ID+"/bucket?name=new-bucket", "")
	if rr4.Code != http.StatusOK {
		t.Fatalf("delete bucket status = %d, body=%s", rr4.Code, rr4.Body.String())
	}
	if deletedPath != "/new-bucket" {
		t.Fatalf("deleted path = %q, want /new-bucket", deletedPath)
	}
	// 缺 name → 400
	if rr5 := doJSON(t, h, "DELETE", "/api/accounts/"+acc.ID+"/bucket", ""); rr5.Code != http.StatusBadRequest {
		t.Fatalf("delete without name = %d, want 400", rr5.Code)
	}
}

// TestDeleteBucketNotEmpty 验证桶非空时返回 409。
func TestDeleteBucketNotEmpty(t *testing.T) {
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusConflict)
		io.WriteString(w, `<?xml version="1.0"?><Error><Code>BucketNotEmpty</Code><Message>The bucket you tried to delete is not empty</Message></Error>`)
	}))
	defer s3fake.Close()
	st, _ := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	acc, _ := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()
	rr := doJSON(t, h, "DELETE", "/api/accounts/"+acc.ID+"/bucket?name=full", "")
	if rr.Code != http.StatusConflict {
		t.Fatalf("delete non-empty bucket = %d, want 409, body=%s", rr.Code, rr.Body.String())
	}
}

// TestListBuckets 补测：列出桶（ListBuckets XML 解析）。
func TestListBuckets(t *testing.T) {
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && !r.URL.Query().Has("list-type") {
			io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>o</ID></Owner><Buckets><Bucket><Name>b1</Name><CreationDate>2026-01-01T00:00:00.000Z</CreationDate></Bucket><Bucket><Name>b2</Name><CreationDate>2026-01-02T00:00:00.000Z</CreationDate></Bucket></Buckets></ListAllMyBucketsResult>`)
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
		AccessKey: "ak", SecretKey: "sk", Bucket: "", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	rr := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/buckets", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("list buckets status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Buckets []struct {
			Name string `json:"name"`
		} `json:"buckets"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body: %v", err)
	}
	if len(resp.Buckets) != 2 || resp.Buckets[0].Name != "b1" {
		t.Fatalf("buckets = %+v", resp.Buckets)
	}
}

