package s3wrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/weilai1949/s3clinet/server/internal/model"
)

func e2eSkip(t *testing.T) {
	if os.Getenv("S3CLINET_E2E") != "1" {
		t.Skip("set S3CLINET_E2E=1 to run end-to-end RustFS validation")
	}
}

func e2eAccount(name string) (*model.Account, string) {
	endpoint := getenv("S3CLINET_ENDPOINT", "http://127.0.0.1:9000")
	ak := getenv("S3CLINET_ACCESS_KEY", "rustfsadmin")
	sk := getenv("S3CLINET_SECRET_KEY", "rustfsadmin")
	return &model.Account{
		Name: name, Endpoint: endpoint, Region: "us-east-1",
		AccessKey: ak, SecretKey: sk, Bucket: "", PathStyle: true,
	}, endpoint
}

// TestE2ERustFS 针对真实 S3 端点做端到端联调（默认指向本地 RustFS）。
// 仅当环境变量 S3CLINET_E2E=1 时运行，否则跳过（普通 go test ./... 不受影响）。
// 用配置：
//
//	S3CLINET_ENDPOINT    （默认 http://127.0.0.1:9000）
//	S3CLINET_ACCESS_KEY  （默认 rustfsadmin）
//	S3CLINET_SECRET_KEY  （默认 rustfsadmin）
func TestE2ERustFS(t *testing.T) {
	e2eSkip(t)
	acc, endpoint := e2eAccount("e2e")
	ctx := context.Background()
	c, err := New(acc)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	bucket := fmt.Sprintf("s3c-e2e-%d", time.Now().UnixNano())
	t.Logf("endpoint=%s bucket=%s", endpoint, bucket)

	// 清理：尽力删除测试遗留（失败仅告警）
	defer func() { _ = cleanupBucket(ctx, c, bucket) }()

	// 1) 建桶 + 连通
	if err := c.CreateBucket(ctx, bucket, "us-east-1", "private"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	if err := c.HeadBucket(ctx, bucket); err != nil {
		t.Fatalf("head bucket: %v", err)
	}

	// 2) PutObject / GetObject / HeadObject
	txt := "hello s3clinet e2e"
	if err := c.PutObject(ctx, bucket, "hello.txt", strings.NewReader(txt), &s3.PutObjectInput{}); err != nil {
		t.Fatalf("put object: %v", err)
	}
	if got := getObjectString(t, ctx, c, bucket, "hello.txt"); got != txt {
		t.Fatalf("get object = %q, want %q", got, txt)
	}

	// 3) CopyObject（服务端复制）
	if err := c.CopyObject(ctx, bucket, "hello.txt", bucket, "copy.txt"); err != nil {
		t.Fatalf("copy object: %v", err)
	}
	if got := getObjectString(t, ctx, c, bucket, "copy.txt"); got != txt {
		t.Fatalf("copied object = %q, want %q", got, txt)
	}

	// 4) PresignPut（浏览器直传路径）：拿到预签名 URL 后真实 HTTP PUT
	presignedPut, err := c.PresignPut(ctx, bucket, "presign.txt", 10*time.Minute)
	if err != nil {
		t.Fatalf("presign put: %v", err)
	}
	if _, err := httpPut(presignedPut, []byte("presigned body")); err != nil {
		t.Fatalf("http put to presigned url: %v", err)
	}
	if got := getObjectString(t, ctx, c, bucket, "presign.txt"); got != "presigned body" {
		t.Fatalf("presigned object = %q, want %q", got, "presigned body")
	}

	// 5) 分段上传（直传 + 预签名每段）→ 组装后回读校验
	partA := bytes.Repeat([]byte("A"), 6<<20) // 6 MiB
	partB := bytes.Repeat([]byte("B"), 6<<20)
	uploadID, err := c.CreateMultipartUpload(ctx, bucket, "multi.bin", "application/octet-stream")
	if err != nil {
		t.Fatalf("create multipart: %v", err)
	}
	urlA, err := c.PresignUploadPart(ctx, bucket, "multi.bin", uploadID, 1, 10*time.Minute)
	if err != nil {
		t.Fatalf("presign part 1: %v", err)
	}
	etagA, err := httpPut(urlA, partA)
	if err != nil {
		t.Fatalf("put part 1: %v", err)
	}
	urlB, err := c.PresignUploadPart(ctx, bucket, "multi.bin", uploadID, 2, 10*time.Minute)
	if err != nil {
		t.Fatalf("presign part 2: %v", err)
	}
	etagB, err := httpPut(urlB, partB)
	if err != nil {
		t.Fatalf("put part 2: %v", err)
	}
	if etagA == "" || etagB == "" {
		t.Fatalf("empty part etag: %q / %q", etagA, etagB)
	}
	if err := c.CompleteMultipartUpload(ctx, bucket, "multi.bin", uploadID, []UploadPartSpec{
		{PartNumber: 1, ETag: etagA},
		{PartNumber: 2, ETag: etagB},
	}); err != nil {
		t.Fatalf("complete multipart: %v", err)
	}
	gotMulti := getObjectBytes(t, ctx, c, bucket, "multi.bin")
	wantMulti := append(append([]byte{}, partA...), partB...)
	if !bytes.Equal(gotMulti, wantMulti) {
		t.Fatalf("multipart content len=%d, want %d", len(gotMulti), len(wantMulti))
	}

	// 6) 对象标签（写后读、空对象无标签）：兼容「200+空 TagSet」与「NoSuchTagSet 错误」两种实现；
	//    其他任何错误都视为失败（此前该探针恒真、从不断言）。
	if err := c.PutObject(ctx, bucket, "notagged.txt", strings.NewReader("x"), &s3.PutObjectInput{}); err != nil {
		t.Fatalf("put notagged.txt: %v", err)
	}
	if _, er := c.GetObjectTags(ctx, bucket, "notagged.txt"); er != nil && !isNoSuchTagSet(er) && !isNotImplemented(er) {
		t.Fatalf("get tags of untagged object = %v, want empty or NoSuchTagSet", er)
	}
	if err := c.PutObjectTags(ctx, bucket, "hello.txt", map[string]string{"env": "e2e", "round": "v1"}); err != nil {
		t.Fatalf("put tags: %v", err)
	}
	tags, err := c.GetObjectTags(ctx, bucket, "hello.txt")
	if err != nil {
		t.Fatalf("get tags: %v", err)
	}
	if len(tags.TagSet) != 2 {
		t.Fatalf("tags len = %d, want 2", len(tags.TagSet))
	}

	// 7) ACL（部分端点实现为空操作，仅要求接口可调用、读回非空 Owner；失败软性告警）
	if err := c.PutObjectAcl(ctx, bucket, "hello.txt", "public-read"); err != nil && !isNotImplemented(err) {
		t.Logf("note: PutObjectAcl err=%v (endpoint may not enforce ACL)", err)
	}
	if acl, er := c.GetObjectAcl(ctx, bucket, "hello.txt"); er == nil && (acl.Owner == nil || derefString(acl.Owner.ID) == "") {
		t.Logf("note: GetObjectAcl owner empty")
	} else if er != nil && !isNotImplemented(er) {
		t.Logf("note: GetObjectAcl err=%v", er)
	}

	// 8) 版本控制：开启 → 覆盖写两次 → 列出多个版本
	if err := c.PutBucketVersioning(ctx, bucket, "Enabled"); err != nil {
		t.Fatalf("put bucket versioning: %v", err)
	}
	if v, err := c.GetBucketVersioning(ctx, bucket); err != nil || v != "Enabled" {
		t.Fatalf("get bucket versioning = %q err=%v, want Enabled", v, err)
	}
	if err := c.PutObject(ctx, bucket, "v.txt", strings.NewReader("v1"), &s3.PutObjectInput{}); err != nil {
		t.Fatalf("put v1: %v", err)
	}
	if err := c.PutObject(ctx, bucket, "v.txt", strings.NewReader("v2"), &s3.PutObjectInput{}); err != nil {
		t.Fatalf("put v2: %v", err)
	}
	vers, err := c.ListObjectVersions(ctx, bucket, "v.txt", "", "", 1000)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(vers.Versions) < 2 {
		t.Fatalf("versions count = %d, want >= 2", len(vers.Versions))
	}
	latest := 0
	for _, v := range vers.Versions {
		if boolPtr(v.IsLatest) {
			latest++
		}
	}
	if latest != 1 {
		t.Fatalf("isLatest count = %d, want 1", latest)
	}

	// 9) 版本恢复 / 删除指定版本
	var oldestID string
	for _, v := range vers.Versions {
		if !boolPtr(v.IsLatest) {
			oldestID = derefString(v.VersionId)
			break
		}
	}
	if oldestID == "" {
		t.Fatalf("no historical version to restore")
	}
	restoredID, err := c.RestoreObjectVersion(ctx, bucket, "v.txt", oldestID)
	if err != nil {
		t.Fatalf("restore version: %v", err)
	}
	t.Logf("restored v.txt from %s -> new version %s", oldestID, restoredID)
	if err := c.DeleteObjectVersion(ctx, bucket, "v.txt", oldestID); err != nil {
		t.Fatalf("delete version: %v", err)
	}

	t.Logf("E2E OK: %d versions, multipart assembled %d bytes", len(vers.Versions), len(gotMulti))
}

// TestE2EBatch1 针对真实 S3 端点验证 Batch1 新能力：删除标记一键还原、版本预签名、存储类型切换。
func TestE2EBatch1(t *testing.T) {
	e2eSkip(t)
	acc, endpoint := e2eAccount("e2e-b1")

	ctx := context.Background()
	c, err := New(acc)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	bucket := fmt.Sprintf("s3c-e2e-b1-%d", time.Now().UnixNano())
	t.Logf("endpoint=%s bucket=%s", endpoint, bucket)
	defer func() { _ = cleanupBucket(ctx, c, bucket) }()

	if err := c.CreateBucket(ctx, bucket, "us-east-1", "private"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	if err := c.PutBucketVersioning(ctx, bucket, "Enabled"); err != nil {
		t.Fatalf("enable versioning: %v", err)
	}

	// 1) 版本控制下写两个版本
	if err := c.PutObject(ctx, bucket, "dm.txt", strings.NewReader("dm1"), &s3.PutObjectInput{}); err != nil {
		t.Fatalf("put dm1: %v", err)
	}
	if err := c.PutObject(ctx, bucket, "dm.txt", strings.NewReader("dm2"), &s3.PutObjectInput{}); err != nil {
		t.Fatalf("put dm2: %v", err)
	}
	vers, err := c.ListObjectVersions(ctx, bucket, "dm.txt", "", "", 1000)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	var dmOldest string
	for _, v := range vers.Versions {
		if derefString(v.Key) == "dm.txt" && !boolPtr(v.IsLatest) {
			dmOldest = derefString(v.VersionId)
			break
		}
	}
	if dmOldest == "" {
		t.Fatalf("no historical dm.txt version")
	}

	// 2) 删除对象 → 产生删除标记，且对象不可读
	if err := c.DeleteObject(ctx, bucket, "dm.txt"); err != nil {
		t.Fatalf("delete dm.txt: %v", err)
	}
	if _, err := c.GetObject(ctx, bucket, "dm.txt"); err == nil {
		t.Fatalf("expected dm.txt unreadable after delete marker")
	}
	vers2, err := c.ListObjectVersions(ctx, bucket, "dm.txt", "", "", 1000)
	if err != nil {
		t.Fatalf("list versions after delete: %v", err)
	}
	dmMarkerID := ""
	for _, d := range vers2.DeleteMarkers {
		if derefString(d.Key) == "dm.txt" {
			dmMarkerID = derefString(d.VersionId)
		}
	}
	if dmMarkerID == "" {
		t.Fatalf("no delete marker for dm.txt")
	}

	// 3) 一键还原删除标记 → 对象回到被删除前（内容 dm2 恢复可见）
	if err := c.RestoreDeleteMarker(ctx, bucket, "dm.txt", dmMarkerID); err != nil {
		t.Fatalf("restore delete marker: %v", err)
	}
	if got := getObjectString(t, ctx, c, bucket, "dm.txt"); got != "dm2" {
		t.Fatalf("after undelete dm.txt = %q, want dm2", got)
	}

	// 4) 版本预签名：拉取历史版本 dmOldest → 内容为 dm1
	url, err := c.PresignGetVersion(ctx, bucket, "dm.txt", dmOldest, 10*time.Minute)
	if err != nil {
		t.Fatalf("presign version: %v", err)
	}
	if !strings.Contains(url, "versionId") {
		t.Fatalf("presigned version url missing versionId: %s", url)
	}
	if got := httpGetString(t, url); got != "dm1" {
		t.Fatalf("presigned version content = %q, want dm1", got)
	}

	// 5) 存储类型切换（copy-to-self；不同端点对 storage class 支持程度不同，
	//    RustFS 等仅支持 STANDARD/REDUCED_REDUNDANCY，对扩展类会返回 InvalidStorageClass。
	//    此处仅记录结果，避免因端点能力受限而误判为失败。）
	newVer, err := c.ChangeObjectStorageClass(ctx, bucket, "dm.txt", "", "STANDARD_IA")
	if err != nil {
		if isInvalidStorageClass(err) {
			t.Logf("note: endpoint does not support STANDARD_IA (RustFS etc.)")
		} else {
			t.Fatalf("change storage class: %v", err)
		}
	} else if newVer == "" {
		t.Logf("note: change storage class produced no new version id (non-versioned or endpoint)")
	}

	t.Logf("E2E Batch1 OK: undeleted=%s", "dm.txt")
}

// TestE2EBucketSettings 针对真实 S3 端点验证桶级配置（CORS / 标签 / 网站托管 / 加密 / 策略）。
// 部分端点能力受限（如商家不支持某类加密），做容错处理。
func TestE2EBucketSettings(t *testing.T) {
	e2eSkip(t)
	acc, endpoint := e2eAccount("e2e-bk")

	ctx := context.Background()
	c, err := New(acc)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	bucket := fmt.Sprintf("s3c-e2e-bk-%d", time.Now().UnixNano())
	t.Logf("endpoint=%s bucket=%s", endpoint, bucket)
	defer func() { _ = cleanupBucket(ctx, c, bucket) }()

	if err := c.CreateBucket(ctx, bucket, "us-east-1", "private"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	// CORS：写 → 读（部分端点 CORS 走配置而非 S3 API，NotImplemented 则跳过）
	if err := c.PutCors(ctx, bucket, []CorsRule{{ID: "r1", AllowedMethods: []string{"GET"}, AllowedOrigins: []string{"*"}}}); err != nil {
		if isNotImplemented(err) {
			t.Logf("note: endpoint does not implement PutBucketCors via S3 API")
		} else {
			t.Fatalf("put cors: %v", err)
		}
	} else if rules, er := c.GetCors(ctx, bucket); er != nil || len(rules) != 1 || rules[0].ID != "r1" {
		t.Fatalf("get cors = %+v err=%v", rules, er)
	}

	// 标签：写 → 读
	if err := c.PutBucketTags(ctx, bucket, map[string]string{"env": "e2e"}); err != nil {
		if isNotImplemented(err) {
			t.Logf("note: endpoint does not implement bucket tagging")
		} else {
			t.Fatalf("put bucket tags: %v", err)
		}
	} else if tags, er := c.GetBucketTags(ctx, bucket); er != nil || tags["env"] != "e2e" {
		t.Fatalf("get bucket tags = %v err=%v", tags, er)
	}

	// 网站托管：写 → 读（部分端点可能拒绝；容错）
	if err := c.PutWebsite(ctx, bucket, WebsiteConfig{IndexDocument: "index.html", ErrorDocument: "error.html"}); err != nil {
		if isNotImplemented(err) || isMalformedXML(err) {
			t.Logf("note: endpoint rejected website hosting config (%v)", err)
		} else {
			t.Fatalf("put website: %v", err)
		}
	} else if wc, er := c.GetWebsite(ctx, bucket); er != nil || wc.IndexDocument != "index.html" {
		t.Fatalf("get website = %+v err=%v", wc, er)
	}

	// 桶策略：写 → 读
	if err := c.PutPolicy(ctx, bucket, `{"Version":"2012-10-17","Statement":[]}`); err != nil {
		if isNotImplemented(err) {
			t.Logf("note: endpoint does not implement bucket policy")
		} else {
			t.Fatalf("put bucket policy: %v", err)
		}
	} else if got, er := c.GetPolicy(ctx, bucket); er != nil || got == "" {
		t.Fatalf("get bucket policy = %q err=%v", got, er)
	}

	// 加密：写（AES256，部分端点不支持则容错）
	if err := c.PutEncryption(ctx, bucket, EncryptionConfig{Algorithm: "AES256", BucketKeyEnabled: true}); err != nil {
		if isNotImplemented(err) {
			t.Logf("note: endpoint does not implement SSE encryption")
		} else {
			t.Fatalf("put encryption: %v", err)
		}
	} else if cfg, er := c.GetEncryption(ctx, bucket); er != nil || cfg.Algorithm != "AES256" {
		t.Fatalf("get encryption = %+v err=%v", cfg, er)
	}

	t.Logf("E2E BucketSettings OK: cors/tags/website/policy/encryption verified")
}

// TestE2ETrash 验证回收站核心：版本控制下删除对象产生删除标记，PurgeObject 彻底清除全部版本+标记。
func TestE2ETrash(t *testing.T) {
	e2eSkip(t)
	acc, endpoint := e2eAccount("e2e-trash")

	ctx := context.Background()
	c, err := New(acc)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	bucket := fmt.Sprintf("s3c-e2e-trash-%d", time.Now().UnixNano())
	t.Logf("endpoint=%s bucket=%s", endpoint, bucket)
	defer func() { _ = cleanupBucket(ctx, c, bucket) }()

	if err := c.CreateBucket(ctx, bucket, "us-east-1", "private"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	if err := c.PutBucketVersioning(ctx, bucket, "Enabled"); err != nil {
		t.Fatalf("enable versioning: %v", err)
	}

	if err := c.PutObject(ctx, bucket, "t.txt", strings.NewReader("v1"), &s3.PutObjectInput{}); err != nil {
		t.Fatalf("put v1: %v", err)
	}
	if err := c.PutObject(ctx, bucket, "t.txt", strings.NewReader("v2"), &s3.PutObjectInput{}); err != nil {
		t.Fatalf("put v2: %v", err)
	}
	if err := c.DeleteObject(ctx, bucket, "t.txt"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	vers, err := c.ListObjectVersions(ctx, bucket, "t.txt", "", "", 1000)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(vers.DeleteMarkers) == 0 {
		t.Fatalf("expected a delete marker for t.txt")
	}

	// 彻底清除：应删除 2 个普通版本 + 1 个删除标记
	n, err := c.PurgeObject(ctx, bucket, "t.txt")
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n != 3 {
		t.Fatalf("purge deleted %d, want 3", n)
	}
	// 对象不可读，且无任何版本/标记残留
	if _, err := c.GetObject(ctx, bucket, "t.txt"); err == nil {
		t.Fatalf("t.txt still readable after purge")
	}
	after, err := c.ListObjectVersions(ctx, bucket, "t.txt", "", "", 1000)
	if err != nil {
		t.Fatalf("list versions after: %v", err)
	}
	found := false
	for _, v := range after.Versions {
		if derefString(v.Key) == "t.txt" {
			found = true
		}
	}
	for _, d := range after.DeleteMarkers {
		if derefString(d.Key) == "t.txt" {
			found = true
		}
	}
	if found {
		t.Fatalf("t.txt stale versions remain after purge")
	}

	t.Logf("E2E Trash OK: purged %d versions+markers", n)
}

func getObjectString(t *testing.T, ctx context.Context, c *Client, bucket, key string) string {
	t.Helper()
	out, err := c.GetObject(ctx, bucket, key)
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	defer out.Body.Close()
	b, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return string(b)
}

func getObjectBytes(t *testing.T, ctx context.Context, c *Client, bucket, key string) []byte {
	t.Helper()
	out, err := c.GetObject(ctx, bucket, key)
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	defer out.Body.Close()
	b, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return b
}

// httpGetString 对预签名 GET URL 执行请求并返回响应体字符串（无 CORS 约束）。
func httpGetString(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("http get %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read http get: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("http get status=%d body=%s", resp.StatusCode, truncate(string(b), 200))
	}
	return string(b)
}

// httpPut 对预签名 URL 执行 PUT，返回响应 ETag 头。
func httpPut(url string, body []byte) (string, error) {
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("put status=%d body=%s", resp.StatusCode, truncate(string(rb), 200))
	}
	return resp.Header.Get("ETag"), nil
}

func cleanupBucket(ctx context.Context, c *Client, bucket string) error {
	// 依次删除所有版本与删除标记（版本控制开启时需逐个版本删除）。
	for {
		out, err := c.ListObjectVersions(ctx, bucket, "", "", "", 1000)
		if err != nil {
			return err
		}
		if len(out.Versions) == 0 && len(out.DeleteMarkers) == 0 && len(out.CommonPrefixes) == 0 {
			break
		}
		for _, v := range out.Versions {
			key, vid := derefString(v.Key), derefString(v.VersionId)
			if key == "" {
				continue
			}
			in := &s3.DeleteObjectInput{Bucket: &bucket, Key: &key}
			if vid != "" {
				in.VersionId = &vid
			}
			_, _ = c.S3().DeleteObject(ctx, in)
		}
		for _, d := range out.DeleteMarkers {
			key, vid := derefString(d.Key), derefString(d.VersionId)
			if key == "" {
				continue
			}
			in := &s3.DeleteObjectInput{Bucket: &bucket, Key: &key}
			if vid != "" {
				in.VersionId = &vid
			}
			_, _ = c.S3().DeleteObject(ctx, in)
		}
	}
	// 删除当前对象（版本控制下也许有遗漏，再清一遍普通对象）
	out, err := c.ListObjects(ctx, bucket, "", "", "", "", 1000)
	if err == nil {
		for _, o := range out.Contents {
			key := derefString(o.Key)
			if key != "" {
				_, _ = c.S3().DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: &key})
			}
		}
	}
	return c.DeleteBucket(ctx, bucket)
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func boolPtr(b *bool) bool { return b != nil && *b }

func isNoSuchTagSet(err error) bool {
	var ae smithy.APIError
	if errors.As(err, &ae) {
		c := ae.ErrorCode()
		return c == "NoSuchTagSet" || c == "NoSuchTagSetError"
	}
	return false
}

func isNotImplemented(err error) bool {
	var ae smithy.APIError
	if errors.As(err, &ae) {
		c := ae.ErrorCode()
		return c == "NotImplemented" || c == "NotSupported" || c == "MethodNotAllowed"
	}
	return false
}

func isInvalidStorageClass(err error) bool {
	var ae smithy.APIError
	if errors.As(err, &ae) {
		c := ae.ErrorCode()
		return c == "InvalidStorageClass" || c == "NotImplemented"
	}
	return false
}

func isMalformedXML(err error) bool {
	var ae smithy.APIError
	if errors.As(err, &ae) {
		return ae.ErrorCode() == "MalformedXML"
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
