package handler

import (
	"net/http"
)

// setHeaders 设置对象 HTTP 头/元数据（CopyObject REPLACE 到自己）。
func (h *Handler) setHeaders(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket      string            `json:"bucket"`
		Key         string            `json:"key"`
		ContentType string            `json:"contentType"`
		Metadata    map[string]string `json:"metadata"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	if req.Key == "" {
		h.writeErr(w, http.StatusBadRequest, "key is required")
		return
	}
	bucket := req.Bucket
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	if err := client.CopyObjectWithMeta(r.Context(), bucket, req.Key, bucket, req.Key, req.ContentType, req.Metadata); err != nil {
		h.writeInternalErr(w, err, "headers operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"updated": req.Key})
}
