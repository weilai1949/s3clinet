package service

import "testing"

func TestStreamCopyConstants(t *testing.T) {
	if MaxSinglePutBytes != 5_000_000_000 {
		t.Fatalf("MaxSinglePutBytes = %d", MaxSinglePutBytes)
	}
	// 编译期确保 API 形状稳定
	var _ = StreamCopy
}
