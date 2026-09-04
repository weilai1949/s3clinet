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

// RunBatch 有界并发执行批量任务，并在每个条目完成时立刻回调进度。
// 旧实现先 wg.Wait() 再排空 results，进度全程停在 0/total、任务结束才一次性灌出；
// 现由独立 goroutine 在全部 worker 退出后关闭 results，主 goroutine 边收边回调。
func RunBatch[I any](
	ctx context.Context,
	items []I,
	workers int,
	keyOf func(I) string,
	do func(ctx context.Context, item I) error,
	onProgress func(Progress),
) BatchResult {
	if workers < 1 {
		workers = 4
	}
	total := len(items)
	type itemRes struct {
		key string
		err error
	}
	jobs := make(chan I)
	results := make(chan itemRes, total) // 全量缓冲：worker 永不阻塞
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range jobs {
				if ctx.Err() != nil {
					results <- itemRes{keyOf(it), ctx.Err()}
					continue
				}
				results <- itemRes{keyOf(it), do(ctx, it)}
			}
		}()
	}
	go func() {
		for _, it := range items {
			jobs <- it
		}
		close(jobs)
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

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

// CopyKeys 同账号内有界并发 CopyObject。
func CopyKeys(
	ctx context.Context,
	client *s3wrap.Client,
	srcBucket, dstBucket string,
	pairs [][2]string, // [srcKey, dstKey]
	workers int,
	onProgress func(Progress),
) BatchResult {
	return RunBatch(ctx, pairs, workers,
		func(p [2]string) string { return p[0] },
		func(ctx context.Context, p [2]string) error {
			return client.CopyObject(ctx, srcBucket, p[0], dstBucket, p[1])
		},
		onProgress)
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
