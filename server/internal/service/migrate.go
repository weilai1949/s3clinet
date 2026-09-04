package service

import (
	"context"
	"strings"

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
	return RunBatch(ctx, keys, workers,
		func(k string) string { return k },
		func(ctx context.Context, k string) error {
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
			return merr
		},
		onProgress)
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
