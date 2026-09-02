package handler

import (
	"context"
	"io"
	"net/http"
	"time"
)

// 流式响应（proxy / download-zip / migrate）并发与写超时保护。
const (
	maxConcurrentStreams = 32
	// 滚动空闲写超时：每次成功写出后刷新；慢网大文件不会因绝对截止被误杀。
	streamIdleTimeout = 5 * time.Minute
)

var streamSlots = make(chan struct{}, maxConcurrentStreams)

// withStreamLimit 限制同时进行的流式/长耗时请求数；饱和时返回 503。
func (h *Handler) withStreamLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case streamSlots <- struct{}{}:
			defer func() { <-streamSlots }()
		case <-r.Context().Done():
			return
		default:
			h.writeErr(w, http.StatusServiceUnavailable, "too many concurrent streaming requests")
			return
		}
		next(w, r)
	}
}

// beginStreamResponse 为流式响应设置初始写超时（大文件下载），在 WriteHeader 之前调用。
// 依赖 statusRecorder.Unwrap，使 ResponseController 能触达底层 conn。
func beginStreamResponse(w http.ResponseWriter) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(streamIdleTimeout))
}

// copyStream 流式复制：客户端断开时停止读源，并在每次写出后刷新空闲写超时。
func copyStream(w http.ResponseWriter, r *http.Request, src io.Reader) {
	rc := http.NewResponseController(w)
	dw := &deadlineWriter{w: w, rc: rc, idle: streamIdleTimeout}
	_, _ = io.Copy(dw, &contextReader{ctx: r.Context(), r: src})
}

type deadlineWriter struct {
	w    http.ResponseWriter
	rc   *http.ResponseController
	idle time.Duration
}

func (d *deadlineWriter) Write(p []byte) (int, error) {
	_ = d.rc.SetWriteDeadline(time.Now().Add(d.idle))
	return d.w.Write(p)
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *contextReader) Read(p []byte) (int, error) {
	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	default:
		return c.r.Read(p)
	}
}
