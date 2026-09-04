package s3wrap

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/smithy-go/middleware"
	"github.com/weilai1949/s3clinet/server/internal/model"
)

// TestNewRejectsInvalidAccounts 表驱动验证 New 的入参校验（nil / 缺密钥 / 端点非法）。
func TestNewRejectsInvalidAccounts(t *testing.T) {
	cases := []struct {
		name    string
		acc     *model.Account
		wantMsg string
	}{
		{
			name:    "nil account",
			acc:     nil,
			wantMsg: "empty account",
		},
		{
			name:    "missing secret key",
			acc:     &model.Account{AccessKey: "ak"},
			wantMsg: "missing access key or secret key",
		},
		{
			name:    "missing access key",
			acc:     &model.Account{SecretKey: "sk"},
			wantMsg: "missing access key or secret key",
		},
		{
			name: "endpoint blocked by ssrf guard",
			acc: &model.Account{
				AccessKey: "ak", SecretKey: "sk",
				Endpoint: "http://metadata.google.internal",
			},
			wantMsg: "endpoint: endpoint host is blocked",
		},
		{
			name: "public endpoint blocked by ssrf guard",
			acc: &model.Account{
				AccessKey: "ak", SecretKey: "sk",
				Endpoint:       "http://127.0.0.1:9000",
				PublicEndpoint: "http://169.254.169.254/latest/meta-data/",
			},
			wantMsg: "publicEndpoint: endpoint host is blocked",
		},
		{
			name: "public endpoint unparseable",
			acc: &model.Account{
				AccessKey: "ak", SecretKey: "sk",
				Endpoint:       "http://127.0.0.1:9000",
				PublicEndpoint: "http://[::1",
			},
			wantMsg: "publicEndpoint: invalid endpoint URL",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cl, err := New(c.acc)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.wantMsg)
			}
			if !strings.Contains(err.Error(), c.wantMsg) {
				t.Fatalf("error = %q, want substring %q", err.Error(), c.wantMsg)
			}
			if cl != nil {
				t.Fatal("client should be nil on error")
			}
		})
	}
}

// TestNewBuildsUsableClient 合法账号应得到可用的 S3 客户端（高级用法出口 S3()）。
func TestNewBuildsUsableClient(t *testing.T) {
	cl, err := New(fakeAccount("http://127.0.0.1:9000"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if cl == nil || cl.S3() == nil {
		t.Fatal("expected non-nil client and SDK handle")
	}
}

// TestSharedHTTPClientIsSingleton 复用同一个 SSRF 防护 HTTP 客户端（进程级单例）。
func TestSharedHTTPClientIsSingleton(t *testing.T) {
	a := sharedHTTPClient()
	b := sharedHTTPClient()
	if a == nil || a != b {
		t.Fatal("sharedHTTPClient should return the same instance across calls")
	}
}

// TestUnsignedPayloadMiddlewareInjectsHash 中间件行为：给下游签名器注入 UNSIGNED-PAYLOAD 并透传请求。
func TestUnsignedPayloadMiddlewareInjectsHash(t *testing.T) {
	m := &unsignedPayloadSetter{}
	if got := m.ID(); got != "s3clinet:unsigned-payload" {
		t.Fatalf("middleware id = %q", got)
	}
	seenHash := ""
	called := false
	next := middleware.FinalizeHandlerFunc(func(ctx context.Context, in middleware.FinalizeInput) (middleware.FinalizeOutput, middleware.Metadata, error) {
		called = true
		seenHash = v4.GetPayloadHash(ctx)
		return middleware.FinalizeOutput{}, middleware.Metadata{}, nil
	})
	_, _, err := m.HandleFinalize(context.Background(), middleware.FinalizeInput{}, next)
	if err != nil {
		t.Fatalf("HandleFinalize: %v", err)
	}
	if !called {
		t.Fatal("next handler was not invoked")
	}
	if seenHash != unsignedPayload {
		t.Fatalf("payload hash = %q, want %q", seenHash, unsignedPayload)
	}
}

// TestPresignedPutURLContainsSigV4Parameters 预签名 PUT 产出 SigV4 查询串（校验参数存在与过期秒数，不校验签名值）。
func TestPresignedPutURLContainsSigV4Parameters(t *testing.T) {
	c, srv := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	raw, err := c.PresignPut(context.Background(), "bkt", "k.txt", 5*time.Minute)
	if err != nil {
		t.Fatalf("presign put: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse presigned url: %v", err)
	}
	wantHost := strings.TrimPrefix(srv.URL, "http://")
	if u.Scheme != "http" || u.Host != wantHost {
		t.Fatalf("unexpected endpoint in %s, want host %q", raw, wantHost)
	}
	if u.Path != "/bkt/k.txt" {
		t.Fatalf("path-style path = %q, want /bkt/k.txt", u.Path)
	}
	q := u.Query()
	if got := q.Get("X-Amz-Algorithm"); got != "AWS4-HMAC-SHA256" {
		t.Fatalf("X-Amz-Algorithm = %q", got)
	}
	if got := q.Get("X-Amz-SignedHeaders"); got != "host" {
		t.Fatalf("X-Amz-SignedHeaders = %q", got)
	}
	if got := q.Get("X-Amz-Expires"); got != "300" {
		t.Fatalf("X-Amz-Expires = %q, want 300", got)
	}
	if !strings.Contains(q.Get("X-Amz-Credential"), "ak/") {
		t.Fatalf("X-Amz-Credential missing access key: %q", q.Get("X-Amz-Credential"))
	}
	if q.Get("X-Amz-Signature") == "" {
		t.Fatal("X-Amz-Signature should be present")
	}
}
