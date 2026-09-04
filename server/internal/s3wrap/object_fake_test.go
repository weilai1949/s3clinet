package s3wrap

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestListObjectsForwardsParametersAndParsesContents 列表：查询参数透传 + Contents XML 解析 + 截断游标。
func TestListObjectsForwardsParametersAndParsesContents(t *testing.T) {
	var gotRawQuery string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult ` + xmlNS + `>
<Name>bkt</Name><Prefix>docs/</Prefix><KeyCount>2</KeyCount><MaxKeys>10</MaxKeys><IsTruncated>true</IsTruncated>
<NextContinuationToken>tok-2</NextContinuationToken>
<Contents><Key>docs/a.txt</Key><LastModified>2024-01-01T00:00:00.000Z</LastModified><ETag>&quot;e1&quot;</ETag><Size>10</Size><StorageClass>STANDARD</StorageClass></Contents>
<Contents><Key>docs/b.txt</Key><LastModified>2024-01-02T00:00:00.000Z</LastModified><ETag>&quot;e2&quot;</ETag><Size>0</Size><StorageClass>STANDARD_IA</StorageClass></Contents>
</ListBucketResult>`))
	}))
	out, err := c.ListObjectsPage(context.Background(), "bkt", "docs/", "/", "tok-1", "docs/_start", 10)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	q := map[string]string{}
	for _, kv := range strings.Split(gotRawQuery, "&") {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			q[parts[0]] = parts[1]
		}
	}
	if q["prefix"] != "docs%2F" || q["max-keys"] != "10" || q["delimiter"] != "%2F" ||
		q["continuation-token"] != "tok-1" || q["start-after"] != "docs%2F_start" {
		t.Fatalf("list params = %v", q)
	}
	if len(out.Objects) != 2 {
		t.Fatalf("contents = %d, want 2", len(out.Objects))
	}
	if out.Objects[0].Key != "docs/a.txt" || out.Objects[0].Size != 10 {
		t.Fatalf("first object = %+v", out.Objects[0])
	}
	if out.Objects[1].StorageClass != "STANDARD_IA" {
		t.Fatalf("storage class = %q", out.Objects[1].StorageClass)
	}
	if !out.IsTruncated || out.NextToken != "tok-2" {
		t.Fatal("truncation cursor should be parsed")
	}
}

// TestListObjectsMinimalCallAndEmptyResult 不传可选参数时不应附加查询参数；空列表返回零条目。
func TestListObjectsMinimalCallAndEmptyResult(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Has("max-keys") || q.Has("delimiter") || q.Has("continuation-token") || q.Has("start-after") {
			t.Errorf("unexpected list params: %v", q)
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult ` + xmlNS + `><Name>bkt</Name><KeyCount>0</KeyCount><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated></ListBucketResult>`))
	}))
	out, err := c.ListObjectsPage(context.Background(), "bkt", "", "", "", "", 0)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if len(out.Objects) != 0 || out.IsTruncated {
		t.Fatalf("empty result = %+v", out)
	}
}

// TestDeleteObjectSingle 单个删除：成功与拒绝路径。
func TestDeleteObjectSingle(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "denied") {
			writeS3Error(w, http.StatusForbidden, "AccessDenied", "no")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := c.DeleteObject(context.Background(), "bkt", "k"); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	err := c.DeleteObject(context.Background(), "bkt", "denied")
	if err == nil {
		t.Fatal("expected AccessDenied error")
	}
	if got := UserMessage(err); got != "access denied" {
		t.Fatalf("UserMessage = %q", got)
	}
}

// TestDeleteObjectsBatchesAtThousand 批量删除：单次 ≤1000，超过分批；空入参不发请求；错误中断。
func TestDeleteObjectsBatchesAtThousand(t *testing.T) {
	var batchSizes []int
	failAll := false
	keys := make([]string, 0, 1001)
	for i := 0; i < 1001; i++ {
		keys = append(keys, "k"+strconv.Itoa(i))
	}
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !r.URL.Query().Has("delete") {
			writeS3Error(w, http.StatusBadRequest, "MalformedXML", "not a delete batch")
			return
		}
		b, _ := io.ReadAll(r.Body)
		batchSizes = append(batchSizes, strings.Count(string(b), "<Key>"))
		if failAll {
			writeS3Error(w, http.StatusForbidden, "AccessDenied", "no")
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><DeleteResult ` + xmlNS + `></DeleteResult>`))
	}))
	ctx := context.Background()
	if err := c.DeleteObjects(ctx, "bkt", []string{"a", "b", "c"}); err != nil {
		t.Fatalf("DeleteObjects(3): %v", err)
	}
	if len(batchSizes) != 1 || batchSizes[0] != 3 {
		t.Fatalf("batches = %v, want [3]", batchSizes)
	}

	batchSizes = nil
	if err := c.DeleteObjects(ctx, "bkt", keys); err != nil {
		t.Fatalf("DeleteObjects(1001): %v", err)
	}
	if len(batchSizes) != 2 || batchSizes[0] != 1000 || batchSizes[1] != 1 {
		t.Fatalf("batching = %v, want [1000 1]", batchSizes)
	}

	batchSizes = nil
	if err := c.DeleteObjects(ctx, "bkt", nil); err != nil {
		t.Fatalf("DeleteObjects(empty): %v", err)
	}
	if len(batchSizes) != 0 {
		t.Fatalf("empty keys should not send requests, got %d", len(batchSizes))
	}

	batchSizes = nil
	failAll = true
	if err := c.DeleteObjects(ctx, "bkt", []string{"x"}); err == nil {
		t.Fatal("expected error when server rejects batch")
	} else if got := HTTPStatus(err); got != 403 {
		t.Fatalf("HTTPStatus = %d, want 403", got)
	}
}

// TestCopyObjectSendsEscapedCopySource 服务端复制：CopySource 头按路径转义 + 错误路径。
func TestCopyObjectSendsEscapedCopySource(t *testing.T) {
	var gotCopySource string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCopySource = r.Header.Get("x-amz-copy-source")
		if strings.Contains(gotCopySource, "deny") {
			writeS3Error(w, http.StatusForbidden, "AccessDenied", "no")
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><CopyObjectResult ` + xmlNS + `><LastModified>2024-01-01T00:00:00.000Z</LastModified><ETag>&quot;e2&quot;</ETag></CopyObjectResult>`))
	}))
	ctx := context.Background()
	if err := c.CopyObject(ctx, "src bucket", "dir/my file.txt", "dst", "copy.txt"); err != nil {
		t.Fatalf("CopyObject: %v", err)
	}
	// url.PathEscape 按整段转义：空格与 "/" 都会编码（SDK 原样放入 x-amz-copy-source 头）。
	if gotCopySource != "src%20bucket%2Fdir%2Fmy%20file.txt" {
		t.Fatalf("CopySource = %q", gotCopySource)
	}
	if err := c.CopyObject(ctx, "deny", "k", "dst", "k"); err == nil {
		t.Fatal("expected error when fake rejects copy")
	} else if got := UserMessage(err); got != "access denied" {
		t.Fatalf("UserMessage = %q", got)
	}
}

// TestCopyObjectWithMetaReplacesMetadata 带元数据复制：REPLACE 指令 + 可选 ContentType + 元数据头。
func TestCopyObjectWithMetaReplacesMetadata(t *testing.T) {
	var directive, contentType, metaColor, copySource string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directive = r.Header.Get("x-amz-metadata-directive")
		contentType = r.Header.Get("Content-Type")
		metaColor = r.Header.Get("x-amz-meta-color")
		copySource = r.Header.Get("x-amz-copy-source")
		w.WriteHeader(http.StatusOK)
	}))
	ctx := context.Background()
	if err := c.CopyObjectWithMeta(ctx, "sb", "sk", "db", "dk", "text/plain", map[string]string{"color": "blue"}); err != nil {
		t.Fatalf("CopyObjectWithMeta: %v", err)
	}
	if directive != "REPLACE" || contentType != "text/plain" || metaColor != "blue" || copySource != "sb%2Fsk" {
		t.Fatalf("directive=%q type=%q meta=%q source=%q", directive, contentType, metaColor, copySource)
	}
	contentType = ""
	if err := c.CopyObjectWithMeta(ctx, "sb", "sk", "db", "dk", "", nil); err != nil {
		t.Fatalf("CopyObjectWithMeta(empty): %v", err)
	}
	if contentType != "" {
		t.Fatalf("empty contentType should not be sent, got %q", contentType)
	}
}

// TestGetObjectVariantsAndErrors Get/Range/Version 三种读取与 NotFound 错误路径。
func TestGetObjectVariantsAndErrors(t *testing.T) {
	var gotRange, gotVersionID string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRange = r.Header.Get("Range")
		gotVersionID = r.URL.Query().Get("versionId")
		if strings.HasSuffix(r.URL.Path, "gone") {
			writeS3Error(w, http.StatusNotFound, "NoSuchKey", "no such key")
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello world"))
	}))
	ctx := context.Background()

	out, err := c.GetObjectStream(ctx, "bkt", "hello.txt", "", "")
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	body, _ := io.ReadAll(out.Body)
	out.Body.Close()
	if string(body) != "hello world" {
		t.Fatalf("body = %q", body)
	}
	if gotRange != "" || gotVersionID != "" {
		t.Fatalf("plain Get should not send range/version, got %q/%q", gotRange, gotVersionID)
	}

	if _, err := c.GetObjectStream(ctx, "bkt", "hello.txt", "", "bytes=0-4"); err != nil {
		t.Fatalf("GetObjectRange: %v", err)
	}
	if gotRange != "bytes=0-4" {
		t.Fatalf("Range header = %q", gotRange)
	}

	if _, err := c.GetObjectStream(ctx, "bkt", "hello.txt", "vid-9", ""); err != nil {
		t.Fatalf("GetObjectVersion: %v", err)
	}
	if gotVersionID != "vid-9" {
		t.Fatalf("versionId param = %q", gotVersionID)
	}

	if _, err := c.GetObjectStream(ctx, "bkt", "gone", "", ""); err == nil {
		t.Fatal("expected NoSuchKey error")
	} else if !IsNotFound(err) || HTTPStatus(err) != 404 {
		t.Fatalf("expected NotFound-shaped 404 error, got %v", err)
	}
}

// TestGetObjectStreamDTO 读取流式 DTO：长度/区间/元数据/内容类型。
func TestGetObjectStreamDTO(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Range", "bytes 0-4/11")
		w.Header().Set("x-amz-meta-color", "blue")
		_, _ = w.Write([]byte("hello"))
	}))
	s, err := c.GetObjectStream(context.Background(), "bkt", "k", "", "bytes=0-4")
	if err != nil {
		t.Fatalf("GetObjectStream: %v", err)
	}
	defer s.Body.Close()
	b, err := io.ReadAll(s.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(b) != "hello" {
		t.Fatalf("body = %q", b)
	}
	if s.ContentType != "text/plain" || s.ContentRange != "bytes 0-4/11" || s.ContentLength == nil || *s.ContentLength != 5 {
		t.Fatalf("stream dto = %+v", s)
	}
	if s.Metadata["color"] != "blue" {
		t.Fatalf("metadata = %v", s.Metadata)
	}
}

// TestHeadObjectMetaDTO HEAD 元数据 DTO：大小/ETag/类型/存储类/自定义元数据。
func TestHeadObjectMetaDTO(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("versionId") != "vid-7" {
			writeS3Error(w, http.StatusBadRequest, "InvalidArgument", "version expected")
			return
		}
		w.Header().Set("Content-Length", "42")
		w.Header().Set("ETag", `"etag-1"`)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Last-Modified", time.Date(2024, 3, 1, 8, 0, 0, 0, time.UTC).Format(http.TimeFormat))
		w.Header().Set("x-amz-storage-class", "STANDARD_IA")
		w.Header().Set("x-amz-meta-owner", "team-a")
		w.WriteHeader(http.StatusOK)
	}))
	m, err := c.HeadObjectMeta(context.Background(), "bkt", "k", "vid-7")
	if err != nil {
		t.Fatalf("HeadObjectMeta: %v", err)
	}
	if m.Size != 42 || m.ETag != `"etag-1"` || m.ContentType != "application/json" || m.StorageClass != "STANDARD_IA" {
		t.Fatalf("meta = %+v", m)
	}
	if m.LastModified.IsZero() || m.Metadata["owner"] != "team-a" {
		t.Fatalf("meta time/metadata = %+v", m)
	}
}

// TestPutObjectSendsBodyAndMetadata 上传：请求体透传 + ContentType + 元数据头 + 无 meta 变体。
func TestPutObjectSendsBodyAndMetadata(t *testing.T) {
	var gotBody, gotType, gotMeta string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "deny") {
			writeS3Error(w, http.StatusForbidden, "AccessDenied", "no")
			return
		}
		b, _ := io.ReadAll(r.Body)
		gotBody, gotType, gotMeta = string(b), r.Header.Get("Content-Type"), r.Header.Get("x-amz-meta-color")
		w.WriteHeader(http.StatusOK)
	}))
	ctx := context.Background()
	if err := c.PutObject(ctx, "bkt", "a/b.txt", strings.NewReader("payload"), "text/csv", map[string]string{"color": "red"}); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	if gotBody != "payload" || gotType != "text/csv" || gotMeta != "red" {
		t.Fatalf("body=%q type=%q meta=%q", gotBody, gotType, gotMeta)
	}
	gotBody, gotType, gotMeta = "", "", ""
	if err := c.PutObject(ctx, "bkt", "plain.txt", strings.NewReader("p2"), "", nil); err != nil {
		t.Fatalf("PutObject(no meta): %v", err)
	}
	if gotBody != "p2" || gotMeta != "" {
		t.Fatalf("nil meta: body=%q meta=%q (SDK 会给请求补默认 Content-Type，不在此断言)", gotBody, gotMeta)
	}
	if err := c.PutObject(ctx, "bkt", "deny", strings.NewReader("x"), "", nil); err == nil {
		t.Fatal("expected error when fake rejects upload")
	}
}

// TestObjectACLRoundTrip ACL：读取 XML（owner + 组/用户授权）与 canned ACL 写入映射。
func TestObjectACLRoundTrip(t *testing.T) {
	var gotACLHeader string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><AccessControlPolicy ` + xmlNS + `>
<Owner><ID>id1</ID><DisplayName>alice</DisplayName></Owner>
<AccessControlList>
<Grant><Grantee xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="CanonicalUser"><ID>uid-2</ID><DisplayName>bob</DisplayName></Grantee><Permission>FULL_CONTROL</Permission></Grant>
<Grant><Grantee xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="Group"><URI>http://acs.amazonaws.com/groups/global/AllUsers</URI></Grantee><Permission>READ</Permission></Grant>
</AccessControlList></AccessControlPolicy>`))
		case r.Method == http.MethodPut:
			gotACLHeader = r.Header.Get("x-amz-acl")
			w.WriteHeader(http.StatusOK)
		}
	}))
	ctx := context.Background()
	owner, public, rows, err := c.GetObjectAcl(ctx, "bkt", "k")
	if err != nil {
		t.Fatalf("GetObjectAcl: %v", err)
	}
	if owner != "alice" {
		t.Fatalf("owner = %q", owner)
	}
	if !public {
		t.Fatal("AllUsers READ grant should be public")
	}
	if len(rows) != 2 || rows[0].Grantee != "bob" || rows[0].Permission != "FULL_CONTROL" {
		t.Fatalf("rows = %+v", rows)
	}

	cases := []struct {
		in   string
		want string
	}{
		{"public-read", "public-read"},
		{"public-read-write", "public-read-write"},
		{"authenticated-read", "authenticated-read"},
		{"aws-exec-read", "aws-exec-read"},
		{"", "private"},
		{"unknown-acl", "private"},
	}
	for _, tc := range cases {
		if err := c.PutObjectAcl(ctx, "bkt", "k", tc.in); err != nil {
			t.Fatalf("PutObjectAcl(%q): %v", tc.in, err)
		}
		if gotACLHeader != tc.want {
			t.Fatalf("PutObjectAcl(%q) header = %q, want %q", tc.in, gotACLHeader, tc.want)
		}
	}
}

// TestObjectTagsRoundTrip 对象标签：写入请求体 / 读取 TagSet / 删除。
func TestObjectTagsRoundTrip(t *testing.T) {
	var putBody, seenMethod string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		switch {
		case r.Method == http.MethodGet:
			if strings.Contains(r.URL.Path, "notags") {
				writeS3Error(w, http.StatusNotFound, "NoSuchTagSet", "none")
				return
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Tagging ` + xmlNS + `><TagSet><Tag><Key>env</Key><Value>e2e</Value></Tag></TagSet></Tagging>`))
		case r.Method == http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			putBody = string(b)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	ctx := context.Background()
	if err := c.PutObjectTags(ctx, "bkt", "k", map[string]string{"env": "e2e", "empty": ""}); err != nil {
		t.Fatalf("PutObjectTags: %v", err)
	}
	if seenMethod != http.MethodPut || !strings.Contains(putBody, "<Tag><Key>env</Key><Value>e2e</Value></Tag>") {
		t.Fatalf("put tags: method=%s body=%q", seenMethod, putBody)
	}
	tags, err := c.GetObjectTags(ctx, "bkt", "k")
	if err != nil {
		t.Fatalf("GetObjectTags: %v", err)
	}
	if len(tags.Tags) != 1 || tags.Tags[0].Key != "env" {
		t.Fatalf("tags = %+v", tags.Tags)
	}
	if _, err := c.GetObjectTags(ctx, "bkt", "notags"); err == nil {
		t.Fatal("expected NoSuchTagSet error")
	}
	if err := c.DeleteObjectTags(ctx, "bkt", "k"); err != nil {
		t.Fatalf("DeleteObjectTags: %v", err)
	}
	if seenMethod != http.MethodDelete {
		t.Fatalf("delete tags method = %s", seenMethod)
	}
}

// TestListObjectVersionsParsesMarkers 版本列表：版本+删除标记+截断游标解析与查询参数透传。
func TestListObjectVersionsParsesMarkers(t *testing.T) {
	var gotRawQuery string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListVersionsResult ` + xmlNS + `>
<Name>bkt</Name><Prefix>t.txt</Prefix><KeyMarker></KeyMarker><VersionIdMarker></VersionIdMarker><MaxKeys>1000</MaxKeys><IsTruncated>true</IsTruncated>
<NextKeyMarker>t.txt</NextKeyMarker><NextVersionIdMarker>vid-next</NextVersionIdMarker>
<Version><Key>t.txt</Key><VersionId>vid1</VersionId><IsLatest>true</IsLatest><LastModified>2024-01-01T00:00:00.000Z</LastModified><ETag>&quot;e&quot;</ETag><Size>2</Size><StorageClass>STANDARD</StorageClass></Version>
<Version><Key>t.txt</Key><VersionId>vid0</VersionId><IsLatest>false</IsLatest><LastModified>2024-01-01T00:00:00.000Z</LastModified><ETag>&quot;e0&quot;</ETag><Size>2</Size><StorageClass>STANDARD</StorageClass></Version>
<DeleteMarker><Key>t.txt</Key><VersionId>dm1</VersionId><IsLatest>false</IsLatest><LastModified>2024-01-02T00:00:00.000Z</LastModified></DeleteMarker>
</ListVersionsResult>`))
	}))
	out, err := c.ListObjectVersions(context.Background(), "bkt", "t.txt", "km", "vm", 1000)
	if err != nil {
		t.Fatalf("ListObjectVersions: %v", err)
	}
	if len(out.Versions) != 2 || len(out.DeleteMarkers) != 1 {
		t.Fatalf("versions=%d markers=%d", len(out.Versions), len(out.DeleteMarkers))
	}
	if out.Versions[0].VersionID != "vid1" || !out.Versions[0].IsLatest {
		t.Fatalf("latest version = %+v", out.Versions[0])
	}
	if out.DeleteMarkers[0].VersionID != "dm1" {
		t.Fatalf("delete marker = %+v", out.DeleteMarkers[0])
	}
	if !out.IsTruncated || out.NextKeyMarker != "t.txt" || out.NextVersionIDMarker != "vid-next" {
		t.Fatal("truncation cursor should be parsed")
	}
	q := rquery(gotRawQuery)
	if q["prefix"] != "t.txt" || q["key-marker"] != "km" || q["version-id-marker"] != "vm" || q["max-keys"] != "1000" {
		t.Fatalf("versions list params = %v", q)
	}
}

// TestListObjectVersionsOmitsEmptyMarkers 空游标不应出现在查询参数中。
func TestListObjectVersionsOmitsEmptyMarkers(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Has("key-marker") || q.Has("version-id-marker") || q.Has("prefix") {
			t.Errorf("unexpected params: %v", q)
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListVersionsResult ` + xmlNS + `><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated></ListVersionsResult>`))
	}))
	out, err := c.ListObjectVersions(context.Background(), "bkt", "", "", "", 1000)
	if err != nil {
		t.Fatalf("ListObjectVersions: %v", err)
	}
	if len(out.Versions) != 0 || len(out.DeleteMarkers) != 0 {
		t.Fatal("expected empty version list")
	}
}

// TestDeleteObjectVersionAndRestoreMarker 删除指定版本 / 移除删除标记都带 versionId。
func TestDeleteObjectVersionSendsVersionID(t *testing.T) {
	var gotVersionID string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVersionID = r.URL.Query().Get("versionId")
		w.WriteHeader(http.StatusNoContent)
	}))
	ctx := context.Background()
	if err := c.DeleteObjectVersion(ctx, "bkt", "k", "vid-del"); err != nil {
		t.Fatalf("DeleteObjectVersion: %v", err)
	}
	if gotVersionID != "vid-del" {
		t.Fatalf("delete version param = %q", gotVersionID)
	}
	if err := c.RestoreDeleteMarker(ctx, "bkt", "k", "vid-marker"); err != nil {
		t.Fatalf("RestoreDeleteMarker: %v", err)
	}
	if gotVersionID != "vid-marker" {
		t.Fatalf("restore marker param = %q", gotVersionID)
	}
}

// TestRestoreObjectVersionCopiesHistory 历史版本恢复：CopySource 带 versionId 查询，返回新版本号。
func TestRestoreObjectVersionCopiesHistory(t *testing.T) {
	var gotCopySource string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCopySource = r.Header.Get("x-amz-copy-source")
		if strings.Contains(gotCopySource, "bad") {
			writeS3Error(w, http.StatusForbidden, "AccessDenied", "no")
			return
		}
		w.Header().Set("x-amz-version-id", "vid-new")
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><CopyObjectResult ` + xmlNS + `><ETag>&quot;e&quot;</ETag></CopyObjectResult>`))
	}))
	vid, err := c.RestoreObjectVersion(context.Background(), "bkt", "k.txt", "vid/old 1")
	if err != nil {
		t.Fatalf("RestoreObjectVersion: %v", err)
	}
	if vid != "vid-new" {
		t.Fatalf("new version = %q", vid)
	}
	if gotCopySource != "bkt%2Fk.txt?versionId=vid%2Fold+1" {
		t.Fatalf("CopySource = %q", gotCopySource)
	}
	if _, err := c.RestoreObjectVersion(context.Background(), "bkt", "k", "bad"); err == nil {
		t.Fatal("expected error when fake rejects restore")
	}
}

// TestChangeObjectStorageClassSendsHeader 存储类型切换：CopySource（含/不含版本）+ storage-class 头 + 新版本号。
func TestChangeObjectStorageClassSendsHeader(t *testing.T) {
	var gotCopySource, gotStorageClass string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCopySource = r.Header.Get("x-amz-copy-source")
		gotStorageClass = r.Header.Get("x-amz-storage-class")
		if gotStorageClass == "DEEP_ARCHIVE" {
			writeS3Error(w, http.StatusBadRequest, "InvalidStorageClass", "unsupported")
			return
		}
		w.Header().Set("x-amz-version-id", "vid-new")
		w.WriteHeader(http.StatusOK)
	}))
	ctx := context.Background()
	vid, err := c.ChangeObjectStorageClass(ctx, "bkt", "k.txt", "vid-1", "GLACIER")
	if err != nil {
		t.Fatalf("ChangeObjectStorageClass: %v", err)
	}
	if vid != "vid-new" || gotStorageClass != "GLACIER" || gotCopySource != "bkt%2Fk.txt?versionId=vid-1" {
		t.Fatalf("vid=%q class=%q source=%q", vid, gotStorageClass, gotCopySource)
	}
	vid, err = c.ChangeObjectStorageClass(ctx, "bkt", "k.txt", "", "STANDARD")
	if err != nil {
		t.Fatalf("ChangeObjectStorageClass(current): %v", err)
	}
	if vid != "vid-new" || gotCopySource != "bkt%2Fk.txt" {
		t.Fatalf("current-version switch: vid=%q source=%q", vid, gotCopySource)
	}
	if _, err := c.ChangeObjectStorageClass(ctx, "bkt", "k", "", "DEEP_ARCHIVE"); err == nil {
		t.Fatal("expected error when fake rejects storage class change")
	}
}

// TestPurgeObjectAcrossPages 回收站彻底清除：跨页收集版本+删除标记、批量删除、跳过其它 key、无版本回退普通删除、错误透传。
func TestPurgeObjectAcrossPages(t *testing.T) {
	var deleteReqs []string
	versionPages := map[string]int{"bkt": 0, "err": 0}
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Has("versions"):
			bucket := firstPathSegment(r.URL.Path)
			if bucket == "err" {
				versionPages["err"]++
				writeS3Error(w, http.StatusInternalServerError, "InternalError", "boom")
				return
			}
			if bucket == "fallback" {
				w.Header().Set("Content-Type", "application/xml")
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListVersionsResult ` + xmlNS + `><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated></ListVersionsResult>`))
				return
			}
			versionPages["bkt"]++
			page := versionPages["bkt"]
			if page == 1 {
				w.Header().Set("Content-Type", "application/xml")
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListVersionsResult ` + xmlNS + `>
<IsTruncated>true</IsTruncated><NextKeyMarker>t.txt</NextKeyMarker><NextVersionIdMarker>vid-next</NextVersionIdMarker>
<Version><Key>t.txt</Key><VersionId>v1</VersionId><IsLatest>true</IsLatest><LastModified>2024-01-01T00:00:00.000Z</LastModified><ETag>&quot;e&quot;</ETag><Size>1</Size></Version>
<Version><Key>t.txt</Key><VersionId>v2</VersionId><IsLatest>false</IsLatest><LastModified>2024-01-01T00:00:00.000Z</LastModified><ETag>&quot;e&quot;</ETag><Size>1</Size></Version>
<Version><Key>other.txt</Key><VersionId>v-other</VersionId><IsLatest>true</IsLatest><LastModified>2024-01-01T00:00:00.000Z</LastModified><ETag>&quot;e&quot;</ETag><Size>1</Size></Version>
<DeleteMarker><Key>t.txt</Key><VersionId>dm1</VersionId><IsLatest>true</IsLatest><LastModified>2024-01-02T00:00:00.000Z</LastModified></DeleteMarker>
</ListVersionsResult>`))
				return
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListVersionsResult ` + xmlNS + `>
<IsTruncated>false</IsTruncated>
<Version><Key>t.txt</Key><VersionId>v3</VersionId><IsLatest>false</IsLatest><LastModified>2024-01-01T00:00:00.000Z</LastModified><ETag>&quot;e&quot;</ETag><Size>1</Size></Version>
</ListVersionsResult>`))
		case q.Has("delete"):
			b, _ := io.ReadAll(r.Body)
			if firstPathSegment(r.URL.Path) == "bkt" {
				deleteReqs = append(deleteReqs, string(b))
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><DeleteResult ` + xmlNS + `></DeleteResult>`))
		default:
			// 无版本回退路径：普通 DeleteObject
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	ctx := context.Background()
	deleted, err := c.PurgeObject(ctx, "bkt", "t.txt")
	if err != nil {
		t.Fatalf("PurgeObject: %v", err)
	}
	if deleted != 4 {
		t.Fatalf("deleted = %d, want 4 (v1+v2+dm1+v3)", deleted)
	}
	if versionPages["bkt"] != 2 {
		t.Fatalf("version pages = %d, want 2", versionPages["bkt"])
	}
	// PurgeObject 每收集一页就批量删除一次：页 1 → v1+v2+dm1，页 2 → v3。
	if len(deleteReqs) != 2 {
		t.Fatalf("delete batches = %d, want 2", len(deleteReqs))
	}
	for _, id := range []string{"v1", "v2", "dm1"} {
		if !strings.Contains(deleteReqs[0], "<VersionId>"+id+"</VersionId>") {
			t.Fatalf("batch 1 missing version %s: %q", id, deleteReqs[0])
		}
	}
	if !strings.Contains(deleteReqs[1], "<VersionId>v3</VersionId>") {
		t.Fatalf("batch 2 missing v3: %q", deleteReqs[1])
	}
	for _, body := range deleteReqs {
		if strings.Contains(body, "other.txt") {
			t.Fatal("versions of other keys must be skipped")
		}
	}

	// 无任何版本 → 回退到普通 DeleteObject，计数 1。
	deleted, err = c.PurgeObject(ctx, "fallback", "t.txt")
	if err != nil {
		t.Fatalf("PurgeObject(fallback): %v", err)
	}
	if deleted != 1 {
		t.Fatalf("fallback deleted = %d, want 1", deleted)
	}

	// 列表失败 → 错误透传。
	if _, err := c.PurgeObject(ctx, "err", "t.txt"); err == nil {
		t.Fatal("expected list error to propagate")
	}
}

// TestDeleteObjectsOnPurgeFailureStops 批量删除失败时 PurgeObject 应返回已删除计数与错误。
func TestPurgeObjectDeleteFailureStops(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("versions") {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListVersionsResult ` + xmlNS + `>
<IsTruncated>false</IsTruncated>
<Version><Key>t.txt</Key><VersionId>v1</VersionId><IsLatest>true</IsLatest><LastModified>2024-01-01T00:00:00.000Z</LastModified><ETag>&quot;e&quot;</ETag><Size>1</Size></Version>
</ListVersionsResult>`))
			return
		}
		writeS3Error(w, http.StatusForbidden, "AccessDenied", "no")
	}))
	deleted, err := c.PurgeObject(context.Background(), "bkt", "t.txt")
	if err == nil {
		t.Fatal("expected delete error to propagate")
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d before failure, want 0", deleted)
	}
}

// TestGetObjectWithVersionSkipsEmptyVersionID 空版本号/区间不应附加参数（与 GetObject 复用 getObjectIn）。
func TestGetObjectWithEmptyOptionalParams(t *testing.T) {
	var gotVersionParam, gotRange string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVersionParam, gotRange = r.URL.Query().Get("versionId"), r.Header.Get("Range")
		_, _ = w.Write([]byte("ok"))
	}))
	if _, err := c.GetObjectStream(context.Background(), "bkt", "k", "", ""); err != nil {
		t.Fatalf("GetObjectVersion(empty): %v", err)
	}
	if gotVersionParam != "" || gotRange != "" {
		t.Fatalf("empty optional params leaked: versionId=%q range=%q", gotVersionParam, gotRange)
	}
}

// rquery 解析 RawQuery 为 map（值已解码）。
func rquery(raw string) map[string]string {
	m := map[string]string{}
	for _, kv := range strings.Split(raw, "&") {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			v, err := url.QueryUnescape(parts[1])
			if err != nil {
				v = parts[1]
			}
			m[parts[0]] = v
		}
	}
	return m
}
