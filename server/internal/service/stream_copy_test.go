package service

import (
	"context"
	"testing"

	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
)

func TestStreamCopyConstants(t *testing.T) {
	if MaxSinglePutBytes != 5_000_000_000 {
		t.Fatalf("MaxSinglePutBytes = %d", MaxSinglePutBytes)
	}
	// 编译期确保 API 形状稳定
	var _ func(context.Context, *s3wrap.Client, *s3wrap.Client, string, string, string, string) error = StreamCopy
}
