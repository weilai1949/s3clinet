package handler

// ol_migrate_test.go —— migrate.go 请求校验与 migrate_exec.go JSON 组装补测。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/weilai1949/s3clinet/server/internal/model"
)

// olMigrateBody 组装迁移请求体。
func olMigrateBody(srcID, srcBucket, dstID, dstBucket string, keys []string) string {
	b, _ := json.Marshal(map[string]any{
		"sourceAccountId": srcID, "sourceBucket": srcBucket,
		"sourceKeys": keys, "targetAccountId": dstID, "targetBucket": dstBucket,
	})
	return string(b)
}

// olCreateAccount 直接落库创建账号（绕过 handler 层校验）。
func olCreateAccount(t *testing.T, env *accEnv, name, endpoint, accessKey, bucket string) *model.Account {
	t.Helper()
	acc, err := env.st.Create(&model.Account{
		Name: name, Endpoint: endpoint, Region: "us-east-1",
		AccessKey: accessKey, SecretKey: "sk", Bucket: bucket, PathStyle: true,
	})
	if err != nil {
		t.Fatalf("create account %s: %v", name, err)
	}
	return acc
}

// TestOlMigrateParseErrors parseMigrateRequest 各校验分支。
func TestOlMigrateParseErrors(t *testing.T) {
	srv := olFake(t, func(r *http.Request) olResp { return olPlain(http.StatusOK) })
	env := accNewEnv(t, srv.URL, "b")
	h := env.h

	// 非法 JSON → 400
	rr := doJSON(t, h, "POST", "/api/migrate", "{bad")
	olExpectStatus(t, rr, http.StatusBadRequest, "bad json")

	// 缺账号 ID → 400
	rr = doJSON(t, h, "POST", "/api/migrate", `{"sourceKeys":["a"]}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "no ids")

	// 空 keys → 400
	rr = doJSON(t, h, "POST", "/api/migrate", `{"sourceAccountId":"s","targetAccountId":"d","sourceKeys":[]}`)
	olExpectStatus(t, rr, http.StatusBadRequest, "no keys")

	// 超过 10000 keys → 400
	keys := make([]string, 10001)
	for i := range keys {
		keys[i] = fmt.Sprintf("k%d", i)
	}
	rr = doJSON(t, h, "POST", "/api/migrate", olMigrateBody("s", "b", "d", "b", keys))
	olExpectStatus(t, rr, http.StatusBadRequest, "too many keys")

	// 源账号不存在 → 404
	rr = doJSON(t, h, "POST", "/api/migrate", olMigrateBody("ghost", "b", "d", "b", []string{"a"}))
	olExpectStatus(t, rr, http.StatusNotFound, "source not found")

	// 目标账号不存在 → 404
	rr = doJSON(t, h, "POST", "/api/migrate", olMigrateBody(env.acc.ID, "b", "ghost", "b", []string{"a"}))
	olExpectStatus(t, rr, http.StatusNotFound, "target not found")

	// 源账号配置非法（缺 AccessKey）→ 400
	badSrc := olCreateAccount(t, env, "badsrc", srv.URL, "", "b")
	rr = doJSON(t, h, "POST", "/api/migrate", olMigrateBody(badSrc.ID, "b", env.acc.ID, "b", []string{"a"}))
	olExpectStatus(t, rr, http.StatusBadRequest, "invalid source config")

	// 目标账号配置非法 → 400
	badDst := olCreateAccount(t, env, "baddst", srv.URL, "", "b")
	rr = doJSON(t, h, "POST", "/api/migrate", olMigrateBody(env.acc.ID, "b", badDst.ID, "b", []string{"a"}))
	olExpectStatus(t, rr, http.StatusBadRequest, "invalid target config")

	// 源桶缺失（账号无默认桶且未传 sourceBucket）→ 400
	noBkt := olCreateAccount(t, env, "nosrcb", srv.URL, "ak", "")
	rr = doJSON(t, h, "POST", "/api/migrate", olMigrateBody(noBkt.ID, "", env.acc.ID, "b", []string{"a"}))
	olExpectStatus(t, rr, http.StatusBadRequest, "no source bucket")

	// 目标桶缺失 → 400
	rr = doJSON(t, h, "POST", "/api/migrate", olMigrateBody(env.acc.ID, "b", noBkt.ID, "", []string{"a"}))
	olExpectStatus(t, rr, http.StatusBadRequest, "no target bucket")
}

// TestOlMigrateResultJSON migrateResultJSON：lastError / failedKeys 装配。
func TestOlMigrateResultJSON(t *testing.T) {
	m := migrateResultJSON(migrateResult{Migrated: 3, Failed: 1, LastError: "boom", FailKeys: []string{"k"}})
	if m["migrated"] != 3 || m["failed"] != 1 || m["lastError"] != "boom" {
		t.Fatalf("migrateResultJSON = %v", m)
	}
	if fk, ok := m["failedKeys"].([]string); !ok || len(fk) != 1 || fk[0] != "k" {
		t.Fatalf("failedKeys = %v", m["failedKeys"])
	}
	// 空结果：不带 lastError/failedKeys 键
	m2 := migrateResultJSON(migrateResult{Migrated: 1})
	if _, has := m2["lastError"]; has {
		t.Fatal("unexpected lastError")
	}
	if _, has := m2["failedKeys"]; has {
		t.Fatal("unexpected failedKeys")
	}
}
