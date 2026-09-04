package handler

// acc_helper_test.go —— 账号/桶/基建类测试的共享辅助（仅本代理新增，函数名以 acc 前缀避免冲突）。
// 约定：不改生产代码与既有测试；桩与假服务全部并发安全（-race）。

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/aws/smithy-go"
	"github.com/weilai1949/s3clinet/server/internal/model"
	"github.com/weilai1949/s3clinet/server/internal/store"
)

// ---- 错误码桩：实现 smithy.APIError，用于白盒直测 writeInternalErr 的映射分支 ----

// accCodeErr 携带固定 S3 错误码的最小 APIError 实现。
type accCodeErr struct{ code string }

func (e accCodeErr) Error() string                 { return "s3 error: " + e.code }
func (e accCodeErr) ErrorCode() string             { return e.code }
func (e accCodeErr) ErrorMessage() string          { return e.code }
func (e accCodeErr) ErrorFault() smithy.ErrorFault { return smithy.FaultUnknown }

// ---- 可注入错误的账号存储桩 ----

// accStubStore 可注入错误的账号存储桩：各方法返回可配置的错误/结果。
type accStubStore struct {
	mu        sync.Mutex
	listErr   error
	getErr    error // 为 nil 且 getAcc 为 nil 时返回 store.ErrNotFound
	getAcc    *model.Account
	createErr error
	updateErr error
	deleteErr error
}

func (s *accStubStore) List() ([]*model.Account, error) { return nil, s.listErr }

func (s *accStubStore) Get(id string) (*model.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.getAcc != nil {
		cp := *s.getAcc
		return &cp, nil
	}
	return nil, store.ErrNotFound
}

func (s *accStubStore) Create(a *model.Account) (*model.Account, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	a.ID = "acc-stub-id"
	return a.Sanitized(), nil
}

func (s *accStubStore) Update(id string, a *model.Account) (*model.Account, error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	cp := *a
	cp.ID = id
	return cp.Sanitized(), nil
}

func (s *accStubStore) Delete(string) error { return s.deleteErr }
func (s *accStubStore) Ping() error         { return nil }
func (s *accStubStore) Close() error        { return nil }

// ---- 假 S3：错误注入器 ----

// accS3Fail 注入的 S3 错误：HTTP 状态 + 错误码（写进 XML <Error><Code>）。
type accS3Fail struct {
	status int
	code   string
}

// accFailInjector 按 "METHOD|操作键" 注入 S3 错误（并发安全）。
type accFailInjector struct {
	mu    sync.Mutex
	rules map[string]accS3Fail
}

func newAccFailInjector() *accFailInjector {
	return &accFailInjector{rules: make(map[string]accS3Fail)}
}

// set 设置某操作键下指定方法的注入错误。
func (f *accFailInjector) set(method, op string, status int, code string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rules[method+"|"+op] = accS3Fail{status: status, code: code}
}

// take 命中则写出错误响应并返回 true（表示请求已处理）。
func (f *accFailInjector) take(w http.ResponseWriter, method, op string) bool {
	f.mu.Lock()
	fail, ok := f.rules[method+"|"+op]
	f.mu.Unlock()
	if ok {
		accErrXML(w, fail.status, fail.code)
	}
	return ok
}

// accErrXML 输出 S3 风格错误 XML（与真实 S3 错误响应同构，SDK 会解析出错误码）。
func accErrXML(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<?xml version="1.0"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, code)
}

// ---- 假 S3：桶级配置（encryption/cors/website/policy/tagging） ----

const (
	accEncryptionXML  = `<?xml version="1.0" encoding="UTF-8"?><ServerSideEncryptionConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Rule><ApplyServerSideEncryptionByDefault><SSEAlgorithm>AES256</SSEAlgorithm></ApplyServerSideEncryptionByDefault><BucketKeyEnabled>true</BucketKeyEnabled></Rule></ServerSideEncryptionConfiguration>`
	accCorsXML        = `<?xml version="1.0" encoding="UTF-8"?><CORSConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><CORSRule><ID>r1</ID><AllowedOrigin>*</AllowedOrigin><AllowedMethod>GET</AllowedMethod><AllowedHeader>*</AllowedHeader><MaxAgeSeconds>3600</MaxAgeSeconds></CORSRule></CORSConfiguration>`
	accWebsiteXML     = `<?xml version="1.0" encoding="UTF-8"?><WebsiteConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IndexDocument><Suffix>index.html</Suffix></IndexDocument><ErrorDocument><Key>error.html</Key></ErrorDocument></WebsiteConfiguration>`
	accTaggingXML     = `<?xml version="1.0" encoding="UTF-8"?><Tagging xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><TagSet><Tag><Key>env</Key><Value>prod</Value></Tag></TagSet></Tagging>`
	accListBucketsXML = `<?xml version="1.0" encoding="UTF-8"?><ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>o</ID></Owner><Buckets><Bucket><Name>b</Name><CreationDate>2026-09-01T00:00:00.000Z</CreationDate></Bucket></Buckets></ListAllMyBucketsResult>`
)

// accSettingsOp 从请求推断桶级配置操作键；无匹配返回空串。
func accSettingsOp(r *http.Request) string {
	q := r.URL.Query()
	switch {
	case q.Has("encryption"):
		return "encryption"
	case q.Has("cors"):
		return "cors"
	case q.Has("website"):
		return "website"
	case q.Has("policy"):
		return "policy"
	case q.Has("tagging"):
		return "tagging"
	}
	return ""
}

// accSettingsFake 桶级配置假 S3：正常返回各配置内容，注入器命中时返回错误。
func accSettingsFake(inj *accFailInjector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := accSettingsOp(r)
		if op != "" && inj.take(w, r.Method, op) {
			return
		}
		ok2xx := func() { w.WriteHeader(http.StatusOK) }
		noContent := func() { w.WriteHeader(http.StatusNoContent) }
		switch {
		case op == "encryption" && r.Method == http.MethodGet:
			io.WriteString(w, accEncryptionXML)
		case op == "encryption" && r.Method == http.MethodPut:
			ok2xx()
		case op == "encryption":
			noContent()
		case op == "cors" && r.Method == http.MethodGet:
			io.WriteString(w, accCorsXML)
		case op == "cors" && r.Method == http.MethodPut:
			ok2xx()
		case op == "cors":
			noContent()
		case op == "website" && r.Method == http.MethodGet:
			io.WriteString(w, accWebsiteXML)
		case op == "website" && r.Method == http.MethodPut:
			ok2xx()
		case op == "website":
			noContent()
		case op == "policy" && r.Method == http.MethodGet:
			// GetBucketPolicy 返回原始策略 JSON（非 XML）
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"Version":"2012-10-17"}`)
		case op == "policy" && r.Method == http.MethodPut:
			ok2xx()
		case op == "policy":
			noContent()
		case op == "tagging" && r.Method == http.MethodGet:
			io.WriteString(w, accTaggingXML)
		case op == "tagging" && r.Method == http.MethodPut:
			ok2xx()
		case op == "tagging":
			noContent()
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

// accBucketsOp 从请求推断桶操作键（list/location/versioning/bucket）。
func accBucketsOp(r *http.Request) string {
	q := r.URL.Query()
	switch {
	case q.Has("location"):
		return "location"
	case q.Has("versioning"):
		return "versioning"
	case r.URL.Path == "/" || r.URL.Path == "":
		return "list" // ListBuckets
	default:
		return "bucket" // PUT/DELETE/HEAD /{name}
	}
}

// accBucketsFake 通用桶操作假 S3：列桶/建桶/删桶/位置/版本控制/HeadBucket。
func accBucketsFake(inj *accFailInjector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := accBucketsOp(r)
		if inj.take(w, r.Method, op) {
			return
		}
		switch {
		case op == "list" && r.Method == http.MethodGet:
			io.WriteString(w, accListBucketsXML)
		case op == "location" && r.Method == http.MethodGet:
			io.WriteString(w, `<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-west-2</LocationConstraint>`)
		case op == "versioning" && r.Method == http.MethodGet:
			io.WriteString(w, `<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"/>`)
		case op == "versioning" && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusOK)
		case op == "bucket" && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusOK)
		case op == "bucket" && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case op == "bucket" && r.Method == http.MethodHead:
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

// ---- 测试环境组装 ----

// accEnv 一套被测 Handler 环境（store + 完整路由 + 裸 Handler + 账号）。
type accEnv struct {
	t   *testing.T
	st  store.AccountStore
	hnd *Handler     // 裸 Handler（白盒调用内部方法）
	h   http.Handler // 完整路由栈（黑盒请求）
	acc *model.Account
}

// accDoRec 以 application/json 发一次请求并返回 recorder。
func (e *accEnv) accDoRec(method, path, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	return doJSON(e.t, e.h, method, path, body)
}

// accNewEnv 基于真实 store 与指向 fakeURL 的账号构建环境；bucket 为默认桶（可为空）。
func accNewEnv(t *testing.T, fakeURL, bucket string) *accEnv {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	acc, err := st.Create(&model.Account{
		Name: "acc-fake", Endpoint: fakeURL, Region: "us-east-1",
		AccessKey: "ak", SecretKey: "sk", Bucket: bucket, PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	hnd := accNewHandler(t, st, nil, "")
	return &accEnv{t: t, st: st, hnd: hnd, h: hnd.Routes(), acc: acc}
}

// accNewHandler 用给定 store/cors/token 构造裸 Handler（静态目录为空临时目录）。
func accNewHandler(t *testing.T, st store.AccountStore, cors []string, token string) *Handler {
	t.Helper()
	return New(st, accDiscardLogger(), t.TempDir(), cors, token, "test")
}

// accDiscardLogger 丢弃输出的 slog（避免测试刷屏）。
func accDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// accStartFake 启动假 S3 并在测试结束时自动关闭。
func accStartFake(t *testing.T, hf http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(hf)
	t.Cleanup(srv.Close)
	return srv
}
