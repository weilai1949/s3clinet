package store

import (
	"path/filepath"
	"testing"

	"github.com/weilai1949/s3clinet/server/internal/model"
)

func TestSQLiteStoreCRUD(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "accounts.db")
	s, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	created, err := s.Create(&model.Account{
		Name: "sqlite", Endpoint: "http://127.0.0.1:9000", AccessKey: "ak", SecretKey: "sk", Region: "us-east-1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.Get(created.ID)
	if err != nil || got.SecretKey != "sk" {
		t.Fatalf("get: %+v err=%v", got, err)
	}
	updated, err := s.Update(created.ID, &model.Account{
		Name: "renamed", Endpoint: got.Endpoint, AccessKey: got.AccessKey, SecretKey: model.MaskedSecret,
	})
	if err != nil || updated.Name != "renamed" {
		t.Fatalf("update: %+v err=%v", updated, err)
	}
	if err := s.Delete(created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(created.ID); err != ErrNotFound {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestOpenSQLiteDriver(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir, "sqlite", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.(*SQLiteStore); !ok {
		t.Fatalf("expected *SQLiteStore, got %T", st)
	}
}
