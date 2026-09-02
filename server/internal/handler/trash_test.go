package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"sync"
	"testing"

	"github.com/weilai1949/s3clinet/server/internal/model"
	"github.com/weilai1949/s3clinet/server/internal/store"
)

const trashXML = `<?xml version="1.0" encoding="UTF-8"?><ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>b</Name><IsTruncated>true</IsTruncated><NextKeyMarker>k9</NextKeyMarker><NextVersionIdMarker>d9</NextVersionIdMarker><DeleteMarker><Key>deleted1.txt</Key><VersionId>d1</VersionId><IsLatest>true</IsLatest><LastModified>2026-09-01T00:00:00.000Z</LastModified></DeleteMarker><DeleteMarker><Key>deleted2.txt</Key><VersionId>d2</VersionId><IsLatest>true</IsLatest><LastModified>2026-09-01T00:00:00.000Z</LastModified></DeleteMarker><Version><Key>alive.txt</Key><VersionId>vv1</VersionId><IsLatest>true</IsLatest><LastModified>2026-09-01T00:00:00.000Z</LastModified><Size>5</Size><ETag>&quot;e&quot;</ETag></Version></ListVersionsResult>`

const purgeXML = `<?xml version="1.0" encoding="UTF-8"?><ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>b</Name><IsTruncated>false</IsTruncated><Version><Key>purge-key</Key><VersionId>v1</VersionId><IsLatest>false</IsLatest><LastModified>2026-09-01T00:00:00.000Z</LastModified><Size>1</Size><ETag>&quot;e1&quot;</ETag></Version><Version><Key>purge-key</Key><VersionId>v2</VersionId><IsLatest>false</IsLatest><LastModified>2026-09-01T00:00:00.000Z</LastModified><Size>2</Size><ETag>&quot;e2&quot;</ETag></Version><DeleteMarker><Key>purge-key</Key><VersionId>dv1</VersionId><IsLatest>true</IsLatest><LastModified>2026-09-01T00:00:00.000Z</LastModified></DeleteMarker></ListVersionsResult>`

// TestTrash 用假 S3 验证：回收站列表（仅删除标记 + 分页游标）与彻底清除（逐版本删除）。
func TestTrash(t *testing.T) {
	var (
		mu         sync.Mutex
		delVers    []string
		bareDelete int
	)
	s3fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// DeleteObjects 批量：POST/?delete，body 为 <Delete><Object><Key>..</Key><VersionId>..</VersionId></Object></Delete>
		if r.Method == http.MethodPost && r.URL.Query().Has("delete") {
			b, _ := io.ReadAll(r.Body)
			re := regexp.MustCompile(`<VersionId>([^<]+)</VersionId>`)
			mu.Lock()
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				delVers = append(delVers, m[1])
			}
			mu.Unlock()
			io.WriteString(w, `<DeleteResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></DeleteResult>`)
			return
		}
		if r.Method == http.MethodDelete {
			if vid := r.URL.Query().Get("versionId"); vid != "" {
				mu.Lock()
				delVers = append(delVers, vid)
				mu.Unlock()
			} else {
				mu.Lock()
				bareDelete++
				mu.Unlock()
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Query().Has("versions") {
			switch r.URL.Query().Get("prefix") {
			case "purge-key":
				io.WriteString(w, purgeXML)
			case "nover":
				io.WriteString(w, `<?xml version="1.0"?><ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>b</Name><IsTruncated>false</IsTruncated></ListVersionsResult>`)
			default:
				io.WriteString(w, trashXML)
			}
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
	h := New(st, logger, t.TempDir(), nil, "", "test").Routes()

	// 回收站列表：只返回删除标记，且分页游标透传
	rr := doJSON(t, h, "GET", "/api/accounts/"+acc.ID+"/trash?bucket=b", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("trash status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		DeleteMarkers []struct {
			Key       string `json:"key"`
			VersionID string `json:"versionId"`
			IsLatest  bool   `json:"isLatest"`
		} `json:"deleteMarkers"`
		IsTruncated         bool   `json:"isTruncated"`
		NextKeyMarker       string `json:"nextKeyMarker"`
		NextVersionIDMarker string `json:"nextVersionIdMarker"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("trash body: %v", err)
	}
	if len(resp.DeleteMarkers) != 2 || resp.DeleteMarkers[0].Key != "deleted1.txt" || resp.DeleteMarkers[0].VersionID != "d1" {
		t.Fatalf("delete markers = %+v", resp.DeleteMarkers)
	}
	if !resp.IsTruncated || resp.NextKeyMarker != "k9" || resp.NextVersionIDMarker != "d9" {
		t.Fatalf("pagination = truncated:%v nextKey:%q nextVer:%q", resp.IsTruncated, resp.NextKeyMarker, resp.NextVersionIDMarker)
	}

	// 彻底清除：逐版本删除（含删除标记），不触发裸删除
	rr2 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/trash/purge", `{"bucket":"b","key":"purge-key"}`)
	if rr2.Code != http.StatusOK {
		t.Fatalf("purge status=%d body=%s", rr2.Code, rr2.Body.String())
	}
	var pr struct {
		Purged  string `json:"purged"`
		Deleted int    `json:"deleted"`
	}
	if err := json.Unmarshal(rr2.Body.Bytes(), &pr); err != nil {
		t.Fatalf("purge body: %v", err)
	}
	mu.Lock()
	gotVer := delVers
	gotBare := bareDelete
	mu.Unlock()
	if pr.Purged != "purge-key" || pr.Deleted != 3 || len(gotVer) != 3 || gotBare != 0 {
		t.Fatalf("pr=%+v delVers=%v bare=%d", pr, gotVer, gotBare)
	}

	// 非版本控制桶兜底：无版本记录 → 裸删除 1 次
	rr3 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/trash/purge", `{"bucket":"b","key":"nover"}`)
	mu.Lock()
	gotBare2 := bareDelete
	mu.Unlock()
	if rr3.Code != http.StatusOK || gotBare2 != 1 {
		t.Fatalf("nover purge status=%d bare=%d", rr3.Code, gotBare2)
	}

	// 缺 key / 参数校验
	if rr4 := doJSON(t, h, "POST", "/api/accounts/"+acc.ID+"/trash/purge", `{"bucket":"b"}`); rr4.Code != http.StatusBadRequest {
		t.Fatalf("purge without key=%d, want 400", rr4.Code)
	}
}
