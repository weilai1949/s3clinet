package model

import "testing"

func TestSanitizedMasksSecret(t *testing.T) {
	a := &Account{ID: "1", Name: "x", SecretKey: "super-secret"}
	s := a.Sanitized()
	if s.SecretKey != MaskedSecret {
		t.Fatalf("secret = %q", s.SecretKey)
	}
	if a.SecretKey != "super-secret" {
		t.Fatal("original mutated")
	}
}

func TestIsMaskedSecret(t *testing.T) {
	if !IsMaskedSecret(MaskedSecret) {
		t.Fatal("expected masked")
	}
	if IsMaskedSecret("real-key") {
		t.Fatal("expected not masked")
	}
}

func TestSanitizedNil(t *testing.T) {
	if (*Account)(nil).Sanitized() != nil {
		t.Fatal("nil account should return nil")
	}
}

// TestBucketOrDefault 默认桶回退：空返回空串由调用方处理，非空原样。
func TestBucketOrDefault(t *testing.T) {
	if got := (&Account{}).BucketOrDefault(); got != "" {
		t.Fatalf("empty bucket = %q", got)
	}
	if got := (&Account{Bucket: "b"}).BucketOrDefault(); got != "b" {
		t.Fatalf("bucket = %q", got)
	}
}
