package config

import (
	"os"
	"strconv"
	"strings"
)

// loadDotEnvFile 从指定路径加载 KEY=VALUE 到环境变量（已存在的环境变量优先）。
// 支持注释行、引号与空行；实现为 30 行的极简解析器，不引入第三方依赖。
func loadDotEnvFile(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, exists := os.LookupEnv(k); !exists {
			_ = os.Setenv(k, v)
		}
	}
}

// Config 汇总服务端配置。所有项均可通过环境变量覆盖，并内置安全默认值。
type Config struct {
	Addr               string   // 监听地址，默认回环 127.0.0.1:8080（更安全）
	DataDir            string   // 数据目录，存放账号持久化文件
	StaticDir          string   // Web 静态资源目录
	Region             string   // 账号缺省 region
	Token              string   // 可选 API 鉴权 token；非空则要求 Bearer
	CORSOrigins        []string // CORS 白名单；空 = 仅同源 + localhost/tauri
	LogLevel           string   // debug|info|warn|error
	LogJSON            bool     // true = slog JSON（容器/生产更易采集）
	StoreDriver        string   // json|sqlite|encrypted，账号存储后端
	StoreKey           string   // encrypted 模式必填；Argon2id+盐派生（仅 S3C2）
	ShutdownTimeoutSec int      // SIGTERM 后等待活跃连接结束的最长时间（秒）
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	return n
}

// FromEnv 从环境变量构建配置；启动时先加载工作目录下的 .env（真实环境变量优先）。
func FromEnv() Config {
	loadDotEnvFile(".env")
	return Config{
		Addr:               envOr("S3C_ADDR", "127.0.0.1:8080"),
		DataDir:            envOr("S3C_DATA_DIR", "./data"),
		StaticDir:          envOr("S3C_STATIC_DIR", "./web/dist"),
		Region:             envOr("S3C_REGION", "us-east-1"),
		Token:              os.Getenv("S3C_TOKEN"),
		CORSOrigins:        splitList(envOr("S3C_CORS_ORIGINS", "")),
		LogLevel:           envOr("S3C_LOG_LEVEL", "info"),
		LogJSON:            envTruthy("S3C_LOG_JSON"),
		StoreDriver:        envOr("S3C_STORE_DRIVER", "json"),
		StoreKey:           os.Getenv("S3C_STORE_KEY"),
		ShutdownTimeoutSec: envOrInt("S3C_SHUTDOWN_TIMEOUT", 30),
	}
}

func envTruthy(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
