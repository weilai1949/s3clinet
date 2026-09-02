package handler

import (
	"errors"
	"net/http"

	"github.com/weilai1949/s3clinet/server/internal/model"
	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
	"github.com/weilai1949/s3clinet/server/internal/store"
)

func (h *Handler) listAccounts(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]any{"accounts": h.store.List()})
}

func (h *Handler) createAccount(w http.ResponseWriter, r *http.Request) {
	var a model.Account
	if err := h.readJSON(r, &a); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	// 账号 id 由服务端生成；忽略客户端提交的 id，避免覆盖/污染已有账号。
	a.ID = ""
	if a.Name == "" {
		h.writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if a.Endpoint == "" || a.AccessKey == "" || a.SecretKey == "" {
		h.writeErr(w, http.StatusBadRequest, "endpoint/accessKey/secretKey are required")
		return
	}
	created, err := h.store.Create(&a)
	if err != nil {
		h.writeInternalErr(w, err, "failed to create account")
		return
	}
	h.writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) getAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := h.store.Get(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.writeErr(w, http.StatusNotFound, "account not found")
			return
		}
		h.writeInternalErr(w, err, "failed to load account")
		return
	}
	h.writeJSON(w, http.StatusOK, a.Sanitized())
}

func (h *Handler) updateAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var a model.Account
	if err := h.readJSON(r, &a); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	// 账号 id 由服务端生成；忽略客户端提交的 id，避免覆盖/污染已有账号。
	a.ID = ""
	if a.Name == "" {
		h.writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if a.Endpoint == "" || a.AccessKey == "" {
		h.writeErr(w, http.StatusBadRequest, "endpoint/accessKey are required")
		return
	}
	updated, err := h.store.Update(id, &a)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.writeErr(w, http.StatusNotFound, "account not found")
			return
		}
		h.writeInternalErr(w, err, "failed to update account")
		return
	}
	h.clients.evict(id)
	h.writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.Delete(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.writeErr(w, http.StatusNotFound, "account not found")
			return
		}
		h.writeInternalErr(w, err, "failed to delete account")
		return
	}
	h.clients.evict(id)
	h.writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

// testAccount 通过 HeadBucket 检测连通性。
func (h *Handler) testAccount(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	bucket := r.URL.Query().Get("bucket")
	if bucket == "" {
		bucket = acc.BucketOrDefault()
	}
	if err := client.HeadBucket(r.Context(), bucket); err != nil {
		h.log.Debug("head bucket failed", "bucket", bucket, "err", err)
		h.writeJSON(w, http.StatusOK, map[string]any{"ok": false, "bucket": bucket, "error": s3UserMessage(err)})
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "bucket": bucket})
}

// previewBuckets 用表单凭证临时列出 Bucket（不落库），便于新建账号时选择默认桶。
func (h *Handler) previewBuckets(w http.ResponseWriter, r *http.Request) {
	var a model.Account
	if err := h.readJSON(r, &a); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	if a.Endpoint == "" || a.AccessKey == "" || a.SecretKey == "" {
		h.writeErr(w, http.StatusBadRequest, "endpoint/accessKey/secretKey are required")
		return
	}
	client, err := s3wrap.New(&a)
	if err != nil {
		h.log.Debug("preview buckets client init", "err", err)
		h.writeErr(w, http.StatusBadRequest, "invalid account configuration")
		return
	}
	out, err := client.ListBuckets(r.Context())
	if err != nil {
		h.writeInternalErr(w, err, "failed to list buckets")
		return
	}
	buckets := make([]bucketItem, 0, len(out.Buckets))
	for _, b := range s3wrap.FormatBuckets(out) {
		buckets = append(buckets, bucketItem{Name: b.Name, CreationDate: b.CreationDate})
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"buckets": buckets})
}

// validBucketName 校验 S3 桶命名规则（简化版）。
func validBucketName(name string) bool {
	if len(name) < 3 || len(name) > 63 {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}
