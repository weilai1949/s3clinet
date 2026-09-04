package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/weilai1949/s3clinet/server/internal/model"
	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
	"github.com/weilai1949/s3clinet/server/internal/service"
)

// copyObject 复制单个对象到目标桶/目标 key（不删除源）。
func (h *Handler) copyObject(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket    string `json:"bucket"`
		Key       string `json:"key"`
		NewKey    string `json:"newKey"`
		NewBucket string `json:"newBucket"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	if req.Key == "" || req.NewKey == "" {
		h.writeErr(w, http.StatusBadRequest, "key and newKey are required")
		return
	}
	bucket := req.Bucket
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	targetBucket := req.NewBucket
	if targetBucket == "" {
		targetBucket = bucket
	}
	if targetBucket == bucket && req.NewKey == req.Key {
		h.writeErr(w, http.StatusBadRequest, "newKey must differ from key in the same bucket")
		return
	}
	if err := client.CopyObject(r.Context(), bucket, req.Key, targetBucket, req.NewKey); err != nil {
		h.writeInternalErr(w, err, "copy operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"copied": req.NewKey, "bucket": targetBucket})
}

// copyMany 批量复制/移动所选文件到目标桶 + 前缀。
func (h *Handler) copyMany(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket       string   `json:"bucket"`
		TargetBucket string   `json:"targetBucket"`
		TargetPrefix string   `json:"targetPrefix"`
		Keys         []string `json:"keys"`
		DeleteSource bool     `json:"deleteSource"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	if len(req.Keys) == 0 {
		h.writeErr(w, http.StatusBadRequest, "keys are required")
		return
	}
	if len(req.Keys) > maxBatchKeys {
		h.writeErr(w, http.StatusBadRequest, "too many keys (max 10000 per request)")
		return
	}
	bucket := req.Bucket
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	targetBucket := req.TargetBucket
	if targetBucket == "" {
		targetBucket = bucket
	}
	pairs := make([][2]string, 0, len(req.Keys))
	for _, k := range req.Keys {
		if k == "" {
			continue
		}
		pairs = append(pairs, [2]string{k, service.BaseKey(k, req.TargetPrefix)})
	}
	if !req.DeleteSource {
		out := service.CopyKeys(r.Context(), client, bucket, targetBucket, pairs, 4, nil)
		h.writeJSON(w, http.StatusOK, copyBatchJSON(out, len(pairs), false))
		return
	}
	out := copyKeysThenDelete(r.Context(), client, bucket, targetBucket, pairs, 4, nil)
	copied, failed := out.OK, out.Failed
	failMsg := out.LastError
	failKeys := out.FailKeys
	resp := map[string]any{"copied": copied, "failed": failed, "total": len(pairs)}
	if failMsg != "" {
		resp["lastError"] = failMsg
	}
	if len(failKeys) > 0 {
		if len(failKeys) > 200 {
			failKeys = failKeys[:200]
		}
		resp["failedKeys"] = failKeys
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// copyManyAsync 异步批量复制/移动：立即返回 jobId，进度复用 migrate jobs SSE。
func (h *Handler) copyManyAsync(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket       string   `json:"bucket"`
		TargetBucket string   `json:"targetBucket"`
		TargetPrefix string   `json:"targetPrefix"`
		Keys         []string `json:"keys"`
		DeleteSource bool     `json:"deleteSource"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	if len(req.Keys) == 0 {
		h.writeErr(w, http.StatusBadRequest, "keys are required")
		return
	}
	if len(req.Keys) > maxBatchKeys {
		h.writeErr(w, http.StatusBadRequest, "too many keys (max 10000 per request)")
		return
	}
	bucket := req.Bucket
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	targetBucket := req.TargetBucket
	if targetBucket == "" {
		targetBucket = bucket
	}
	pairs := make([][2]string, 0, len(req.Keys))
	for _, k := range req.Keys {
		if k == "" {
			continue
		}
		pairs = append(pairs, [2]string{k, service.BaseKey(k, req.TargetPrefix)})
	}
	ctx, cancel := context.WithTimeout(context.Background(), migrateJobTimeout)
	job := h.migrateJobs.Create(len(pairs), cancel)
	go func() {
		defer cancel()
		var out service.BatchResult
		if !req.DeleteSource {
			out = service.CopyKeys(ctx, client, bucket, targetBucket, pairs, 4, func(p service.Progress) {
				job.Emit(service.ProgressFrom(p))
			})
		} else {
			out = copyKeysThenDelete(ctx, client, bucket, targetBucket, pairs, 4, func(p service.Progress) {
				job.Emit(service.ProgressFrom(p))
			})
		}
		status := "done"
		if ctx.Err() != nil {
			status = "cancelled"
		}
		job.Finish(service.ResultFromBatch(out), status)
	}()
	h.writeJSON(w, http.StatusAccepted, map[string]any{"jobId": job.ID, "total": job.Total})
}

// copyKeysThenDelete 复制成功后再删源（移动），进度回调与 CopyKeys 同形。
func copyKeysThenDelete(
	ctx context.Context, client *s3wrap.Client, srcBucket, dstBucket string,
	pairs [][2]string, workers int, onProgress func(service.Progress),
) service.BatchResult {
	return service.RunBatch(ctx, pairs, workers,
		func(p [2]string) string { return p[0] },
		func(ctx context.Context, p [2]string) error {
			if err := client.CopyObject(ctx, srcBucket, p[0], dstBucket, p[1]); err != nil {
				return err
			}
			if err := client.DeleteObject(ctx, srcBucket, p[0]); err != nil {
				return fmt.Errorf("copied %s but failed to delete source: %w", p[0], err)
			}
			return nil
		},
		onProgress)
}

type copyPrefixReq struct {
	Bucket       string `json:"bucket"`
	Prefix       string `json:"prefix"`
	TargetBucket string `json:"targetBucket"`
	TargetPrefix string `json:"targetPrefix"`
}

func (h *Handler) parseCopyPrefix(w http.ResponseWriter, r *http.Request) (
	req copyPrefixReq, client *s3wrap.Client, bucket, targetBucket string, ok bool,
) {
	var acc *model.Account
	client, acc, ok = h.accountClient(w, r)
	if !ok {
		return
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		ok = false
		return
	}
	if req.Prefix == "" {
		h.writeErr(w, http.StatusBadRequest, "prefix is required")
		ok = false
		return
	}
	bucket = req.Bucket
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	targetBucket = req.TargetBucket
	if targetBucket == "" {
		targetBucket = bucket
	}
	if targetBucket == bucket &&
		(req.TargetPrefix == req.Prefix || strings.HasPrefix(req.TargetPrefix, req.Prefix)) {
		h.writeErr(w, http.StatusBadRequest, "targetPrefix must not overlap source prefix in the same bucket")
		ok = false
		return
	}
	ok = true
	return
}

func (h *Handler) listPrefixKeys(ctx context.Context, client *s3wrap.Client, bucket, prefix string, maxCopy int) ([]string, bool, error) {
	var keys []string
	truncated := false
	token := ""
	for {
		page, err := client.ListObjectsPage(ctx, bucket, prefix, "", token, "", 1000)
		if err != nil {
			return nil, false, err
		}
		for _, o := range page.Objects {
			keys = append(keys, o.Key)
			if len(keys) >= maxCopy {
				truncated = true
				break
			}
		}
		if truncated {
			break
		}
		if !page.IsTruncated || page.NextToken == "" {
			break
		}
		token = page.NextToken
	}
	return keys, truncated, nil
}

func copyBatchJSON(out service.BatchResult, total int, truncated bool) map[string]any {
	resp := map[string]any{"copied": out.OK, "failed": out.Failed, "total": total, "truncated": truncated}
	if out.LastError != "" {
		resp["lastError"] = out.LastError
	}
	if len(out.FailKeys) > 0 {
		resp["failedKeys"] = out.FailKeys
	}
	return resp
}

// copyPrefix 同步递归复制前缀。
func (h *Handler) copyPrefix(w http.ResponseWriter, r *http.Request) {
	req, client, bucket, targetBucket, ok := h.parseCopyPrefix(w, r)
	if !ok {
		return
	}
	keys, truncated, err := h.listPrefixKeys(r.Context(), client, bucket, req.Prefix, 100_000)
	if err != nil {
		h.writeInternalErr(w, err, "copy operation failed")
		return
	}
	pairs := make([][2]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, [2]string{k, service.RelKey(k, req.Prefix, req.TargetPrefix)})
	}
	out := service.CopyKeys(r.Context(), client, bucket, targetBucket, pairs, 4, nil)
	h.writeJSON(w, http.StatusOK, copyBatchJSON(out, len(keys), truncated))
}

// copyPrefixAsync 异步前缀复制：立即返回 jobId，进度复用 migrate jobs SSE/轮询。
func (h *Handler) copyPrefixAsync(w http.ResponseWriter, r *http.Request) {
	req, client, bucket, targetBucket, ok := h.parseCopyPrefix(w, r)
	if !ok {
		return
	}
	keys, truncated, err := h.listPrefixKeys(r.Context(), client, bucket, req.Prefix, 100_000)
	if err != nil {
		h.writeInternalErr(w, err, "copy operation failed")
		return
	}
	pairs := make([][2]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, [2]string{k, service.RelKey(k, req.Prefix, req.TargetPrefix)})
	}
	ctx, cancel := context.WithTimeout(context.Background(), migrateJobTimeout)
	job := h.migrateJobs.Create(len(pairs), cancel)
	go func() {
		defer cancel()
		out := service.CopyKeys(ctx, client, bucket, targetBucket, pairs, 4, func(p service.Progress) {
			job.Emit(service.ProgressFrom(p))
		})
		status := "done"
		if ctx.Err() != nil {
			status = "cancelled"
		}
		job.Finish(service.ResultFromBatch(out), status)
	}()
	h.writeJSON(w, http.StatusAccepted, map[string]any{
		"jobId": job.ID, "total": job.Total, "truncated": truncated,
	})
}
