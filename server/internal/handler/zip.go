package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/weilai1949/s3clinet/server/internal/service"
)

// downloadZip 将所选对象流式打包为 ZIP 下载（不落盘、不占内存）。
func (h *Handler) downloadZip(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket string   `json:"bucket"`
		Keys   []string `json:"keys"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	if len(req.Keys) == 0 {
		h.writeErr(w, http.StatusBadRequest, "keys are required")
		return
	}
	if len(req.Keys) > maxZipKeys {
		h.writeErr(w, http.StatusBadRequest, fmt.Sprintf("too many keys, max %d", maxZipKeys))
		return
	}
	bucket := req.Bucket
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="objects-%d.zip"`, time.Now().Unix()))
	beginStreamResponse(w)

	_, _ = service.WriteObjectsZip(r.Context(), func(ctx context.Context, key string) (io.ReadCloser, string, error) {
		out, err := client.GetObjectStream(ctx, bucket, key, "", "")
		if err != nil {
			return nil, "", err
		}
		return out.Body, out.ContentType, nil
	}, req.Keys, w)
}
