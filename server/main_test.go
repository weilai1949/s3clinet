package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// TestParseLevel 日志级别解析：全部分支表驱动。
func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"info", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"DEBUG", slog.LevelInfo}, // 大小写敏感，未识别回退 info
	}
	for _, c := range cases {
		if got := parseLevel(c.in); got != c.want {
			t.Fatalf("parseLevel(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestIsLoopbackAddr 回环判定：显式回环、无 host、无法解析均按回环处理。
func TestIsLoopbackAddr(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"127.0.0.1:8080", true},
		{"127.0.0.1:", true},
		{"[::1]:8080", true},
		{"::1:8080", true},
		{":8080", true}, // 未指定 host = 全接口，但解析 host 为空串按回环
		{"not an addr", true},
		{"0.0.0.0:8080", false},
		{"192.168.1.5:8080", false},
	}
	for _, c := range cases {
		if got := isLoopbackAddr(c.in); got != c.want {
			t.Fatalf("isLoopbackAddr(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestCorsSummary CORS 汇总：空缺省安全提示，非空逗号拼接。
func TestCorsSummary(t *testing.T) {
	if got := corsSummary(nil); got != "safe-default(localhost/tauri)" {
		t.Fatalf("corsSummary(nil) = %q", got)
	}
	if got := corsSummary([]string{"http://a", "http://b"}); got != "http://a,http://b" {
		t.Fatalf("corsSummary = %q", got)
	}
}

// TestRunHealthcheck 健康检查三态：200→0、非 200→1、连接失败→1。
func TestRunHealthcheck(t *testing.T) {
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(okSrv.Close)
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(badSrv.Close)

	cases := []struct {
		name string
		addr string
		want int
	}{
		{"ok", okSrv.Listener.Addr().String(), 0},
		{"non-200", badSrv.Listener.Addr().String(), 1},
		{"refused", "127.0.0.1:1", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("S3C_ADDR", c.addr)
			if got := runHealthcheck(); got != c.want {
				t.Fatalf("runHealthcheck(addr=%q) = %d, want %d", c.addr, got, c.want)
			}
		})
	}
}

// reserveLoopbackPort 申请一个回环空闲端口（先绑再放，缓解竞态）。
func reserveLoopbackPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close probe listener: %v", err)
	}
	return addr
}

// pollHealth 轮询 /api/health 直到 200 或超时。
func pollHealth(t *testing.T, url string) bool {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// TestRunServerStartsAndShutsDownGracefully runServer 直接驱动：健康端点可访问、
// ctx 取消后优雅退出返回 0、端口释放。
func TestRunServerStartsAndShutsDownGracefully(t *testing.T) {
	addr := reserveLoopbackPort(t)
	t.Setenv("S3C_ADDR", addr)
	t.Setenv("S3C_DATA_DIR", t.TempDir())
	t.Setenv("S3C_STORE_DRIVER", "json")
	t.Setenv("S3C_TOKEN", "unit-test-token-0123456789")
	t.Setenv("S3C_SHUTDOWN_TIMEOUT", "5")

	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		code := runServer(ctx)
		done <- result{code: code}
	}()

	base := fmt.Sprintf("http://%s/api/health", addr)
	if !pollHealth(t, base) {
		cancel()
		<-done
		t.Fatalf("server did not become healthy at %s", base)
	}
	cancel()
	res := <-done
	if res.code != 0 {
		t.Fatalf("runServer return = %d, want 0", res.code)
	}
	// 端口应已释放：立刻重绑成功即视为关闭完成。
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("port %s should be released after shutdown: %v", addr, err)
	}
	_ = l.Close()
}

// TestRunServerRejectsNonLoopbackWithoutToken 非回环监听 + 无 token → 拒绝启动返回 1。
func TestRunServerRejectsNonLoopbackWithoutToken(t *testing.T) {
	t.Setenv("S3C_ADDR", "0.0.0.0:0")
	t.Setenv("S3C_TOKEN", "")
	t.Setenv("S3C_DATA_DIR", t.TempDir())
	if code := runServer(context.Background()); code != 1 {
		t.Fatalf("runServer(non-loopback, no token) = %d, want 1", code)
	}
}

// TestRunServerStoreInitFails 存储初始化失败（encrypted 缺 key）→ 返回 1。
func TestRunServerStoreInitFails(t *testing.T) {
	addr := reserveLoopbackPort(t)
	t.Setenv("S3C_ADDR", addr)
	t.Setenv("S3C_TOKEN", "unit-test-token-0123456789")
	t.Setenv("S3C_DATA_DIR", t.TempDir())
	t.Setenv("S3C_STORE_DRIVER", "encrypted")
	t.Setenv("S3C_STORE_KEY", "")
	if code := runServer(context.Background()); code != 1 {
		t.Fatalf("runServer(store init failure) = %d, want 1", code)
	}
}

// TestRunServerListenBindFailure 端口被占用 → ListenAndServe 失败进入优雅关闭并返回 0
// （服务错误路径会触发内部 cancel，使 runServer 走完 shutdown 流程）。
func TestRunServerListenBindFailure(t *testing.T) {
	// 先占用端口，使 ListenAndServe 绑定失败。
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind blocker: %v", err)
	}
	defer l.Close()
	t.Setenv("S3C_ADDR", l.Addr().String())
	t.Setenv("S3C_DATA_DIR", t.TempDir())
	t.Setenv("S3C_STORE_DRIVER", "json")
	t.Setenv("S3C_TOKEN", "unit-test-token-0123456789")
	code := runServer(context.Background())
	if code != 0 {
		t.Fatalf("runServer(bind failure) = %d, want 0（失败也应走优雅关闭返回 0）", code)
	}
}

// TestMainChild 子进程入口：按 S3C_MAIN_CHILD 模式直接调用 main()（内部 os.Exit）。
func TestMainChild(t *testing.T) {
	mode := os.Getenv("S3C_MAIN_CHILD")
	if mode == "" {
		t.Skip("child only")
	}
	switch mode {
	case "healthcheck":
		os.Args = []string{os.Args[0], "-healthcheck"}
	}
	main()
}

// childCmd 构造子进程（继承环境 + 注入变量），超时强杀兜底。
func childCmd(t *testing.T, mode string, env map[string]string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), "S3C_MAIN_CHILD="+mode)
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return cmd
}

// TestMainHealthcheckSubprocess 用 fork 方式驱动 main() 的 -healthcheck 分支。
func TestMainHealthcheckSubprocess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fork 模式依赖类 unix 信号与进程语义")
	}
	t.Run("ok", func(t *testing.T) {
		okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(okSrv.Close)
		cmd := childCmd(t, "healthcheck", map[string]string{"S3C_ADDR": okSrv.Listener.Addr().String()})
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("healthcheck child should exit 0, got err=%v out=%s", err, out)
		}
	})
	t.Run("refused", func(t *testing.T) {
		cmd := childCmd(t, "healthcheck", map[string]string{"S3C_ADDR": "127.0.0.1:1"})
		if err := cmd.Run(); err == nil {
			t.Fatal("healthcheck against refused port should exit non-zero")
		}
	})
}

// TestMainServerSubprocess fork 子进程跑完整 main()：启动→健康→SIGTERM→优雅退出 0。
func TestMainServerSubprocess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("依赖 SIGTERM")
	}
	addr := reserveLoopbackPort(t)
	cmd := childCmd(t, "server", map[string]string{
		"S3C_ADDR":             addr,
		"S3C_DATA_DIR":         t.TempDir(),
		"S3C_TOKEN":            "unit-test-token-0123456789",
		"S3C_STORE_DRIVER":     "json",
		"S3C_SHUTDOWN_TIMEOUT": "5",
	})
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })

	base := fmt.Sprintf("http://%s/api/health", addr)
	if !pollHealth(t, base) {
		_ = cmd.Process.Signal(syscall.SIGKILL)
		t.Fatalf("child server not healthy at %s", base)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal child: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			var ee *exec.ExitError
			if !errors.As(err, &ee) || ee.ExitCode() != 0 {
				t.Fatalf("child should exit 0 after SIGTERM, got %v", err)
			}
		}
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("child did not exit after SIGTERM")
	}
}

// TestRunServerJSONLog S3C_LOG_JSON 开关：JSON handler 分支正常启停。
func TestRunServerJSONLog(t *testing.T) {
	addr := reserveLoopbackPort(t)
	t.Setenv("S3C_ADDR", addr)
	t.Setenv("S3C_DATA_DIR", t.TempDir())
	t.Setenv("S3C_STORE_DRIVER", "json")
	t.Setenv("S3C_TOKEN", "unit-test-token-0123456789")
	t.Setenv("S3C_LOG_JSON", "1")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- runServer(ctx) }()
	if !pollHealth(t, fmt.Sprintf("http://%s/api/health", addr)) {
		cancel()
		<-done
		t.Fatal("server not healthy")
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("runServer(json log) = %d, want 0", code)
	}
}

// TestRunHealthcheckBadAddr 地址缺端口：SplitHostPort 失败回退 127.0.0.1:8080（探测失败→1）。
func TestRunHealthcheckBadAddr(t *testing.T) {
	t.Setenv("S3C_ADDR", "no-port-in-here")
	if got := runHealthcheck(); got != 1 {
		t.Fatalf("runHealthcheck(bad addr) = %d, want 1", got)
	}
}

// TestRunServerShortTokenWarning 短 token 仅打印警告，不阻止启动。
func TestRunServerShortTokenWarning(t *testing.T) {
	addr := reserveLoopbackPort(t)
	t.Setenv("S3C_ADDR", addr)
	t.Setenv("S3C_TOKEN", "short")
	t.Setenv("S3C_DATA_DIR", t.TempDir())
	t.Setenv("S3C_STORE_DRIVER", "json")
	t.Setenv("S3C_LOG_FORMAT", "text")
	// 不消费 health 端点直接 cancel 触发优雅关闭
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if code := runServer(ctx); code != 0 {
		t.Fatalf("runServer(short token) = %d, want 0", code)
	}
}
