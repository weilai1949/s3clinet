package config

import (
	"errors"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr error
	}{
		{"loopback no token ok", Config{Addr: "127.0.0.1:8080", Token: ""}, nil},
		{"empty host no token ok", Config{Addr: ":8080", Token: ""}, nil},
		{"non-loopback no token rejected", Config{Addr: "0.0.0.0:8080", Token: ""}, ErrTokenRequiredNonLoopback},
		{"non-loopback with token ok", Config{Addr: "0.0.0.0:8080", Token: strings.Repeat("a", MinTokenLength)}, nil},
		{"short token rejected (loopback)", Config{Addr: "127.0.0.1:8080", Token: "short"}, ErrShortToken},
		{"short token rejected (non-loopback)", Config{Addr: "0.0.0.0:8080", Token: "short"}, ErrShortToken},
		{"multi token shortest applies", Config{Addr: "127.0.0.1:8080", Token: strings.Repeat("a", MinTokenLength) + ",short"}, ErrShortToken},
		{"multi token all long ok", Config{Addr: "127.0.0.1:8080", Token: strings.Repeat("a", MinTokenLength) + "," + strings.Repeat("b", MinTokenLength+5)}, nil},
		{"empty token piece ignored in shortest", Config{Addr: "127.0.0.1:8080", Token: "," + strings.Repeat("a", MinTokenLength)}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.cfg.Validate()
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() err=%v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want %v", c.wantErr)
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("Validate() err=%v, want wraps %v", err, c.wantErr)
			}
		})
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8080", true},
		{"::1:8080", true},
		{"[::1]:8080", true},
		{":8080", true},
		{"0.0.0.0:8080", false},
		{"192.168.1.1:8080", false},
		{"localhost:8080", false}, // 非 IP 视为非回环
	}
	for _, c := range cases {
		if got := IsLoopbackAddr(c.addr); got != c.want {
			t.Errorf("IsLoopbackAddr(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
}
