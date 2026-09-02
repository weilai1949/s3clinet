package service

import (
	"archive/zip"
	"context"
	"io"
	"path"
	"strings"
	"sync"
)

const zipFetchWorkers = 4

type zipFetched struct {
	key  string
	body io.ReadCloser
	ct   string
	err  error
}

// WriteObjectsZip 将对象流式写入 ZIP；拉取有界并发（最多 4），写入串行（zip.Writer 非并发安全）。
// 失败的 key 记入返回列表，并在 zip 内写入失败清单。
func WriteObjectsZip(
	ctx context.Context,
	get func(ctx context.Context, key string) (body io.ReadCloser, contentType string, err error),
	keys []string,
	w io.Writer,
) (failKeys []string, err error) {
	zw := zip.NewWriter(w)
	defer func() {
		if cerr := zw.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	if len(keys) == 0 {
		return nil, nil
	}

	jobs := make(chan string)
	results := make(chan zipFetched) // 无缓冲：最多 zipFetchWorkers 个未写入 body 在途
	var wg sync.WaitGroup
	workers := zipFetchWorkers
	if len(keys) < workers {
		workers = len(keys)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range jobs {
				if ctx.Err() != nil {
					results <- zipFetched{key: k, err: ctx.Err()}
					continue
				}
				body, ct, gerr := get(ctx, k)
				results <- zipFetched{key: k, body: body, ct: ct, err: gerr}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, k := range keys {
			select {
			case <-ctx.Done():
				return
			case jobs <- k:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	for item := range results {
		if item.err != nil || item.body == nil {
			failKeys = append(failKeys, item.key)
			if item.body != nil {
				item.body.Close()
			}
			continue
		}
		name := SanitizeZipName(item.key)
		hdr := &zip.FileHeader{Name: name, Method: zip.Store}
		if !LikelyCompressed(name, item.ct) {
			hdr.Method = zip.Deflate
		}
		f, herr := zw.CreateHeader(hdr)
		if herr != nil {
			item.body.Close()
			failKeys = append(failKeys, item.key)
			continue
		}
		_, copyErr := io.Copy(f, &ctxReader{ctx: ctx, r: item.body})
		item.body.Close()
		if copyErr != nil {
			failKeys = append(failKeys, item.key)
		}
	}

	if len(failKeys) > 0 && ctx.Err() == nil {
		if f, ferr := zw.Create("_下载失败清单.txt"); ferr == nil {
			_, _ = io.WriteString(f, strings.Join(failKeys, "\n"))
		}
	}
	return failKeys, nil
}

// SanitizeZipName 把对象 key 转成安全的 ZIP 条目名，避免 zip-slip。
func SanitizeZipName(key string) string {
	name := strings.TrimPrefix(key, "/")
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimRight(p, ". ")
		if p == "" || p == "." || p == ".." {
			p = "_"
		}
		p = strings.ReplaceAll(p, ":", "_")
		out = append(out, p)
	}
	if len(out) == 0 {
		return "download"
	}
	cleaned := path.Clean(strings.Join(out, "/"))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "download"
	}
	return cleaned
}

// LikelyCompressed 判断内容是否已压缩（已压缩则 ZIP 用 Store）。
func LikelyCompressed(name, ct string) bool {
	ct = strings.ToLower(ct)
	if strings.Contains(ct, "zip") || strings.Contains(ct, "gzip") || strings.Contains(ct, "compress") ||
		strings.Contains(ct, "image/") || strings.Contains(ct, "video/") || strings.Contains(ct, "audio/") {
		return true
	}
	lower := strings.ToLower(name)
	for _, ext := range []string{".zip", ".gz", ".tgz", ".bz2", ".xz", ".7z", ".rar", ".jpg", ".jpeg", ".png", ".gif", ".webp", ".mp4", ".mkv", ".mp3", ".pdf"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
