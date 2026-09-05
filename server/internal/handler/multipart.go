package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
)

// multipartInit 初始化分段上传，返回 UploadID。
func (h *Handler) multipartInit(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket      string `json:"bucket"`
		Key         string `json:"key"`
		ContentType string `json:"contentType"`
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
	uploadID, err := client.CreateMultipartUpload(r.Context(), bucket, req.Key, req.ContentType)
	if err != nil {
		h.writeInternalErr(w, err, "multipart operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"uploadId": uploadID, "key": req.Key, "bucket": bucket})
}

// multipartPart 预签名单个分段的 PUT URL。
func (h *Handler) multipartPart(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket     string `json:"bucket"`
		Key        string `json:"key"`
		UploadID   string `json:"uploadId"`
		PartNumber int32  `json:"partNumber"`
		ExpiresIn  int64  `json:"expiresIn"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	if req.Key == "" || req.UploadID == "" {
		h.writeErr(w, http.StatusBadRequest, "key and uploadId are required")
		return
	}
	if req.PartNumber < 1 || req.PartNumber > 10_000 {
		h.writeErr(w, http.StatusBadRequest, "partNumber must be 1-10000")
		return
	}
	bucket := req.Bucket
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	expires := time.Duration(req.ExpiresIn) * time.Second
	if expires <= 0 {
		expires = time.Hour
	}
	maxExpires := 24 * time.Hour
	if expires > maxExpires {
		expires = maxExpires
	}
	// 过期被钳制到 [1h, 24h]，PresignUploadPart 实际不可能失败。
	u, _ := client.PresignUploadPart(r.Context(), bucket, req.Key, req.UploadID, req.PartNumber, expires)
	h.writeJSON(w, http.StatusOK, map[string]any{"partNumber": req.PartNumber, "url": u, "expiresIn": int64(expires.Seconds())})
}

// multipartComplete 汇总分段并完成分段上传。
func (h *Handler) multipartComplete(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket   string `json:"bucket"`
		Key      string `json:"key"`
		UploadID string `json:"uploadId"`
		Parts    []struct {
			PartNumber int32  `json:"partNumber"`
			ETag       string `json:"etag"`
		} `json:"parts"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	if req.Key == "" || req.UploadID == "" || len(req.Parts) == 0 {
		h.writeErr(w, http.StatusBadRequest, "key, uploadId and parts are required")
		return
	}
	bucket := req.Bucket
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	specs := make([]s3wrap.UploadPartSpec, 0, len(req.Parts))
	seen := map[int32]bool{}
	for _, p := range req.Parts {
		if p.PartNumber < 1 || p.PartNumber > 10000 || p.ETag == "" {
			h.writeErr(w, http.StatusBadRequest, "each part needs partNumber in 1..10000 and etag")
			return
		}
		if seen[p.PartNumber] {
			h.writeErr(w, http.StatusBadRequest, "duplicate partNumber: "+strconv.Itoa(int(p.PartNumber)))
			return
		}
		seen[p.PartNumber] = true
		specs = append(specs, s3wrap.UploadPartSpec{PartNumber: p.PartNumber, ETag: p.ETag})
	}
	if err := client.CompleteMultipartUpload(r.Context(), bucket, req.Key, req.UploadID, specs); err != nil {
		h.writeInternalErr(w, err, "multipart operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"completed": req.Key})
}

// multipartAbort 中止分段上传（上传失败时清理使用，尽力而为）。
func (h *Handler) multipartAbort(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket   string `json:"bucket"`
		Key      string `json:"key"`
		UploadID string `json:"uploadId"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	if req.Key == "" || req.UploadID == "" {
		h.writeErr(w, http.StatusBadRequest, "key and uploadId are required")
		return
	}
	bucket := req.Bucket
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	if err := client.AbortMultipartUpload(r.Context(), bucket, req.Key, req.UploadID); err != nil {
		h.writeInternalErr(w, err, "multipart operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"aborted": true})
}
