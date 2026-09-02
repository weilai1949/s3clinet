package s3wrap

import (
	"testing"

	"github.com/weilai1949/s3clinet/server/internal/model"
)

func TestNormalizeEndpoint(t *testing.T) {
	cases := []struct {
		in     string
		useSSL bool
		want   string
	}{
		{"localhost:9000", false, "http://localhost:9000"},
		{"localhost:9000/", false, "http://localhost:9000"},
		{"http://localhost:9000", false, "http://localhost:9000"},
		{"https://s3.amazonaws.com/", true, "https://s3.amazonaws.com"},
		{"minio.example.com", true, "https://minio.example.com"},
		{"", false, ""},
	}
	for _, c := range cases {
		if got := normalizeEndpoint(c.in, c.useSSL); got != c.want {
			t.Errorf("normalizeEndpoint(%q,%v) = %q, want %q", c.in, c.useSSL, got, c.want)
		}
	}
}

func TestEscapeKeyPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"a/b/c.txt", "a/b/c.txt"},
		{"folder/hello world.txt", "folder/hello%20world.txt"},
		{"", ""},
	}
	for _, c := range cases {
		if got := escapeKeyPath(c.in); got != c.want {
			t.Errorf("escapeKeyPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPublicURL(t *testing.T) {
	cases := []struct {
		name              string
		acc               *model.Account
		bucket, key, want string
	}{
		{
			name:   "path-style http",
			acc:    &model.Account{Endpoint: "http://127.0.0.1:9000", PathStyle: true},
			bucket: "mybucket", key: "a/b.txt",
			want: "http://127.0.0.1:9000/mybucket/a/b.txt",
		},
		{
			name:   "virtual-host https",
			acc:    &model.Account{Endpoint: "https://s3.amazonaws.com", UseSSL: true, PathStyle: false},
			bucket: "mybucket", key: "file.txt",
			want: "https://mybucket.s3.amazonaws.com/file.txt",
		},
		{
			name:   "public endpoint override",
			acc:    &model.Account{Endpoint: "http://minio:9000", PublicEndpoint: "http://127.0.0.1:9000", PathStyle: true},
			bucket: "b", key: "k",
			want: "http://127.0.0.1:9000/b/k",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := &Client{acc: c.acc}
			if got := client.PublicURL(c.bucket, c.key); got != c.want {
				t.Errorf("PublicURL() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestPresignEndpointFallback(t *testing.T) {
	// PublicEndpoint 为空时应回退到 Endpoint（New 内逻辑；此处验证 normalize 一致性）。
	acc := &model.Account{
		Endpoint:  "http://127.0.0.1:9000",
		AccessKey: "ak", SecretKey: "sk", PathStyle: true,
	}
	ep := acc.PublicEndpoint
	if ep == "" {
		ep = acc.Endpoint
	}
	if got := normalizeEndpoint(ep, acc.UseSSL); got != "http://127.0.0.1:9000" {
		t.Fatalf("presign endpoint = %q", got)
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("expected error for nil account")
	}
	if _, err := New(&model.Account{AccessKey: "ak"}); err == nil {
		t.Fatal("expected error for missing secret key")
	}
}
