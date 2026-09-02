package s3wrap

import (
	"context"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// UploadPartSpec 描述一个已完成的分段（ETag + 段号），用于 CompleteMultipartUpload。
type UploadPartSpec struct {
	PartNumber int32
	ETag       string
}

// CreateMultipartUpload 初始化分段上传，返回 UploadID。
func (c *Client) CreateMultipartUpload(ctx context.Context, bucket, key, contentType string) (string, error) {
	in := &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if contentType != "" {
		in.ContentType = aws.String(contentType)
	}
	out, err := c.s3.CreateMultipartUpload(ctx, in)
	if err != nil {
		return "", err
	}
	return derefString(out.UploadId), nil
}

// UploadPart 服务端上传单个分段（跨 endpoint 大文件迁移），返回 ETag。
func (c *Client) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int32, body io.Reader) (string, error) {
	out, err := c.s3.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(key),
		UploadId:   aws.String(uploadID),
		PartNumber: aws.Int32(partNumber),
		Body:       body,
	})
	if err != nil {
		return "", err
	}
	return strings.Trim(derefString(out.ETag), `"`), nil
}

// CompleteMultipartUpload 汇总已上传分段，完成分段上传。
func (c *Client) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []UploadPartSpec) error {
	completed := make([]types.CompletedPart, 0, len(parts))
	for _, p := range parts {
		completed = append(completed, types.CompletedPart{
			ETag:       aws.String(p.ETag),
			PartNumber: aws.Int32(p.PartNumber),
		})
	}
	_, err := c.s3.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(bucket),
		Key:             aws.String(key),
		UploadId:        aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: completed},
	})
	return err
}

// AbortMultipartUpload 中止分段上传（失败清理用）。
func (c *Client) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	_, err := c.s3.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	return err
}
