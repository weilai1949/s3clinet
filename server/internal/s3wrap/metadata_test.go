package s3wrap

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateUserMetadata(t *testing.T) {
	mk := func(n int) string { return strings.Repeat("a", n) }
	cases := []struct {
		name    string
		in      map[string]string
		wantErr bool
	}{
		{"nil", nil, false},
		{"empty", map[string]string{}, false},
		{"ok", map[string]string{"env": "prod", "owner": "weilai"}, false},
		{"empty key", map[string]string{"": "x"}, true},
		{"key too long", map[string]string{mk(MaxUserMetaKeyLen + 1): "v"}, true},
		{"value too long", map[string]string{"k": mk(MaxUserMetaValueLen + 1)}, true},
		{"non-ascii key", map[string]string{"环境": "prod"}, true},
		{"non-printable key", map[string]string{"a\x01b": "v"}, true},
		{"invalid utf8 value", map[string]string{"k": string([]byte{0xff, 0xfe, 0xfd})}, true},
		{"total too big", map[string]string{"k1": mk(1000), "k2": mk(1000), "k3": mk(200)}, true},
		{"total under limit", map[string]string{"k1": mk(100), "k2": mk(100)}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateUserMetadata(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("want err, got nil")
				}
				if !errors.Is(err, ErrUserMetadataInvalid) {
					t.Fatalf("err = %v, want wraps ErrUserMetadataInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("want nil, got %v", err)
			}
		})
	}
}
