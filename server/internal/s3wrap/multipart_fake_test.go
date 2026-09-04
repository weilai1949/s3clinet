package s3wrap

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestCreateMultipartUploadReturnsID 初始化分段上传：返回 UploadID；ContentType 透传到请求头。
func TestCreateMultipartUploadReturnsID(t *testing.T) {
	var gotType string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><InitiateMultipartUploadResult ` + xmlNS + `><Bucket>bkt</Bucket><Key>k</Key><UploadId>upid-123</UploadId></InitiateMultipartUploadResult>`))
	}))
	ctx := context.Background()
	id, err := c.CreateMultipartUpload(ctx, "bkt", "big.bin", "application/octet-stream")
	if err != nil {
		t.Fatalf("CreateMultipartUpload: %v", err)
	}
	if id != "upid-123" {
		t.Fatalf("upload id = %q", id)
	}
	if gotType != "application/octet-stream" {
		t.Fatalf("content type header = %q", gotType)
	}
}

// TestCreateMultipartUploadWithoutContentType 未指定 Content-Type 时不应带该头。
func TestCreateMultipartUploadWithoutContentType(t *testing.T) {
	var gotType string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><InitiateMultipartUploadResult ` + xmlNS + `><UploadId>upid-2</UploadId></InitiateMultipartUploadResult>`))
	}))
	id, err := c.CreateMultipartUpload(context.Background(), "bkt", "k", "")
	if err != nil || id != "upid-2" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if gotType != "" {
		t.Fatalf("no content type requested, got %q", gotType)
	}
}

// TestCreateMultipartUploadError 初始化失败时错误透传。
func TestCreateMultipartUploadError(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeS3Error(w, http.StatusForbidden, "AccessDenied", "no")
	}))
	if _, err := c.CreateMultipartUpload(context.Background(), "bkt", "k", ""); err == nil {
		t.Fatal("expected error")
	}
}

// TestUploadPartTrimsQuotesFromETag 分段上传：返回的 ETag 应去掉引号、无引号原样返回、错误路径透传。
func TestUploadPartTrimsQuotesFromETag(t *testing.T) {
	var gotPart, gotUploadID string
	calls := 0
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotPart = r.URL.Query().Get("partNumber")
		gotUploadID = r.URL.Query().Get("uploadId")
		switch calls {
		case 1:
			w.Header().Set("ETag", `"abc-123"`)
		case 2:
			w.Header().Set("ETag", "plain-etag")
		default:
			writeS3Error(w, http.StatusInternalServerError, "InternalError", "boom")
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	ctx := context.Background()
	etag, err := c.UploadPart(ctx, "bkt", "k", "upid-1", 2, strings.NewReader("part-2"))
	if err != nil {
		t.Fatalf("UploadPart: %v", err)
	}
	if etag != "abc-123" {
		t.Fatalf("etag = %q, want quotes trimmed", etag)
	}
	if gotPart != "2" || gotUploadID != "upid-1" {
		t.Fatalf("part=%q uploadID=%q", gotPart, gotUploadID)
	}
	etag, err = c.UploadPart(ctx, "bkt", "k", "upid-1", 3, strings.NewReader("x"))
	if err != nil || etag != "plain-etag" {
		t.Fatalf("unquoted etag=%q err=%v", etag, err)
	}
	if _, err := c.UploadPart(ctx, "bkt", "k", "upid-1", 4, strings.NewReader("x")); err == nil {
		t.Fatal("expected error for failed part upload")
	}
}

// TestCompleteMultipartUploadAssemblesParts 组装：请求体包含各分段 ETag/PartNumber。
func TestCompleteMultipartUploadAssemblesParts(t *testing.T) {
	var gotBody string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><CompleteMultipartUploadResult ` + xmlNS + `><Location>http://127.0.0.1/b/k</Location><Bucket>b</Bucket><Key>k</Key><ETag>&quot;final&quot;</ETag></CompleteMultipartUploadResult>`))
	}))
	if err := c.CompleteMultipartUpload(context.Background(), "bkt", "k", "upid-9", []UploadPartSpec{
		{PartNumber: 1, ETag: "e1"},
		{PartNumber: 2, ETag: "e2"},
	}); err != nil {
		t.Fatalf("CompleteMultipartUpload: %v", err)
	}
	if !strings.Contains(gotBody, "<Part><ETag>e1</ETag><PartNumber>1</PartNumber></Part>") ||
		!strings.Contains(gotBody, "<Part><ETag>e2</ETag><PartNumber>2</PartNumber></Part>") {
		t.Fatalf("complete body = %q", gotBody)
	}
}

// TestCompleteMultipartUploadError 组装失败（如分段缺失）时错误透传。
func TestCompleteMultipartUploadError(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeS3Error(w, http.StatusBadRequest, "MalformedXML", "bad parts")
	}))
	err := c.CompleteMultipartUpload(context.Background(), "bkt", "k", "upid-9", []UploadPartSpec{{PartNumber: 1, ETag: "e1"}})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := UserMessage(err); got != "invalid request" || HTTPStatus(err) != 400 {
		t.Fatalf("message=%q status=%d, want invalid request/400", UserMessage(err), HTTPStatus(err))
	}
}

// TestAbortMultipartUploadSendsUploadID 中止：DELETE + uploadId 查询参数。
func TestAbortMultipartUploadSendsUploadID(t *testing.T) {
	var gotUploadID, gotMethod string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUploadID, gotMethod = r.URL.Query().Get("uploadId"), r.Method
		if gotUploadID == "gone" {
			writeS3Error(w, http.StatusNotFound, "NoSuchUpload", "no such upload")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := c.AbortMultipartUpload(context.Background(), "bkt", "k", "upid-7"); err != nil {
		t.Fatalf("AbortMultipartUpload: %v", err)
	}
	if gotMethod != http.MethodDelete || gotUploadID != "upid-7" {
		t.Fatalf("method=%s uploadID=%q", gotMethod, gotUploadID)
	}
	err := c.AbortMultipartUpload(context.Background(), "bkt", "k", "gone")
	if err == nil {
		t.Fatal("expected NoSuchUpload error when abort fails")
	}
	if got := UserMessage(err); got != "multipart upload not found" {
		t.Fatalf("UserMessage = %q", got)
	}
}
