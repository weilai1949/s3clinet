package s3wrap

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/weilai1949/s3clinet/server/internal/model"
)

// fakeAccount 构造一个指向指定 endpoint 的账号（path-style、固定密钥），与 e2e 账号形态一致。
func fakeAccount(endpoint string) *model.Account {
	return &model.Account{
		Name:      "fake",
		Endpoint:  endpoint,
		Region:    "us-east-1",
		AccessKey: "ak",
		SecretKey: "sk",
		PathStyle: true,
	}
}

// newFakeS3 启动 httptest 假 S3（path-style /{bucket}/{key}），返回连接它的 Client 与 server 便于断言请求。
func newFakeS3(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := New(fakeAccount(srv.URL))
	if err != nil {
		t.Fatalf("new client for fake s3: %v", err)
	}
	return c, srv
}

// newFakeS3Account 在假 S3 之上定制账号属性（virtual-host、UseSSL 等）。
func newFakeS3Account(t *testing.T, handler http.Handler, mutate func(*model.Account)) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	acc := fakeAccount(srv.URL)
	if mutate != nil {
		mutate(acc)
	}
	c, err := New(acc)
	if err != nil {
		t.Fatalf("new client for fake s3: %v", err)
	}
	return c, srv
}

// writeS3Error 返回标准 S3 错误 XML（handler 测试同款风格）。
func writeS3Error(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, message)
}

// firstPathSegment 解析 path-style 请求里的首段（桶名）。
func firstPathSegment(path string) string {
	for i := 0; i < len(path); i++ {
		if path[i] != '/' {
			path = path[i:]
			break
		}
	}
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return path
}
