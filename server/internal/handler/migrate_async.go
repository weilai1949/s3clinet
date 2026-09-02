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
		status := "done"
		if ctx.Err() != nil {
			status = "cancelled"
			if out.LastError == "" {
				out.LastError = "migration cancelled or timed out"
			}
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
		case p, open := <-ch:
			if !open {
				progress, _, done := job.Snapshot()
				if done {
					b, _ := json.Marshal(progress)
					_ = writeSSE("", b)
				}
				return
			}
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
