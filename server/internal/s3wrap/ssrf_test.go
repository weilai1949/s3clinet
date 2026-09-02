package s3wrap

import (
	"net"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"169.254.169.254", true},
		{"169.254.0.1", true},
		{"100.100.100.200", true},
		{"127.0.0.1", false},
		{"10.0.0.1", false},
		{"192.168.1.1", false},
		{"8.8.8.8", false},
	}
	for _, c := range cases {
		if got := isBlockedIP(net.ParseIP(c.ip)); got != c.want {
			t.Errorf("isBlockedIP(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
}

func TestValidateEndpoint(t *testing.T) {
	if err := ValidateEndpoint("http://metadata.google.internal/"); err == nil {
		t.Fatal("expected metadata hostname blocked")
	}
	if err := ValidateEndpoint("http://169.254.169.254/latest/meta-data/"); err == nil {
		t.Fatal("expected IMDS IP blocked")
	}
	if err := ValidateEndpoint("http://127.0.0.1:9000"); err != nil {
		t.Fatalf("loopback should be allowed: %v", err)
	}
	if err := ValidateEndpoint(""); err != nil {
		t.Fatalf("empty ok: %v", err)
	}
}

func TestIsBlockedHostname(t *testing.T) {
	if !isBlockedHostname("metadata.google.internal") {
		t.Fatal("expected blocked")
	}
	if isBlockedHostname("minio.local") {
		t.Fatal("minio.local should not be blocked by hostname alone")
	}
}
