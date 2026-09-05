package handler

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weilai1949/s3clinet/server/internal/model"
	"github.com/weilai1949/s3clinet/server/internal/store"
)

// miniStore 包装真实 store，但可注入指定方法的错误。
type miniStore struct {
	store.AccountStore
	listErr   error
	getErr    error
	createErr error
	updateErr error
	deleteErr error
}

func (m *miniStore) List() ([]*model.Account, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.AccountStore.List()
}
func (m *miniStore) Get(id string) (*model.Account, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.AccountStore.Get(id)
}
func (m *miniStore) Create(a *model.Account) (*model.Account, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	return m.AccountStore.Create(a)
}
func (m *miniStore) Update(id string, a *model.Account) (*model.Account, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	return m.AccountStore.Update(id, a)
}
func (m *miniStore) Delete(id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	return m.AccountStore.Delete(id)
}

func newHandlerWithStore(t *testing.T, st store.AccountStore) http.Handler {
	t.Helper()
	h := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir(), nil, "", "test", false)
	return h.Routes()
}

func postJSON(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func putJSON(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func del(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// ---- accounts.go ----

func TestGapAccountsList(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	mini := &miniStore{AccountStore: st, listErr: io.EOF}
	h := newHandlerWithStore(t, mini)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/accounts", nil))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("listErr = %d", rr.Code)
	}
	// 正常
	h2 := newHandlerWithStore(t, st)
	rr = httptest.NewRecorder()
	h2.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/accounts", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("list ok = %d", rr.Code)
	}
}

func TestGapAccountsCreate(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := newHandlerWithStore(t, st)
	cases := []struct {
		name string
		body string
		want int
	}{
		{"bad json", "{not", http.StatusBadRequest},
		{"no name", `{"endpoint":"e","accessKey":"a","secretKey":"s"}`, http.StatusBadRequest},
		{"missing fields", `{"name":"n"}`, http.StatusBadRequest},
		{"ok", `{"name":"n","endpoint":"http://127.0.0.1:1","accessKey":"a","secretKey":"s"}`, http.StatusCreated},
	}
	for _, c := range cases {
		rr := postJSON(t, h, "/api/accounts", c.body)
		if rr.Code != c.want {
			t.Fatalf("%s = %d %s", c.name, rr.Code, rr.Body.String())
		}
	}
	// store.Create 错误 → 500
	mini := &miniStore{AccountStore: st, createErr: io.EOF}
	h2 := newHandlerWithStore(t, mini)
	rr := postJSON(t, h2, "/api/accounts", `{"name":"n","endpoint":"http://127.0.0.1:1","accessKey":"a","secretKey":"s"}`)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("create err = %d", rr.Code)
	}
}

func TestGapAccountsGet(t *testing.T) {
	st, _ := store.New(filepath.Join(t.TempDir(), "a.json"))
	a, _ := st.Create(&model.Account{Name: "x", Endpoint: "http://127.0.0.1:1", AccessKey: "a", SecretKey: "s"})
	h := newHandlerWithStore(t, st)
	// ok
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/accounts/"+a.ID, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("get ok = %d", rr.Code)
	}
	// not found
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/accounts/nope", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get missing = %d", rr.Code)
	}
	// store error
	mini := &miniStore{AccountStore: st, getErr: io.EOF}
	h2 := newHandlerWithStore(t, mini)
	rr = httptest.NewRecorder()
	h2.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/accounts/"+a.ID, nil))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("get err = %d", rr.Code)
	}
}

func TestGapAccountsUpdate(t *testing.T) {
	st, _ := store.New(filepath.Join(t.TempDir(), "a.json"))
	a, _ := st.Create(&model.Account{Name: "x", Endpoint: "http://127.0.0.1:1", AccessKey: "a", SecretKey: "s"})
	h := newHandlerWithStore(t, st)
	cases := []struct {
		name, body string
		want       int
	}{
		{"bad json", "{x", http.StatusBadRequest},
		{"no name", `{"endpoint":"e","accessKey":"a"}`, http.StatusBadRequest},
		{"missing endpoint", `{"name":"y","accessKey":"a"}`, http.StatusBadRequest},
		{"ok masked", `{"name":"y","endpoint":"e","accessKey":"a","secretKey":"` + model.MaskedSecret + `"}`, http.StatusOK},
		{"not found", `{"name":"y","endpoint":"e","accessKey":"a"}`, http.StatusNotFound},
	}
	for i, c := range cases {
		_ = i
		req := httptest.NewRequest(http.MethodPut, "/api/accounts/"+a.ID+"/nope-suffix", bytes.NewBufferString(c.body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		if c.name == "not found" {
			req = httptest.NewRequest(http.MethodPut, "/api/accounts/nope", bytes.NewBufferString(c.body))
		} else {
			req = httptest.NewRequest(http.MethodPut, "/api/accounts/"+a.ID, bytes.NewBufferString(c.body))
		}
		req.Header.Set("Content-Type", "application/json")
		rr = httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != c.want {
			t.Fatalf("%s = %d %s", c.name, rr.Code, rr.Body.String())
		}
	}
	// store.Update 错误 → 500
	mini := &miniStore{AccountStore: st, updateErr: io.EOF}
	h2 := newHandlerWithStore(t, mini)
	req := httptest.NewRequest(http.MethodPut, "/api/accounts/"+a.ID, bytes.NewBufferString(`{"name":"y","endpoint":"e","accessKey":"a"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h2.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("update err = %d", rr.Code)
	}
}

func TestGapAccountsDelete(t *testing.T) {
	st, _ := store.New(filepath.Join(t.TempDir(), "a.json"))
	a, _ := st.Create(&model.Account{Name: "x", Endpoint: "http://127.0.0.1:1", AccessKey: "a", SecretKey: "s"})
	h := newHandlerWithStore(t, st)
	// not found
	rr := del(t, h, "/api/accounts/nope")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("delete missing = %d", rr.Code)
	}
	// ok
	rr = del(t, h, "/api/accounts/"+a.ID)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete ok = %d", rr.Code)
	}
	// store err
	mini := &miniStore{AccountStore: st, deleteErr: io.EOF}
	h2 := newHandlerWithStore(t, mini)
	rr = del(t, h2, "/api/accounts/"+a.ID)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("delete err = %d", rr.Code)
	}
}

func TestGapAccountsTestAccount(t *testing.T) {
	// ListBuckets = GET /（无 bucket 段）；ListObjectsV2 = GET /<bucket>?list-type=2
	// 用一个通用 fake：根路径返回 ListAllMyBucketsResult；其他 GET 返回空 ListBucketResult。
	okHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "" {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0"?><ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>x</ID><DisplayName>x</DisplayName></Owner><Buckets><Bucket><Name>b</Name><CreationDate>2024-01-01T00:00:00.000Z</CreationDate></Bucket></Buckets></ListAllMyBucketsResult>`))
			return
		}
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></ListBucketResult>`))
	}
	srvOK := accStartFake(t, okHandler)
	env := accNewEnv(t, srvOK.URL, "") // 无默认桶
	rr := env.accDoRec("POST", "/api/accounts/"+env.acc.ID+"/test", `{}`)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"ok":true`) {
		t.Fatalf("test no-bucket ok = %d %s", rr.Code, rr.Body.String())
	}
	// ListBuckets 失败
	srvErr := accStartFake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>InternalError</Code><Message>x</Message></Error>`))
	})
	env2 := accNewEnv(t, srvErr.URL, "")
	rr = env2.accDoRec("POST", "/api/accounts/"+env2.acc.ID+"/test", `{}`)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"ok":false`) {
		t.Fatalf("test list err = %d %s", rr.Code, rr.Body.String())
	}
	// 有默认桶时走 HeadBucket
	srvOK2 := accStartFake(t, func(w http.ResponseWriter, r *http.Request) {
		// HEAD /b
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	env3 := accNewEnv(t, srvOK2.URL, "b")
	rr = env3.accDoRec("POST", "/api/accounts/"+env3.acc.ID+"/test", `{}`)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"bucket":"b"`) {
		t.Fatalf("test head ok = %d %s", rr.Code, rr.Body.String())
	}
	// HeadBucket 失败
	srvErr2 := accStartFake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>NoSuchBucket</Code><Message>x</Message></Error>`))
	})
	env4 := accNewEnv(t, srvErr2.URL, "b")
	rr = env4.accDoRec("POST", "/api/accounts/"+env4.acc.ID+"/test", `{}`)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"ok":false`) {
		t.Fatalf("test head err = %d %s", rr.Code, rr.Body.String())
	}
}

func TestGapAccountsPreviewBuckets(t *testing.T) {
	srvOK := accStartFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "" {
			_, _ = w.Write([]byte(`<?xml version="1.0"?><ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>x</ID><DisplayName>x</DisplayName></Owner><Buckets><Bucket><Name>b</Name><CreationDate>2024-01-01T00:00:00.000Z</CreationDate></Bucket></Buckets></ListAllMyBucketsResult>`))
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	st, _ := store.New(filepath.Join(t.TempDir(), "a.json"))
	h := newHandlerWithStore(t, st)
	body := `{"endpoint":"` + srvOK.URL + `","accessKey":"a","secretKey":"s"}`
	rr := postJSON(t, h, "/api/accounts/preview-buckets", body)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"name":"b"`) {
		t.Fatalf("preview ok = %d %s", rr.Code, rr.Body.String())
	}
	// 错误路径
	for _, c := range []struct {
		name, body string
		want       int
	}{
		{"bad json", "{x", http.StatusBadRequest},
		{"missing fields", `{}`, http.StatusBadRequest},
	} {
		rr := postJSON(t, h, "/api/accounts/preview-buckets", c.body)
		if rr.Code != c.want {
			t.Fatalf("%s = %d %s", c.name, rr.Code, rr.Body.String())
		}
	}
	// ListBuckets 失败
	srvErr := accStartFake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>InternalError</Code><Message>x</Message></Error>`))
	})
	body = `{"endpoint":"` + srvErr.URL + `","accessKey":"a","secretKey":"s"}`
	rr = postJSON(t, h, "/api/accounts/preview-buckets", body)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("preview s3 err = %d", rr.Code)
	}
}

func TestGapValidBucketName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"too short", "ab", false},
		{"too long", strings.Repeat("a", 64), false},
		{"uppercase", "Bucket", false},
		{"underscore", "a_b", false},
		{"ok", "my-bucket.1", true},
		{"ok dots", "a.b.c", true},
		{"ok digits", "abc123", true},
		{"ok 3 char", "abc", true},
		{"ok 63", "a" + strings.Repeat("b", 62), true},
		{"underscore mid", "a__b", false},
		{"special", "a$b", false},
		{"upper digit mix", "aB1", false},
		{"leading dash", "-abc", false},
		{"trailing dash", "abc-", false},
		{"leading dot", ".abc", false},
		{"trailing dot", "abc.", false},
		{"consecutive dots", "a..b", false},
		{"dot then dash", "a.-b", false},
		{"dash then dot", "a-.b", false},
	}
	for _, c := range cases {
		if got := validBucketName(c.in); got != c.want {
			t.Fatalf("%s: %q → %v, want %v", c.name, c.in, got, c.want)
		}
	}
}

// ---- buckets.go ----

func TestGapBucketsList(t *testing.T) {
	srv := accStartFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "" {
			_, _ = w.Write([]byte(`<?xml version="1.0"?><ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>x</ID><DisplayName>x</DisplayName></Owner><Buckets><Bucket><Name>b1</Name><CreationDate>2024-01-01T00:00:00.000Z</CreationDate></Bucket></Buckets></ListAllMyBucketsResult>`))
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	env := accNewEnv(t, srv.URL, "")
	rr := env.accDoRec("GET", "/api/accounts/"+env.acc.ID+"/buckets", "")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"name":"b1"`) {
		t.Fatalf("list buckets = %d %s", rr.Code, rr.Body.String())
	}
	// error
	srvErr := accStartFake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>InternalError</Code><Message>x</Message></Error>`))
	})
	env2 := accNewEnv(t, srvErr.URL, "")
	rr = env2.accDoRec("GET", "/api/accounts/"+env2.acc.ID+"/buckets", "")
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("list buckets err = %d", rr.Code)
	}
}

func TestGapBucketsCreate(t *testing.T) {
	srv := accStartFake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	env := accNewEnv(t, srv.URL, "")
	base := "/api/accounts/" + env.acc.ID + "/bucket"
	for _, c := range []struct {
		name, body string
		want       int
	}{
		{"bad json", "{x", http.StatusBadRequest},
		{"bad name", `{"name":"Bucket-1"}`, http.StatusBadRequest},
		{"bad acl", `{"name":"myb","acl":"public"}`, http.StatusBadRequest},
		{"ok no acl", `{"name":"myb"}`, http.StatusOK},
		{"ok private", `{"name":"myb2","acl":"private"}`, http.StatusOK},
		{"ok public-read", `{"name":"myb3","acl":"public-read"}`, http.StatusOK},
		{"ok public-rw", `{"name":"myb4","acl":"public-read-write"}`, http.StatusOK},
	} {
		rr := postJSON(t, env.h, base, c.body)
		if rr.Code != c.want {
			t.Fatalf("%s = %d %s", c.name, rr.Code, rr.Body.String())
		}
	}
	// S3 错误
	srvErr := accStartFake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>InternalError</Code><Message>x</Message></Error>`))
	})
	env2 := accNewEnv(t, srvErr.URL, "")
	base2 := "/api/accounts/" + env2.acc.ID + "/bucket"
	rr := postJSON(t, env2.h, base2, `{"name":"myb"}`)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("create s3 err = %d", rr.Code)
	}
}

func TestGapBucketsDelete(t *testing.T) {
	srv := accStartFake(t, func(w http.ResponseWriter, r *http.Request) {
		// BucketNotEmpty → 409
		if strings.HasSuffix(r.URL.Path, "/b1") {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>BucketNotEmpty</Code><Message>x</Message></Error>`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	env := accNewEnv(t, srv.URL, "")
	base := "/api/accounts/" + env.acc.ID + "/bucket"
	// no name
	rr := del(t, env.h, base)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("no name = %d", rr.Code)
	}
	// BucketNotEmpty
	rr = del(t, env.h, base+"?name=b1")
	if rr.Code != http.StatusConflict {
		t.Fatalf("BucketNotEmpty = %d", rr.Code)
	}
	// ok
	rr = del(t, env.h, base+"?name=b2")
	if rr.Code != http.StatusOK {
		t.Fatalf("delete ok = %d", rr.Code)
	}
	// 通用 S3 错误
	srvErr := accStartFake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>InternalError</Code><Message>x</Message></Error>`))
	})
	env2 := accNewEnv(t, srvErr.URL, "")
	base2 := "/api/accounts/" + env2.acc.ID + "/bucket"
	rr = del(t, env2.h, base2+"?name=b3")
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("delete err = %d", rr.Code)
	}
}

func TestGapBucketsInfo(t *testing.T) {
	srv := accStartFake(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/" || r.URL.Path == "":
			_, _ = w.Write([]byte(`<?xml version="1.0"?><ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>x</ID><DisplayName>x</DisplayName></Owner><Buckets><Bucket><Name>b</Name><CreationDate>2024-01-01T00:00:00.000Z</CreationDate></Bucket></Buckets></ListAllMyBucketsResult>`))
		case q.Has("location"):
			_, _ = w.Write([]byte(`<?xml version="1.0"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-west-2</LocationConstraint>`))
		case q.Has("versioning"):
			_, _ = w.Write([]byte(`<?xml version="1.0"?><VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	})
	env := accNewEnv(t, srv.URL, "")
	// ok with bucket query
	rr := env.accDoRec("GET", "/api/accounts/"+env.acc.ID+"/bucket-info?bucket=b", "")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"region":"us-west-2"`) {
		t.Fatalf("info ok = %d %s", rr.Code, rr.Body.String())
	}
	// 缺桶（账号无默认桶）→ 400
	env2 := accNewEnv(t, srv.URL, "")
	rr = env2.accDoRec("GET", "/api/accounts/"+env2.acc.ID+"/bucket-info", "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("no bucket = %d", rr.Code)
	}
}

func TestGapBucketsVersioning(t *testing.T) {
	srv := accStartFake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	env := accNewEnv(t, srv.URL, "")
	base := "/api/accounts/" + env.acc.ID + "/bucket-versioning"
	for _, c := range []struct {
		name, body string
		want       int
	}{
		{"bad json", "{x", http.StatusBadRequest},
		{"missing bucket", `{"status":"Enabled"}`, http.StatusBadRequest},
		{"bad status", `{"bucket":"b","status":"On"}`, http.StatusBadRequest},
		{"ok Enabled", `{"bucket":"b","status":"Enabled"}`, http.StatusOK},
		{"ok Suspended", `{"bucket":"b","status":"Suspended"}`, http.StatusOK},
	} {
		rr := putJSON(t, env.h, base, c.body)
		if rr.Code != c.want {
			t.Fatalf("%s = %d %s", c.name, rr.Code, rr.Body.String())
		}
	}
	// S3 错误
	srvErr := accStartFake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>InternalError</Code><Message>x</Message></Error>`))
	})
	env2 := accNewEnv(t, srvErr.URL, "")
	base2 := "/api/accounts/" + env2.acc.ID + "/bucket-versioning"
	rr := putJSON(t, env2.h, base2, `{"bucket":"b","status":"Enabled"}`)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("versioning err = %d", rr.Code)
	}
}

// ---- bucket_settings.go 补：put/website policy/tags 的 errb 分支，删除类的 errb ----

func TestGapBucketSettingsErrors(t *testing.T) {
	srvErr := accStartFake(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>InternalError</Code><Message>x</Message></Error>`))
	})
	env := accNewEnv(t, srvErr.URL, "b")
	base := "/api/accounts/" + env.acc.ID + "/bucket"
	cases := []struct{ name, method, path, body string }{
		{"put enc", "PUT", base + "/encryption", `{"bucket":"b","algorithm":"AES256"}`},
		{"del enc", "DELETE", base + "/encryption?bucket=b", ""},
		{"put cors", "PUT", base + "/cors", `{"bucket":"b","rules":[{"id":"r1","allowedMethods":["GET"],"allowedOrigins":["*"]}]}`},
		{"del cors", "DELETE", base + "/cors?bucket=b", ""},
		{"put web", "PUT", base + "/website", `{"bucket":"b","indexDocument":"i.html"}`},
		{"del web", "DELETE", base + "/website?bucket=b", ""},
		{"put pol", "PUT", base + "/policy", `{"bucket":"b","policy":"{}"}`},
		{"del pol", "DELETE", base + "/policy?bucket=b", ""},
		{"put tag", "PUT", base + "/tags", `{"bucket":"b","tags":[{"key":"k","value":"v"}]}`},
		{"del tag", "DELETE", base + "/tags?bucket=b", ""},
	}
	for _, c := range cases {
		var rr *httptest.ResponseRecorder
		switch c.method {
		case http.MethodPut:
			rr = putJSON(t, env.h, c.path, c.body)
		case http.MethodDelete:
			rr = del(t, env.h, c.path)
		}
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("%s = %d %s", c.name, rr.Code, rr.Body.String())
		}
	}
}

// TestGapBucketSettingsMappedErrors 各配置端点收到 S3 非 500 错误（403/400 等）时，
// writeInternalErr 必须用 s3HTTPStatus + s3UserMessage 分支返回对应的状态码，
// 而不是回退到 publicMsg 的 500。这是 bucket_settings.go 49-51/63-65/... 块的覆盖。
func TestGapBucketSettingsMappedErrors(t *testing.T) {
	srv := accStartFake(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>AccessDenied</Code><Message>x</Message></Error>`))
	})
	env := accNewEnv(t, srv.URL, "b")
	base := "/api/accounts/" + env.acc.ID + "/bucket"
	cases := []struct {
		name, method, path, body string
	}{
		{"get enc", "GET", base + "/encryption?bucket=b", ""},
		{"put enc", "PUT", base + "/encryption", `{"bucket":"b","algorithm":"AES256"}`},
		{"del enc", "DELETE", base + "/encryption?bucket=b", ""},
		{"get cors", "GET", base + "/cors?bucket=b", ""},
		{"put cors", "PUT", base + "/cors", `{"bucket":"b","rules":[{"id":"r1","allowedMethods":["GET"],"allowedOrigins":["*"]}]}`},
		{"del cors", "DELETE", base + "/cors?bucket=b", ""},
		{"get web", "GET", base + "/website?bucket=b", ""},
		{"put web", "PUT", base + "/website", `{"bucket":"b","indexDocument":"i.html"}`},
		{"del web", "DELETE", base + "/website?bucket=b", ""},
		{"get pol", "GET", base + "/policy?bucket=b", ""},
		{"put pol", "PUT", base + "/policy", `{"bucket":"b","policy":"{}"}`},
		{"del pol", "DELETE", base + "/policy?bucket=b", ""},
		{"get tag", "GET", base + "/tags?bucket=b", ""},
		{"put tag", "PUT", base + "/tags", `{"bucket":"b","tags":[{"key":"k","value":"v"}]}`},
		{"del tag", "DELETE", base + "/tags?bucket=b", ""},
	}
	for _, c := range cases {
		var rr *httptest.ResponseRecorder
		switch c.method {
		case http.MethodGet:
			rr = env.accDoRec(c.method, c.path, c.body)
		case http.MethodPut:
			rr = putJSON(t, env.h, c.path, c.body)
		case http.MethodDelete:
			rr = del(t, env.h, c.path)
		}
		if rr.Code != http.StatusForbidden {
			t.Fatalf("%s = %d %s (want 403)", c.name, rr.Code, rr.Body.String())
		}
	}
}

// TestGapAccountsBadAccount 各种路径下账号不存在 → 404（accountClient 早返）。
func TestGapAccountsBadAccount(t *testing.T) {
	h := newTestHandler(t, nil, "")
	gets := []string{
		"/api/accounts/nope/buckets",
		"/api/accounts/nope/bucket-info",
		"/api/accounts/nope/bucket/encryption?bucket=b",
		"/api/accounts/nope/bucket/cors?bucket=b",
		"/api/accounts/nope/bucket/website?bucket=b",
		"/api/accounts/nope/bucket/policy?bucket=b",
		"/api/accounts/nope/bucket/tags?bucket=b",
	}
	for _, path := range gets {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("GET %s = %d (want 404)", path, rr.Code)
		}
	}
	// /test 是 POST
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/accounts/nope/test", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("POST /test = %d (want 404)", rr.Code)
	}
	// bucket-versioning (PUT)
	rr = putJSON(t, h, "/api/accounts/nope/bucket-versioning", `{"bucket":"b","status":"Enabled"}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("bucket-versioning bad id = %d", rr.Code)
	}
	// bucket settings PUTs (5 个) 全部应 404
	base := "/api/accounts/nope/bucket"
	puts := []struct{ path, body string }{
		{"/encryption", `{"bucket":"b","algorithm":"AES256"}`},
		{"/cors", `{"bucket":"b","rules":[{"id":"r1","allowedMethods":["GET"],"allowedOrigins":["*"]}]}`},
		{"/website", `{"bucket":"b","indexDocument":"i.html"}`},
		{"/policy", `{"bucket":"b","policy":"{}"}`},
		{"/tags", `{"bucket":"b","tags":[{"key":"k","value":"v"}]}`},
	}
	for _, p := range puts {
		rr = putJSON(t, h, base+p.path, p.body)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("PUT %s bad id = %d", p.path, rr.Code)
		}
	}
	// bucket settings DELETEs (5 个) 全部应 404
	deletes := []string{"/encryption", "/cors", "/website", "/policy", "/tags"}
	for _, p := range deletes {
		rr = del(t, h, base+p)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("DELETE %s bad id = %d", p, rr.Code)
		}
	}
	// POST/DELETE /bucket 账号不存在
	rr = postJSON(t, h, "/api/accounts/nope/bucket", `{"name":"myb"}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("POST /bucket bad id = %d", rr.Code)
	}
	rr = del(t, h, "/api/accounts/nope/bucket?name=x")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("DELETE /bucket bad id = %d", rr.Code)
	}
}

// TestGapBucketSettingsNoDefaultBucket bucket 设置接口在账号无默认桶且请求未带桶 → 400。
// 覆盖 delete*/put* 内 `if !ok { return }` 的 bucketOr 早返分支。
func TestGapBucketSettingsNoDefaultBucket(t *testing.T) {
	srv := accStartFake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	env := accNewEnv(t, srv.URL, "") // 无默认桶
	base := "/api/accounts/" + env.acc.ID + "/bucket"
	// GET/DELETE 不带 bucket 参数 → bucketOr 返回 400
	gets := []string{"/encryption", "/cors", "/website", "/policy", "/tags"}
	for _, p := range gets {
		rr := env.accDoRec("GET", base+p, "")
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("GET %s = %d (want 400)", p, rr.Code)
		}
		rr = del(t, env.h, base+p) // 无 query 的 DELETE
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("DELETE %s = %d (want 400)", p, rr.Code)
		}
	}
	// PUT 无 bucket 字段 → bucketOr 400；body 也非法 JSON 触发 readJSON 400
	puts := []string{"/encryption", "/cors", "/website", "/policy", "/tags"}
	for _, p := range puts {
		// 合法 JSON 但缺 bucket 字段 → bucketOr 400
		rr := putJSON(t, env.h, base+p, `{}`)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("PUT %s {} = %d (want 400)", p, rr.Code)
		}
		// 非法 JSON → readJSON 400
		rr = putJSON(t, env.h, base+p, `{bad`)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("PUT %s {bad = %d (want 400)", p, rr.Code)
		}
	}
}

// TestGapBucketSettingsEmptyDeleteErrors 空规则/空策略/空标签触发底层 DeleteXxx；
// 配 S3 500 错误覆盖 writeInternalErr 错误分支。
func TestGapBucketSettingsEmptyDeleteErrors(t *testing.T) {
	srv := accStartFake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>InternalError</Code><Message>x</Message></Error>`))
	})
	env := accNewEnv(t, srv.URL, "b")
	base := "/api/accounts/" + env.acc.ID + "/bucket"
	// PUT cors 空 rules → 内部 DeleteCors，500
	rr := putJSON(t, env.h, base+"/cors", `{"bucket":"b","rules":[]}`)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("put cors empty rules = %d", rr.Code)
	}
	// PUT policy 空字符串 → 内部 DeletePolicy，500
	rr = putJSON(t, env.h, base+"/policy", `{"bucket":"b","policy":""}`)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("put policy empty = %d", rr.Code)
	}
	// PUT tags 空数组 → 内部 DeleteBucketTags，500
	rr = putJSON(t, env.h, base+"/tags", `{"bucket":"b","tags":[]}`)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("put tags empty = %d", rr.Code)
	}
}

// TestGapAccountsPreviewBucketsInvalidConfig s3wrap.New 在 endpoint 不合规时返回 error → 400。
// 169.254.169.254 是 AWS IMDS，被 SSRF 阻断表拦截。
func TestGapAccountsPreviewBucketsInvalidConfig(t *testing.T) {
	st, _ := store.New(filepath.Join(t.TempDir(), "a.json"))
	h := newHandlerWithStore(t, st)
	body := `{"endpoint":"http://169.254.169.254","accessKey":"a","secretKey":"s"}`
	rr := postJSON(t, h, "/api/accounts/preview-buckets", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("preview imds = %d %s", rr.Code, rr.Body.String())
	}
}

// TestGapBucketsS3ErrorPaths createBucket / deleteBucket / getBucketInfo 错误路径。
func TestGapBucketsS3ErrorPaths(t *testing.T) {
	srv := accStartFake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>InternalError</Code><Message>x</Message></Error>`))
	})
	env := accNewEnv(t, srv.URL, "")
	// createBucket S3 错误（覆盖 if !ok return 之外的真实错误分支）
	rr := postJSON(t, env.h, "/api/accounts/"+env.acc.ID+"/bucket", `{"name":"myb"}`)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("createBucket s3 err = %d", rr.Code)
	}
	// deleteBucket 缺 name
	rr = del(t, env.h, "/api/accounts/"+env.acc.ID+"/bucket")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("deleteBucket no name = %d", rr.Code)
	}
	// getBucketInfo 账号无默认桶 + 无 query → 400
	rr = env.accDoRec("GET", "/api/accounts/"+env.acc.ID+"/bucket-info", "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("getBucketInfo no bucket = %d", rr.Code)
	}
	// getBucketInfo 正常路径覆盖 rerr/verr 警告分支
	srvOK := accStartFake(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/" || r.URL.Path == "":
			_, _ = w.Write([]byte(`<?xml version="1.0"?><ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>x</ID><DisplayName>x</DisplayName></Owner><Buckets><Bucket><Name>b</Name><CreationDate>2024-01-01T00:00:00.000Z</CreationDate></Bucket></Buckets></ListAllMyBucketsResult>`))
		case q.Has("location"):
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>InternalError</Code><Message>x</Message></Error>`))
		case q.Has("versioning"):
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>InternalError</Code><Message>x</Message></Error>`))
		}
	})
	env2 := accNewEnv(t, srvOK.URL, "b")
	rr = env2.accDoRec("GET", "/api/accounts/"+env2.acc.ID+"/bucket-info?bucket=b", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("getBucketInfo rerr/verr warn = %d %s", rr.Code, rr.Body.String())
	}
}
