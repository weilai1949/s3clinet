package handler

// ol_metadata_test.go —— metadata.go 错误分支/边界补测（仅新增，不改生产代码）。

import (
	"net/http"
	"strings"
	"testing"
)

// olVersionsXML 构造 ListObjectVersions 响应（一个版本 + 一个删除标记）。
func olVersionsXML(key string, withDM bool) string {
	s := `<?xml version="1.0"?><ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IsTruncated>false</IsTruncated>` +
		`<Version><Key>` + key + `</Key><VersionId>v1</VersionId><IsLatest>true</IsLatest>` +
		`<LastModified>2026-01-01T00:00:00.000Z</LastModified><Size>3</Size><ETag>"e"</ETag><StorageClass>STANDARD</StorageClass></Version>`
	if withDM {
		s += `<DeleteMarker><Key>` + key + `</Key><VersionId>d1</VersionId><IsLatest>false</IsLatest>` +
			`<LastModified>2026-01-02T00:00:00.000Z</LastModified></DeleteMarker>`
	}
	return s + `</ListVersionsResult>`
}

// TestOlMetadata404 各元数据接口对不存在账号返回 404。
func TestOlMetadata404(t *testing.T) {
	cases := []struct{ name, method, path string }{
		{"acl-get", "GET", "/api/accounts/nope/object-acl?bucket=b&key=k"},
		{"acl-put", "PUT", "/api/accounts/nope/object-acl"},
		{"tags-get", "GET", "/api/accounts/nope/object-tags?bucket=b&key=k"},
		{"tags-put", "PUT", "/api/accounts/nope/object-tags"},
		{"lifecycle-get", "GET", "/api/accounts/nope/lifecycle?bucket=b"},
		{"lifecycle-put", "PUT", "/api/accounts/nope/lifecycle"},
		{"versions", "GET", "/api/accounts/nope/versions?bucket=b"},
		{"version-del", "DELETE", "/api/accounts/nope/version?bucket=b&key=k&versionId=v"},
		{"restore", "POST", "/api/accounts/nope/version/restore"},
		{"dm-restore", "POST", "/api/accounts/nope/delete-marker/restore"},
	}
	for _, c := range cases {
		env := accNewEnv(t, "http://127.0.0.1:1", "b")
		rr := env.accDoRec(c.method, c.path, `{"key":"k","versionId":"v"}`)
		olExpectStatus(t, rr, http.StatusNotFound, c.name)
	}
}

// TestOlMetadataBadJSON 请求体非法 → 400。
func TestOlMetadataBadJSON(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env := accNewEnv(t, srv.URL, "b")
	id := env.acc.ID
	cases := []struct{ name, method, path string }{
		{"acl-put", "PUT", "/api/accounts/" + id + "/object-acl"},
		{"tags-put", "PUT", "/api/accounts/" + id + "/object-tags"},
		{"lifecycle-put", "PUT", "/api/accounts/" + id + "/lifecycle"},
		{"restore", "POST", "/api/accounts/" + id + "/version/restore"},
		{"dm-restore", "POST", "/api/accounts/" + id + "/delete-marker/restore"},
	}
	for _, c := range cases {
		rr := env.accDoRec(c.method, c.path, "{bad")
		olExpectStatus(t, rr, http.StatusBadRequest, c.name)
	}
}

// TestOlMetadataNoBucket 账号无默认桶且请求未带桶 → 400。
func TestOlMetadataNoBucket(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env := accNewEnv(t, srv.URL, "")
	id := env.acc.ID
	cases := []struct{ name, method, path, body string }{
		{"acl-get", "GET", "/api/accounts/" + id + "/object-acl?key=k", ""},
		{"acl-put", "PUT", "/api/accounts/" + id + "/object-acl", `{"key":"k","acl":"private"}`},
		{"tags-get", "GET", "/api/accounts/" + id + "/object-tags?key=k", ""},
		{"tags-put", "PUT", "/api/accounts/" + id + "/object-tags", `{"key":"k"}`},
		{"lifecycle-get", "GET", "/api/accounts/" + id + "/lifecycle", ""},
		{"lifecycle-put", "PUT", "/api/accounts/" + id + "/lifecycle", `{"rules":[]}`},
		{"versions", "GET", "/api/accounts/" + id + "/versions", ""},
		{"version-del", "DELETE", "/api/accounts/" + id + "/version?key=k&versionId=v", ""},
		{"restore", "POST", "/api/accounts/" + id + "/version/restore", `{"key":"k","versionId":"v"}`},
		{"dm-restore", "POST", "/api/accounts/" + id + "/delete-marker/restore", `{"key":"k","versionId":"v"}`},
	}
	for _, c := range cases {
		rr := env.accDoRec(c.method, c.path, c.body)
		olExpectStatus(t, rr, http.StatusBadRequest, c.name)
	}
}

// TestOlMetadataS3Errors 假 S3 注入错误：ACL/标签/生命周期/版本接口的错误分支。
func TestOlMetadataS3Errors(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		q := r.URL.Query()
		switch {
		case q.Has("acl"):
			return olErr(http.StatusForbidden, "AccessDenied")
		case q.Has("tagging"):
			return olErr(http.StatusForbidden, "AccessDenied")
		case q.Has("lifecycle"):
			return olErr(http.StatusForbidden, "AccessDenied")
		case q.Has("versions"):
			return olErr(http.StatusForbidden, "AccessDenied")
		case r.Method == http.MethodDelete:
			return olErr(http.StatusForbidden, "AccessDenied")
		case r.Method == http.MethodPut && r.Header.Get("x-amz-copy-source") != "":
			return olErr(http.StatusForbidden, "AccessDenied") // CopyObject（版本恢复）
		}
		return olResp{}
	})
	env := accNewEnv(t, srv.URL, "b")
	id := env.acc.ID

	// ACL 读失败 → 403
	rr := env.accDoRec("GET", "/api/accounts/"+id+"/object-acl?bucket=b&key=k", "")
	olExpectStatus(t, rr, http.StatusForbidden, "acl get err")
	// ACL 读缺 key → 400
	rr = env.accDoRec("GET", "/api/accounts/"+id+"/object-acl?bucket=b", "")
	olExpectStatus(t, rr, http.StatusBadRequest, "acl no key")
	// ACL 写失败 → 403
	rr = env.accDoRec("PUT", "/api/accounts/"+id+"/object-acl", `{"key":"k","acl":"private"}`)
	olExpectStatus(t, rr, http.StatusForbidden, "acl put err")
	// ACL 非法值 → 400
	rr = env.accDoRec("PUT", "/api/accounts/"+id+"/object-acl", `{"key":"k","acl":"weird"}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "acl invalid")

	// 标签读失败（非 NoSuchTagSet）→ 403
	rr = env.accDoRec("GET", "/api/accounts/"+id+"/object-tags?bucket=b&key=k", "")
	olExpectStatus(t, rr, http.StatusForbidden, "tags get err")
	// 标签读缺 key → 400
	rr = env.accDoRec("GET", "/api/accounts/"+id+"/object-tags?bucket=b", "")
	olExpectStatus(t, rr, http.StatusBadRequest, "tags no key")
	// 标签写失败 → 403
	rr = env.accDoRec("PUT", "/api/accounts/"+id+"/object-tags", `{"key":"k","tags":[{"key":"a","value":"b"}]}`)
	olExpectStatus(t, rr, http.StatusForbidden, "tags put err")
	// 标签 key 超长（129 字符）→ 400
	longKey := strings.Repeat("k", 129)
	rr = env.accDoRec("PUT", "/api/accounts/"+id+"/object-tags", `{"key":"k","tags":[{"key":"`+longKey+`","value":"b"}]}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "tags key too long")
	// 超过 10 个标签 → 400
	var sb strings.Builder
	sb.WriteString(`{"key":"k","tags":[`)
	for i := 0; i < 11; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"key":"t` + string(rune('a'+i)) + `","value":"v"}`)
	}
	sb.WriteString("]}")
	rr = env.accDoRec("PUT", "/api/accounts/"+id+"/object-tags", sb.String())
	olExpectStatus(t, rr, http.StatusBadRequest, "tags too many")
	// 空标签 = 清空：删除标签失败 → 403
	rr = env.accDoRec("PUT", "/api/accounts/"+id+"/object-tags", `{"key":"k","tags":[]}`)
	olExpectStatus(t, rr, http.StatusForbidden, "tags delete err")

	// 生命周期读失败 → 403
	rr = env.accDoRec("GET", "/api/accounts/"+id+"/lifecycle?bucket=b", "")
	olExpectStatus(t, rr, http.StatusForbidden, "lifecycle get err")
	// 生命周期清空（删除失败）→ 403
	rr = env.accDoRec("PUT", "/api/accounts/"+id+"/lifecycle", `{"rules":[]}`)
	olExpectStatus(t, rr, http.StatusForbidden, "lifecycle delete err")
	// 生命周期写入失败 → 403
	rr = env.accDoRec("PUT", "/api/accounts/"+id+"/lifecycle", `{"rules":[{"id":"r1","prefix":"p/","days":1}]}`)
	olExpectStatus(t, rr, http.StatusForbidden, "lifecycle put err")

	// 版本列表（maxKeys=5 生效）→ 200
	rr = env.accDoRec("GET", "/api/accounts/"+id+"/versions?bucket=b&maxKeys=5", "")
	olExpectStatus(t, rr, http.StatusForbidden, "versions err")
	// 版本列表 maxKeys 非法值 → 走默认分支（同样进入错误）
	rr = env.accDoRec("GET", "/api/accounts/"+id+"/versions?bucket=b&maxKeys=abc", "")
	olExpectStatus(t, rr, http.StatusForbidden, "versions bad maxKeys")

	// 删除指定版本失败 → 403
	rr = env.accDoRec("DELETE", "/api/accounts/"+id+"/version?bucket=b&key=k&versionId=v1", "")
	olExpectStatus(t, rr, http.StatusForbidden, "version delete err")
	// 删除版本缺 key → 400
	rr = env.accDoRec("DELETE", "/api/accounts/"+id+"/version?bucket=b&versionId=v1", "")
	olExpectStatus(t, rr, http.StatusBadRequest, "version no key")

	// 版本恢复：缺 key → 400；复制失败 → 403
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/version/restore", `{"versionId":"v1"}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "restore no key")
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/version/restore", `{"key":"k","versionId":"v1"}`)
	olExpectStatus(t, rr, http.StatusForbidden, "restore err")

	// 删除标记还原：复制失败 → 403
	rr = env.accDoRec("POST", "/api/accounts/"+id+"/delete-marker/restore", `{"key":"k","versionId":"d1"}`)
	olExpectStatus(t, rr, http.StatusForbidden, "dm restore err")
}

// TestOlLifecycle404 生命周期配置不存在 → 空规则列表（200）。
func TestOlLifecycle404(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		if r.URL.Query().Has("lifecycle") {
			return olErr(http.StatusNotFound, "NoSuchLifecycleConfiguration")
		}
		return olResp{}
	})
	env := accNewEnv(t, srv.URL, "b")
	rr := env.accDoRec("GET", "/api/accounts/"+env.acc.ID+"/lifecycle?bucket=b", "")
	olExpectStatus(t, rr, http.StatusOK, "lifecycle 404")
}

// TestOlLifecycleClear 清空生命周期规则（DELETE 成功）→ updated 0。
func TestOlLifecycleClear(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		if r.Method == http.MethodDelete && r.URL.Query().Has("lifecycle") {
			return olPlain(http.StatusNoContent)
		}
		return olResp{}
	})
	env := accNewEnv(t, srv.URL, "b")
	rr := env.accDoRec("PUT", "/api/accounts/"+env.acc.ID+"/lifecycle", `{"rules":[]}`)
	olExpectStatus(t, rr, http.StatusOK, "lifecycle clear")
	if !strings.Contains(rr.Body.String(), `"updated":0`) {
		t.Fatalf("lifecycle clear body: %s", rr.Body.String())
	}
}

// TestOlVersionsMaxKeys 版本列表正常返回（覆盖 maxKeys 解析分支）。
func TestOlVersionsMaxKeys(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp {
		if r.URL.Query().Has("versions") {
			return olXML(http.StatusOK, olVersionsXML("k", true))
		}
		return olResp{}
	})
	env := accNewEnv(t, srv.URL, "b")
	rr := env.accDoRec("GET", "/api/accounts/"+env.acc.ID+"/versions?bucket=b&maxKeys=5", "")
	olExpectStatus(t, rr, http.StatusOK, "versions ok")
	if !strings.Contains(rr.Body.String(), `"versionId":"v1"`) || !strings.Contains(rr.Body.String(), `"versionId":"d1"`) {
		t.Fatalf("versions body: %s", rr.Body.String())
	}
}
