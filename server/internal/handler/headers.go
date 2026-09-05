package handler

import (
	"errors"
	"net/http"

	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
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
	// user metadata 在 API 边界校验：S3 限制的 key/value/total 长度，避免落到 S3 端以 500 返回。
	if err := s3wrap.ValidateUserMetadata(req.Metadata); err != nil {
		if errors.Is(err, s3wrap.ErrUserMetadataInvalid) {
			h.writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		h.writeErr(w, http.StatusBadRequest, "invalid metadata")
		return
	}
	if err := client.CopyObjectWithMeta(r.Context(), bucket, req.Key, bucket, req.Key, req.ContentType, req.Metadata); err != nil {
		h.writeInternalErr(w, err, "headers operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"updated": req.Key})
}
