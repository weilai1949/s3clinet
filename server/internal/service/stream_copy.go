// Package service 承载与 HTTP 无关的领域编排（迁移流式复制、批量任务等），
// 避免 handler 上帝包继续膨胀。handler 仅做鉴权/解析/响应映射。
package service

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
)

// MaxSinglePutBytes 为 S3 PutObject / CopyObject 真实上限。
const MaxSinglePutBytes int64 = 5_000_000_000

const multipartPartSize = 64 << 20

// StreamCopy 跨客户端复制对象；>5GB 或未知大小走 multipart。
func StreamCopy(ctx context.Context, src, dst *s3wrap.Client, srcBucket, srcKey, dstBucket, dstKey string) error {
	out, err := src.GetObject(ctx, srcBucket, srcKey)
	if err != nil {
		return err
	}
	defer out.Body.Close()
	ct := ""
	if out.ContentType != nil {
		ct = *out.ContentType
	}
	size := int64(-1)
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	if size >= 0 && size <= MaxSinglePutBytes {
		meta := &s3.PutObjectInput{
			ContentType: out.ContentType,
			Metadata:    out.Metadata,
		}
		return dst.PutObject(ctx, dstBucket, dstKey, out.Body, meta)
	}
	return MultipartStreamCopy(ctx, dst, dstBucket, dstKey, ct, out.Body)
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
		if err := ctx.Err(); err != nil {
			abort()
			return err
		}
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
		var ct *string
		if contentType != "" {
			ct = &contentType
		}
		return dst.PutObject(ctx, bucket, key, bytes.NewReader(nil), &s3.PutObjectInput{ContentType: ct})
	}
	if err := dst.CompleteMultipartUpload(ctx, bucket, key, uploadID, parts); err != nil {
		abort()
		return err
	}
	return nil
}
