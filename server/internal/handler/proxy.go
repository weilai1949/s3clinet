package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
	"github.com/weilai1949/s3clinet/server/internal/service"
)

// proxyObject 安全代理对象内容：
//   - mode=download：强制 Content-Disposition: attachment 流式转发（下载永不进渲染管道），支持 Range
//   - mode=inline：透传 Content-Type 流式转发（图片/PDF/媒体预览），支持 Range
//   - mode=text：读取前 maxBytes 字节并强制 text/plain + nosniff（文本预览，杜绝 HTML 注入）
func (h *Handler) proxyObject(w http.ResponseWriter, r *http.Request) {
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
	mode := q.Get("mode")
	if mode == "" {
		mode = "download"
	}
	versionID := q.Get("versionId")

	switch mode {
	case "text":
		// 文本预览：限制读取量，强制纯文本输出，前端以转义文本渲染。
		maxBytes := 1 << 20 // 默认 1MB
		if mb := q.Get("maxBytes"); mb != "" {
			if n, err := strconv.Atoi(mb); err == nil && n > 0 && n <= 2<<20 {
				maxBytes = n
			}
		}
		out, err := client.GetObjectStream(r.Context(), bucket, key, "", "")
		if err != nil {
			h.proxyErr(w, err)
			return
		}
		defer out.Body.Close()
		buf := make([]byte, maxBytes+1)
		n, _ := io.ReadFull(out.Body, buf)
		truncated := n > maxBytes
		if truncated {
			n = maxBytes
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Length", strconv.Itoa(n))
		if truncated {
			w.Header().Set("X-Preview-Truncated", "1")
		}
		beginStreamResponse(w)
		w.WriteHeader(http.StatusOK)
		w.Write(buf[:n])

	case "inline", "download":
		rng := r.Header.Get("Range")
		out, err := client.GetObjectStream(r.Context(), bucket, key, versionID, rng)
		if err != nil {
			h.proxyErr(w, err)
			return
		}
		defer out.Body.Close()
		ct := out.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		// 透传源类型；download 模式或非安全类型强制 attachment（浏览器直接保存，不渲染），
		// 避免恶意对象（HTML/SVG 等）在应用源站被渲染执行。
		disp := "inline"
		if mode == "download" || !inlineSafeCT(ct) {
			disp = "attachment"
		}
		name := sanitizeFilename(path.Base(key))
		w.Header().Set("Content-Type", ct)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, disp, name, url.QueryEscape(name)))
		if out.ContentLength != nil {
			w.Header().Set("Content-Length", strconv.FormatInt(*out.ContentLength, 10))
		}
		if out.ContentRange != "" {
			w.Header().Set("Content-Range", out.ContentRange)
		}
		beginStreamResponse(w)
		if rng != "" && out.ContentRange != "" {
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		copyStream(w, r, out.Body)

	default:
		h.writeErr(w, http.StatusBadRequest, "invalid mode (download|inline|text)")
	}
}

// inlineSafeCT 判断 Content-Type 是否允许以 inline 方式渲染（白名单）。
// 显式排除 image/svg+xml（可含脚本，inline 有 XSS 风险）。
func inlineSafeCT(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
	switch {
	case ct == "image/svg+xml":
		return false
	case strings.HasPrefix(ct, "image/"):
		return true
	case strings.HasPrefix(ct, "audio/"):
		return true
	case strings.HasPrefix(ct, "video/"):
		return true
	case ct == "application/pdf", ct == "text/plain":
		return true
	default:
		return false
	}
}

// proxyErr 将 S3 代理错误映射为 404/416/500。
func (h *Handler) proxyErr(w http.ResponseWriter, err error) {
	if s3wrap.IsNotFound(err) {
		h.writeErr(w, http.StatusNotFound, "object not found")
		return
	}
	if s3wrap.HasErrorCode(err, "InvalidRange") {
		h.writeErr(w, http.StatusRequestedRangeNotSatisfiable, "requested range not satisfiable")
		return
	}
	h.writeInternalErr(w, err, "failed to proxy object")
}

// sanitizeZipName 委托 service 防腐/路径消毒。
func sanitizeZipName(key string) string { return service.SanitizeZipName(key) }

// sanitizeFilename 去除文件名中的路径分隔符与引号，避免 Content-Disposition 注入。
func sanitizeFilename(name string) string {
	name = strings.NewReplacer("/", "_", "\\", "_", `"`, "_", "\n", "_", "\r", "_").Replace(name)
	if name == "" {
		return "download"
	}
	return name
}
