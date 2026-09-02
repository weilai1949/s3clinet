package service

import (
	"context"
	"strings"
	"sync"

	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
)

// MigrateKeys 跨账号/桶迁移：同端点优先 CopyObject（EntityTooLarge 回退流式），异端点走 StreamCopy。
func MigrateKeys(
	ctx context.Context,
	src, dst *s3wrap.Client,
	srcBucket, dstBucket string,
	keys []string,
	targetPrefix string,
	sameEP bool,
	workers int,
	onProgress func(Progress),
) BatchResult {
	if workers < 1 {
		workers = 4
	}
	total := len(keys)
	type res struct {
		key string
		err error
	}
	jobs := make(chan string)
	results := make(chan res, total)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range jobs {
				if ctx.Err() != nil {
					results <- res{k, ctx.Err()}
					continue
				}
				dstKey := targetPrefix + k
				var merr error
				if sameEP {
					merr = src.CopyObject(ctx, srcBucket, k, dstBucket, dstKey)
					if merr != nil && s3wrap.IsEntityTooLarge(merr) {
						merr = StreamCopy(ctx, src, dst, srcBucket, k, dstBucket, dstKey)
					}
				} else {
					merr = StreamCopy(ctx, src, dst, srcBucket, k, dstBucket, dstKey)
				}
				results <- res{k, merr}
			}
		}()
	}
	for _, k := range keys {
		jobs <- k
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

// SameEndpoint 比较两个 endpoint（保留 scheme，去尾斜杠，缺省补 http://）。
func SameEndpoint(a, b string) bool {
	na, nb := normalizeEndpoint(a), normalizeEndpoint(b)
	return na != "" && nb != "" && na == nb
}

func normalizeEndpoint(ep string) string {
	ep = strings.TrimSpace(ep)
	ep = strings.TrimRight(ep, "/")
	if ep == "" {
		return ""
	}
	lower := strings.ToLower(ep)
	if !strings.Contains(lower, "://") {
		lower = "http://" + lower
	}
	return lower
}
