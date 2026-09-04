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

// ---- 对象域防腐层 DTO（handler / service 不直接依赖 AWS SDK 类型） ----

// ListPage ListObjectsV2 结果页。
type ListPage struct {
	Objects        []ObjectItem
	CommonPrefixes []string
	IsTruncated    bool
	NextToken      string
}

// listPageFrom 把 ListObjectsV2 输出转换为 ListPage。
func listPageFrom(out *s3.ListObjectsV2Output) *ListPage {
	page := &ListPage{
		Objects:        make([]ObjectItem, 0, len(out.Contents)),
		CommonPrefixes: make([]string, 0, len(out.CommonPrefixes)),
		IsTruncated:    boolOrFalse(out.IsTruncated),
		NextToken:      derefString(out.NextContinuationToken),
	}
	for _, o := range out.Contents {
		page.Objects = append(page.Objects, FromS3Object(o))
	}
	for _, p := range out.CommonPrefixes {
		page.CommonPrefixes = append(page.CommonPrefixes, derefString(p.Prefix))
	}
	return page
}

// TagRow 对象标签键值。
type TagRow struct {
	Key   string
	Value string
}

// TaggingSet 对象标签集。
type TaggingSet struct {
	Tags []TagRow
}

// taggingSetFrom 把 GetObjectTagging 输出转换为 TaggingSet。
func taggingSetFrom(out *s3.GetObjectTaggingOutput) *TaggingSet {
	set := &TaggingSet{Tags: make([]TagRow, 0, len(out.TagSet))}
	for _, t := range out.TagSet {
		set.Tags = append(set.Tags, TagRow{Key: derefString(t.Key), Value: derefString(t.Value)})
	}
	return set
}

// VersionEntry 对象版本 / 删除标记条目。
type VersionEntry struct {
	Key          string
	VersionID    string
	ETag         string
	LastModified time.Time
	Size         int64
	IsLatest     bool
	StorageClass string
}

// VersionsPage ListObjectVersions 结果页。
type VersionsPage struct {
	Versions            []VersionEntry
	DeleteMarkers       []VersionEntry
	IsTruncated         bool
	NextKeyMarker       string
	NextVersionIDMarker string
}

// versionsPageFrom 把 ListObjectVersions 输出转换为 VersionsPage。
func versionsPageFrom(out *s3.ListObjectVersionsOutput) *VersionsPage {
	page := &VersionsPage{
		Versions:            make([]VersionEntry, 0, len(out.Versions)),
		DeleteMarkers:       make([]VersionEntry, 0, len(out.DeleteMarkers)),
		IsTruncated:         boolOrFalse(out.IsTruncated),
		NextKeyMarker:       derefString(out.NextKeyMarker),
		NextVersionIDMarker: derefString(out.NextVersionIdMarker),
	}
	for _, v := range out.Versions {
		page.Versions = append(page.Versions, VersionEntry{
			Key: derefString(v.Key), VersionID: derefString(v.VersionId), ETag: derefString(v.ETag),
			LastModified: timeOrZero(v.LastModified), Size: derefInt64(v.Size),
			IsLatest: boolOrFalse(v.IsLatest), StorageClass: string(v.StorageClass),
		})
	}
	for _, d := range out.DeleteMarkers {
		page.DeleteMarkers = append(page.DeleteMarkers, VersionEntry{
			Key: derefString(d.Key), VersionID: derefString(d.VersionId),
			LastModified: timeOrZero(d.LastModified), IsLatest: boolOrFalse(d.IsLatest),
		})
	}
	return page
}

// PresignPostResult POST 表单预签名结果。
type PresignPostResult struct {
	URL    string
	Fields map[string]string
}
