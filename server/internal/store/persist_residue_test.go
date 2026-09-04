package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/weilai1949/s3clinet/server/internal/model"
)

// TestStorePersistsAfterTmpResidue 回归：进程在写 tmp 后 rename 前崩溃会留下残骸，
// 后续保存不得因 O_EXCL 命中残骸而永久失败（json 驱动）。
func TestStorePersistsAfterTmpResidue(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Create(sample()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// 模拟崩溃残骸。
	if err := os.WriteFile(s.path+".tmp", []byte("junk"), 0o600); err != nil {
		t.Fatalf("write residue: %v", err)
	}
	// 崩溃前的账号应仍然可读。
	if accounts, listErr := s.List(); listErr != nil || len(accounts) != 1 {
		t.Fatalf("List: err=%v accounts=%d, want nil/1", listErr, len(accounts))
	}
	// 残骸存在时再次保存必须成功（自动清理残骸）。
	if _, err := s.Create(&model.Account{Name: "second", Endpoint: "http://localhost:9000", AccessKey: "ak", SecretKey: "sk"}); err != nil {
		t.Fatalf("Create after tmp residue: %v", err)
	}
	// 残骸应被清走，磁盘内容为合法 JSON。
	if _, err := os.Stat(s.path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp residue not cleaned: %v", err)
	}
	if _, err := os.Stat(s.path); err != nil {
		t.Fatalf("account file missing: %v", err)
	}
}

// TestEncryptedStorePersistsAfterTmpResidue 加密驱动同样的残骸恢复语义。
func TestEncryptedStorePersistsAfterTmpResidue(t *testing.T) {
	dir := t.TempDir()
	encPath := filepath.Join(dir, "accounts.json.enc")
	s, err := NewEncrypted(encPath, "test-secret-key")
	if err != nil {
		t.Fatalf("NewEncrypted: %v", err)
	}
	if _, err := s.Create(&model.Account{Name: "a", Endpoint: "http://127.0.0.1:9000", AccessKey: "ak", SecretKey: "sk", Region: "us-east-1"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := os.WriteFile(encPath+".tmp", []byte("junk"), 0o600); err != nil {
		t.Fatalf("write residue: %v", err)
	}
	if _, err := s.Create(&model.Account{Name: "b", Endpoint: "http://127.0.0.1:9000", AccessKey: "ak", SecretKey: "sk", Region: "us-east-1"}); err != nil {
		t.Fatalf("Create after tmp residue: %v", err)
	}
	if _, err := os.Stat(encPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp residue not cleaned: %v", err)
	}
	// 重开应能解密两个账号。
	s2, err := NewEncrypted(encPath, "test-secret-key")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	accounts, listErr := s2.List()
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	if len(accounts) != 2 {
		t.Fatalf("accounts = %d, want 2", len(accounts))
	}
}
