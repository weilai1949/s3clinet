package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
)

// listTrash 回收站：返回桶内一页删除标记（ListObjectVersions 过滤为删除标记），带分页游标。
// 由前端逐页加载并在空页时自动向后继续，避免因「版本/删除标记混合」造成漏项。
func (h *Handler) listTrash(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	bucket, ok := h.bucketOr(w, acc, q.Get("bucket"))
	if !ok {
		return
	}
	maxKeys := int32(1000)
	if mk := q.Get("maxKeys"); mk != "" {
		if n, err := strconv.Atoi(mk); err == nil && n > 0 && n <= 1000 {
			maxKeys = int32(n)
		}
	}
	out, err := client.ListObjectVersions(r.Context(), bucket, q.Get("prefix"), q.Get("keyMarker"), q.Get("versionIdMarker"), maxKeys)
	if err != nil {
		h.writeInternalErr(w, err, "trash operation failed")
		return
	}
	deleteMarkers := []map[string]any{}
	for _, d := range out.DeleteMarkers {
		deleteMarkers = append(deleteMarkers, map[string]any{
			"key":          derefString(d.Key),
			"versionId":    derefString(d.VersionId),
			"isLatest":     boolOrFalse(d.IsLatest),
			"lastModified": timeOrZero(d.LastModified).Format(time.RFC3339),
		})
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"deleteMarkers":       deleteMarkers,
		"isTruncated":         boolOrFalse(out.IsTruncated),
		"nextKeyMarker":       derefString(out.NextKeyMarker),
		"nextVersionIdMarker": derefString(out.NextVersionIdMarker),
	})
}

// purgeTrashObject 彻底清除某 key 的全部版本与删除标记（永久删除对象，不可还原）。
func (h *Handler) purgeTrashObject(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket string `json:"bucket"`
		Key    string `json:"key"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	if req.Key == "" {
		h.writeErr(w, http.StatusBadRequest, "key is required")
		return
	}
	bucket, ok := h.bucketOr(w, acc, req.Bucket)
	if !ok {
		return
	}
	n, err := client.PurgeObject(r.Context(), bucket, req.Key)
	if err != nil {
		if s3wrap.IsAPIError(err) {
			h.writeJSON(w, http.StatusConflict, map[string]any{"purged": req.Key, "deleted": n, "error": s3UserMessage(err)})
			return
		}
		h.writeInternalErr(w, err, "trash operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"purged": req.Key, "deleted": n})
}
