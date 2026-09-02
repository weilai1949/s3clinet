package s3wrap

import (
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// ---- 领域 DTO（handler / 前端不直接依赖 AWS SDK 类型） ----

// ObjectItem 列出对象的结果项（对应 ListObjectsV2 Contents）。
type ObjectItem struct {
	Key          string
	Size         int64
	LastModified time.Time
	ETag         string
	StorageClass string
}

// FromS3Object 把 ListObjectsV2 的单条 Contents 转换为 DTO。
func FromS3Object(o types.Object) ObjectItem {
	return ObjectItem{
		Key:          derefString(o.Key),
		Size:         derefInt64(o.Size),
		LastModified: timeOrZero(o.LastModified),
		ETag:         derefString(o.ETag),
		StorageClass: string(o.StorageClass),
	}
}

// BucketItem 列出桶的结果项（对应 ListBuckets Bucket）。
type BucketItem struct {
	Name         string
	CreationDate time.Time
}

// FormatBuckets 把 ListBuckets 输出转换为 DTO 列表。
func FormatBuckets(out *s3.ListBucketsOutput) []BucketItem {
	buckets := make([]BucketItem, 0, len(out.Buckets))
	for _, b := range out.Buckets {
		buckets = append(buckets, BucketItem{Name: derefString(b.Name), CreationDate: timeOrZero(b.CreationDate)})
	}
	return buckets
}

// GrantRow 对象 ACL 的单条授权展示行。
type GrantRow struct {
	Grantee    string
	Permission string
}

// DescribeACL 把 GetObjectAcl 输出转换为展示信息（所有者 / 公开性 / 授权行）。
func DescribeACL(out *s3.GetObjectAclOutput) (owner string, public bool, rows []GrantRow) {
	if out.Owner != nil {
		owner = derefString(out.Owner.DisplayName)
		if owner == "" {
			owner = derefString(out.Owner.ID)
		}
	}
	for _, g := range out.Grants {
		rows = append(rows, GrantRow{Grantee: GranteeLabel(g.Grantee), Permission: string(g.Permission)})
	}
	return owner, aclIsPublic(out.Grants), rows
}

// GranteeLabel 授权对象的人类可读描述（组 URI / 显示名 / ID）。
func GranteeLabel(g *types.Grantee) string {
	if g == nil {
		return ""
	}
	if g.Type == types.TypeGroup {
		uri := derefString(g.URI)
		if strings.Contains(uri, "AllUsers") {
			return "所有用户 (AllUsers)"
		}
		if strings.Contains(uri, "AuthenticatedUsers") {
			return "认证用户 (AuthenticatedUsers)"
		}
		return "组: " + uri
	}
	name := derefString(g.DisplayName)
	if name == "" {
		name = derefString(g.ID)
	}
	return name
}

// aclIsPublic 判断 ACL 是否对匿名/认证组开放（拥有 READ 等权限）。
func aclIsPublic(grants []types.Grant) bool {
	for _, g := range grants {
		if g.Grantee == nil || g.Grantee.Type != types.TypeGroup {
			continue
		}
		uri := derefString(g.Grantee.URI)
		if strings.Contains(uri, "AllUsers") || strings.Contains(uri, "AuthenticatedUsers") {
			return true
		}
	}
	return false
}

func derefInt64(i *int64) int64 {
	if i == nil {
		return 0
	}
	return *i
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
