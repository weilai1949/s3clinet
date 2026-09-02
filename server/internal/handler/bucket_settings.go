package handler

import (
	"encoding/json"
	"net/http"

	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
)

// isNoSuchBucketSetting 判断是否为「该桶配置尚未创建」的错误码（用于 GET 时返回空配置）。
func isNoSuchBucketSetting(err error, codes ...string) bool {
	return s3wrap.HasErrorCode(err, codes...)
}

// ---- 桶服务端加密（SSE） ----

var allowedSSEAlgorithms = map[string]bool{
	"AES256":       true,
	"aws:kms":      true,
	"aws:kms:dsse": true,
}

func (h *Handler) getBucketEncryption(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	bucket, ok := h.bucketOr(w, acc, r.URL.Query().Get("bucket"))
	if !ok {
		return
	}
	cfg, err := client.GetEncryption(r.Context(), bucket)
	if err != nil {
		if isNoSuchBucketSetting(err, "ServerSideEncryptionConfigurationNotFoundError", "NoSuchConfiguration") {
			h.writeJSON(w, http.StatusOK, map[string]any{"bucket": bucket, "configured": false, "algorithm": "", "kmsKeyId": "", "bucketKeyEnabled": false})
			return
		}
		h.writeInternalErr(w, err, "bucket settings operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"bucket": bucket, "configured": true,
		"algorithm": cfg.Algorithm, "kmsKeyId": cfg.KMSKeyID, "bucketKeyEnabled": cfg.BucketKeyEnabled,
	})
}

func (h *Handler) putBucketEncryption(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket           string `json:"bucket"`
		Algorithm        string `json:"algorithm"`
		KMSKeyID         string `json:"kmsKeyId"`
		BucketKeyEnabled bool   `json:"bucketKeyEnabled"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	bucket, ok := h.bucketOr(w, acc, req.Bucket)
	if !ok {
		return
	}
	if !allowedSSEAlgorithms[req.Algorithm] {
		h.writeErr(w, http.StatusBadRequest, "algorithm must be AES256, aws:kms or aws:kms:dsse")
		return
	}
	if err := client.PutEncryption(r.Context(), bucket, s3wrap.EncryptionConfig{
		Algorithm: req.Algorithm, KMSKeyID: req.KMSKeyID, BucketKeyEnabled: req.BucketKeyEnabled,
	}); err != nil {
		h.writeInternalErr(w, err, "bucket settings operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"configured": true, "algorithm": req.Algorithm})
}

func (h *Handler) deleteBucketEncryption(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	bucket, ok := h.bucketOr(w, acc, r.URL.Query().Get("bucket"))
	if !ok {
		return
	}
	if err := client.DeleteEncryption(r.Context(), bucket); err != nil {
		h.writeInternalErr(w, err, "bucket settings operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"deleted": bucket})
}

// ---- 桶 CORS ----

func (h *Handler) getBucketCors(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	bucket, ok := h.bucketOr(w, acc, r.URL.Query().Get("bucket"))
	if !ok {
		return
	}
	rules, err := client.GetCors(r.Context(), bucket)
	if err != nil {
		if isNoSuchBucketSetting(err, "NoSuchCORSConfiguration", "NoSuchConfiguration") {
			h.writeJSON(w, http.StatusOK, map[string]any{"bucket": bucket, "rules": []any{}})
			return
		}
		h.writeInternalErr(w, err, "bucket settings operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"bucket": bucket, "rules": rules})
}

func (h *Handler) putBucketCors(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket string            `json:"bucket"`
		Rules  []s3wrap.CorsRule `json:"rules"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	bucket, ok := h.bucketOr(w, acc, req.Bucket)
	if !ok {
		return
	}
	if len(req.Rules) == 0 {
		if err := client.DeleteCors(r.Context(), bucket); err != nil {
			h.writeInternalErr(w, err, "bucket settings operation failed")
			return
		}
		h.writeJSON(w, http.StatusOK, map[string]any{"deleted": bucket})
		return
	}
	if err := client.PutCors(r.Context(), bucket, req.Rules); err != nil {
		h.writeInternalErr(w, err, "bucket settings operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"updated": len(req.Rules)})
}

func (h *Handler) deleteBucketCors(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	bucket, ok := h.bucketOr(w, acc, r.URL.Query().Get("bucket"))
	if !ok {
		return
	}
	if err := client.DeleteCors(r.Context(), bucket); err != nil {
		h.writeInternalErr(w, err, "bucket settings operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"deleted": bucket})
}

// ---- 桶静态网站托管 ----

func (h *Handler) getBucketWebsite(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	bucket, ok := h.bucketOr(w, acc, r.URL.Query().Get("bucket"))
	if !ok {
		return
	}
	wc, err := client.GetWebsite(r.Context(), bucket)
	if err != nil {
		if isNoSuchBucketSetting(err, "NoSuchWebsiteConfiguration", "NoSuchConfiguration") {
			h.writeJSON(w, http.StatusOK, map[string]any{"bucket": bucket, "configured": false, "indexDocument": "", "errorDocument": "", "redirectAllRequestsTo": ""})
			return
		}
		h.writeInternalErr(w, err, "bucket settings operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"bucket": bucket, "configured": true,
		"indexDocument": wc.IndexDocument, "errorDocument": wc.ErrorDocument, "redirectAllRequestsTo": wc.RedirectAllRequestsTo,
	})
}

func (h *Handler) putBucketWebsite(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket                string `json:"bucket"`
		IndexDocument         string `json:"indexDocument"`
		ErrorDocument         string `json:"errorDocument"`
		RedirectAllRequestsTo string `json:"redirectAllRequestsTo"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	bucket, ok := h.bucketOr(w, acc, req.Bucket)
	if !ok {
		return
	}
	if req.RedirectAllRequestsTo == "" && req.IndexDocument == "" {
		h.writeErr(w, http.StatusBadRequest, "indexDocument or redirectAllRequestsTo is required")
		return
	}
	if err := client.PutWebsite(r.Context(), bucket, s3wrap.WebsiteConfig{
		IndexDocument: req.IndexDocument, ErrorDocument: req.ErrorDocument, RedirectAllRequestsTo: req.RedirectAllRequestsTo,
	}); err != nil {
		h.writeInternalErr(w, err, "bucket settings operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"configured": true})
}

func (h *Handler) deleteBucketWebsite(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	bucket, ok := h.bucketOr(w, acc, r.URL.Query().Get("bucket"))
	if !ok {
		return
	}
	if err := client.DeleteWebsite(r.Context(), bucket); err != nil {
		h.writeInternalErr(w, err, "bucket settings operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"deleted": bucket})
}

// ---- 桶策略 ----

func (h *Handler) getBucketPolicy(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	bucket, ok := h.bucketOr(w, acc, r.URL.Query().Get("bucket"))
	if !ok {
		return
	}
	policy, err := client.GetPolicy(r.Context(), bucket)
	if err != nil {
		if isNoSuchBucketSetting(err, "NoSuchBucketPolicy", "NoSuchConfiguration") {
			h.writeJSON(w, http.StatusOK, map[string]any{"bucket": bucket, "configured": false, "policy": ""})
			return
		}
		h.writeInternalErr(w, err, "bucket settings operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"bucket": bucket, "configured": true, "policy": policy})
}

func (h *Handler) putBucketPolicy(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket string `json:"bucket"`
		Policy string `json:"policy"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	bucket, ok := h.bucketOr(w, acc, req.Bucket)
	if !ok {
		return
	}
	if req.Policy == "" {
		if err := client.DeletePolicy(r.Context(), bucket); err != nil {
			h.writeInternalErr(w, err, "bucket settings operation failed")
			return
		}
		h.writeJSON(w, http.StatusOK, map[string]any{"deleted": bucket})
		return
	}
	if !json.Valid([]byte(req.Policy)) {
		h.writeErr(w, http.StatusBadRequest, "policy must be valid JSON")
		return
	}
	if err := client.PutPolicy(r.Context(), bucket, req.Policy); err != nil {
		h.writeInternalErr(w, err, "bucket settings operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"configured": true})
}

func (h *Handler) deleteBucketPolicy(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	bucket, ok := h.bucketOr(w, acc, r.URL.Query().Get("bucket"))
	if !ok {
		return
	}
	if err := client.DeletePolicy(r.Context(), bucket); err != nil {
		h.writeInternalErr(w, err, "bucket settings operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"deleted": bucket})
}

// ---- 桶标签 ----

func (h *Handler) getBucketTags(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	bucket, ok := h.bucketOr(w, acc, r.URL.Query().Get("bucket"))
	if !ok {
		return
	}
	tags, err := client.GetBucketTags(r.Context(), bucket)
	if err != nil {
		if isNoSuchBucketSetting(err, "NoSuchTagSet", "NoSuchConfiguration") {
			h.writeJSON(w, http.StatusOK, map[string]any{"bucket": bucket, "tags": []any{}})
			return
		}
		h.writeInternalErr(w, err, "bucket settings operation failed")
		return
	}
	list := []map[string]string{}
	for k, v := range tags {
		list = append(list, map[string]string{"key": k, "value": v})
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"bucket": bucket, "tags": list})
}

func (h *Handler) putBucketTags(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket string `json:"bucket"`
		Tags   []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"tags"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	bucket, ok := h.bucketOr(w, acc, req.Bucket)
	if !ok {
		return
	}
	tags := map[string]string{}
	for _, t := range req.Tags {
		if t.Key == "" {
			h.writeErr(w, http.StatusBadRequest, "tag key is required")
			return
		}
		tags[t.Key] = t.Value
	}
	if len(tags) == 0 {
		if err := client.DeleteBucketTags(r.Context(), bucket); err != nil {
			h.writeInternalErr(w, err, "bucket settings operation failed")
			return
		}
		h.writeJSON(w, http.StatusOK, map[string]any{"deleted": bucket})
		return
	}
	if err := client.PutBucketTags(r.Context(), bucket, tags); err != nil {
		h.writeInternalErr(w, err, "bucket settings operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"updated": len(tags)})
}

func (h *Handler) deleteBucketTags(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	bucket, ok := h.bucketOr(w, acc, r.URL.Query().Get("bucket"))
	if !ok {
		return
	}
	if err := client.DeleteBucketTags(r.Context(), bucket); err != nil {
		h.writeInternalErr(w, err, "bucket settings operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"deleted": bucket})
}
