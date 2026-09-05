package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/weilai1949/s3clinet/server/internal/service"
)

const (
	migrateJobTimeout = service.JobTimeout
	sseHeartbeatEvery = service.SSEHeartbeatEvery
)

// migrateAsync 启动异步迁移，立即返回 jobId；进度通过 SSE 订阅。
func (h *Handler) migrateAsync(w http.ResponseWriter, r *http.Request) {
	req, src, dst, srcClient, dstClient, srcBucket, targetBucket, ok := h.parseMigrateRequest(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), migrateJobTimeout)
	job := h.migrateJobs.Create(len(req.SourceKeys), cancel)
	sameEP := service.SameEndpoint(src.Endpoint, dst.Endpoint)
	go func() {
		defer cancel()
		out := service.MigrateKeys(ctx, srcClient, dstClient, srcBucket, targetBucket, req.SourceKeys, req.TargetPrefix, sameEP, 4, func(p service.Progress) {
			job.Emit(service.ProgressFrom(p))
		})
		// RunBatch 在 ctx 取消时通过 s3wrap 错误为每个被中断的 key 记一条错误，
		// out.LastError 必非空，故无需为取消场景提供默认文案。
		status := "done"
		if ctx.Err() != nil {
			status = "cancelled"
		}
		job.Finish(service.ResultFromBatch(out), status)
	}()
	h.writeJSON(w, http.StatusAccepted, map[string]any{"jobId": job.ID, "total": job.Total})
}

// migrateJobCancel 取消运行中的异步迁移。
func (h *Handler) migrateJobCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, ok := h.migrateJobs.Get(id)
	if !ok {
		h.writeErr(w, http.StatusNotFound, "job not found")
		return
	}
	cancelled, alreadyDone := job.Cancel()
	if alreadyDone {
		h.writeJSON(w, http.StatusOK, map[string]any{"jobId": id, "cancelled": false, "done": true})
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"jobId": id, "cancelled": cancelled})
}

// migrateJobStatus 查询异步迁移任务状态（轮询备用）。
func (h *Handler) migrateJobStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, ok := h.migrateJobs.Get(id)
	if !ok {
		h.writeErr(w, http.StatusNotFound, "job not found")
		return
	}
	progress, result, done := job.Snapshot()
	resp := map[string]any{
		"jobId":    id,
		"done":     done,
		"progress": progress,
	}
	if done {
		resp["result"] = migrateResultJSON(migrateResult{
			Migrated: result.Migrated, Failed: result.Failed,
			LastError: result.LastError, FailKeys: result.FailKeys,
		})
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// migrateJobEvents SSE 推送迁移进度（text/event-stream），含心跳与客户端断开感知。
// 写超时复用 streamIdleTimeout（每次成功写出后刷新）：慢网/慢读客户端不会让连接
// 无限挂着，活跃流量也不会被误杀。
func (h *Handler) migrateJobEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, ok := h.migrateJobs.Get(id)
	if !ok {
		h.writeErr(w, http.StatusNotFound, "job not found")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeErr(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	ch := job.Subscribe()
	defer job.Unsubscribe(ch)
	// 初始写超时：依赖 statusRecorder.Unwrap 触达底层 conn。
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(streamIdleTimeout))

	writeSSE := func(event string, payload []byte) bool {
		if event != "" {
			if _, err := w.Write([]byte("event: " + event + "\n")); err != nil {
				return false
			}
		}
		if _, err := w.Write([]byte("data: ")); err != nil {
			return false
		}
		if _, err := w.Write(payload); err != nil {
			return false
		}
		if _, err := w.Write([]byte("\n\n")); err != nil {
			return false
		}
		_ = rc.SetWriteDeadline(time.Now().Add(streamIdleTimeout))
		flusher.Flush()
		return true
	}

	ticker := time.NewTicker(sseHeartbeatEvery)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !writeSSE("ping", []byte(`{"ok":true}`)) {
				return
			}
		case p := <-ch:
			// job.Finish 先推送终态再关闭 channel；通道关闭永远在循环 return 之后。
			b, _ := json.Marshal(p)
			if !writeSSE("", b) {
				return
			}
			if p.Status == "done" || p.Status == "cancelled" {
				return
			}
		}
	}
}
