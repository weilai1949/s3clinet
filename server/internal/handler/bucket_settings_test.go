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

const encryptionXML = `<?xml version="1.0" encoding="UTF-8"?><ServerSideEncryptionConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Rule><ApplyServerSideEncryptionByDefault><SSEAlgorithm>AES256</SSEAlgorithm></ApplyServerSideEncryptionByDefault><BucketKeyEnabled>true</BucketKeyEnabled></Rule></ServerSideEncryptionConfiguration>`
const corsXML = `<?xml version="1.0" encoding="UTF-8"?><CORSConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><CORSRule><ID>r1</ID><AllowedOrigin>*</AllowedOrigin><AllowedMethod>GET</AllowedMethod><AllowedHeader>*</AllowedHeader><MaxAgeSeconds>3600</MaxAgeSeconds></CORSRule></CORSConfiguration>`
const websiteXML = `<?xml version="1.0" encoding="UTF-8"?><WebsiteConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IndexDocument><Suffix>index.html</Suffix></IndexDocument><ErrorDocument><Key>error.html</Key></ErrorDocument></WebsiteConfiguration>`
const taggingXML = `<?xml version="1.0" encoding="UTF-8"?><Tagging xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><TagSet><Tag><Key>env</Key><Value>prod</Value></Tag><Tag><Key>team</Key><Value>data</Value></Tag></TagSet></Tagging>`

// TestBucketSettings 用假 S3 验证桶级配置（加密/CORS/网站托管/策略/标签）的读取、写入与清空。
func TestBucketSettings(t *testing.T) {
	var (
		mu       sync.Mutex
		encBody  string
		corsBody string
		webBody  string
		polBody  string
		tagBody  string
		deletes  []string
	)
	// 未配置标识：请求路径里带 "noconfig" 前缀返回 NoSuch* 错误。
	notConfigured := func(w http.ResponseWriter, code string) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `<?xml version="1.0"?><Error><Code>`+code+`</Code><Message>not found</Message></Error>`)
	}

	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		// path-style：/b?config，首段即桶名
		seg := strings.Split(strings.Trim(r.URL.Path, "/"), "/")[0]
		none := strings.Contains(seg, "noconfig")
		switch {
		case q.Has("encryption"):
			if r.Method == http.MethodGet {
				if none {
					notConfigured(w, "ServerSideEncryptionConfigurationNotFoundError")
				} else {
					io.WriteString(w, encryptionXML)
				}
				return
			}
			if r.Method == http.MethodPut {
				mu.Lock()
				b, _ := io.ReadAll(r.Body)
				encBody = string(b)
				mu.Unlock()
				w.WriteHeader(http.StatusOK)
				return
			}
			if r.Method == http.MethodDelete {
				mu.Lock()
				deletes = append(deletes, "encryption")
				mu.Unlock()
				w.WriteHeader(http.StatusNoContent)
				return
			}
		case q.Has("cors"):
			if r.Method == http.MethodGet {
				if none {
					notConfigured(w, "NoSuchCORSConfiguration")
				} else {
					io.WriteString(w, corsXML)
				}
				return
			}
			if r.Method == http.MethodPut {
				mu.Lock()
				b, _ := io.ReadAll(r.Body)
				corsBody = string(b)
				mu.Unlock()
				w.WriteHeader(http.StatusOK)
				return
			}
			if r.Method == http.MethodDelete {
				mu.Lock()
				deletes = append(deletes, "cors")
				mu.Unlock()
				w.WriteHeader(http.StatusNoContent)
				return
			}
		case q.Has("website"):
			if r.Method == http.MethodGet {
				if none {
					notConfigured(w, "NoSuchWebsiteConfiguration")
				} else {
					io.WriteString(w, websiteXML)
				}
				return
			}
			if r.Method == http.MethodPut {
				mu.Lock()
				b, _ := io.ReadAll(r.Body)
				webBody = string(b)
				mu.Unlock()
				w.WriteHeader(http.StatusOK)
				return
			}
			if r.Method == http.MethodDelete {
				mu.Lock()
				deletes = append(deletes, "website")
				mu.Unlock()
				w.WriteHeader(http.StatusNoContent)
				return
			}
		case q.Has("policy"):
			if r.Method == http.MethodGet {
				if none {
					notConfigured(w, "NoSuchBucketPolicy")
				} else {
					// GetBucketPolicy 返回原始策略 JSON（非 XML）
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, `{"Version":"2012-10-17"}`)
				}
				return
			}
			if r.Method == http.MethodPut {
				mu.Lock()
				b, _ := io.ReadAll(r.Body)
				polBody = string(b)
				mu.Unlock()
				w.WriteHeader(http.StatusOK)
				return
			}
			if r.Method == http.MethodDelete {
				mu.Lock()
				deletes = append(deletes, "policy")
				mu.Unlock()
				w.WriteHeader(http.StatusNoContent)
				return
			}
		case q.Has("tagging"):
			if r.Method == http.MethodGet {
				if none {
					notConfigured(w, "NoSuchTagSet")
				} else {
					io.WriteString(w, taggingXML)
				}
				return
			}
			if r.Method == http.MethodPut {
				mu.Lock()
				b, _ := io.ReadAll(r.Body)
				tagBody = string(b)
				mu.Unlock()
				w.WriteHeader(http.StatusOK)
				return
			}
			if r.Method == http.MethodDelete {
				mu.Lock()
				deletes = append(deletes, "tagging")
				mu.Unlock()
				w.WriteHeader(http.StatusNoContent)
				return
			}
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
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	// ---- 加密：读已配置 / 写 / 非法算法 400 / 未配置读 ---
	rr := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/bucket/encryption?bucket=b", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("get encryption status=%d body=%s", rr.Code, rr.Body.String())
	}
	var enc struct {
		Configured bool   `json:"configured"`
		Algorithm  string `json:"algorithm"`
		KeyEnabled bool   `json:"bucketKeyEnabled"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &enc); err != nil {
		t.Fatalf("enc body: %v", err)
	}
	if !enc.Configured || enc.Algorithm != "AES256" || !enc.KeyEnabled {
		t.Fatalf("enc=%+v", enc)
	}
	if rr2 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/bucket/encryption", `{"bucket":"b","algorithm":"AES256"}`); rr2.Code != http.StatusOK {
		t.Fatalf("put encryption=%d", rr2.Code)
	}
	mu.Lock()
	gotEnc := encBody
	mu.Unlock()
	if !strings.Contains(gotEnc, "<SSEAlgorithm>AES256</SSEAlgorithm>") {
		t.Fatalf("enc put body=%s", gotEnc)
	}
	if rr3 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/bucket/encryption", `{"bucket":"b","algorithm":"bogus"}`); rr3.Code != http.StatusBadRequest {
		t.Fatalf("bad encryption algorithm=%d, want 400", rr3.Code)
	}
	// 未配置读 → configured=false
	var nenc struct {
		Configured bool `json:"configured"`
	}
	_ = json.Unmarshal(doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/bucket/encryption?bucket=noconfigb", "").Body.Bytes(), &nenc)
	if nenc.Configured {
		t.Fatalf("noconfig encryption configured=true, want false")
	}

	// ---- CORS：读已配置 / 写 / 清空(空 rules 触发 DELETE) ---
	rr4 := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/bucket/cors?bucket=b", "")
	var cors struct {
		Rules []struct {
			ID             string   `json:"id"`
			AllowedMethods []string `json:"allowedMethods"`
			AllowedOrigins []string `json:"allowedOrigins"`
			MaxAgeSeconds  int32    `json:"maxAgeSeconds"`
		} `json:"rules"`
	}
	_ = json.Unmarshal(rr4.Body.Bytes(), &cors)
	if len(cors.Rules) != 1 || cors.Rules[0].ID != "r1" || cors.Rules[0].MaxAgeSeconds != 3600 {
		t.Fatalf("cors=%+v", cors.Rules)
	}
	if rr5 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/bucket/cors", `{"bucket":"b","rules":[{"id":"r2","allowedMethods":["GET"],"allowedOrigins":["*"]}]}`); rr5.Code != http.StatusOK {
		t.Fatalf("put cors=%d", rr5.Code)
	}
	mu.Lock()
	gotCors := corsBody
	mu.Unlock()
	if !strings.Contains(gotCors, "<ID>r2</ID>") || !strings.Contains(gotCors, "<AllowedMethod>GET</AllowedMethod>") {
		t.Fatalf("cors put body=%s", gotCors)
	}
	if rr6 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/bucket/cors", `{"bucket":"b","rules":[]}`); rr6.Code != http.StatusOK {
		t.Fatalf("clear cors=%d", rr6.Code)
	}
	mu.Lock()
	hadCorsDel := containsStr(deletes, "cors")
	mu.Unlock()
	if !hadCorsDel {
		t.Fatalf("empty cors rules did not trigger delete")
	}

	// ---- 网站托管：读 / 写 / 非法(缺索引与重定向) 400 ---
	var web struct {
		Configured    bool   `json:"configured"`
		IndexDocument string `json:"indexDocument"`
		ErrorDocument string `json:"errorDocument"`
	}
	_ = json.Unmarshal(doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/bucket/website?bucket=b", "").Body.Bytes(), &web)
	if !web.Configured || web.IndexDocument != "index.html" || web.ErrorDocument != "error.html" {
		t.Fatalf("web=%+v", web)
	}
	if rw := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/bucket/website", `{"bucket":"b","indexDocument":"index.html"}`); rw.Code != http.StatusOK {
		t.Fatalf("put website=%d", rw.Code)
	}
	mu.Lock()
	gotWeb := webBody
	mu.Unlock()
	if !strings.Contains(gotWeb, "<IndexDocument><Suffix>index.html</Suffix></IndexDocument>") {
		t.Fatalf("web put body=%s", gotWeb)
	}
	if rw2 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/bucket/website", `{"bucket":"b"}`); rw2.Code != http.StatusBadRequest {
		t.Fatalf("website missing index=%d, want 400", rw2.Code)
	}

	// ---- 桶策略：读 / 写 / 非法 JSON 400 ---
	var pol struct {
		Configured bool   `json:"configured"`
		Policy     string `json:"policy"`
	}
	_ = json.Unmarshal(doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/bucket/policy?bucket=b", "").Body.Bytes(), &pol)
	if !pol.Configured || pol.Policy == "" {
		t.Fatalf("policy=%+v", pol)
	}
	if rp := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/bucket/policy", `{"bucket":"b","policy":"{\"Version\":\"2012-10-17\"}"}`); rp.Code != http.StatusOK {
		t.Fatalf("put policy=%d body=%s", rp.Code, rp.Body.String())
	}
	mu.Lock()
	gotPol := polBody
	mu.Unlock()
	if !strings.Contains(gotPol, "2012-10-17") {
		t.Fatalf("policy put body=%s", gotPol)
	}
	if rp2 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/bucket/policy", `{"bucket":"b","policy":"not-json"}`); rp2.Code != http.StatusBadRequest {
		t.Fatalf("bad policy json=%d, want 400", rp2.Code)
	}

	// ---- 桶级 DELETE：encryption / cors / website / policy / tags 均可删除 ---
	for _, path := range []string{
		"bucket/encryption", "bucket/cors", "bucket/website", "bucket/policy", "bucket/tags",
	} {
		if rd := doJSON(t, h, "DELETE", "/api/accounts/"+acc.ID+"/"+path+"?bucket=b", ""); rd.Code != http.StatusOK {
			t.Fatalf("delete %s = %d body=%s", path, rd.Code, rd.Body.String())
		}
	}

	// ---- 桶标签：读 / 写 / 空 tags 触发 DELETE ---
	var tags struct {
		Tags []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"tags"`
	}
	_ = json.Unmarshal(doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/bucket/tags?bucket=b", "").Body.Bytes(), &tags)
	if len(tags.Tags) != 2 {
		t.Fatalf("bucket tags len=%d, want 2", len(tags.Tags))
	}
	if rt := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/bucket/tags", `{"bucket":"b","tags":[{"key":"k","value":"v"}]}`); rt.Code != http.StatusOK {
		t.Fatalf("put tags=%d", rt.Code)
	}
	mu.Lock()
	gotTag := tagBody
	mu.Unlock()
	if !strings.Contains(gotTag, "<Key>k</Key>") {
		t.Fatalf("tag put body=%s", gotTag)
	}
	if rt2 := doJSON(t, h, "PUT", "/api/accounts/"+acc.ID+"/bucket/tags", `{"bucket":"b","tags":[]}`); rt2.Code != http.StatusOK {
		t.Fatalf("clear tags=%d", rt2.Code)
	}
	mu.Lock()
	hadTagDel := containsStr(deletes, "tagging")
	mu.Unlock()
	if !hadTagDel {
		t.Fatalf("empty bucket tags did not trigger delete")
	}
}
