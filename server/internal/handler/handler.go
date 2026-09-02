package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/weilai1949/s3clinet/server/internal/model"
	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
	"github.com/weilai1949/s3clinet/server/internal/service"
	"github.com/weilai1949/s3clinet/server/internal/store"
)

// Handler 承载 HTTP 接口逻辑。
type Handler struct {
	store       store.AccountStore
	log         *slog.Logger
	staticDir   string
	corsOrigins []string // CORS 白名单；空 = 仅同源 + localhost/tauri
	tokens      []string // Bearer 鉴权；可多个（S3C_TOKEN 逗号分隔，支持轮换）
	version     string   // 服务端版本号（ldflags 注入），用于 /api/health 上报
	clients     *clientCache
	migrateJobs *service.JobRegistry
	limiter     *ipLimiter
}

// New 构造 handler。token 支持逗号分隔多值（轮换/吊销：去掉旧 token 即可）。
func New(st store.AccountStore, log *slog.Logger, staticDir string, corsOrigins []string, token, version string) *Handler {
	return &Handler{
		store: st, log: log, staticDir: staticDir, corsOrigins: corsOrigins,
		tokens: splitTokens(token), version: version, clients: newClientCache(),
		migrateJobs: service.NewJobRegistry(),
		limiter:     newIPLimiter(),
	}
}

func splitTokens(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Shutdown 取消进行中的异步迁移并停止 reap 循环。
func (h *Handler) Shutdown() {
	if h.migrateJobs != nil {
		h.migrateJobs.Stop()
	}
}

// ---- DTO ----

type objectItem struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"lastModified"`
	ETag         string    `json:"etag"`
	ContentType  string    `json:"contentType"`
	StorageClass string    `json:"storageClass"`
	IsDir        bool      `json:"isDir"`
}

type listObjectsResponse struct {
	Objects        []objectItem `json:"objects"`
	CommonPrefixes []string     `json:"commonPrefixes"`
	IsTruncated    bool         `json:"isTruncated"`
	NextToken      string       `json:"nextToken"`
}

func fromS3Object(o s3wrap.ObjectItem) objectItem {
	it := objectItem{
		Key:          o.Key,
		Size:         o.Size,
		LastModified: o.LastModified,
		ETag:         o.ETag,
		StorageClass: o.StorageClass,
	}
	it.IsDir = strings.HasSuffix(it.Key, "/")
	return it
}

// ---- helpers ----

const maxBody = 4 << 20 // 4MB request body cap

// maxZipKeys 限制单次打包的对象数，防止一次请求无界流式输出。
const maxZipKeys = 1000

// maxBatchKeys 批量复制/迁移等操作单次 key 上限。
const maxBatchKeys = 10_000

func (h *Handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.log.Error("encode response", "err", err)
	}
}

func (h *Handler) writeErr(w http.ResponseWriter, status int, msg string) {
	h.writeJSON(w, status, map[string]any{"error": msg})
}

// writeBadJSON 记录解析失败详情，向客户端返回固定消息。
func (h *Handler) writeBadJSON(w http.ResponseWriter, err error) {
	h.log.Debug("invalid request body", "err", err)
	h.writeErr(w, http.StatusBadRequest, "invalid request body")
}

// writeInternalErr 记录内部错误详情到日志，向客户端返回通用消息，避免泄露 S3/SDK 细节。
// 若 err 可识别为 S3 API 错误，使用 s3HTTPStatus + s3UserMessage（统一 NoSuchBucket→404 等）。
func (h *Handler) writeInternalErr(w http.ResponseWriter, err error, publicMsg string) {
	if err != nil {
		if code := s3HTTPStatus(err); code != 500 {
			msg := s3UserMessage(err)
			if msg == "" {
				msg = publicMsg
			}
			if publicMsg == "" {
				publicMsg = msg
			}
			h.log.Debug("s3 mapped error", "err", err, "status", code, "public", msg)
			h.writeErr(w, code, msg)
			return
		}
	}
	if publicMsg == "" {
		publicMsg = "internal server error"
	}
	h.log.Error("handler error", "err", err, "public", publicMsg)
	h.writeErr(w, http.StatusInternalServerError, publicMsg)
}

func (h *Handler) readJSON(r *http.Request, v any) error {
	// 强制 JSON 请求体：浏览器对非 JSON 的 POST（text/plain 等）不预检，会放行跨域写；
	// 要求 application/json 让所有变更请求触发 CORS 预检（配合 withCORS 的 403）。
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		return errors.New("Content-Type must be application/json")
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	// 拒绝 JSON 之后的尾部数据，避免歧义请求体。
	if dec.More() {
		return errors.New("unexpected trailing data after JSON body")
	}
	return nil
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt64(i *int64) int64 {
	if i == nil {
		return 0
	}
	return *i
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func boolOrFalse(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}

// accountClient 标准前缀：读取账号（404）并构建 S3 客户端（400）。
// 返回 client、account 与 ok；ok=false 时响应已写出，调用方直接 return。
func (h *Handler) accountClient(w http.ResponseWriter, r *http.Request) (*s3wrap.Client, *model.Account, bool) {
	acc, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		h.writeErr(w, http.StatusNotFound, "account not found")
		return nil, nil, false
	}
	client, err := h.clients.get(acc)
	if err != nil {
		h.log.Debug("s3 client init", "err", err)
		h.writeErr(w, http.StatusBadRequest, "invalid account configuration")
		return nil, nil, false
	}
	return client, acc, true
}

// bucketOr 解析 bucket；为空时写 400 并返回 ok=false。
func (h *Handler) bucketOr(w http.ResponseWriter, acc *model.Account, b string) (string, bool) {
	if b == "" {
		b = acc.BucketOrDefault()
	}
	if b == "" {
		h.writeErr(w, http.StatusBadRequest, "bucket is required")
		return "", false
	}
	return b, true
}
