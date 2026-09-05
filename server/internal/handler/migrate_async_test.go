package handler

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weilai1949/s3clinet/server/internal/model"
	"github.com/weilai1949/s3clinet/server/internal/service"
	"github.com/weilai1949/s3clinet/server/internal/store"
)

func TestMigrateAsyncSSE(t *testing.T) {
	var mu sync.Mutex
	puts := 0
	srcS3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, "data")
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer srcS3.Close()
	dstS3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			mu.Lock()
			puts++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer dstS3.Close()

	st, _ := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	src, _ := st.Create(&model.Account{Name: "s", Endpoint: srcS3.URL, Region: "us-east-1", AccessKey: "ak", SecretKey: "sk", Bucket: "b", PathStyle: true})
	dst, _ := st.Create(&model.Account{Name: "d", Endpoint: dstS3.URL, Region: "us-east-1", AccessKey: "ak", SecretKey: "sk", Bucket: "b2", PathStyle: true})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, logger, t.TempDir(), nil, "", "test", false).Routes()

	body := `{"sourceAccountId":"` + src.ID + `","sourceBucket":"b","sourceKeys":["a.txt","b.txt"],"targetAccountId":"` + dst.ID + `","targetBucket":"b2","targetPrefix":""}`
	rr := doJSON(t, h, "POST", "/api/migrate/async", body)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("async status=%d body=%s", rr.Code, rr.Body.String())
	}
	var start struct {
		JobID string `json:"jobId"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &start); err != nil || start.JobID == "" {
		t.Fatalf("jobId: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/migrate/jobs/"+start.JobID+"/events", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("sse status=%d", w.Code)
	}
	sc := bufio.NewScanner(w.Body)
	var last service.JobProgress
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") {
			_ = json.Unmarshal([]byte(line[6:]), &last)
		}
	}
	if last.Status != "done" || last.Migrated != 2 {
		t.Fatalf("progress=%+v", last)
	}
	mu.Lock()
	if puts != 2 {
		t.Fatalf("puts=%d", puts)
	}
	mu.Unlock()

	// 等待 job 注册表写入 result
	time.Sleep(50 * time.Millisecond)
	stRR := doJSON(t, h, "GET", "/api/migrate/jobs/"+start.JobID, "")
	if stRR.Code != http.StatusOK {
		t.Fatalf("status poll=%d", stRR.Code)
	}
}
