package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Routes 注册所有路由并返回 http.Handler。
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", h.health)
	mux.HandleFunc("GET /api/metrics", h.metrics)
	mux.HandleFunc("GET /api/accounts", h.listAccounts)
	mux.HandleFunc("POST /api/accounts", h.createAccount)
	mux.HandleFunc("GET /api/accounts/{id}", h.getAccount)
	mux.HandleFunc("PUT /api/accounts/{id}", h.updateAccount)
	mux.HandleFunc("DELETE /api/accounts/{id}", h.deleteAccount)
	mux.HandleFunc("POST /api/accounts/{id}/test", h.testAccount)
	mux.HandleFunc("POST /api/accounts/preview-buckets", h.previewBuckets)
	mux.HandleFunc("GET /api/accounts/{id}/buckets", h.listBuckets)
	mux.HandleFunc("POST /api/accounts/{id}/bucket", h.createBucket)
	mux.HandleFunc("DELETE /api/accounts/{id}/bucket", h.deleteBucket)
	mux.HandleFunc("GET /api/accounts/{id}/objects", h.listObjects)
	mux.HandleFunc("GET /api/accounts/{id}/head", h.headObject)
	mux.HandleFunc("GET /api/accounts/{id}/proxy", h.withStreamLimit(h.proxyObject))
	mux.HandleFunc("POST /api/accounts/{id}/set-headers", h.setHeaders)
	mux.HandleFunc("GET /api/accounts/{id}/lifecycle", h.getLifecycle)
	mux.HandleFunc("PUT /api/accounts/{id}/lifecycle", h.putLifecycle)
	mux.HandleFunc("POST /api/accounts/{id}/presign", h.presign)
	mux.HandleFunc("POST /api/accounts/{id}/mkdir", h.mkdirObject)
	mux.HandleFunc("POST /api/accounts/{id}/rename", h.renameObject)
	mux.HandleFunc("POST /api/accounts/{id}/copy-object", h.copyObject)
	mux.HandleFunc("POST /api/accounts/{id}/copy-objects", h.copyMany)
	mux.HandleFunc("POST /api/accounts/{id}/copy-objects/async", h.copyManyAsync)
	mux.HandleFunc("POST /api/accounts/{id}/delete", h.deleteObjects)
	mux.HandleFunc("POST /api/accounts/{id}/delete-prefix", h.withStreamLimit(h.deletePrefix))
	mux.HandleFunc("POST /api/accounts/{id}/delete-prefix/async", h.deletePrefixAsync)
	mux.HandleFunc("POST /api/accounts/{id}/copy-prefix", h.withStreamLimit(h.copyPrefix))
	mux.HandleFunc("POST /api/accounts/{id}/copy-prefix/async", h.copyPrefixAsync)
	mux.HandleFunc("POST /api/accounts/{id}/download-zip", h.withStreamLimit(h.downloadZip))
	mux.HandleFunc("POST /api/accounts/{id}/multipart/init", h.multipartInit)
	mux.HandleFunc("POST /api/accounts/{id}/multipart/part", h.multipartPart)
	mux.HandleFunc("POST /api/accounts/{id}/multipart/complete", h.multipartComplete)
	mux.HandleFunc("POST /api/accounts/{id}/multipart/abort", h.multipartAbort)
	mux.HandleFunc("GET /api/accounts/{id}/object-acl", h.getObjectAcl)
	mux.HandleFunc("PUT /api/accounts/{id}/object-acl", h.putObjectAcl)
	mux.HandleFunc("GET /api/accounts/{id}/object-tags", h.getObjectTags)
	mux.HandleFunc("PUT /api/accounts/{id}/object-tags", h.putObjectTags)
	mux.HandleFunc("GET /api/accounts/{id}/bucket-info", h.getBucketInfo)
	mux.HandleFunc("PUT /api/accounts/{id}/bucket-versioning", h.putBucketVersioning)
	// 桶级配置：加密 / CORS / 网站托管 / 桶策略 / 桶标签
	mux.HandleFunc("GET /api/accounts/{id}/bucket/encryption", h.getBucketEncryption)
	mux.HandleFunc("PUT /api/accounts/{id}/bucket/encryption", h.putBucketEncryption)
	mux.HandleFunc("DELETE /api/accounts/{id}/bucket/encryption", h.deleteBucketEncryption)
	mux.HandleFunc("GET /api/accounts/{id}/bucket/cors", h.getBucketCors)
	mux.HandleFunc("PUT /api/accounts/{id}/bucket/cors", h.putBucketCors)
	mux.HandleFunc("DELETE /api/accounts/{id}/bucket/cors", h.deleteBucketCors)
	mux.HandleFunc("GET /api/accounts/{id}/bucket/website", h.getBucketWebsite)
	mux.HandleFunc("PUT /api/accounts/{id}/bucket/website", h.putBucketWebsite)
	mux.HandleFunc("DELETE /api/accounts/{id}/bucket/website", h.deleteBucketWebsite)
	mux.HandleFunc("GET /api/accounts/{id}/bucket/policy", h.getBucketPolicy)
	mux.HandleFunc("PUT /api/accounts/{id}/bucket/policy", h.putBucketPolicy)
	mux.HandleFunc("DELETE /api/accounts/{id}/bucket/policy", h.deleteBucketPolicy)
	mux.HandleFunc("GET /api/accounts/{id}/bucket/tags", h.getBucketTags)
	mux.HandleFunc("PUT /api/accounts/{id}/bucket/tags", h.putBucketTags)
	mux.HandleFunc("DELETE /api/accounts/{id}/bucket/tags", h.deleteBucketTags)
	mux.HandleFunc("GET /api/accounts/{id}/versions", h.listObjectVersions)
	mux.HandleFunc("DELETE /api/accounts/{id}/version", h.deleteObjectVersion)
	mux.HandleFunc("POST /api/accounts/{id}/version/restore", h.restoreObjectVersion)
	mux.HandleFunc("POST /api/accounts/{id}/delete-marker/restore", h.restoreDeleteMarker)
	mux.HandleFunc("POST /api/accounts/{id}/storage-class", h.changeStorageClass)
	// 回收站：列出删除标记 / 彻底清除
	mux.HandleFunc("GET /api/accounts/{id}/trash", h.listTrash)
	mux.HandleFunc("POST /api/accounts/{id}/trash/purge", h.purgeTrashObject)

	// 跨账号迁移
	mux.HandleFunc("POST /api/migrate", h.withStreamLimit(h.migrate))
	mux.HandleFunc("POST /api/migrate/async", h.migrateAsync)
	mux.HandleFunc("GET /api/migrate/jobs/{id}", h.migrateJobStatus)
	mux.HandleFunc("POST /api/migrate/jobs/{id}/cancel", h.migrateJobCancel)
	mux.HandleFunc("GET /api/migrate/jobs/{id}/events", h.migrateJobEvents)

	// 静态资源（SPA）
	spa := http.FileServer(http.Dir(h.staticDir))
	mux.Handle("/", h.spaHandler(spa))

	return h.withLogging(h.withSecurityHeaders(h.withCORS(h.withAuth(h.withRateLimit(mux)))))
}

func (h *Handler) spaHandler(fs http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		p := filepath.Join(h.staticDir, filepath.Clean("/"+r.URL.Path))
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		// SPA fallback：仅对页面路由（无扩展名）回退 index.html；
		// 带扩展名的资源缺失时返回 404，避免把 HTML 当 JS/CSS 加载。
		if filepath.Ext(r.URL.Path) != "" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(h.staticDir, "index.html"))
	})
}
