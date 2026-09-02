package service

import (
	"context"
	"path"
	"strings"
	"sync"

	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
)

// BatchResult 批量复制/迁移汇总。
type BatchResult struct {
	OK        int
	Failed    int
	LastError string
	FailKeys  []string
}

// Progress 批量进度回调。
type Progress struct {
	Done   int
	Total  int
	OK     int
	Failed int
	Key    string
	Error  string
	Status string // running|done|cancelled
}

// CopyKeys 同账号内有界并发 CopyObject。
func CopyKeys(
	ctx context.Context,
	client *s3wrap.Client,
	srcBucket, dstBucket string,
	pairs [][2]string, // [srcKey, dstKey]
	workers int,
	onProgress func(Progress),
) BatchResult {
	if workers < 1 {
		workers = 4
	}
	total := len(pairs)
	type res struct {
		key string
		err error
	}
	jobs := make(chan [2]string)
	results := make(chan res, total)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				if ctx.Err() != nil {
					results <- res{p[0], ctx.Err()}
					continue
				}
				err := client.CopyObject(ctx, srcBucket, p[0], dstBucket, p[1])
				results <- res{p[0], err}
			}
		}()
	}
	for _, p := range pairs {
		jobs <- p
	}
	close(jobs)
	wg.Wait()
	close(results)

	var out BatchResult
	done := 0
	for r := range results {
		done++
		if r.err != nil {
			out.Failed++
			out.FailKeys = append(out.FailKeys, r.key)
			if out.LastError == "" {
				out.LastError = "failed at " + r.key + ": " + s3wrap.UserMessage(r.err)
			}
			if onProgress != nil {
				onProgress(Progress{
					Done: done, Total: total, OK: out.OK, Failed: out.Failed,
					Key: r.key, Error: s3wrap.UserMessage(r.err), Status: "running",
				})
			}
			continue
		}
		out.OK++
		if onProgress != nil {
			onProgress(Progress{
				Done: done, Total: total, OK: out.OK, Failed: out.Failed,
				Key: r.key, Status: "running",
			})
		}
	}
	if len(out.FailKeys) > 200 {
		out.FailKeys = out.FailKeys[:200]
	}
	if onProgress != nil {
		status := "done"
		if ctx.Err() != nil {
			status = "cancelled"
		}
		onProgress(Progress{Done: total, Total: total, OK: out.OK, Failed: out.Failed, Status: status})
	}
	return out
}

// RelKey 将源 key 相对 prefix 映射到目标前缀。
func RelKey(srcKey, srcPrefix, targetPrefix string) string {
	rel := strings.TrimPrefix(srcKey, srcPrefix)
	return targetPrefix + rel
}

// BaseKey 取 basename 拼到前缀（copyMany 用）。
func BaseKey(srcKey, targetPrefix string) string {
	return targetPrefix + path.Base(srcKey)
}
