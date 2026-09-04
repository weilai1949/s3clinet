package s3wrap

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/weilai1949/s3clinet/server/internal/model"
)

// newErroringFakeS3 假 S3：按查询参数返回 500 错误，用于覆盖各错误分支。
// handler 区分 path 首段与子资源查询参数（?location / ?versioning / ?website）。
func newErroringFakeS3(t *testing.T) *Client {
	t.Helper()
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", "boom")
	}))
	return c
}

// TestBucketReadErrorPaths 表驱动覆盖桶读取接口的错误返回路径。
func TestBucketReadErrorPaths(t *testing.T) {
	ctx := context.Background()
	c := newErroringFakeS3(t)

	if _, err := c.ListBuckets(ctx); err == nil {
		t.Error("ListBuckets: expected error")
	}
	if _, err := c.GetBucketLocation(ctx, "bkt"); err == nil {
		t.Error("GetBucketLocation: expected error")
	}
	if got, err := c.GetBucketVersioning(ctx, "bkt"); err == nil || got != "" {
		t.Errorf("GetBucketVersioning: got %q err=%v, want \"\"+error", got, err)
	}
	if _, err := c.GetWebsite(ctx, "bkt"); err == nil {
		t.Error("GetWebsite: expected error")
	}
}

// TestListObjectsPageError 列举对象失败应原样透传错误。
func TestListObjectsPageError(t *testing.T) {
	c := newErroringFakeS3(t)
	page, err := c.ListObjectsPage(context.Background(), "bkt", "", "", "", "", 0)
	if err == nil {
		t.Fatal("expected error")
	}
	if page != nil {
		t.Fatal("page should be nil on error")
	}
}

// TestGetObjectAclError 读 ACL 失败应返回错误（owner 为空、无授权行）。
func TestGetObjectAclError(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.RawQuery, "acl") || r.URL.Query().Has("acl") {
			writeS3Error(w, http.StatusNotFound, "NoSuchKey", "no acl")
			return
		}
		writeS3Error(w, http.StatusInternalServerError, "InternalError", "boom")
	}))
	owner, public, rows, err := c.GetObjectAcl(context.Background(), "bkt", "k")
	if err == nil {
		t.Fatal("expected error")
	}
	if owner != "" || public || len(rows) != 0 {
		t.Fatalf("on error rows should be empty, got owner=%q public=%v rows=%d", owner, public, len(rows))
	}
}

// TestHeadObjectMetaError HEAD 失败应返回错误（404 无 body 场景也成立）。
func TestHeadObjectMetaError(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	meta, err := c.HeadObjectMeta(context.Background(), "bkt", "k", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if meta != nil {
		t.Fatal("meta should be nil on error")
	}
	if !IsNotFound(err) {
		t.Fatalf("error should be NotFound-shaped, got %v", err)
	}
}

// TestHeadObjectMetaVersionedAndNilFields 带版本 HEAD 与缺省字段：验证 DTO 零值行为。
func TestHeadObjectMetaVersionedAndNilFields(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("versionId") != "v9" {
			t.Errorf("expected versionId=v9 in query, got %q", r.URL.RawQuery)
		}
		w.Header().Set("ETag", `"e9"`)
		w.WriteHeader(http.StatusOK)
	}))
	meta, err := c.HeadObjectMeta(context.Background(), "bkt", "k", "v9")
	if err != nil {
		t.Fatalf("HeadObjectMeta: %v", err)
	}
	if meta == nil || meta.Size != 0 || meta.ContentType != "" || meta.StorageClass != "" || !meta.LastModified.IsZero() || meta.ETag != `"e9"` {
		t.Fatalf("meta = %+v", meta)
	}
}

// TestPresignExpiryGuards 表驱动验证四个预签名入口的 zero-expiry 守卫。
func TestPresignExpiryGuards(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ctx := context.Background()
	cases := []struct {
		name string
		act  func() error
	}{
		{"PresignPut", func() error { _, err := c.PresignPut(ctx, "b", "k", 0); return err }},
		{"PresignPost", func() error { _, err := c.PresignPost(ctx, "b", "k", 0); return err }},
		{"PresignGetVersion", func() error { _, err := c.PresignGetVersion(ctx, "b", "k", "", -time.Second); return err }},
		{"PresignUploadPart", func() error { _, err := c.PresignUploadPart(ctx, "b", "k", "u", 1, 0); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.act(); !errors.Is(err, errInvalidExpiry) {
				t.Fatalf("err = %v, want errInvalidExpiry", err)
			}
		})
	}
}

// TestPresignTransportErrorPaths 预签名客户端缺 region 时端点解析失败，覆盖签名错误透传分支。
func TestPresignTransportErrorPaths(t *testing.T) {
	broken := &Client{presign: s3.NewPresignClient(s3.NewFromConfig(aws.Config{}))}
	ctx := context.Background()
	cases := []struct {
		name string
		act  func() error
	}{
		{"PresignPut", func() error { _, err := broken.PresignPut(ctx, "b", "k", time.Minute); return err }},
		{"PresignPost", func() error { _, err := broken.PresignPost(ctx, "b", "k", time.Minute); return err }},
		{"PresignGetVersion", func() error { _, err := broken.PresignGetVersion(ctx, "b", "k", "v", time.Minute); return err }},
		{"PresignUploadPart", func() error { _, err := broken.PresignUploadPart(ctx, "b", "k", "u", 1, time.Minute); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.act()
			if err == nil {
				t.Fatal("expected presign error")
			}
			if !strings.Contains(err.Error(), "failed to resolve service endpoint") {
				t.Fatalf("err = %v, want endpoint resolution failure", err)
			}
		})
	}
}

// TestPurgeObjectFallsBackToDelete 桶里没有任何版本记录时，Purge 走普通 DELETE 兜底。
func TestPurgeObjectFallsBackToDelete(t *testing.T) {
	var sawDelete bool
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Has("versions"):
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListVersionsResult ` + xmlNS + `></ListVersionsResult>`))
		case r.Method == http.MethodDelete:
			sawDelete = true
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	n, err := c.PurgeObject(context.Background(), "bkt", "k")
	if err != nil {
		t.Fatalf("PurgeObject: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted = %d, want 1 (fallback delete)", n)
	}
	if !sawDelete {
		t.Fatal("fallback DELETE should be issued")
	}
}

// TestPurgeObjectFallbackDeleteError 兜底 DELETE 失败时 Purge 应返回错误且删除计数为 0。
func TestPurgeObjectFallbackDeleteError(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("versions") {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListVersionsResult ` + xmlNS + `></ListVersionsResult>`))
			return
		}
		writeS3Error(w, http.StatusInternalServerError, "InternalError", "delete failed")
	}))
	n, err := c.PurgeObject(context.Background(), "bkt", "k")
	if err == nil {
		t.Fatal("expected fallback delete error")
	}
	if n != 0 {
		t.Fatalf("deleted = %d, want 0", n)
	}
}

// TestPurgeObjectVersionedTruncation 版本页带 NextVersionIDMarker 续传游标的循环分支。
func TestPurgeObjectVersionedTruncation(t *testing.T) {
	var sawDelete bool
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Has("versions"):
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListVersionsResult ` + xmlNS + ` IsTruncated="true">
<Version><Key>k</Key><VersionId>v1</VersionId><IsLatest>true</IsLatest></Version>
<NextKeyMarker>k</NextKeyMarker><NextVersionIdMarker>v2</NextVersionIdMarker>
</ListVersionsResult>`))
		case r.Method == http.MethodPost && r.URL.Query().Has("delete"):
			sawDelete = true
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><DeleteResult ` + xmlNS + `><Deleted><Key>k</Key></Deleted></DeleteResult>`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	n, err := c.PurgeObject(context.Background(), "bkt", "k")
	if err != nil {
		t.Fatalf("PurgeObject: %v", err)
	}
	if !sawDelete {
		t.Fatal("batch delete should be issued")
	}
	if n < 1 {
		t.Fatalf("deleted = %d, want >= 1", n)
	}
}

// TestNewWithEmptyRegionFallsBackToDefault region 为空的账号应回退 us-east-1 并成功建客户端。
func TestNewWithEmptyRegionFallsBackToDefault(t *testing.T) {
	c, _ := newFakeS3Account(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func(a *model.Account) {
		a.Region = ""
	})
	if c == nil || c.S3() == nil {
		t.Fatal("expected usable client with default region")
	}
}

// TestNewLoadConfigFailure 无效共享配置文件时 LoadDefaultConfig 报错，应被包装为 load config 错误。
func TestNewLoadConfigFailure(t *testing.T) {
	dir := t.TempDir()
	bad := dir + "/awsconfig"
	if err := os.WriteFile(bad, []byte("not-valid-!!!!\n[profile x\nbroken\n"), 0o600); err != nil {
		t.Fatalf("write bad config: %v", err)
	}
	t.Setenv("AWS_CONFIG_FILE", bad)
	t.Setenv("AWS_PROFILE", "x")
	acc := fakeAccount("http://127.0.0.1:9000")
	acc.Region = "us-east-1"
	cl, err := New(acc)
	if err == nil {
		t.Fatal("expected load config error")
	}
	if cl != nil {
		t.Fatal("client should be nil on error")
	}
	if !strings.Contains(err.Error(), "load config") {
		t.Fatalf("err = %v, want wrapped load config failure", err)
	}
}

// TestFormatBucketsGaps 空桶列表应返回空切片。
func TestFormatBucketsGaps(t *testing.T) {
	got := FormatBuckets(&s3.ListBucketsOutput{})
	if got == nil || len(got) != 0 {
		t.Fatalf("FormatBuckets(empty) = %+v, want non-nil empty slice", got)
	}
}
