package s3wrap

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const xmlNS = `xmlns="http://s3.amazonaws.com/doc/2006-03-01/"`

// TestListBucketsParsesXML 假 S3 返回 ListAllMyBucketsResult，应解析出桶名与创建时间。
func TestListBucketsParsesXML(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListAllMyBucketsResult ` + xmlNS + `>
<Owner><ID>owner-id</ID><DisplayName>owner</DisplayName></Owner>
<Buckets>
<Bucket><Name>alpha</Name><CreationDate>2024-05-01T00:00:00.000Z</CreationDate></Bucket>
<Bucket><Name>beta</Name><CreationDate>2024-06-01T12:30:00.000Z</CreationDate></Bucket>
</Buckets></ListAllMyBucketsResult>`))
	}))
	out, err := c.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets: %v", err)
	}
	if len(out.Buckets) != 2 {
		t.Fatalf("buckets = %d, want 2", len(out.Buckets))
	}
	if derefString(out.Buckets[0].Name) != "alpha" || derefString(out.Buckets[1].Name) != "beta" {
		t.Fatalf("bucket names = %v/%v", derefString(out.Buckets[0].Name), derefString(out.Buckets[1].Name))
	}
	if want := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC); out.Buckets[0].CreationDate == nil || !out.Buckets[0].CreationDate.Equal(want) {
		t.Fatalf("creation date = %v, want %v", out.Buckets[0].CreationDate, want)
	}
}

// TestHeadBucketExistsAndMissing 连通性探测：存在 200 / 不存在 404。
func TestHeadBucketExistsAndMissing(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if firstPathSegment(r.URL.Path) == "missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	if err := c.HeadBucket(context.Background(), "exists"); err != nil {
		t.Fatalf("HeadBucket(existing): %v", err)
	}
	if err := c.HeadBucket(context.Background(), "missing"); err == nil {
		t.Fatal("HeadBucket(missing) should fail")
	} else if !IsNotFound(err) {
		t.Fatalf("missing bucket error should be NotFound-shaped, got %v", err)
	}
}

// TestCreateBucketSendsRegionAndACL 建桶行为：非 us-east-1 附带 LocationConstraint，acl 写入 x-amz-acl 头。
func TestCreateBucketSendsRegionAndACL(t *testing.T) {
	var gotBody, gotACL, gotPath string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotACL, gotPath = string(b), r.Header.Get("x-amz-acl"), r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	if err := c.CreateBucket(context.Background(), "logs", "ap-southeast-1", "public-read"); err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	if gotPath != "/logs" {
		t.Fatalf("create path = %q, want /logs", gotPath)
	}
	if gotACL != "public-read" {
		t.Fatalf("x-amz-acl = %q, want public-read", gotACL)
	}
	if !strings.Contains(gotBody, "<LocationConstraint>ap-southeast-1</LocationConstraint>") {
		t.Fatalf("body missing LocationConstraint: %q", gotBody)
	}

	gotBody = ""
	if err := c.CreateBucket(context.Background(), "plain", "us-east-1", ""); err != nil {
		t.Fatalf("CreateBucket(us-east-1): %v", err)
	}
	if gotBody != "" {
		t.Fatalf("us-east-1 should not send configuration, body = %q", gotBody)
	}
}

// TestDeleteBucketConflict 删除桶成功 / 桶非空冲突错误映射。
func TestDeleteBucketConflict(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "full") {
			writeS3Error(w, http.StatusConflict, "BucketNotEmpty", "bucket not empty")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := c.DeleteBucket(context.Background(), "empty"); err != nil {
		t.Fatalf("DeleteBucket: %v", err)
	}
	err := c.DeleteBucket(context.Background(), "full")
	if err == nil {
		t.Fatal("expected BucketNotEmpty error")
	}
	if got := UserMessage(err); got != "bucket not empty" {
		t.Fatalf("UserMessage = %q, want bucket not empty", got)
	}
	if got := HTTPStatus(err); got != 409 {
		t.Fatalf("HTTPStatus = %d, want 409", got)
	}
}

// TestLifecycleRulesReadAndWrite 生命周期规则：三种 Filter 形态解析 + 写入请求体 + 删除。
func TestLifecycleRulesReadAndWrite(t *testing.T) {
	var putBody string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><LifecycleConfiguration ` + xmlNS + `>
<Rule><ID>filter-prefix</ID><Filter><Prefix>logs/</Prefix></Filter><Status>Enabled</Status><Expiration><Days>30</Days></Expiration></Rule>
<Rule><ID>filter-and</ID><Filter><And><Prefix>base/</Prefix></And></Filter><Status>Enabled</Status><Expiration><Days>7</Days></Expiration></Rule>
<Rule><ID>legacy-prefix</ID><Prefix>old/</Prefix><Status>Enabled</Status><Expiration><Days>1</Days></Expiration></Rule>
<Rule><ID>no-expiration</ID><Filter><Prefix>tmp/</Prefix></Filter><Status>Enabled</Status></Rule>
</LifecycleConfiguration>`))
		case r.Method == http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			putBody = string(b)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	ctx := context.Background()
	specs, err := c.GetLifecycle(ctx, "bkt")
	if err != nil {
		t.Fatalf("GetLifecycle: %v", err)
	}
	want := []LifecycleRuleSpec{
		{ID: "filter-prefix", Prefix: "logs/", Days: 30},
		{ID: "filter-and", Prefix: "base/", Days: 7},
		{ID: "legacy-prefix", Prefix: "old/", Days: 1},
		{ID: "no-expiration", Prefix: "tmp/", Days: 0},
	}
	if len(specs) != len(want) {
		t.Fatalf("specs = %+v, want %d rules", specs, len(want))
	}
	for i, s := range specs {
		if s != want[i] {
			t.Fatalf("spec[%d] = %+v, want %+v", i, s, want[i])
		}
	}

	if err := c.PutLifecycle(ctx, "bkt", []LifecycleRuleSpec{{ID: "r1", Prefix: "docs/", Days: 90}}); err != nil {
		t.Fatalf("PutLifecycle: %v", err)
	}
	if !strings.Contains(putBody, "<ID>r1</ID>") || !strings.Contains(putBody, "<Prefix>docs/</Prefix>") || !strings.Contains(putBody, "<Days>90</Days>") {
		t.Fatalf("put lifecycle body = %q", putBody)
	}

	if err := c.DeleteLifecycle(ctx, "bkt"); err != nil {
		t.Fatalf("DeleteLifecycle: %v", err)
	}
}

// TestGetLifecycleError 未配置生命周期时错误应透传。
func TestGetLifecycleError(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeS3Error(w, http.StatusNotFound, "NoSuchLifecycleConfiguration", "none")
	}))
	if _, err := c.GetLifecycle(context.Background(), "bkt"); err == nil {
		t.Fatal("expected error for missing lifecycle config")
	}
}

// TestBucketLocationConstraint LocationConstraint 为空视为 us-east-1。
func TestBucketLocationConstraint(t *testing.T) {
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := `<LocationConstraint ` + xmlNS + `/>`
		if !strings.Contains(r.URL.Path, "default") {
			body = `<LocationConstraint ` + xmlNS + `>us-west-2</LocationConstraint>`
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>` + body))
	}))
	ctx := context.Background()
	got, err := c.GetBucketLocation(ctx, "default-bucket")
	if err != nil {
		t.Fatalf("GetBucketLocation: %v", err)
	}
	if got != "us-east-1" {
		t.Fatalf("empty constraint = %q, want us-east-1", got)
	}
	got, err = c.GetBucketLocation(ctx, "west-bucket")
	if err != nil {
		t.Fatalf("GetBucketLocation: %v", err)
	}
	if got != "us-west-2" {
		t.Fatalf("constraint = %q, want us-west-2", got)
	}
}

// TestBucketVersioningGetAndPut 版本控制：读取已配置/未配置状态 + 写入请求体。
func TestBucketVersioningGetAndPut(t *testing.T) {
	var putBody string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			status := ""
			if strings.Contains(r.URL.Path, "enabled") {
				status = "<Status>Enabled</Status>"
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><VersioningConfiguration ` + xmlNS + `>` + status + `</VersioningConfiguration>`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		putBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	ctx := context.Background()
	status, err := c.GetBucketVersioning(ctx, "unconfigured")
	if err != nil || status != "" {
		t.Fatalf("unconfigured versioning = %q err=%v, want empty", status, err)
	}
	status, err = c.GetBucketVersioning(ctx, "enabled")
	if err != nil || status != "Enabled" {
		t.Fatalf("enabled versioning = %q err=%v, want Enabled", status, err)
	}
	if err := c.PutBucketVersioning(ctx, "bkt", "Suspended"); err != nil {
		t.Fatalf("PutBucketVersioning: %v", err)
	}
	if !strings.Contains(putBody, "<Status>Suspended</Status>") {
		t.Fatalf("put versioning body = %q", putBody)
	}
}

// TestBucketEncryptionConfig 加密配置：读全字段 / 未配置 / 空规则 / 校验与写入 / 删除。
func TestBucketEncryptionConfig(t *testing.T) {
	var putBody string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			switch firstPathSegment(r.URL.Path) {
			case "full":
				w.Header().Set("Content-Type", "application/xml")
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ServerSideEncryptionConfiguration ` + xmlNS + `><Rule><ApplyServerSideEncryptionByDefault><SSEAlgorithm>aws:kms</SSEAlgorithm><KMSMasterKeyID>key-1</KMSMasterKeyID></ApplyServerSideEncryptionByDefault><BucketKeyEnabled>true</BucketKeyEnabled></Rule></ServerSideEncryptionConfiguration>`))
			case "empty":
				w.Header().Set("Content-Type", "application/xml")
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ServerSideEncryptionConfiguration ` + xmlNS + `/>`))
			default:
				writeS3Error(w, http.StatusNotFound, "ServerSideEncryptionConfigurationNotFoundError", "none")
			}
		case r.Method == http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			putBody = string(b)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	ctx := context.Background()
	cfg, err := c.GetEncryption(ctx, "full")
	if err != nil {
		t.Fatalf("GetEncryption: %v", err)
	}
	if cfg.Algorithm != "aws:kms" || cfg.KMSKeyID != "key-1" || !cfg.BucketKeyEnabled {
		t.Fatalf("encryption = %+v", cfg)
	}
	if _, err := c.GetEncryption(ctx, "unconfigured"); err == nil {
		t.Fatal("expected error for unconfigured encryption")
	}
	if _, err := c.GetEncryption(ctx, "empty"); err == nil || err.Error() != "no encryption rules" {
		t.Fatalf("empty rules error = %v", err)
	}
	if err := c.PutEncryption(ctx, "bkt", EncryptionConfig{}); err == nil || !strings.Contains(err.Error(), "algorithm is required") {
		t.Fatalf("empty algorithm error = %v", err)
	}
	if err := c.PutEncryption(ctx, "bkt", EncryptionConfig{Algorithm: "AES256", KMSKeyID: "kms-key", BucketKeyEnabled: true}); err != nil {
		t.Fatalf("PutEncryption: %v", err)
	}
	if !strings.Contains(putBody, "<SSEAlgorithm>AES256</SSEAlgorithm>") ||
		!strings.Contains(putBody, "<KMSMasterKeyID>kms-key</KMSMasterKeyID>") ||
		!strings.Contains(putBody, "<BucketKeyEnabled>true</BucketKeyEnabled>") {
		t.Fatalf("put encryption body = %q", putBody)
	}
	if err := c.DeleteEncryption(ctx, "bkt"); err != nil {
		t.Fatalf("DeleteEncryption: %v", err)
	}
}

// TestBucketCorsRules CORS：读取全字段 / 未配置错误 / 写入（空 ID 省略）/ 删除。
func TestBucketCorsRules(t *testing.T) {
	var putBody string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			if strings.Contains(r.URL.Path, "nocors") {
				writeS3Error(w, http.StatusNotFound, "NoSuchCORSConfiguration", "none")
				return
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><CORSConfiguration ` + xmlNS + `><CORSRule><ID>r1</ID><AllowedOrigin>*</AllowedOrigin><AllowedMethod>GET</AllowedMethod><AllowedMethod>PUT</AllowedMethod><AllowedHeader>x-amz-meta</AllowedHeader><ExposeHeader>ETag</ExposeHeader><MaxAgeSeconds>3600</MaxAgeSeconds></CORSRule></CORSConfiguration>`))
		case r.Method == http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			putBody = string(b)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	ctx := context.Background()
	rules, err := c.GetCors(ctx, "bkt")
	if err != nil {
		t.Fatalf("GetCors: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("rules = %d, want 1", len(rules))
	}
	got := rules[0]
	if got.ID != "r1" || got.MaxAgeSeconds != 3600 ||
		strings.Join(got.AllowedMethods, ",") != "GET,PUT" ||
		strings.Join(got.AllowedOrigins, ",") != "*" ||
		strings.Join(got.AllowedHeaders, ",") != "x-amz-meta" ||
		strings.Join(got.ExposeHeaders, ",") != "ETag" {
		t.Fatalf("cors rule = %+v", got)
	}
	if _, err := c.GetCors(ctx, "nocors"); err == nil {
		t.Fatal("expected NoSuchCORSConfiguration error")
	}
	if err := c.PutCors(ctx, "bkt", []CorsRule{{AllowedMethods: []string{"GET"}, AllowedOrigins: []string{"https://example.com"}, MaxAgeSeconds: 60}}); err != nil {
		t.Fatalf("PutCors: %v", err)
	}
	if !strings.Contains(putBody, "<AllowedMethod>GET</AllowedMethod>") ||
		!strings.Contains(putBody, "<AllowedOrigin>https://example.com</AllowedOrigin>") ||
		strings.Contains(putBody, "<ID>") {
		t.Fatalf("put cors body = %q (empty ID should be omitted)", putBody)
	}
	if err := c.DeleteCors(ctx, "bkt"); err != nil {
		t.Fatalf("DeleteCors: %v", err)
	}
}

// TestBucketWebsiteConfig 网站托管：读取三字段 / 部分字段为空 / 写入各形态 / 删除。
func TestBucketWebsiteConfig(t *testing.T) {
	var putBody string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			if strings.Contains(r.URL.Path, "full") {
				w.Header().Set("Content-Type", "application/xml")
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><WebsiteConfiguration ` + xmlNS + `><IndexDocument><Suffix>index.html</Suffix></IndexDocument><ErrorDocument><Key>error.html</Key></ErrorDocument><RedirectAllRequestsTo><HostName>mirror.example.com</HostName></RedirectAllRequestsTo></WebsiteConfiguration>`))
				return
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><WebsiteConfiguration ` + xmlNS + `><IndexDocument><Suffix>idx.html</Suffix></IndexDocument></WebsiteConfiguration>`))
		case r.Method == http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			putBody = string(b)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	ctx := context.Background()
	wc, err := c.GetWebsite(ctx, "full")
	if err != nil {
		t.Fatalf("GetWebsite: %v", err)
	}
	if wc.IndexDocument != "index.html" || wc.ErrorDocument != "error.html" || wc.RedirectAllRequestsTo != "mirror.example.com" {
		t.Fatalf("website = %+v", wc)
	}
	minimal, err := c.GetWebsite(ctx, "minimal")
	if err != nil {
		t.Fatalf("GetWebsite(minimal): %v", err)
	}
	if minimal.IndexDocument != "idx.html" || minimal.ErrorDocument != "" || minimal.RedirectAllRequestsTo != "" {
		t.Fatalf("minimal website = %+v", minimal)
	}
	if err := c.PutWebsite(ctx, "bkt", WebsiteConfig{IndexDocument: "index.html", ErrorDocument: "err.html"}); err != nil {
		t.Fatalf("PutWebsite: %v", err)
	}
	if !strings.Contains(putBody, "<IndexDocument><Suffix>index.html</Suffix></IndexDocument>") ||
		!strings.Contains(putBody, "<ErrorDocument><Key>err.html</Key></ErrorDocument>") {
		t.Fatalf("put website body = %q", putBody)
	}
	if err := c.PutWebsite(ctx, "bkt", WebsiteConfig{RedirectAllRequestsTo: "target.example.com"}); err != nil {
		t.Fatalf("PutWebsite(redirect): %v", err)
	}
	if !strings.Contains(putBody, "<RedirectAllRequestsTo><HostName>target.example.com</HostName></RedirectAllRequestsTo>") {
		t.Fatalf("redirect body = %q", putBody)
	}
	if err := c.DeleteWebsite(ctx, "bkt"); err != nil {
		t.Fatalf("DeleteWebsite: %v", err)
	}
}

// TestBucketPolicyCRUD 桶策略：读写 JSON 字符串与删除。
func TestBucketPolicyCRUD(t *testing.T) {
	policy := `{"Version":"2012-10-17","Statement":[]}`
	var putBody string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			if strings.Contains(r.URL.Path, "nopolicy") {
				writeS3Error(w, http.StatusNotFound, "NoSuchBucketPolicy", "none")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(policy))
		case r.Method == http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			putBody = string(b)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	ctx := context.Background()
	got, err := c.GetPolicy(ctx, "bkt")
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if got != policy {
		t.Fatalf("policy = %q", got)
	}
	if _, err := c.GetPolicy(ctx, "nopolicy"); err == nil {
		t.Fatal("expected NoSuchBucketPolicy error")
	}
	if err := c.PutPolicy(ctx, "bkt", policy); err != nil {
		t.Fatalf("PutPolicy: %v", err)
	}
	if putBody != policy {
		t.Fatalf("put policy body = %q", putBody)
	}
	if err := c.DeletePolicy(ctx, "bkt"); err != nil {
		t.Fatalf("DeletePolicy: %v", err)
	}
}

// TestBucketTagsCRUD 桶标签：读取映射 / NoSuchTagSet / 写入请求体 / 删除。
func TestBucketTagsCRUD(t *testing.T) {
	var putBody string
	c, _ := newFakeS3(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			if strings.Contains(r.URL.Path, "notags") {
				writeS3Error(w, http.StatusNotFound, "NoSuchTagSet", "none")
				return
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Tagging ` + xmlNS + `><TagSet><Tag><Key>env</Key><Value>prod</Value></Tag><Tag><Key>team</Key><Value>data</Value></Tag></TagSet></Tagging>`))
		case r.Method == http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			putBody = string(b)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	ctx := context.Background()
	tags, err := c.GetBucketTags(ctx, "bkt")
	if err != nil {
		t.Fatalf("GetBucketTags: %v", err)
	}
	if len(tags) != 2 || tags["env"] != "prod" || tags["team"] != "data" {
		t.Fatalf("tags = %v", tags)
	}
	if _, err := c.GetBucketTags(ctx, "notags"); err == nil {
		t.Fatal("expected NoSuchTagSet error")
	}
	if err := c.PutBucketTags(ctx, "bkt", map[string]string{"k1": "v1"}); err != nil {
		t.Fatalf("PutBucketTags: %v", err)
	}
	if !strings.Contains(putBody, "<Tag><Key>k1</Key><Value>v1</Value></Tag>") {
		t.Fatalf("put tags body = %q", putBody)
	}
	if err := c.DeleteBucketTags(ctx, "bkt"); err != nil {
		t.Fatalf("DeleteBucketTags: %v", err)
	}
}
