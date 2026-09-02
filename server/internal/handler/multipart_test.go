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

// TestMultipartUpload 用假 S3 验证分段上传：init / 分段预签名 / complete / abort。
func TestMultipartUpload(t *testing.T) {
	var (
		mu            sync.Mutex
		initCalls     int
		completeCalls int
		abortCalls    int
		completedBody string
	)
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodPost && q.Has("uploads"): // CreateMultipartUpload
			mu.Lock()
			initCalls++
			mu.Unlock()
			io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><InitiateMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Bucket>b</Bucket><Key>big.bin</Key><UploadId>UPLOAD123</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == http.MethodPost && q.Get("uploadId") != "": // CompleteMultipartUpload
			mu.Lock()
			completeCalls++
			mu.Unlock()
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			completedBody = string(b)
			mu.Unlock()
			io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><CompleteMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Location>http://x/b/big.bin</Location><Bucket>b</Bucket><Key>big.bin</Key><ETag>"etag"</ETag></CompleteMultipartUploadResult>`)
		case r.Method == http.MethodDelete && q.Get("uploadId") != "": // AbortMultipartUpload
			mu.Lock()
			abortCalls++
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
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	// init：返回 UploadID
	rr := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/multipart/init", `{"key":"big.bin","contentType":"application/octet-stream"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("init status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var initResp struct {
		UploadID string `json:"uploadId"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &initResp); err != nil || initResp.UploadID != "UPLOAD123" {
		t.Fatalf("init uploadId = %q, err=%v, want UPLOAD123", initResp.UploadID, err)
	}

	// 分段预签名：URL 应含 uploadId 与 partNumber
	rr2 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/multipart/part",
		`{"key":"big.bin","uploadId":"UPLOAD123","partNumber":1}`)
	if rr2.Code != http.StatusOK {
		t.Fatalf("part status = %d, body=%s", rr2.Code, rr2.Body.String())
	}
	var partResp struct {
		PartNumber int32  `json:"partNumber"`
		URL        string `json:"url"`
	}
	if err := json.Unmarshal(rr2.Body.Bytes(), &partResp); err != nil {
		t.Fatalf("part body: %v", err)
	}
	if partResp.PartNumber != 1 || !strings.Contains(partResp.URL, "UPLOAD123") || !strings.Contains(partResp.URL, "partNumber=1") {
		t.Fatalf("part = %+v, want partNumber=1 and url containing uploadId/partNumber", partResp)
	}

	// complete：汇总分段
	rr3 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/multipart/complete",
		`{"key":"big.bin","uploadId":"UPLOAD123","parts":[{"partNumber":1,"etag":"\"e1\""}]}`)
	if rr3.Code != http.StatusOK {
		t.Fatalf("complete status = %d, body=%s", rr3.Code, rr3.Body.String())
	}
	mu.Lock()
	if completeCalls != 1 {
		t.Fatalf("completeCalls = %d, want 1", completeCalls)
	}
	if !strings.Contains(completedBody, "<PartNumber>1</PartNumber>") || !strings.Contains(completedBody, "e1") {
		t.Fatalf("completed body = %s", completedBody)
	}
	mu.Unlock()

	// abort：中止分段
	rr4 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/multipart/abort", `{"key":"big.bin","uploadId":"UPLOAD123"}`)
	if rr4.Code != http.StatusOK {
		t.Fatalf("abort status = %d, body=%s", rr4.Code, rr4.Body.String())
	}
	mu.Lock()
	if abortCalls != 1 {
		t.Fatalf("abortCalls = %d, want 1", abortCalls)
	}
	initCheck := initCalls
	mu.Unlock()
	if initCheck != 1 {
		t.Fatalf("initCalls = %d, want 1", initCheck)
	}

	// 参数校验：partNumber 越界 / 缺 uploadId
	if rr5 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/multipart/part", `{"key":"x","uploadId":"u","partNumber":0}`); rr5.Code != http.StatusBadRequest {
		t.Fatalf("part bad number = %d, want 400", rr5.Code)
	}
	if rr6 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/multipart/complete", `{"key":"x","parts":[{"partNumber":1,"etag":"e"}]}`); rr6.Code != http.StatusBadRequest {
		t.Fatalf("complete without uploadId = %d, want 400", rr6.Code)
	}
}
