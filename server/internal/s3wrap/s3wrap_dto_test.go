package s3wrap

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestFromS3Object(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	o := FromS3Object(types.Object{
		Key:          aws.String("a.txt"),
		Size:         aws.Int64(42),
		LastModified: &ts,
		ETag:         aws.String(`"etag"`),
		StorageClass: types.ObjectStorageClassStandardIa,
	})
	if o.Key != "a.txt" || o.Size != 42 || o.StorageClass != "STANDARD_IA" {
		t.Fatalf("unexpected %+v", o)
	}
}

func TestGranteeLabel(t *testing.T) {
	if GranteeLabel(nil) != "" {
		t.Fatal("nil grantee")
	}
	g := &types.Grantee{
		Type: types.TypeGroup,
		URI:  aws.String("http://acs.amazonaws.com/groups/global/AllUsers"),
	}
	if got := GranteeLabel(g); got != "所有用户 (AllUsers)" {
		t.Fatalf("got %q", got)
	}
}

func TestDescribeACL(t *testing.T) {
	out := &s3.GetObjectAclOutput{
		Owner: &types.Owner{DisplayName: aws.String("owner")},
		Grants: []types.Grant{{
			Grantee:    &types.Grantee{Type: types.TypeGroup, URI: aws.String("http://acs.amazonaws.com/groups/global/AllUsers")},
			Permission: types.PermissionRead,
		}},
	}
	owner, public, rows := DescribeACL(out)
	if owner != "owner" || !public || len(rows) != 1 {
		t.Fatalf("owner=%q public=%v rows=%d", owner, public, len(rows))
	}
}
