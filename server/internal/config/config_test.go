package config

import (
	"os"
	"reflect"
	"testing"
)

func TestSplitList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b ,c ", []string{"a", "b", "c"}},
		{"a,,b", []string{"a", "b"}},
		{",a,", []string{"a"}},
	}
	for _, c := range cases {
		if got := splitList(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitList(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestFromEnvDefaults 验证安全默认值（回环绑定、无鉴权、无 CORS 白名单）。
func TestFromEnvDefaults(t *testing.T) {
	for _, k := range []string{"S3C_ADDR", "S3C_DATA_DIR", "S3C_STATIC_DIR", "S3C_REGION", "S3C_TOKEN", "S3C_CORS_ORIGINS", "S3C_LOG_LEVEL", "S3C_LOG_JSON", "S3C_STORE_DRIVER", "S3C_STORE_KEY", "S3C_SHUTDOWN_TIMEOUT"} {
		t.Setenv(k, "")
	}
	cfg := FromEnv()
	if cfg.Addr != "127.0.0.1:8080" {
		t.Errorf("Addr = %q, want 127.0.0.1:8080", cfg.Addr)
	}
	if cfg.DataDir != "./data" {
		t.Errorf("DataDir = %q, want ./data", cfg.DataDir)
	}
	if cfg.StaticDir != "./web/dist" {
		t.Errorf("StaticDir = %q, want ./web/dist", cfg.StaticDir)
	}
	if cfg.Region != "us-east-1" {
		t.Errorf("Region = %q, want us-east-1", cfg.Region)
	}
	if cfg.Token != "" {
		t.Errorf("Token = %q, want empty", cfg.Token)
	}
	if len(cfg.CORSOrigins) != 0 {
		t.Errorf("CORSOrigins = %v, want empty", cfg.CORSOrigins)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.LogJSON {
		t.Errorf("LogJSON = true, want false")
	}
	if cfg.StoreDriver != "json" {
		t.Errorf("StoreDriver = %q, want json", cfg.StoreDriver)
	}
	if cfg.ShutdownTimeoutSec != 30 {
		t.Errorf("ShutdownTimeoutSec = %d, want 30", cfg.ShutdownTimeoutSec)
	}
}

func TestFromEnvOverrides(t *testing.T) {
	t.Setenv("S3C_ADDR", "0.0.0.0:9000")
	t.Setenv("S3C_DATA_DIR", "/tmp/s3c")
	t.Setenv("S3C_STATIC_DIR", "/srv/web")
	t.Setenv("S3C_REGION", "cn-north-1")
	t.Setenv("S3C_TOKEN", "topsecret")
	t.Setenv("S3C_CORS_ORIGINS", " https://a.example, http://b.example , ")
	t.Setenv("S3C_LOG_LEVEL", "debug")
	cfg := FromEnv()
	if cfg.Addr != "0.0.0.0:9000" {
		t.Errorf("Addr = %q, want 0.0.0.0:9000", cfg.Addr)
	}
	if cfg.Token != "topsecret" {
		t.Errorf("Token = %q, want topsecret", cfg.Token)
	}
	want := []string{"https://a.example", "http://b.example"}
	if !reflect.DeepEqual(cfg.CORSOrigins, want) {
		t.Errorf("CORSOrigins = %v, want %v", cfg.CORSOrigins, want)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.LogLevel)
	}
}

func TestLoadDotEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/.env"
	content := "# comment\nS3C_TOKEN=from-dotenv\nS3C_REGION=\"cn-beijing\"\n\nBADLINE\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	loadDotEnvFile(path)
	if got := os.Getenv("S3C_TOKEN"); got != "from-dotenv" {
		t.Errorf("S3C_TOKEN = %q, want from-dotenv", got)
	}
	if got := os.Getenv("S3C_REGION"); got != "cn-beijing" {
		t.Errorf("S3C_REGION = %q, want cn-beijing", got)
	}
	// 已存在的环境变量优先于 .env
	t.Setenv("S3C_TOKEN", "real-env")
	loadDotEnvFile(path)
	if got := os.Getenv("S3C_TOKEN"); got != "real-env" {
		t.Errorf("S3C_TOKEN = %q, want real-env (env wins)", got)
	}
}
