package s3wrap

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/weilai1949/s3clinet/server/internal/model"
)

// TestPresignGetVersionURLIncludesVersionID 版本预签名：带版本号含 versionId，当前版本不含。
func TestPresignGetVersionURLIncludesVersionID(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ctx := context.Background()
	raw, err := c.PresignGetVersion(ctx, "bkt", "k.txt", "vid-77", 10*60*time.Second)
	if err != nil {
		t.Fatalf("PresignGetVersion: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	if got := u.Query().Get("versionId"); got != "vid-77" {
		t.Fatalf("versionId param = %q", got)
	}
	raw, err = c.PresignGetVersion(ctx, "bkt", "k.txt", "", 10*60*time.Second)
	if err != nil {
		t.Fatalf("PresignGetVersion(current): %v", err)
	}
	u, _ = url.Parse(raw)
	if u.Query().Has("versionId") {
		t.Fatalf("current version should not carry versionId: %s", raw)
	}
}

// TestPresignUploadPartURLIncludesPartCoordinates 分段预签名 URL 携带 partNumber 与 uploadId。
func TestPresignUploadPartURLIncludesPartCoordinates(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	raw, err := c.PresignUploadPart(context.Background(), "bkt", "big.bin", "upid-42", 3, 15*time.Minute)
	if err != nil {
		t.Fatalf("PresignUploadPart: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	q := u.Query()
	if got := q.Get("partNumber"); got != "3" {
		t.Fatalf("partNumber = %q", got)
	}
	if got := q.Get("uploadId"); got != "upid-42" {
		t.Fatalf("uploadId = %q", got)
	}
	if got := q.Get("X-Amz-Expires"); got != "900" {
		t.Fatalf("X-Amz-Expires = %q, want 900", got)
	}
	if u.Path != "/bkt/big.bin" {
		t.Fatalf("path = %q", u.Path)
	}
}

// TestPresignPostFormFields POST 表单预签名：包含 key/bucket/policy 与签名参数。
func TestPresignPostFormFields(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	form, err := c.PresignPost(context.Background(), "bkt", "k.txt", 10*time.Minute)
	if err != nil {
		t.Fatalf("PresignPost: %v", err)
	}
	if form.URL == "" {
		t.Fatal("form URL should not be empty")
	}
	if got := form.Fields["key"]; got != "k.txt" {
		t.Fatalf("form key = %q", got)
	}
	// SDK v2 将桶放在 POST URL 路径（path-style），表单字段不含 bucket。
	if !strings.HasSuffix(form.URL, "/bkt") {
		t.Fatalf("form url should carry bucket path, got %q", form.URL)
	}
	if form.Fields["policy"] == "" {
		t.Fatal("form policy should not be empty")
	}
	if got := form.Fields["X-Amz-Algorithm"]; got != "AWS4-HMAC-SHA256" {
		t.Fatalf("form algorithm = %q", got)
	}
	if form.Fields["X-Amz-Signature"] == "" {
		t.Fatal("form signature should be present")
	}
}

// TestPresignPostError 无效过期时长（0）应报错。
func TestPresignPostRejectsZeroExpiry(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	if _, err := c.PresignPost(context.Background(), "bkt", "k", 0); err == nil {
		t.Fatal("expected error for zero expiry")
	}
}

// TestPresignURLFollowsPathStyleAndVirtualHost 预签名 URL 形态随账号 path-style 配置切换。
func TestPresignURLFollowsPathStyle(t *testing.T) {
	ctx := context.Background()
	// path-style：host 与 endpoint 一致，路径 /bucket/key。
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	raw, err := c.PresignPut(ctx, "bkt", "k.txt", time.Minute)
	if err != nil {
		t.Fatalf("PresignPut(path-style): %v", err)
	}
	u, _ := url.Parse(raw)
	if !strings.HasPrefix(u.Path, "/bkt/") {
		t.Fatalf("path-style path = %q", u.Path)
	}

	// virtual-host：host 形如 bucket.endpoint。
	c2, _ := newFakeS3Account(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func(a *model.Account) {
		a.PathStyle = false
		// 预签名只构造 URL 不发请求；IP 端点会被 SDK 强制 path-style，
		// 故换成不可解析但形态合法的主机名以观察 virtual-host 形态。
		a.Endpoint = strings.Replace(a.Endpoint, "127.0.0.1", "s3fake.local", 1)
	})
	raw, err = c2.PresignPut(ctx, "mybucket", "k.txt", time.Minute)
	if err != nil {
		t.Fatalf("PresignPut(virtual-host): %v", err)
	}
	u, _ = url.Parse(raw)
	if !strings.HasPrefix(u.Host, "mybucket.") {
		t.Fatalf("virtual-host host = %q (want mybucket.*), url=%s", u.Host, raw)
	}
	if u.Path != "/k.txt" {
		t.Fatalf("virtual-host path = %q", u.Path)
	}
}
