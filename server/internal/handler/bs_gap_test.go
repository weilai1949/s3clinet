package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestBucketSettingsGapMatrix 用桶名驱动的矩阵 fake 覆盖桶配置端点的全部分支：
// 成功 / NoSuch* / 通用 S3 错误 / 参数校验失败 / 空规则走 DELETE 语义。
func TestBucketSettingsGapMatrix(t *testing.T) {
	var mu sync.Mutex
	deletes := []string{}
	notConfigured := func(w http.ResponseWriter, code string) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>` + code + `</Code><Message>nf</Message></Error>`))
	}
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		seg := strings.Split(strings.Trim(r.URL.Path, "/"), "/")[0]
		q := r.URL.Query()
		genericErr := strings.Contains(seg, "errb")
		if genericErr {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>InternalError</Code><Message>x</Message></Error>`))
			return
		}
		codeFor := func(cfg string) string {
			switch cfg {
			case "encryption":
				return "ServerSideEncryptionConfigurationNotFoundError"
			case "cors":
				return "NoSuchCORSConfiguration"
			case "website":
				return "NoSuchWebsiteConfiguration"
			case "policy":
				return "NoSuchBucketPolicy"
			case "tags":
				return "NoSuchTagSet"
			}
			return "NoSuchConfiguration"
		}
		switch {
		case q.Has("encryption"):
			if r.Method == http.MethodGet && strings.Contains(seg, "noconf") {
				notConfigured(w, codeFor("encryption"))
				return
			}
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(encryptionXML))
				return
			}
			w.WriteHeader(http.StatusOK)
		case q.Has("cors"):
			if r.Method == http.MethodGet && strings.Contains(seg, "noconf") {
				notConfigured(w, codeFor("cors"))
				return
			}
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(corsXML))
				return
			}
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusOK)
		case q.Has("website"):
			if r.Method == http.MethodGet && strings.Contains(seg, "noconf") {
				notConfigured(w, codeFor("website"))
				return
			}
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(websiteXML))
				return
			}
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusOK)
		case q.Has("policy"):
			if r.Method == http.MethodGet && strings.Contains(seg, "noconf") {
				notConfigured(w, codeFor("policy"))
				return
			}
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"Version":"2012-10-17"}`))
				return
			}
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusOK)
		case q.Has("tagging"):
			if r.Method == http.MethodGet && strings.Contains(seg, "noconf") {
				notConfigured(w, codeFor("tags"))
				return
			}
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(taggingXML))
				return
			}
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			_ = deletes
			w.WriteHeader(http.StatusOK)
		}
	}))
	env := accNewEnv(t, s3fake.URL, "")
	do := func(method, path, body string) *httptest.ResponseRecorder {
		return doJSON(t, env.h, method, path, body)
	}
	base := "/api/accounts/" + env.acc.ID + "/bucket"

	// ---- 加密 ----
	if rr := do("GET", base+"/encryption?bucket=b", ""); rr.Code != http.StatusOK {
		t.Fatalf("get enc ok = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("GET", base+"/encryption?bucket=noconf", ""); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"configured":false`) {
		t.Fatalf("get enc noconfig = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("GET", base+"/encryption?bucket=errb", ""); rr.Code != http.StatusInternalServerError {
		t.Fatalf("get enc err = %d", rr.Code)
	}
	if rr := do("PUT", base+"/encryption", `{"bucket":"b","algorithm":"BAD"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("put enc bad algo = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("PUT", base+"/encryption", `{"bucket":"b","algorithm":"aws:kms","kmsKeyId":"kid","bucketKeyEnabled":true}`); rr.Code != http.StatusOK {
		t.Fatalf("put enc kms = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("PUT", base+"/encryption", `{"bucket":"b","algorithm":"AES256"}`); rr.Code != http.StatusOK {
		t.Fatalf("put enc ok = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("PUT", base+"/encryption", `{"bucket":"b","algorithm":"AES256"`); rr.Code != http.StatusBadRequest {
		t.Fatalf("put enc bad json = %d", rr.Code)
	}
	if rr := do("DELETE", base+"/encryption?bucket=b", ""); rr.Code != http.StatusOK {
		t.Fatalf("delete enc ok = %d", rr.Code)
	}
	if rr := do("DELETE", base+"/encryption?bucket=errb", ""); rr.Code != http.StatusInternalServerError {
		t.Fatalf("delete enc err = %d", rr.Code)
	}

	// ---- CORS ----
	if rr := do("GET", base+"/cors?bucket=b", ""); rr.Code != http.StatusOK {
		t.Fatalf("get cors ok = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("GET", base+"/cors?bucket=noconf", ""); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"rules":[]`) {
		t.Fatalf("get cors noconfig = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("GET", base+"/cors?bucket=errb", ""); rr.Code != http.StatusInternalServerError {
		t.Fatalf("get cors err = %d", rr.Code)
	}
	if rr := do("PUT", base+"/cors", `{"bucket":"b"}`); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"deleted"`) {
		t.Fatalf("put cors empty rules -> delete = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("PUT", base+"/cors", `{"bucket":"b","rules":[{"id":"r1","allowedMethods":["GET"],"allowedOrigins":["*"],"allowedHeaders":["*"],"maxAgeSeconds":3600}]}`); rr.Code != http.StatusOK {
		t.Fatalf("put cors ok = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("PUT", base+"/cors", `{"bucket":"b","rules":[{"id":"x"}]}`+`{"x":1}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("put cors trailing junk = %d", rr.Code)
	}
	if rr := do("PUT", base+"/cors", `{"bucket":"errb","rules":[{"id":"r1","allowedMethods":["GET"],"allowedOrigins":["*"]}]}`); rr.Code != http.StatusInternalServerError {
		t.Fatalf("put cors err = %d", rr.Code)
	}
	if rr := do("DELETE", base+"/cors?bucket=b", ""); rr.Code != http.StatusOK {
		t.Fatalf("delete cors ok = %d", rr.Code)
	}
	if rr := do("DELETE", base+"/cors?bucket=errb", ""); rr.Code != http.StatusInternalServerError {
		t.Fatalf("delete cors err = %d", rr.Code)
	}

	// ---- 网站 ----
	if rr := do("GET", base+"/website?bucket=b", ""); rr.Code != http.StatusOK {
		t.Fatalf("get web ok = %d", rr.Code)
	}
	if rr := do("GET", base+"/website?bucket=noconf", ""); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"configured":false`) {
		t.Fatalf("get web noconfig = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("GET", base+"/website?bucket=errb", ""); rr.Code != http.StatusInternalServerError {
		t.Fatalf("get web err = %d", rr.Code)
	}
	if rr := do("PUT", base+"/website", `{"bucket":"b"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("put web missing index = %d", rr.Code)
	}
	if rr := do("PUT", base+"/website", `{"bucket":"b","indexDocument":"index.html","errorDocument":"e.html"}`); rr.Code != http.StatusOK {
		t.Fatalf("put web ok = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("PUT", base+"/website", `{"bucket":"b","redirectAllRequestsTo":"https://x"}`); rr.Code != http.StatusOK {
		t.Fatalf("put web redirect = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("DELETE", base+"/website?bucket=b", ""); rr.Code != http.StatusOK {
		t.Fatalf("delete web ok = %d", rr.Code)
	}
	if rr := do("DELETE", base+"/website?bucket=errb", ""); rr.Code != http.StatusInternalServerError {
		t.Fatalf("delete web err = %d", rr.Code)
	}

	// ---- 策略 ----
	if rr := do("GET", base+"/policy?bucket=b", ""); rr.Code != http.StatusOK {
		t.Fatalf("get pol ok = %d", rr.Code)
	}
	if rr := do("GET", base+"/policy?bucket=noconf", ""); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"configured":false`) {
		t.Fatalf("get pol noconfig = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("GET", base+"/policy?bucket=errb", ""); rr.Code != http.StatusInternalServerError {
		t.Fatalf("get pol err = %d", rr.Code)
	}
	if rr := do("PUT", base+"/policy", `{"bucket":"b"}`); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"deleted"`) {
		t.Fatalf("put pol empty -> delete = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("PUT", base+"/policy", `{"bucket":"b","policy":"not-json"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("put pol invalid json = %d", rr.Code)
	}
	if rr := do("PUT", base+"/policy", `{"bucket":"b","policy":"{\"Version\":\"2012-10-17\"}"}`); rr.Code != http.StatusOK {
		t.Fatalf("put pol ok = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("DELETE", base+"/policy?bucket=b", ""); rr.Code != http.StatusOK {
		t.Fatalf("delete pol ok = %d", rr.Code)
	}
	if rr := do("DELETE", base+"/policy?bucket=errb", ""); rr.Code != http.StatusInternalServerError {
		t.Fatalf("delete pol err = %d", rr.Code)
	}

	// ---- 标签 ----
	if rr := do("GET", base+"/tags?bucket=b", ""); rr.Code != http.StatusOK {
		t.Fatalf("get tags ok = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("GET", base+"/tags?bucket=noconf", ""); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"tags":[]`) {
		t.Fatalf("get tags noconfig = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("GET", base+"/tags?bucket=errb", ""); rr.Code != http.StatusInternalServerError {
		t.Fatalf("get tags err = %d", rr.Code)
	}
	if rr := do("PUT", base+"/tags", `{"bucket":"b","tags":[{"key":"","value":"v"}]}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("put tags empty key = %d", rr.Code)
	}
	if rr := do("PUT", base+"/tags", `{"bucket":"b"}`); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"deleted"`) {
		t.Fatalf("put tags empty -> delete = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("PUT", base+"/tags", `{"bucket":"b","tags":[{"key":"env","value":"prod"}]}`); rr.Code != http.StatusOK {
		t.Fatalf("put tags ok = %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("DELETE", base+"/tags?bucket=b", ""); rr.Code != http.StatusOK {
		t.Fatalf("delete tags ok = %d", rr.Code)
	}
	if rr := do("DELETE", base+"/tags?bucket=errb", ""); rr.Code != http.StatusInternalServerError {
		t.Fatalf("delete tags err = %d", rr.Code)
	}

	// ---- 公共前置分支：无桶 / 坏账号 ----
	if rr := do("GET", base+"/encryption", ""); rr.Code != http.StatusBadRequest {
		t.Fatalf("missing bucket = %d", rr.Code)
	}
	if rr := do("GET", "/api/accounts/nope/bucket/encryption?bucket=b", ""); rr.Code != http.StatusNotFound {
		t.Fatalf("bad account = %d", rr.Code)
	}
}
