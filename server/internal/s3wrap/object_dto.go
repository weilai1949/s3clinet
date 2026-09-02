package s3wrap

import (
	"context"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ObjectStream 对象内容流（防腐层 DTO，避免 handler 依赖 *s3.GetObjectOutput）。
type ObjectStream struct {
	Body          io.ReadCloser
	ContentType   string
	ContentLength *int64
	ContentRange  string
	Metadata      map[string]string
}

// ObjectMeta 对象头信息（防腐层 DTO）。
type ObjectMeta struct {
	Size         int64
	LastModified time.Time
	ETag         string
	ContentType  string
	StorageClass string
	Metadata     map[string]string
}

// GetObjectStream 读取对象内容流。
func (c *Client) GetObjectStream(ctx context.Context, bucket, key, versionID, rangeHeader string) (*ObjectStream, error) {
	out, err := c.getObjectIn(ctx, bucket, key, versionID, rangeHeader)
	if err != nil {
		return nil, err
	}
	return objectStreamFrom(out), nil
}

func objectStreamFrom(out *s3.GetObjectOutput) *ObjectStream {
	s := &ObjectStream{
		Body:          out.Body,
		ContentType:   aws.ToString(out.ContentType),
		ContentLength: out.ContentLength,
		ContentRange:  aws.ToString(out.ContentRange),
		Metadata:      out.Metadata,
	}
	return s
}

// HeadObjectMeta 获取对象元数据 DTO。
func (c *Client) HeadObjectMeta(ctx context.Context, bucket, key, versionID string) (*ObjectMeta, error) {
	out, err := c.HeadObject(ctx, bucket, key, versionID)
	if err != nil {
		return nil, err
	}
	m := &ObjectMeta{
		Size:         aws.ToInt64(out.ContentLength),
		ETag:         aws.ToString(out.ETag),
		ContentType:  aws.ToString(out.ContentType),
		StorageClass: string(out.StorageClass),
		Metadata:     out.Metadata,
	}
	if out.LastModified != nil {
		m.LastModified = *out.LastModified
	}
	return m, nil
}
