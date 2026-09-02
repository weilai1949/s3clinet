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
