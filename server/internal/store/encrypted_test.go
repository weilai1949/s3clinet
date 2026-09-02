package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weilai1949/s3clinet/server/internal/model"
)

func TestEncryptedStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	encPath := filepath.Join(dir, "accounts.json.enc")
	s, err := NewEncrypted(encPath, "test-secret-key")
	if err != nil {
		t.Fatalf("NewEncrypted: %v", err)
	}
	created, err := s.Create(&model.Account{
		Name: "a", Endpoint: "http://127.0.0.1:9000", AccessKey: "ak", SecretKey: "sk", Region: "us-east-1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	s2, err := NewEncrypted(encPath, "test-secret-key")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := s2.Get(created.ID)
	if err != nil || got.SecretKey != "sk" {
		t.Fatalf("get = %+v err=%v", got, err)
	}
	raw, err := os.ReadFile(encPath)
	if err != nil || len(raw) < 4 || string(raw[:4]) != string(encMagicV2) {
		t.Fatalf("expected v2 magic header, err=%v len=%d", err, len(raw))
	}
}

func TestEncryptedRejectsNonS3C2(t *testing.T) {
	dir := t.TempDir()
	encPath := filepath.Join(dir, "accounts.json.enc")
	for _, blob := range [][]byte{
		[]byte("not-a-valid-enc-file"),
		append([]byte("S3C1"), make([]byte, 32)...),
	} {
		if err := os.WriteFile(encPath, blob, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := NewEncrypted(encPath, "any-key")
		if err == nil {
			t.Fatalf("expected error for non-S3C2 blob %q", blob[:min(8, len(blob))])
		}
		if !strings.Contains(err.Error(), "S3C2") && !strings.Contains(err.Error(), "too short") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestOpenJSONDefault(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir, "json", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.(*Store); !ok {
		t.Fatalf("expected *Store, got %T", st)
	}
}
