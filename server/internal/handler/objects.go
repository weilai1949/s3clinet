package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
	"github.com/weilai1949/s3clinet/server/internal/service"
)

func (h *Handler) listObjects(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	bucket := q.Get("bucket")
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	maxKeys, _ := strconv.Atoi(q.Get("maxKeys"))
	if maxKeys < 1 {
		maxKeys = 1000
	}
	if maxKeys > 1000 {
		maxKeys = 1000
	}

	page, err := client.ListObjectsPage(r.Context(), bucket, q.Get("prefix"), q.Get("delimiter"), q.Get("continuationToken"), q.Get("startAfter"), int32(maxKeys))
	if err != nil {
		h.writeInternalErr(w, err, "list objects failed")
		return
	}
	resp := listObjectsResponse{
		Objects:        make([]objectItem, 0, len(page.Objects)),
		CommonPrefixes: make([]string, 0, len(page.CommonPrefixes)),
		IsTruncated:    page.IsTruncated,
		NextToken:      page.NextToken,
	}
	for _, o := range page.Objects {
		resp.Objects = append(resp.Objects, fromS3Object(o))
	}
	resp.CommonPrefixes = append(resp.CommonPrefixes, page.CommonPrefixes...)
	h.writeJSON(w, http.StatusOK, resp)
}

// headObject 返回单个对象的元数据详情（HeadObject）。
func (h *Handler) headObject(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	bucket := q.Get("bucket")
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	key := q.Get("key")
	if key == "" {
		h.writeErr(w, http.StatusBadRequest, "key is required")
		return
	}
	out, err := client.HeadObjectMeta(r.Context(), bucket, key, q.Get("versionId"))
	if err != nil {
		if s3wrap.IsNotFound(err) {
			h.writeErr(w, http.StatusNotFound, "object not found")
			return
		}
		h.writeInternalErr(w, err, "head object failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"key":          key,
		"size":         out.Size,
		"lastModified": out.LastModified,
		"etag":         out.ETag,
		"contentType":  out.ContentType,
		"storageClass": out.StorageClass,
		"metadata":     out.Metadata,
	})
}

// allowedStorageClasses 允许切换到的存储类型（S3 标准枚举的常用子集）。
var allowedStorageClasses = map[string]bool{
	"STANDARD":            true,
	"REDUCED_REDUNDANCY":  true,
	"STANDARD_IA":         true,
	"ONEZONE_IA":          true,
	"INTELLIGENT_TIERING": true,
	"GLACIER":             true,
	"GLACIER_IR":          true,
	"DEEP_ARCHIVE":        true,
	"EXPRESS_ONEZONE":     true,
}

// changeStorageClass 切换对象存储类型（服务端 CopyObject 复制到自身，并携带新 StorageClass）。
// 版本控制桶下写出一条新版本；versionId 为空表示切换当前对象。
func (h *Handler) changeStorageClass(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket       string `json:"bucket"`
		Key          string `json:"key"`
		VersionID    string `json:"versionId"`
		StorageClass string `json:"storageClass"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	if req.Key == "" {
		h.writeErr(w, http.StatusBadRequest, "key is required")
		return
	}
	if req.StorageClass == "" || !allowedStorageClasses[req.StorageClass] {
		h.writeErr(w, http.StatusBadRequest, "unsupported storageClass: "+req.StorageClass)
		return
	}
	bucket, ok := h.bucketOr(w, acc, req.Bucket)
	if !ok {
		return
	}
	newVersion, err := client.ChangeObjectStorageClass(r.Context(), bucket, req.Key, req.VersionID, req.StorageClass)
	if err != nil {
		if s3wrap.HasErrorCode(err, "InvalidRequest", "InvalidStorageClass") {
			h.writeErr(w, http.StatusBadRequest, s3UserMessage(err))
			return
		}
		h.writeInternalErr(w, err, "change storage class failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"changed":      req.Key,
		"versionId":    newVersion,
		"storageClass": req.StorageClass,
	})
}

// mkdirObject 通过 PUT 空对象创建"文件夹"（S3 无真实目录，key 以 / 结尾）。
func (h *Handler) mkdirObject(w http.ResponseWriter, r *http.Request) {
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
	bucket := req.Bucket
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	key := strings.TrimRight(req.Key, "/") + "/"
	if err := client.PutObject(r.Context(), bucket, key, nil, "", nil); err != nil {
		h.writeInternalErr(w, err, "create folder failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"created": key, "bucket": bucket})
}

// renameObject 复制到新 key 后删除源（跨桶移动需目标桶参数，默认同桶）。
func (h *Handler) renameObject(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket    string `json:"bucket"`
		Key       string `json:"key"`
		NewKey    string `json:"newKey"`
		NewBucket string `json:"newBucket"` // 可选：跨桶移动
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
	// 同桶内 newKey 与 key 相同视为无效（跨桶同名移动允许）。
	if targetBucket == bucket && req.Key == req.NewKey {
		h.writeErr(w, http.StatusBadRequest, "newKey must differ from key in the same bucket")
		return
	}
	// 先复制，成功后才删除源，避免复制失败丢数据。
	if err := client.CopyObject(r.Context(), bucket, req.Key, targetBucket, req.NewKey); err != nil {
		h.writeInternalErr(w, err, "rename copy failed")
		return
	}
	if err := client.DeleteObject(r.Context(), bucket, req.Key); err != nil {
		// 复制已成功但删除失败：源与目标同时存在，提示用户手动处理。
		h.writeInternalErr(w, err, "copied to new key but failed to delete source")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"renamed": req.NewKey})
}

// deleteObjects 批量删除。
func (h *Handler) deleteObjects(w http.ResponseWriter, r *http.Request) {
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
	bucket := req.Bucket
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	if err := client.DeleteObjects(r.Context(), bucket, req.Keys); err != nil {
		h.writeInternalErr(w, err, "delete objects failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"deleted": len(req.Keys)})
}

// deletePrefix 递归删除前缀下的全部对象（循环 ListObjectsV2 + 批量 DeleteObjects）。
func (h *Handler) deletePrefix(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket string `json:"bucket"`
		Prefix string `json:"prefix"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	if req.Prefix == "" {
		h.writeErr(w, http.StatusBadRequest, "prefix is required (拒绝空前缀以免误删全桶)")
		return
	}
	bucket := req.Bucket
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	deleted, truncated, err := runDeletePrefix(r.Context(), client, bucket, req.Prefix)
	if err != nil {
		h.writeInternalErr(w, err, "delete prefix failed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted, "truncated": truncated})
}

// deletePrefixAsync 异步递归删除前缀；进度复用 migrate jobs（migrated=已删除数）。
func (h *Handler) deletePrefixAsync(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Bucket string `json:"bucket"`
		Prefix string `json:"prefix"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	if req.Prefix == "" {
		h.writeErr(w, http.StatusBadRequest, "prefix is required (拒绝空前缀以免误删全桶)")
		return
	}
	bucket := req.Bucket
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	// 先列举以拿到 total（与 copy-prefix/async 一致），再异步删除。
	keys, truncated, err := h.listPrefixKeys(r.Context(), client, bucket, req.Prefix, 100_000)
	if err != nil {
		h.writeInternalErr(w, err, "delete prefix list failed")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), migrateJobTimeout)
	job := h.migrateJobs.Create(len(keys), cancel)
	go func() {
		defer cancel()
		deleted, failed := 0, 0
		var lastErr string
		var failKeys []string
		const batch = 1000
		for i := 0; i < len(keys); i += batch {
			if ctx.Err() != nil {
				break
			}
			end := i + batch
			if end > len(keys) {
				end = len(keys)
			}
			chunk := keys[i:end]
			if err := client.DeleteObjects(ctx, bucket, chunk); err != nil {
				failed += len(chunk)
				if lastErr == "" {
					lastErr = s3UserMessage(err)
				}
				for _, k := range chunk {
					if len(failKeys) < 200 {
						failKeys = append(failKeys, k)
					}
				}
			} else {
				deleted += len(chunk)
			}
			job.Emit(service.JobProgress{
				Done: deleted + failed, Total: len(keys), Migrated: deleted, Failed: failed,
				Error: lastErr, Status: "running",
			})
		}
		status := "done"
		if ctx.Err() != nil {
			status = "cancelled"
		}
		job.Finish(service.JobResult{
			Migrated: deleted, Failed: failed, LastError: lastErr, FailKeys: failKeys,
		}, status)
	}()
	h.writeJSON(w, http.StatusAccepted, map[string]any{
		"jobId": job.ID, "total": job.Total, "truncated": truncated,
	})
}

func runDeletePrefix(
	ctx context.Context, client *s3wrap.Client, bucket, prefix string,
) (deleted int, truncated bool, err error) {
	const maxDelete = 100_000
	token := ""
	for {
		if deleted >= maxDelete {
			truncated = true
			break
		}
		page, listErr := client.ListObjectsPage(ctx, bucket, prefix, "", token, "", 1000)
		if listErr != nil {
			return deleted, truncated, listErr
		}
		keys := make([]string, 0, len(page.Objects))
		for _, o := range page.Objects {
			keys = append(keys, o.Key)
		}
		if len(keys) > 0 {
			if deleted+len(keys) > maxDelete {
				keys = keys[:maxDelete-deleted]
				truncated = true
			}
			if delErr := client.DeleteObjects(ctx, bucket, keys); delErr != nil {
				return deleted, truncated, delErr
			}
			deleted += len(keys)
		}
		if truncated || !page.IsTruncated || page.NextToken == "" {
			break
		}
		token = page.NextToken
	}
	return deleted, truncated, nil
}

// presign 生成 v4 签名 URL（get/put/post）。
func (h *Handler) presign(w http.ResponseWriter, r *http.Request) {
	client, acc, ok := h.accountClient(w, r)
	if !ok {
		return
	}
	var req struct {
		Method    string `json:"method"`
		Key       string `json:"key"`
		Bucket    string `json:"bucket"`
		VersionID string `json:"versionId"`
		ExpiresIn int64  `json:"expiresIn"`
	}
	if err := h.readJSON(r, &req); err != nil {
		h.writeBadJSON(w, err)
		return
	}
	if req.Key == "" {
		h.writeErr(w, http.StatusBadRequest, "key is required")
		return
	}
	if req.Method == "" {
		req.Method = "put"
	}
	bucket := req.Bucket
	if bucket, ok = h.bucketOr(w, acc, bucket); !ok {
		return
	}
	expires := time.Duration(req.ExpiresIn) * time.Second
	if expires <= 0 {
		expires = time.Hour // 默认 1h（慢网大文件友好；上限见下方校验）
	}
	// 上限 24h（S3 允许最长 7 天；控制台场景收紧）。
	maxExpires := 24 * time.Hour
	if expires > maxExpires {
		expires = maxExpires
	}

	switch strings.ToLower(req.Method) {
	case "get":
		// 过期被钳制到 [1h, 24h]，PresignGetVersion 实际不可能失败。
		u, _ := client.PresignGetVersion(r.Context(), bucket, req.Key, req.VersionID, expires)
		h.writeJSON(w, http.StatusOK, map[string]any{"method": "get", "bucket": bucket, "key": req.Key, "url": u, "expiresIn": int64(expires.Seconds())})
	case "post":
		// 过期被钳制到 [1h, 24h]，PresignPost 实际不可能失败。
		post, _ := client.PresignPost(r.Context(), bucket, req.Key, expires)
		h.writeJSON(w, http.StatusOK, map[string]any{
			"method": "post", "bucket": bucket, "key": req.Key,
			"url": post.URL, "fields": post.Fields, "expiresIn": int64(expires.Seconds()),
		})
	case "put":
		// 过期被钳制到 [1h, 24h]，PresignPut 实际不可能失败。
		u, _ := client.PresignPut(r.Context(), bucket, req.Key, expires)
		h.writeJSON(w, http.StatusOK, map[string]any{"method": "put", "bucket": bucket, "key": req.Key, "url": u, "expiresIn": int64(expires.Seconds())})
	default:
		h.writeErr(w, http.StatusBadRequest, "invalid method (get|put|post)")
	}
}
