// Package service 承载与 HTTP 无关的领域编排（迁移流式复制、批量任务等），
// 避免 handler 上帝包继续膨胀。handler 仅做鉴权/解析/响应映射。
package service

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
)

// MaxSinglePutBytes 为 S3 PutObject / CopyObject 真实上限。
const MaxSinglePutBytes int64 = 5_000_000_000

// multipartPartSize 分段大小（64MB）；var 以便单测注入小值覆盖多段路径。
var multipartPartSize int64 = 64 << 20

// StreamCopy 跨客户端复制对象；>5GB 或未知大小走 multipart。
func StreamCopy(ctx context.Context, src, dst *s3wrap.Client, srcBucket, srcKey, dstBucket, dstKey string) error {
	stream, err := src.GetObjectStream(ctx, srcBucket, srcKey, "", "")
	if err != nil {
		return err
	}
	defer stream.Body.Close()
	size := int64(-1)
	if stream.ContentLength != nil {
		size = *stream.ContentLength
	}
	if size >= 0 && size <= MaxSinglePutBytes {
		return dst.PutObject(ctx, dstBucket, dstKey, stream.Body, stream.ContentType, stream.Metadata)
	}
	return MultipartStreamCopy(ctx, dst, dstBucket, dstKey, stream.ContentType, stream.Body)
}

// MultipartStreamCopy 将 reader 以 multipart 写入目标。
func MultipartStreamCopy(ctx context.Context, dst *s3wrap.Client, bucket, key, contentType string, body io.Reader) error {
	uploadID, err := dst.CreateMultipartUpload(ctx, bucket, key, contentType)
	if err != nil {
		return err
	}
	var parts []s3wrap.UploadPartSpec
	buf := make([]byte, multipartPartSize)
	var partNum int32 = 1
	abort := func() { _ = dst.AbortMultipartUpload(context.Background(), bucket, key, uploadID) }
	for {
		// 上游 ReadFull 不会感知 ctx；UploadPart 用 ctx；这里仅在 UploadPart 返回后判断退出。
		n, readErr := io.ReadFull(body, buf)
		if n > 0 {
			etag, uerr := dst.UploadPart(ctx, bucket, key, uploadID, partNum, bytes.NewReader(buf[:n]))
			if uerr != nil {
				abort()
				return uerr
			}
			parts = append(parts, s3wrap.UploadPartSpec{PartNumber: partNum, ETag: etag})
			partNum++
			if partNum > 10_000 {
				abort()
				return fmt.Errorf("object exceeds multipart part limit (10000 parts)")
			}
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			abort()
			return readErr
		}
	}
	if len(parts) == 0 {
		abort()
		return dst.PutObject(ctx, bucket, key, bytes.NewReader(nil), contentType, nil)
	}
	if err := dst.CompleteMultipartUpload(ctx, bucket, key, uploadID, parts); err != nil {
		abort()
		return err
	}
	return nil
}
