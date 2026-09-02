package handler

import (
	"net/http"
	"time"

	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
)

func (h *Handler) listBuckets(w http.ResponseWriter, r *http.Request) {
	client, _, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	out, err := client.ListBuckets(r.Context())
	if err != nil {
		h.writeInternalErr(w, err, "bucket operation failed")
		return
	}
	buckets := make([]bucketItem, 0, len(out.Buckets))
	for _, b := range s3wrap.FormatBuckets(out) {
		buckets = append(buckets, bucketItem{Name: b.Name, CreationDate: b.CreationDate})
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"buckets": buckets})
}

// createBucket 创建桶（控制台共性能力：名称 + 权限 + 地域）。
func (h *Handler) createBucket(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Name   string `json:"name"`
		Region string `json:"region"`
		ACL    string `json:"acl"` // private | public-read | public-read-write
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	if !validBucketName(req.Name) {
		h.writeErr(w, http.StatusBadRequest, "bucket name 需为 3-63 位小写字母/数字/连字符")
		return
	}
	switch req.ACL {
	case "", "private", "public-read", "public-read-write":
	default:
		h.writeErr(w, http.StatusBadRequest, "invalid acl")
		return
	}
	region := req.Region
	if region == "" {
		region = acc.Region
	}
	if err := client.CreateBucket(r.Context(), req.Name, region, req.ACL); err != nil {
		h.writeInternalErr(w, err, "bucket operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"created": req.Name, "region": region, "acl": req.ACL})
}

// deleteBucket 删除桶（桶须为空）。
func (h *Handler) deleteBucket(w http.ResponseWriter, r *http.Request) {
	client, _, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		h.writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := client.DeleteBucket(r.Context(), name); err != nil {
		if s3wrap.HasErrorCode(err, "BucketNotEmpty") {
			h.writeErr(w, http.StatusConflict, "bucket not empty, delete all objects first")
			return
		}
		h.writeInternalErr(w, err, "bucket operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"deleted": name})
}

type bucketItem struct {
	Name         string    `json:"name"`
	CreationDate time.Time `json:"creationDate"`
}

// getBucketInfo 返回桶属性：区域、创建时间、版本控制状态。
func (h *Handler) getBucketInfo(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	bucket := r.URL.Query().Get("bucket")
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	region, rerr := client.GetBucketLocation(r.Context(), bucket)
	if rerr != nil {
		h.log.Warn("get bucket location failed", "bucket", bucket, "err", rerr)
	}
	versioning, verr := client.GetBucketVersioning(r.Context(), bucket)
	if verr != nil {
		h.log.Warn("get bucket versioning failed", "bucket", bucket, "err", verr)
	}
	created := ""
	if out, cerr := client.ListBuckets(r.Context()); cerr == nil {
		for _, b := range s3wrap.FormatBuckets(out) {
			if b.Name == bucket {
				created = b.CreationDate.Format(time.RFC3339)
				break
			}
		}
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"bucket":     bucket,
		"region":     region,
		"createdAt":  created,
		"versioning": versioning,
	})
}

// putBucketVersioning 开启/暂停桶版本控制（Enabled | Suspended）。
func (h *Handler) putBucketVersioning(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket string `json:"bucket"`
		Status string `json:"status"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	bucket := req.Bucket
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	switch req.Status {
	case "Enabled", "Suspended":
	default:
		h.writeErr(w, http.StatusBadRequest, "status must be Enabled or Suspended")
		return
	}
	if err := client.PutBucketVersioning(r.Context(), bucket, req.Status); err != nil {
		h.writeInternalErr(w, err, "bucket operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"versioning": req.Status})
}
