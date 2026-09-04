package s3wrap

import (
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// TestDerefAndTimeHelpers 表驱动验证指针解引用辅助函数的 nil / 非 nil 两路。
func TestDerefAndTimeHelpers(t *testing.T) {
	n := int64(7)
	if got := derefInt64(nil); got != 0 {
		t.Errorf("derefInt64(nil) = %d, want 0", got)
	}
	if got := derefInt64(&n); got != 7 {
		t.Errorf("derefInt64(&7) = %d, want 7", got)
	}
	if got := timeOrZero(nil); !got.IsZero() {
		t.Errorf("timeOrZero(nil) = %v, want zero time", got)
	}
	ts := time.Date(2025, 6, 1, 8, 0, 0, 0, time.UTC)
	if got := timeOrZero(&ts); !got.Equal(ts) {
		t.Errorf("timeOrZero(&ts) = %v, want %v", got, ts)
	}
}

// TestFromS3ObjectNilFields 全空字段的对象也能安全转换（零值 DTO）。
func TestFromS3ObjectNilFields(t *testing.T) {
	o := FromS3Object(types.Object{})
	if o.Key != "" || o.Size != 0 || o.ETag != "" || o.StorageClass != "" || !o.LastModified.IsZero() {
		t.Fatalf("unexpected zero-value conversion: %+v", o)
	}
}

// TestListPageFromGaps 空页与带 CommonPrefixes/游标 的分页转换。
func TestListPageFromGaps(t *testing.T) {
	// 空页：应得到非 nil 的空切片，且游标为空。
	empty := listPageFrom(&s3.ListObjectsV2Output{})
	if empty.Objects == nil || empty.CommonPrefixes == nil {
		t.Fatal("empty page should return non-nil slices")
	}
	if len(empty.Objects) != 0 || len(empty.CommonPrefixes) != 0 || empty.IsTruncated || empty.NextToken != "" {
		t.Fatalf("unexpected empty page: %+v", empty)
	}

	// 完整页：对象、目录前缀、截断与续传游标。
	ts := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	page := listPageFrom(&s3.ListObjectsV2Output{
		Contents: []types.Object{
			{Key: aws.String("a/b.txt"), Size: aws.Int64(3), LastModified: &ts, ETag: aws.String(`"e"`), StorageClass: types.ObjectStorageClassStandard},
		},
		CommonPrefixes:        []types.CommonPrefix{{Prefix: aws.String("a/")}, {Prefix: aws.String("b/")}},
		IsTruncated:           aws.Bool(true),
		NextContinuationToken: aws.String("tok-1"),
	})
	if len(page.Objects) != 1 || page.Objects[0].Key != "a/b.txt" || page.Objects[0].Size != 3 {
		t.Fatalf("objects = %+v", page.Objects)
	}
	wantPrefixes := []string{"a/", "b/"}
	if !reflect.DeepEqual(page.CommonPrefixes, wantPrefixes) {
		t.Fatalf("prefixes = %v, want %v", page.CommonPrefixes, wantPrefixes)
	}
	if !page.IsTruncated || page.NextToken != "tok-1" {
		t.Fatalf("truncated/token = %v/%q", page.IsTruncated, page.NextToken)
	}
}

// TestVersionsPageFromGaps 空页与含版本/删除标记页的转换行为。
func TestVersionsPageFromGaps(t *testing.T) {
	empty := versionsPageFrom(&s3.ListObjectVersionsOutput{})
	if empty.Versions == nil || empty.DeleteMarkers == nil {
		t.Fatal("empty page should return non-nil slices")
	}
	if len(empty.Versions) != 0 || len(empty.DeleteMarkers) != 0 || empty.IsTruncated {
		t.Fatalf("unexpected empty versions page: %+v", empty)
	}

	ts := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
	page := versionsPageFrom(&s3.ListObjectVersionsOutput{
		Versions: []types.ObjectVersion{{
			Key: aws.String("a.txt"), VersionId: aws.String("v1"), ETag: aws.String(`"e1"`),
			LastModified: &ts, Size: aws.Int64(9), IsLatest: aws.Bool(true), StorageClass: types.ObjectVersionStorageClassStandard,
		}},
		DeleteMarkers: []types.DeleteMarkerEntry{{
			Key: aws.String("gone.txt"), VersionId: aws.String("v2"), LastModified: &ts, IsLatest: aws.Bool(false),
		}},
		IsTruncated:         aws.Bool(true),
		NextKeyMarker:       aws.String("m"),
		NextVersionIdMarker: aws.String("mv"),
	})
	if len(page.Versions) != 1 {
		t.Fatalf("versions = %d", len(page.Versions))
	}
	v := page.Versions[0]
	if v.Key != "a.txt" || v.VersionID != "v1" || v.ETag != `"e1"` || v.Size != 9 || !v.IsLatest || !v.LastModified.Equal(ts) || v.StorageClass != "STANDARD" {
		t.Fatalf("version entry = %+v", v)
	}
	if len(page.DeleteMarkers) != 1 {
		t.Fatalf("delete markers = %d", len(page.DeleteMarkers))
	}
	d := page.DeleteMarkers[0]
	if d.Key != "gone.txt" || d.VersionID != "v2" || d.IsLatest || d.Size != 0 {
		t.Fatalf("delete marker entry = %+v", d)
	}
	if !page.IsTruncated || page.NextKeyMarker != "m" || page.NextVersionIDMarker != "mv" {
		t.Fatalf("paging fields = %+v", page)
	}
}

// TestTaggingSetFromGaps 空标签集与多标签转换。
func TestTaggingSetFromGaps(t *testing.T) {
	empty := taggingSetFrom(&s3.GetObjectTaggingOutput{})
	if empty == nil || empty.Tags == nil || len(empty.Tags) != 0 {
		t.Fatalf("empty tagging = %+v", empty)
	}
	set := taggingSetFrom(&s3.GetObjectTaggingOutput{TagSet: []types.Tag{
		{Key: aws.String("env"), Value: aws.String("prod")},
		{Key: aws.String("team"), Value: aws.String("sre")},
	}})
	want := []TagRow{{"env", "prod"}, {"team", "sre"}}
	if !reflect.DeepEqual(set.Tags, want) {
		t.Fatalf("tags = %+v, want %+v", set.Tags, want)
	}
}

// TestDescribeACLOwnerFallbacks owner 显示名为空时回退到 ID；owner 缺省 / 授权缺省均安全。
func TestDescribeACLOwnerFallbacks(t *testing.T) {
	// nil owner：owner 为空、非公开、无行。
	owner, public, rows := DescribeACL(&s3.GetObjectAclOutput{})
	if owner != "" || public || len(rows) != 0 {
		t.Fatalf("empty acl output: owner=%q public=%v rows=%v", owner, public, rows)
	}

	// DisplayName 为空 → 回退 ID；grantee 为 nil → 跳过（不公开）。
	owner, public, rows = DescribeACL(&s3.GetObjectAclOutput{
		Owner: &types.Owner{ID: aws.String("id-9")},
		Grants: []types.Grant{
			{Grantee: nil, Permission: types.PermissionRead},
			{Grantee: &types.Grantee{Type: types.TypeCanonicalUser, DisplayName: aws.String("u1")}, Permission: types.PermissionFullControl},
		},
	})
	if owner != "id-9" {
		t.Fatalf("owner = %q, want id-9", owner)
	}
	if public {
		t.Fatal("canonical-user grants are not public")
	}
	if len(rows) != 2 || rows[0].Grantee != "" || rows[1].Grantee != "u1" || rows[1].Permission != "FULL_CONTROL" {
		t.Fatalf("rows = %+v", rows)
	}
}

// TestGranteeLabelGaps 表驱动覆盖授权对象标签的全部分支。
func TestGranteeLabelGaps(t *testing.T) {
	cases := []struct {
		name    string
		grantee *types.Grantee
		want    string
	}{
		{"nil", nil, ""},
		{"all users", &types.Grantee{Type: types.TypeGroup, URI: aws.String("http://acs.amazonaws.com/groups/global/AllUsers")}, "所有用户 (AllUsers)"},
		{"authenticated users", &types.Grantee{Type: types.TypeGroup, URI: aws.String("http://acs.amazonaws.com/groups/global/AuthenticatedUsers")}, "认证用户 (AuthenticatedUsers)"},
		{"other group uri", &types.Grantee{Type: types.TypeGroup, URI: aws.String("http://acs.amazonaws.com/groups/s3/LogDelivery")}, "组: http://acs.amazonaws.com/groups/s3/LogDelivery"},
		{"group empty uri", &types.Grantee{Type: types.TypeGroup}, "组: "},
		{"user display name", &types.Grantee{Type: types.TypeCanonicalUser, DisplayName: aws.String("alice")}, "alice"},
		{"user id fallback", &types.Grantee{Type: types.TypeCanonicalUser, ID: aws.String("u-42")}, "u-42"},
		{"user both empty", &types.Grantee{Type: types.TypeCanonicalUser}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := GranteeLabel(c.grantee); got != c.want {
				t.Fatalf("GranteeLabel = %q, want %q", got, c.want)
			}
		})
	}
}

// TestACLIsPublicOnlyGroupCounts 组 URI 命中即公开；无组授权则非公开。
func TestACLIsPublicOnlyGroupCounts(t *testing.T) {
	if aclIsPublic(nil) {
		t.Fatal("no grants should not be public")
	}
	public := aclIsPublic([]types.Grant{{
		Grantee:    &types.Grantee{Type: types.TypeGroup, URI: aws.String("http://acs.amazonaws.com/groups/global/AllUsers")},
		Permission: types.PermissionReadAcp,
	}})
	if !public {
		t.Fatal("AllUsers group should be public")
	}
}
