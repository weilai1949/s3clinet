package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/weilai1949/s3clinet/server/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	p := filepath.Join(t.TempDir(), "accounts.json")
	s, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func sample() *model.Account {
	return &model.Account{
		Name:      "demo",
		Endpoint:  "http://localhost:9000",
		Region:    "us-east-1",
		AccessKey: "ak",
		SecretKey: "sk-secret",
		Bucket:    "b",
	}
}

func TestCreateAndSanitize(t *testing.T) {
	s := newTestStore(t)
	a, err := s.Create(sample())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.ID == "" {
		t.Fatal("expected generated id")
	}
	if a.SecretKey != "******" {
		t.Fatalf("expected masked secret, got %q", a.SecretKey)
	}
	// 内部 Get 应保留真实密钥
	full, err := s.Get(a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if full.SecretKey != "sk-secret" {
		t.Fatalf("expected real secret, got %q", full.SecretKey)
	}
}

func TestUpdateKeepsSecretWhenNotProvided(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create(sample())

	// 修改名称，不提供 secretKey
	upd := &model.Account{Name: "renamed", Endpoint: a.Endpoint, AccessKey: a.AccessKey}
	got, err := s.Update(a.ID, upd)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Name != "renamed" {
		t.Fatalf("name not updated: %q", got.Name)
	}
	full, _ := s.Get(a.ID)
	if full.SecretKey != "sk-secret" {
		t.Fatalf("secret should be preserved, got %q", full.SecretKey)
	}
}

func TestDelete(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create(sample())
	if err := s.Delete(a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(a.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPersistRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "accounts.json")
	s, _ := New(p)
	a, _ := s.Create(sample())
	_ = a

	// 重新从磁盘加载
	s2, err := New(p)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	list, err := s2.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 account, got %d", len(list))
	}
	if list[0].SecretKey != "******" {
		t.Fatalf("expected masked secret on reload, got %q", list[0].SecretKey)
	}
}

func TestAtomicWriteLeavesNoTemp(t *testing.T) {
	p := filepath.Join(t.TempDir(), "accounts.json")
	s, _ := New(p)
	_, _ = s.Create(sample())
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file should not remain after persist")
	}
}

func TestFilePermissionsTightened(t *testing.T) {
	p := filepath.Join(t.TempDir(), "accounts.json")
	s, _ := New(p)
	if _, err := s.Create(sample()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// 含明文密钥，必须仅属主可读写
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600, got %v", fi.Mode().Perm())
	}
}
