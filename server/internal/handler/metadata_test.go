package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/weilai1949/s3clinet/server/internal/model"
	"github.com/weilai1949/s3clinet/server/internal/store"
)

// TestObjectAcl 用假 S3 验证：读取对象 ACL（公有性/授权/公开链接）与设置 canned ACL。
func TestObjectAcl(t *testing.T) {
	const publicACLXML = `<?xml version="1.0" encoding="UTF-8"?><AccessControlPolicy xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>owner-id</ID><DisplayName>owner</DisplayName></Owner><AccessControlList><Grant><Grantee xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="CanonicalUser"><ID>owner-id</ID><DisplayName>owner</DisplayName></Grantee><Permission>FULL_CONTROL</Permission></Grant><Grant><Grantee xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="Group"><URI>http://acs.amazonaws.com/groups/global/AllUsers</URI></Grantee><Permission>READ</Permission></Grant></AccessControlList></AccessControlPolicy>`
	const privateACLXML = `<?xml version="1.0" encoding="UTF-8"?><AccessControlPolicy xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>owner-id</ID><DisplayName>owner</DisplayName></Owner><AccessControlList><Grant><Grantee xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="CanonicalUser"><ID>owner-id</ID><DisplayName>owner</DisplayName></Grantee><Permission>FULL_CONTROL</Permission></Grant></AccessControlList></AccessControlPolicy>`

	var (
		mu      sync.Mutex
		putAcl  string
		putAcls []string
	)
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !r.URL.Query().Has("acl") {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch r.Method {
		case http.MethodGet:
			if strings.HasSuffix(r.URL.Path, "private.txt") {
				io.WriteString(w, privateACLXML)
			} else {
				io.WriteString(w, publicACLXML)
			}
		case http.MethodPut:
			mu.Lock()
			putAcl = r.Header.Get("X-Amz-Acl")
			putAcls = append(putAcls, putAcl)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer s3fake.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test", false).Routes()

	// 公开对象：public=true、含 AllUsers 授权、返回公开链接
	rr := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/object-acl?key=public.txt", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("get acl status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Public bool   `json:"public"`
		Owner  string `json:"owner"`
		URL    string `json:"url"`
		Grants []struct {
			Grantee    string `json:"grantee"`
			Permission string `json:"permission"`
		} `json:"grants"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body: %v", err)
	}
	if !resp.Public || resp.Owner != "owner" {
		t.Fatalf("public=%v owner=%q, want true/owner", resp.Public, resp.Owner)
	}
	if resp.URL == "" || !strings.Contains(resp.URL, "/b/public.txt") {
		t.Fatalf("url=%q, want path-style public url", resp.URL)
	}
	if len(resp.Grants) != 2 {
		t.Fatalf("grants len = %d, want 2", len(resp.Grants))
	}

	// 私有对象：public=false
	rr2 := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/object-acl?key=private.txt", "")
	var resp2 struct {
		Public bool `json:"public"`
	}
	_ = json.Unmarshal(rr2.Body.Bytes(), &resp2)
	if resp2.Public {
		t.Fatalf("private object public=true, want false")
	}

	// 设置 ACL（public-read → x-amz-acl 头）
	rr3 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/object-acl", `{"key":"public.txt","acl":"public-read"}`)
	if rr3.Code != http.StatusOK {
		t.Fatalf("put acl status = %d, body=%s", rr3.Code, rr3.Body.String())
	}
	mu.Lock()
	gotAcl := putAcl
	mu.Unlock()
	if gotAcl != "public-read" {
		t.Fatalf("x-amz-acl = %q, want public-read", gotAcl)
	}

	// 非法 ACL → 400；缺 key → 400
	if rr4 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/object-acl", `{"key":"x","acl":"foo"}`); rr4.Code != http.StatusBadRequest {
		t.Fatalf("invalid acl = %d, want 400", rr4.Code)
	}
	if rr5 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/object-acl", `{"acl":"private"}`); rr5.Code != http.StatusBadRequest {
		t.Fatalf("put without key = %d, want 400", rr5.Code)
	}
}

// TestObjectTags 用假 S3 验证：读取/写入/清空对象标签（含 NoSuchTagSet 空列表与校验）。
func TestObjectTags(t *testing.T) {
	const taggedXML = `<?xml version="1.0" encoding="UTF-8"?><Tagging><TagSet><Tag><Key>k1</Key><Value>v1</Value></Tag><Tag><Key>k2</Key><Value>v2</Value></Tag></TagSet></Tagging>`

	var (
		mu          sync.Mutex
		putBody     string
		deleteCalls int
	)
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !r.URL.Query().Has("tagging") {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch r.Method {
		case http.MethodGet:
			if strings.HasSuffix(r.URL.Path, "notags.txt") {
				w.WriteHeader(http.StatusNotFound)
				io.WriteString(w, `<?xml version="1.0"?><Error><Code>NoSuchTagSet</Code><Message>The TagSet does not exist</Message></Error>`)
				return
			}
			io.WriteString(w, taggedXML)
		case http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			putBody = string(b)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			mu.Lock()
			deleteCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer s3fake.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test", false).Routes()

	// 读取标签
	rr := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/object-tags?key=tagged.txt", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("get tags status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Tags []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"tags"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body: %v", err)
	}
	if len(resp.Tags) != 2 || resp.Tags[0].Key != "k1" || resp.Tags[0].Value != "v1" {
		t.Fatalf("tags = %+v, want k1=v1,k2=v2", resp.Tags)
	}

	// 无标签对象 → NoSuchTagSet → 空列表
	rr2 := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/object-tags?key=notags.txt", "")
	if rr2.Code != http.StatusOK {
		t.Fatalf("get no-tags status = %d, want 200", rr2.Code)
	}
	var resp2 struct {
		Tags []struct {
			Key string `json:"key"`
		} `json:"tags"`
	}
	_ = json.Unmarshal(rr2.Body.Bytes(), &resp2)
	if len(resp2.Tags) != 0 {
		t.Fatalf("no-tags list = %+v, want empty", resp2.Tags)
	}

	// 写入标签
	rr3 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/object-tags",
		`{"key":"tagged.txt","tags":[{"key":"k1","value":"v1"},{"key":"k2","value":"v2"}]}`)
	if rr3.Code != http.StatusOK {
		t.Fatalf("put tags status = %d, body=%s", rr3.Code, rr3.Body.String())
	}
	mu.Lock()
	if !strings.Contains(putBody, "<Key>k1</Key>") || !strings.Contains(putBody, "<Value>v1</Value>") {
		t.Fatalf("putBody = %s", putBody)
	}
	mu.Unlock()

	// 空标签 → 删除全部标签
	rr4 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/object-tags", `{"key":"tagged.txt","tags":[]}`)
	if rr4.Code != http.StatusOK {
		t.Fatalf("put empty tags status = %d", rr4.Code)
	}
	mu.Lock()
	if deleteCalls != 1 {
		t.Fatalf("deleteCalls = %d, want 1", deleteCalls)
	}
	mu.Unlock()

	// 校验：空 key / 重复 key / 缺 key
	if rr5 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/object-tags", `{"key":"x","tags":[{"key":"","value":"v"}]}`); rr5.Code != http.StatusBadRequest {
		t.Fatalf("empty tag key = %d, want 400", rr5.Code)
	}
	if rr6 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/object-tags", `{"key":"x","tags":[{"key":"k","value":"1"},{"key":"k","value":"2"}]}`); rr6.Code != http.StatusBadRequest {
		t.Fatalf("duplicate tag key = %d, want 400", rr6.Code)
	}
	if rr7 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/object-tags", `{"tags":[{"key":"k","value":"1"}]}`); rr7.Code != http.StatusBadRequest {
		t.Fatalf("put without key = %d, want 400", rr7.Code)
	}
}

// TestListObjectVersions 用假 S3 验证：对象版本列表（版本 + 删除标记）。
func TestListObjectVersions(t *testing.T) {
	const versionsXML = `<?xml version="1.0" encoding="UTF-8"?><ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>b</Name><IsTruncated>false</IsTruncated><Version><Key>a.txt</Key><VersionId>v2</VersionId><IsLatest>true</IsLatest><LastModified>2026-09-01T01:00:00.000Z</LastModified><ETag>&quot;e2&quot;</ETag><Size>2</Size><StorageClass>STANDARD</StorageClass></Version><Version><Key>a.txt</Key><VersionId>v1</VersionId><IsLatest>false</IsLatest><LastModified>2026-09-01T00:00:00.000Z</LastModified><ETag>&quot;e1&quot;</ETag><Size>1</Size><StorageClass>STANDARD</StorageClass></Version><DeleteMarker><Key>deleted.txt</Key><VersionId>dv1</VersionId><IsLatest>true</IsLatest><LastModified>2026-09-01T00:00:00.000Z</LastModified></DeleteMarker></ListVersionsResult>`

	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("versions") {
			io.WriteString(w, versionsXML)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer s3fake.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test", false).Routes()

	rr := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/versions?bucket=b&prefix=a.txt", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("versions status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Versions []struct {
			Key          string `json:"key"`
			VersionID    string `json:"versionId"`
			IsLatest     bool   `json:"isLatest"`
			Size         int64  `json:"size"`
			ETag         string `json:"etag"`
			StorageClass string `json:"storageClass"`
		} `json:"versions"`
		DeleteMarkers []struct {
			Key       string `json:"key"`
			VersionID string `json:"versionId"`
			IsLatest  bool   `json:"isLatest"`
		} `json:"deleteMarkers"`
		IsTruncated bool `json:"isTruncated"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body: %v", err)
	}
	if len(resp.Versions) != 2 {
		t.Fatalf("versions len = %d, want 2", len(resp.Versions))
	}
	if resp.Versions[0].VersionID != "v2" || !resp.Versions[0].IsLatest || resp.Versions[0].Size != 2 {
		t.Fatalf("latest version = %+v", resp.Versions[0])
	}
	if resp.Versions[0].StorageClass != "STANDARD" {
		t.Fatalf("latest version storageClass = %q, want STANDARD", resp.Versions[0].StorageClass)
	}
	if resp.Versions[1].VersionID != "v1" || resp.Versions[1].IsLatest {
		t.Fatalf("old version = %+v", resp.Versions[1])
	}
	if len(resp.DeleteMarkers) != 1 || resp.DeleteMarkers[0].Key != "deleted.txt" || !resp.DeleteMarkers[0].IsLatest {
		t.Fatalf("delete markers = %+v", resp.DeleteMarkers)
	}
	if resp.IsTruncated {
		t.Fatalf("isTruncated = true, want false")
	}
}

// TestDeleteAndRestoreVersion 用假 S3 验证：删除指定版本、恢复某版本到当前、参数校验。
func TestDeleteAndRestoreVersion(t *testing.T) {
	var (
		mu             sync.Mutex
		deletedVersion string
		deletes        int
	)
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Query().Has("versionId"):
			mu.Lock()
			deletedVersion = r.URL.Query().Get("versionId")
			deletes++
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPut && r.Header.Get("X-Amz-Copy-Source") != "":
			w.Header().Set("X-Amz-Version-Id", "restored-1")
			io.WriteString(w, `<CopyObjectResult><LastModified>2026-09-01T00:00:00.000Z</LastModified><ETag>&quot;abc&quot;</ETag></CopyObjectResult>`)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer s3fake.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test", false).Routes()

	// 删除指定版本
	rr := doJSON(t, h, "DELETE", "/api/accounts/"+acc.ID+"/version?key=a.txt&versionId=v1", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("delete version status = %d, body=%s", rr.Code, rr.Body.String())
	}
	mu.Lock()
	if deletedVersion != "v1" || deletes != 1 {
		t.Fatalf("deletedVersion=%q deletes=%d, want v1/1", deletedVersion, deletes)
	}
	mu.Unlock()

	// 恢复某版本到当前
	rr2 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/version/restore", `{"key":"a.txt","versionId":"v1"}`)
	if rr2.Code != http.StatusOK {
		t.Fatalf("restore status = %d, body=%s", rr2.Code, rr2.Body.String())
	}
	var resp2 struct {
		Restored  string `json:"restored"`
		VersionID string `json:"versionId"`
	}
	if err := json.Unmarshal(rr2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("body: %v", err)
	}
	if resp2.Restored != "a.txt" || resp2.VersionID != "restored-1" {
		t.Fatalf("restore resp = %+v", resp2)
	}

	// 参数校验：缺 key / 缺 versionId / restore 缺 versionId
	if rr3 := doJSON(t, h, "DELETE", "/api/accounts/"+acc.ID+"/version?versionId=v1", ""); rr3.Code != http.StatusBadRequest {
		t.Fatalf("delete without key = %d, want 400", rr3.Code)
	}
	if rr4 := doJSON(t, h, "DELETE", "/api/accounts/"+acc.ID+"/version?key=a.txt", ""); rr4.Code != http.StatusBadRequest {
		t.Fatalf("delete without versionId = %d, want 400", rr4.Code)
	}
	if rr5 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/version/restore", `{"key":"a.txt"}`); rr5.Code != http.StatusBadRequest {
		t.Fatalf("restore without versionId = %d, want 400", rr5.Code)
	}
}

// TestLifecycle 用假 S3 验证生命周期规则读取与写入。
func TestLifecycle(t *testing.T) {
	var putBody string
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Query().Has("lifecycle"):
			io.WriteString(w, lifecycleXML(lifecycleRuleXML("r1", "logs/", 30)))
		case r.Method == http.MethodPut && r.URL.Query().Has("lifecycle"):
			b, _ := io.ReadAll(r.Body)
			putBody = string(b)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet: // GetBucketLifecycleConfiguration 404（无规则）
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `<?xml version="1.0"?><Error><Code>NoSuchLifecycleConfiguration</Code></Error>`)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer s3fake.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "fake", Endpoint: s3fake.URL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test", false).Routes()

	// 读取（带规则）
	rr := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/lifecycle?bucket=b", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("get lifecycle status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Rules []struct {
			ID     string `json:"id"`
			Prefix string `json:"prefix"`
			Days   int32  `json:"days"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body: %v", err)
	}
	if len(resp.Rules) != 1 || resp.Rules[0].ID != "r1" || resp.Rules[0].Prefix != "logs/" || resp.Rules[0].Days != 30 {
		t.Fatalf("rules = %+v", resp.Rules)
	}

	// 写入（覆盖）
	rr2 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/lifecycle",
		`{"bucket":"b","rules":[{"id":"r2","prefix":"tmp/","days":7}]}`)
	if rr2.Code != http.StatusOK {
		t.Fatalf("put lifecycle status = %d, body=%s", rr2.Code, rr2.Body.String())
	}
	if !strings.Contains(putBody, "<ID>r2</ID>") || !strings.Contains(putBody, "<Prefix>tmp/</Prefix>") || !strings.Contains(putBody, "<Days>7</Days>") {
		t.Fatalf("put body = %s", putBody)
	}

	// 非法规则（days < 1 / 重复 id）→ 400
	if rr3 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/lifecycle",
		`{"rules":[{"id":"x","days":0}]}`); rr3.Code != http.StatusBadRequest {
		t.Fatalf("bad days = %d, want 400", rr3.Code)
	}
	if rr4 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/lifecycle",
		`{"rules":[{"id":"x","days":1},{"id":"x","days":2}]}`); rr4.Code != http.StatusBadRequest {
		t.Fatalf("duplicate id = %d, want 400", rr4.Code)
	}
}

// TestSetHeadersInvalidUserMetadata400 验证 setHeaders 在 user metadata 不合规时返回 400。
// 避免把非法请求落到 S3 端以 500 形式回传。
func TestSetHeadersInvalidUserMetadata400(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	acc, err := st.Create(&model.Account{
		Name: "a", Endpoint: "http://127.0.0.1:1", Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test", false).Routes()
	_ = httptest.NewRecorder // 兼容 import 暂未使用

	// 启动假 S3（仅兜底：所有测试都不应触达，但万一校验漏掉会回 500）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	// 改 endpoint 让请求落在假 S3（不影响 400 校验路径，因为校验先于 SDK 调用）
	acc.Endpoint = srv.URL
	if _, err := st.Update(acc.ID, acc); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		body string
	}{
		{"empty key", `{"key":"a","metadata":{"":"v"}}`},
		{"non-ascii key", `{"key":"a","metadata":{"环境":"v"}}`},
		{"key too long", `{"key":"a","metadata":{"` + strings.Repeat("k", 200) + `":"v"}}`},
		{"value too long", `{"key":"a","metadata":{"k":"` + strings.Repeat("v", 300) + `"}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/set-headers", c.body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
			}
		})
	}
}
