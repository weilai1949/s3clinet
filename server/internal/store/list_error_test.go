package store

import (
	"testing"

	"github.com/weilai1949/s3clinet/server/internal/model"
)

// TestSQLiteListReturnsErrorAfterClose 回归：SQLite 查询失败不得静默返回空列表
// （用户会误以为账号全丢），必须把错误返回给调用方以映射 500。
func TestSQLiteListReturnsErrorAfterClose(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir, "sqlite", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := st.Create(&model.Account{Name: "a", Endpoint: "http://127.0.0.1:9000", AccessKey: "ak", SecretKey: "sk", Region: "us-east-1"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	accounts, listErr := st.List()
	if listErr == nil {
		t.Fatalf("List after close: want error, got nil with %d accounts", len(accounts))
	}
	if len(accounts) != 0 {
		t.Fatalf("List after close returned accounts: %d", len(accounts))
	}
}
