package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/weilai1949/s3clinet/server/internal/config"
	"github.com/weilai1949/s3clinet/server/internal/handler"
	"github.com/weilai1949/s3clinet/server/internal/store"
)

// version 由构建时注入（ldflags -X main.version=...）；缺省与发版号对齐，便于本地 go run/build。
var version = "v1.0.0-20260901182023"

// healthPath 供容器 HEALTHCHECK 使用。
const healthPath = "/api/health"

func main() {
	// 容器健康检查子命令：探测自身 /api/health，成功返回 0。
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		os.Exit(runHealthcheck())
	}

	cfg := config.FromEnv()

	level := parseLevel(cfg.LogLevel)
	var logHandler slog.Handler
	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogJSON {
		logHandler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		logHandler = slog.NewTextHandler(os.Stdout, opts)
	}
	logger := slog.New(logHandler)
	slog.SetDefault(logger)

	// Token 过短易被暴力猜测，给出提示。
	if cfg.Token != "" && len(cfg.Token) < 12 {
		logger.Warn("安全警告：S3C_TOKEN 过短（<12 字符），容易被暴力猜测，建议使用更长的随机值。")
	}
	// 非回环监听必须开启鉴权：否则任何能触达端口的人都能读写全部账号密钥与对象。
	if cfg.Token == "" && !isLoopbackAddr(cfg.Addr) {
		logger.Error("拒绝启动：监听非回环地址且未设置 S3C_TOKEN。请设置 S3C_TOKEN（建议 openssl rand -hex 32）或改用 127.0.0.1 监听。")
		os.Exit(1)
	}

	// 账号存储（json / sqlite / encrypted）
	st, err := store.Open(cfg.DataDir, cfg.StoreDriver, cfg.StoreKey)
	if err != nil {
		logger.Error("init store", "err", err)
		os.Exit(1)
	}
	defer func() {
		if err := st.Close(); err != nil {
			logger.Error("close store", "err", err)
		}
	}()

	h := handler.New(st, logger, cfg.StaticDir, cfg.CORSOrigins, cfg.Token, version)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           h.Routes(),
		ReadHeaderTimeout: 15 * time.Second,
		// 仅限制读取请求体阶段，抵御慢速请求体攻击；不设 WriteTimeout，
		// 避免截断大文件（proxy / download-zip）的流式输出。
		ReadTimeout: 60 * time.Second,
		IdleTimeout: 120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("s3clinet server",
			"version", version,
			"addr", cfg.Addr,
			"dataDir", cfg.DataDir,
			"store", cfg.StoreDriver,
			"staticDir", cfg.StaticDir,
			"auth", cfg.Token != "",
			"cors", corsSummary(cfg.CORSOrigins),
			"region", cfg.Region,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down...")
	h.Shutdown()
	shutdownTimeout := time.Duration(cfg.ShutdownTimeoutSec) * time.Second
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "err", err, "timeout", shutdownTimeout.String())
	} else {
		logger.Info("shutdown complete")
	}
}

// isLoopbackAddr 判断监听地址是否仅绑定本机回环（127.0.0.1/::1 或未指定 host）。
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return true // 无法解析时按回环判断，避免误报
	}
	return host == "127.0.0.1" || host == "::1" || host == "[::1]" || host == ""
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// runHealthcheck 探测自身健康端点，用于容器 HEALTHCHECK。
func runHealthcheck() int {
	addr := config.FromEnv().Addr
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = "127.0.0.1", "8080"
	}
	if host == "0.0.0.0" || host == "::" || host == "" {
		host = "127.0.0.1"
	}
	base := fmt.Sprintf("http://%s:%s%s", host, port, healthPath)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(base)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck error:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck status: %d\n", resp.StatusCode)
		return 1
	}
	return 0
}

func corsSummary(origins []string) string {
	if len(origins) == 0 {
		return "safe-default(localhost/tauri)"
	}
	return strings.Join(origins, ",")
}
