package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestEnvOrInt 表驱动覆盖 envOrInt 的全部分支：
// 未设置/空串取默认；非数字、小于 1 取默认；合法值正常解析。
func TestEnvOrInt(t *testing.T) {
	cases := []struct {
		name string
		val  string
		set  bool
		def  int
		want int
	}{
		{"unset uses default", "", false, 30, 30},
		{"empty uses default", "", true, 30, 30},
		{"non numeric uses default", "abc", true, 30, 30},
		{"mixed uses default", "5x", true, 30, 30},
		{"zero uses default", "0", true, 30, 30},
		{"negative uses default", "-3", true, 7, 7},
		{"one is valid", "1", true, 30, 1},
		{"positive parses", "42", true, 30, 42},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.set {
				t.Setenv("S3C_TEST_TIMEOUT", c.val)
			} else {
				t.Setenv("S3C_TEST_TIMEOUT", "") // 显式置空，隔离外部环境
			}
			if got := envOrInt("S3C_TEST_TIMEOUT", c.def); got != c.want {
				t.Errorf("envOrInt(%q, %d) = %d, want %d", c.val, c.def, got, c.want)
			}
		})
	}
}

// TestFromEnvShutdownTimeout 覆盖 FromEnv 中 S3C_SHUTDOWN_TIMEOUT 的默认/非法/合法三种取值。
func TestFromEnvShutdownTimeout(t *testing.T) {
	cases := []struct {
		name string
		val  string
		want int
	}{
		{"default", "", 30},
		{"invalid falls back", "abc", 30},
		{"zero falls back", "0", 30},
		{"valid", "5", 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("S3C_SHUTDOWN_TIMEOUT", c.val)
			if got := FromEnv().ShutdownTimeoutSec; got != c.want {
				t.Errorf("ShutdownTimeoutSec = %d, want %d", got, c.want)
			}
		})
	}
}

// TestLoadDotEnvFileEmptyKey 键为空的行（"=value"）应被跳过；正常键照常写入环境变量。
func TestLoadDotEnvFileEmptyKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# 注释\n=v1\n   =v2\nS3C_STORE_DRIVER=sqlite\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	loadDotEnvFile(path)
	if got := os.Getenv("S3C_STORE_DRIVER"); got != "sqlite" {
		t.Errorf("S3C_STORE_DRIVER = %q, want sqlite", got)
	}
}

// TestLoadDotEnvFileMissingFile 文件不存在时应静默返回（不 panic、不改环境）。
func TestLoadDotEnvFileMissingFile(t *testing.T) {
	t.Setenv("S3C_TOKEN", "")
	loadDotEnvFile(filepath.Join(t.TempDir(), "definitely-missing.env"))
	if got := os.Getenv("S3C_TOKEN"); got != "" {
		t.Errorf("S3C_TOKEN = %q, want unchanged empty", got)
	}
}

// TestFromEnvLogJSONVariants 表驱动验证 S3C_LOG_JSON 的真值判定（大小写与取值变体）。
func TestFromEnvLogJSONVariants(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"", false},
		{"0", false},
		{"no", false},
		{"false", false},
		{"1", true},
		{"TRUE", true},
		{"Yes", true},
		{"on", true},
		{"true ", true}, // 带空格也应视为真
	}
	for _, c := range cases {
		t.Run("S3C_LOG_JSON="+c.val, func(t *testing.T) {
			t.Setenv("S3C_LOG_JSON", c.val)
			if got := FromEnv().LogJSON; got != c.want {
				t.Errorf("LogJSON(%q) = %v, want %v", c.val, got, c.want)
			}
		})
	}
}

// TestFromEnvAllFields 单条用例覆盖全部环境变量与解析结果（含 StoreKey 与 CORS 分隔）。
func TestFromEnvAllFields(t *testing.T) {
	t.Setenv("S3C_ADDR", "0.0.0.0:8081")
	t.Setenv("S3C_DATA_DIR", "/var/lib/s3c")
	t.Setenv("S3C_STATIC_DIR", "/opt/web")
	t.Setenv("S3C_REGION", "cn-hangzhou")
	t.Setenv("S3C_TOKEN", "tk")
	t.Setenv("S3C_CORS_ORIGINS", "http://a,http://b")
	t.Setenv("S3C_LOG_LEVEL", "warn")
	t.Setenv("S3C_LOG_JSON", "on")
	t.Setenv("S3C_STORE_DRIVER", "sqlite")
	t.Setenv("S3C_STORE_KEY", "k3y")
	t.Setenv("S3C_SHUTDOWN_TIMEOUT", "12")
	cfg := FromEnv()
	if cfg.Addr != "0.0.0.0:8081" {
		t.Errorf("Addr = %q", cfg.Addr)
	}
	if cfg.DataDir != "/var/lib/s3c" || cfg.StaticDir != "/opt/web" || cfg.Region != "cn-hangzhou" {
		t.Errorf("basic fields = %+v", cfg)
	}
	if cfg.Token != "tk" || cfg.StoreKey != "k3y" || cfg.StoreDriver != "sqlite" {
		t.Errorf("auth/store fields = %+v", cfg)
	}
	if cfg.LogLevel != "warn" || !cfg.LogJSON {
		t.Errorf("log fields = %q/%v", cfg.LogLevel, cfg.LogJSON)
	}
	if cfg.ShutdownTimeoutSec != 12 {
		t.Errorf("ShutdownTimeoutSec = %d", cfg.ShutdownTimeoutSec)
	}
	if !reflect.DeepEqual(cfg.CORSOrigins, []string{"http://a", "http://b"}) {
		t.Errorf("CORSOrigins = %v", cfg.CORSOrigins)
	}
}
