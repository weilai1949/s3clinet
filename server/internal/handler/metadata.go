package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
)

// ---- 对象 ACL（权限） ----

type aclGrant struct {
	Grantee    string `json:"grantee"`
	Permission string `json:"permission"`
}

// getObjectAcl 读取对象 ACL，返回公有性、授权列表与公开链接。
func (h *Handler) getObjectAcl(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	bucket := r.URL.Query().Get("bucket")
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		h.writeErr(w, http.StatusBadRequest, "key is required")
		return
	}
	out, err := client.GetObjectAcl(r.Context(), bucket, key)
	if err != nil {
		h.writeInternalErr(w, err, "metadata operation failed")
		return
	}
	owner, public, rows := s3wrap.DescribeACL(out)
	grants := []aclGrant{}
	for _, r := range rows {
		grants = append(grants, aclGrant{Grantee: r.Grantee, Permission: r.Permission})
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"bucket": bucket,
		"key":    key,
		"owner":  owner,
		"public": public,
		"grants": grants,
		"url":    client.PublicURL(bucket, key),
	})
}

// putObjectAcl 设置对象 canned ACL（private / public-read 等）。
func (h *Handler) putObjectAcl(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket string `json:"bucket"`
		Key    string `json:"key"`
		ACL    string `json:"acl"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	if req.Key == "" {
		h.writeErr(w, http.StatusBadRequest, "key is required")
		return
	}
	switch req.ACL {
	case "private", "public-read", "public-read-write", "authenticated-read", "aws-exec-read":
	default:
		h.writeErr(w, http.StatusBadRequest, "unsupported acl: "+req.ACL)
		return
	}
	bucket := req.Bucket
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	if err := client.PutObjectAcl(r.Context(), bucket, req.Key, req.ACL); err != nil {
		h.writeInternalErr(w, err, "metadata operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"acl": req.ACL})
}

// getObjectTags 读取对象标签；无标签（NoSuchTagSet 等）返回空列表。
func (h *Handler) getObjectTags(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	bucket := r.URL.Query().Get("bucket")
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		h.writeErr(w, http.StatusBadRequest, "key is required")
		return
	}
	out, err := client.GetObjectTags(r.Context(), bucket, key)
	if err != nil {
		if s3wrap.HasErrorCode(err, "NoSuchTagSet", "NoSuchTagSetError", "NotFound") {
			h.writeJSON(w, http.StatusOK, map[string]any{"tags": []any{}})
			return
		}
		h.writeInternalErr(w, err, "metadata operation failed")
		return
	}
	tags := []map[string]string{}
	for _, t := range out.TagSet {
		tags = append(tags, map[string]string{"key": derefString(t.Key), "value": derefString(t.Value)})
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

// putObjectTags 覆盖写入对象标签；tags 为空则删除全部标签。
func (h *Handler) putObjectTags(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket string `json:"bucket"`
		Key    string `json:"key"`
		Tags   []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"tags"`
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
	tags := map[string]string{}
	for _, t := range req.Tags {
		if t.Key == "" {
			h.writeErr(w, http.StatusBadRequest, "tag key is required")
			return
		}
		if len(t.Key) > 128 || len(t.Value) > 256 {
			h.writeErr(w, http.StatusBadRequest, "tag key max 128 chars, value max 256 chars")
			return
		}
		if _, dup := tags[t.Key]; dup {
			h.writeErr(w, http.StatusBadRequest, "duplicate tag key: "+t.Key)
			return
		}
		tags[t.Key] = t.Value
	}
	if len(tags) > 10 {
		h.writeErr(w, http.StatusBadRequest, "at most 10 tags per object")
		return
	}
	if len(tags) == 0 {
		if err := client.DeleteObjectTags(r.Context(), bucket, req.Key); err != nil {
			h.writeInternalErr(w, err, "metadata operation failed")
			return
		}
		h.writeJSON(w, http.StatusOK, map[string]any{"tags": []any{}})
		return
	}
	if err := client.PutObjectTags(r.Context(), bucket, req.Key, tags); err != nil {
		h.writeInternalErr(w, err, "metadata operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"tags": req.Tags})
}

// getLifecycle 读取桶生命周期规则（简化版：前缀过期删除）。
func (h *Handler) getLifecycle(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	bucket := r.URL.Query().Get("bucket")
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	specs, err := client.GetLifecycle(r.Context(), bucket)
	if err != nil {
		if s3wrap.IsNotFound(err) || s3wrap.HasErrorCode(err, "NoSuchLifecycleConfiguration") {
			h.writeJSON(w, http.StatusOK, map[string]any{"rules": []any{}})
			return
		}
		h.writeInternalErr(w, err, "metadata operation failed")
		return
	}
	rules := make([]map[string]any, 0, len(specs))
	for _, s := range specs {
		rules = append(rules, map[string]any{"id": s.ID, "prefix": s.Prefix, "days": s.Days})
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

// putLifecycle 覆盖写入桶生命周期规则。
func (h *Handler) putLifecycle(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket string `json:"bucket"`
		Rules  []struct {
			ID     string `json:"id"`
			Prefix string `json:"prefix"`
			Days   int32  `json:"days"`
		} `json:"rules"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	bucket := req.Bucket
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	specs := make([]s3wrap.LifecycleRuleSpec, 0, len(req.Rules))
	seen := map[string]bool{}
	for _, rl := range req.Rules {
		if rl.ID == "" || rl.Days < 1 {
			h.writeErr(w, http.StatusBadRequest, "each rule needs id and days >= 1")
			return
		}
		if seen[rl.ID] {
			h.writeErr(w, http.StatusBadRequest, "duplicate rule id: "+rl.ID)
			return
		}
		seen[rl.ID] = true
		specs = append(specs, s3wrap.LifecycleRuleSpec{ID: rl.ID, Prefix: rl.Prefix, Days: rl.Days})
	}
	if len(specs) == 0 {
		// 清空规则：空 PUT 会被多数实现拒绝，须调用 DeleteBucketLifecycle
		if err := client.DeleteLifecycle(r.Context(), bucket); err != nil {
			h.writeInternalErr(w, err, "metadata operation failed")
			return
		}
		h.writeJSON(w, http.StatusOK, map[string]any{"updated": 0})
		return
	}
	if err := client.PutLifecycle(r.Context(), bucket, specs); err != nil {
		h.writeInternalErr(w, err, "metadata operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"updated": len(specs)})
}

// listObjectVersions 列出对象各版本（含删除标记）与分页游标。
func (h *Handler) listObjectVersions(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	bucket := q.Get("bucket")
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
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
		h.writeInternalErr(w, err, "metadata operation failed")
		return
	}
	versions := []map[string]any{}
	for _, v := range out.Versions {
		versions = append(versions, map[string]any{
			"key":          derefString(v.Key),
			"versionId":    derefString(v.VersionId),
			"isLatest":     boolOrFalse(v.IsLatest),
			"lastModified": timeOrZero(v.LastModified).Format(time.RFC3339),
			"size":         derefInt64(v.Size),
			"etag":         derefString(v.ETag),
			"storageClass": string(v.StorageClass),
		})
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
		"versions":            versions,
		"deleteMarkers":       deleteMarkers,
		"isTruncated":         boolOrFalse(out.IsTruncated),
		"nextKeyMarker":       derefString(out.NextKeyMarker),
		"nextVersionIdMarker": derefString(out.NextVersionIdMarker),
	})
}

// deleteObjectVersion 删除对象指定版本（versionId 必填；删除标记亦用此接口）。
func (h *Handler) deleteObjectVersion(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	bucket, ok := h.bucketOr(w, acc, q.Get("bucket"))
	if !ok {
		return
	}
	key := q.Get("key")
	if key == "" {
		h.writeErr(w, http.StatusBadRequest, "key is required")
		return
	}
	versionID := q.Get("versionId")
	if versionID == "" {
		h.writeErr(w, http.StatusBadRequest, "versionId is required")
		return
	}
	if err := client.DeleteObjectVersion(r.Context(), bucket, key, versionID); err != nil {
		h.writeInternalErr(w, err, "metadata operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"deleted": key, "versionId": versionID})
}

// restoreObjectVersion 将指定版本恢复为当前版本（复制该版本到当前 key）。
func (h *Handler) restoreObjectVersion(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket    string `json:"bucket"`
		Key       string `json:"key"`
		VersionID string `json:"versionId"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	if req.Key == "" {
		h.writeErr(w, http.StatusBadRequest, "key is required")
		return
	}
	if req.VersionID == "" {
		h.writeErr(w, http.StatusBadRequest, "versionId is required")
		return
	}
	bucket, ok := h.bucketOr(w, acc, req.Bucket)
	if !ok {
		return
	}
	newVersion, err := client.RestoreObjectVersion(r.Context(), bucket, req.Key, req.VersionID)
	if err != nil {
		h.writeInternalErr(w, err, "metadata operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"restored": req.Key, "versionId": newVersion})
}

// restoreDeleteMarker 移除删除标记版本，一键还原已删除对象（撤销删除）。
func (h *Handler) restoreDeleteMarker(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket    string `json:"bucket"`
		Key       string `json:"key"`
		VersionID string `json:"versionId"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	if req.Key == "" {
		h.writeErr(w, http.StatusBadRequest, "key is required")
		return
	}
	if req.VersionID == "" {
		h.writeErr(w, http.StatusBadRequest, "versionId is required")
		return
	}
	bucket, ok := h.bucketOr(w, acc, req.Bucket)
	if !ok {
		return
	}
	if err := client.RestoreDeleteMarker(r.Context(), bucket, req.Key, req.VersionID); err != nil {
		h.writeInternalErr(w, err, "metadata operation failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"restored": req.Key, "versionId": req.VersionID})
}
