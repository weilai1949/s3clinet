package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weilai1949/s3clinet/server/internal/model"
)

func gapAcc(name string) *model.Account {
	return &model.Account{Name: name, Endpoint: "http://127.0.0.1:1", Region: "us-east-1", AccessKey: "ak", SecretKey: "sk", PathStyle: true}
}

// nukeParentDir 移除 store 文件所在目录，使后续 persistLocked 的 tmp 创建/rename 必然失败。
func nukeParentDir(t *testing.T, path string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove parent dir: %v", err)
	}
}

// ---- json Store：加载异常 + 持久化失败回滚 ----

func TestGapJSONNewParseError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "accounts.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(p); err == nil || !strings.Contains(err.Error(), "parse account file") {
		t.Fatalf("New(garbage) = %v", err)
	}
}

func TestGapJSONNewEmptyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "accounts.json")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := New(p)
	if err != nil {
		t.Fatalf("New(empty) = %v", err)
	}
	list, err := st.List()
	if err != nil || len(list) != 0 {
		t.Fatalf("empty store list = %v %v", list, err)
	}
}

func TestGapJSONNewPathIsDir(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "as-dir")
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := New(p); err == nil || !strings.Contains(err.Error(), "read account file") {
		t.Fatalf("New(dir path) = %v", err)
	}
}

// TestGapJSONPersistFailureRollback 持久化失败时内存状态必须回滚（Create/Update/Delete 三路）。
func TestGapJSONPersistFailureRollback(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	st, err := New(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.Create(gapAcc("ok"))
	if err != nil {
		t.Fatalf("baseline create: %v", err)
	}
	nukeParentDir(t, filepath.Join(dir, "accounts.json"))

	// Create 失败 → 回滚
	if _, err := st.Create(gapAcc("doomed")); err == nil {
		t.Fatal("expected persist failure")
	}
	list, _ := st.List()
	if len(list) != 1 {
		t.Fatalf("after failed create list = %d, want 1", len(list))
	}
	// Update 失败 → 返回错误
	upd := gapAcc("renamed")
	if _, err := st.Update(a.ID, upd); err == nil {
		t.Fatal("expected update persist failure")
	}
	// Delete 失败 → 返回错误
	if err := st.Delete(a.ID); err == nil {
		t.Fatal("expected delete persist failure")
	}
	if list, _ := st.List(); len(list) != 1 {
		t.Fatalf("delete rollback failed, list = %d", len(list))
	}
}

func TestGapJSONUpdateMaskedKeepsSecret(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.Create(gapAcc("m"))
	if err != nil {
		t.Fatal(err)
	}
	upd := gapAcc("m2")
	upd.SecretKey = model.MaskedSecret
	got, err := st.Update(a.ID, upd)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.SecretKey != model.MaskedSecret {
		t.Fatalf("masked update should keep secret shape, got %q", got.SecretKey)
	}
}

func TestGapJSONClosePing(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	if err := st.Ping(); err != nil {
		t.Fatalf("Ping = %v", err)
	}
}

// ---- encrypted：错误信封 + 全生命周期 + 持久化失败回滚 ----

func TestGapEncryptedNewErrors(t *testing.T) {
	if _, err := NewEncrypted(filepath.Join(t.TempDir(), "a.enc"), ""); err == nil {
		t.Fatal("empty store key must be rejected")
	}
	// 路径是目录 → 读取失败（非 NotExist）
	dir := filepath.Join(t.TempDir(), "enc")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := NewEncrypted(dir, "pw"); err == nil || !strings.Contains(err.Error(), "read encrypted account file") {
		t.Fatalf("NewEncrypted(dir) = %v", err)
	}
}

// writeEnvelope 手工构造 S3C2 信封（salt + AES-GCM(plain)）。
func writeEnvelope(t *testing.T, path, password string, plain []byte, corruptSalt bool) {
	t.Helper()
	salt := []byte("0123456789abcdef")
	key := deriveKey(password, salt)
	blob, err := encryptAESGCM(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	data := append(append([]byte("S3C2"), salt...), blob...)
	if corruptSalt {
		data[5] ^= 0xff // 破坏盐 → 密钥不同 → 认证失败
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestGapEncryptedLoadBranches(t *testing.T) {
	dir := t.TempDir()
	// 过短文件
	short := filepath.Join(dir, "short.enc")
	if err := os.WriteFile(short, []byte("S3C2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewEncrypted(short, "pw"); err == nil || !strings.Contains(err.Error(), "too short") {
		t.Fatalf("short file = %v", err)
	}
	// 错误 magic
	bad := filepath.Join(dir, "bad.enc")
	if err := os.WriteFile(bad, append([]byte("XXXX"), make([]byte, 32)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewEncrypted(bad, "pw"); err == nil || !strings.Contains(err.Error(), "not S3C2") {
		t.Fatalf("bad magic = %v", err)
	}
	// 盐被篡改 → 解密认证失败
	tampered := filepath.Join(dir, "t.enc")
	writeEnvelope(t, tampered, "pw", []byte(`[]`), true)
	if _, err := NewEncrypted(tampered, "pw"); err == nil || !strings.Contains(err.Error(), "decrypt") {
		t.Fatalf("tampered = %v", err)
	}
	// 信封合法但内部非 JSON
	junk := filepath.Join(dir, "j.enc")
	writeEnvelope(t, junk, "pw", []byte("not-json"), false)
	if _, err := NewEncrypted(junk, "pw"); err == nil || !strings.Contains(err.Error(), "parse account file") {
		t.Fatalf("junk json = %v", err)
	}
}

func TestGapEncryptedLifecycleAndRollback(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "accounts.json.enc")
	st, err := NewEncrypted(path, "pw-very-long")
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.Create(gapAcc("enc"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// 幂等重开：数据能读回
	st2, err := NewEncrypted(path, "pw-very-long")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	list, err := st2.List()
	if err != nil || len(list) != 1 || list[0].Name != "enc" {
		t.Fatalf("reopen list = %v %v", list, err)
	}
	// 掩码更新不覆盖密钥
	upd := gapAcc("enc2")
	upd.SecretKey = model.MaskedSecret
	if _, err := st2.Update(a.ID, upd); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got, _ := st2.Get(a.ID); got.Name != "enc2" {
		t.Fatalf("get after update = %+v", got)
	}
	// Update 不存在 → ErrNotFound
	if _, err := st2.Update("nope", upd); err != ErrNotFound {
		t.Fatalf("update missing = %v", err)
	}
	// 持久化失败回滚
	nukeParentDir(t, path)
	if _, err := st.Create(gapAcc("doomed")); err == nil {
		t.Fatal("expected persist failure")
	}
	if _, err := st2.Update("nope", upd); err == nil && err != ErrNotFound {
		t.Fatalf("update after nuke = %v", err)
	}
	if err := st.Delete(a.ID); err == nil {
		t.Fatal("expected delete persist failure after nuke")
	}
	// Close/Ping
	if err := st.Close(); err != nil {
		t.Fatalf("close = %v", err)
	}
	if err := st.Ping(); err != nil {
		t.Fatalf("ping = %v", err)
	}
}

func TestGapAESGCMEdge(t *testing.T) {
	// aes.NewCipher 仅在 key 长度非法（≠16/24/32）时报错——测试此边界。
	if _, err := encryptAESGCM([]byte("short"), []byte("x")); err == nil {
		t.Fatal("encrypt: 8-byte key must fail")
	}
	if _, err := decryptAESGCM([]byte("short"), []byte("xx")); err == nil {
		t.Fatal("decrypt: 8-byte key must fail")
	}
	key := deriveKey("pw", []byte("0123456789abcdef"))
	if _, err := decryptAESGCM(key, []byte("n")); err == nil || !strings.Contains(err.Error(), "too short") {
		t.Fatalf("short blob = %v", err)
	}
	// 完整加解密往返
	enc, err := encryptAESGCM(key, []byte("hello world"))
	if err != nil {
		t.Fatal(err)
	}
	dec, err := decryptAESGCM(key, enc)
	if err != nil || string(dec) != "hello world" {
		t.Fatalf("roundtrip = %q err %v", dec, err)
	}
}

// ---- sqlite：关库错误路径 + 迁移幂等 + openSQLite 失败 ----

func TestGapSQLiteClosedErrors(t *testing.T) {
	st, err := openSQLite(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Create(gapAcc("s1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.List(); err == nil {
		t.Fatal("List after close must error")
	}
	if _, err := st.Get("x"); err == nil || err == ErrNotFound {
		t.Fatalf("Get after close must error (not NotFound), got %v", err)
	}
	if _, err := st.Create(gapAcc("s2")); err == nil {
		t.Fatal("Create after close must error")
	}
	if _, err := st.Update("x", gapAcc("u")); err == nil {
		t.Fatal("Update after close must error")
	}
	if err := st.Delete("x"); err == nil {
		t.Fatal("Delete after close must error")
	}
	// Close 不置 nil，Ping 走 db.Ping() 报「database is closed」；nil 分支为防御性
	if err := st.Ping(); err == nil {
		t.Fatal("Ping after close must error")
	}
}

func TestGapSQLiteReopenIdempotentAndCRUD(t *testing.T) {
	dir := t.TempDir()
	st1, err := openSQLite(filepath.Join(dir, "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := st1.Create(gapAcc("s"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st1.Close(); err != nil {
		t.Fatal(err)
	}
	// 重开：migrate user_version 已 ≥1 → no-op 分支
	st2, err := openSQLite(filepath.Join(dir, "accounts.db"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	list, err := st2.List()
	if err != nil || len(list) != 1 || !list[0].PathStyle {
		t.Fatalf("reopen list = %v %v", list, err)
	}
	// Update 全字段 + 掩码保留
	upd := gapAcc("s2")
	upd.UseSSL = true
	upd.Bucket = "b"
	upd.PublicEndpoint = "https://p"
	upd.SecretKey = model.MaskedSecret
	if _, err := st2.Update(a.ID, upd); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := st2.Get(a.ID)
	if err != nil || got.Bucket != "b" || !got.UseSSL {
		t.Fatalf("get = %+v %v", got, err)
	}
	// Update 不存在 → ErrNotFound
	if _, err := st2.Update("nope", upd); err != ErrNotFound {
		t.Fatalf("update missing = %v", err)
	}
	// Get 不存在 → ErrNotFound
	if _, err := st2.Get("nope"); err != ErrNotFound {
		t.Fatalf("get missing = %v", err)
	}
	// Delete：先不存在再成功
	if err := st2.Delete("nope"); err != ErrNotFound {
		t.Fatalf("delete missing = %v", err)
	}
	if err := st2.Delete(a.ID); err != nil {
		t.Fatalf("delete = %v", err)
	}
}

func TestGapOpenSQLitePathIsDir(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "dbdir")
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := openSQLite(p); err == nil {
		t.Fatal("openSQLite on directory must fail (ping error)")
	}
}

func TestGapSQLiteBool(t *testing.T) {
	if sqliteBool(true) != 1 || sqliteBool(false) != 0 {
		t.Fatal("sqliteBool mapping wrong")
	}
}

// ---- open.go 驱动分发 ----

func TestGapOpenDispatch(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir, "sqlite", "")
	if err != nil {
		t.Fatalf("sqlite dispatch: %v", err)
	}
	if _, ok := st.(AccountStore); !ok {
		t.Fatal("sqlite should satisfy AccountStore")
	}
	if _, ok := st.(*SQLiteStore); !ok {
		t.Fatal("driver=sqlite should yield *SQLiteStore")
	}
	st.Close()
	if _, err := Open(dir, "encrypted", ""); err == nil {
		t.Fatal("encrypted without key must fail")
	}
	for _, d := range []string{"", "json", "JSON", "  json  ", "bogus-driver"} {
		s, err := Open(dir, d, "")
		if err != nil {
			t.Fatalf("Open(driver=%q) = %v", d, err)
		}
		if _, ok := s.(*Store); !ok {
			t.Fatalf("driver=%q should fall back to *Store, got %T", d, s)
		}
		_ = s.Close()
	}
}

// ---- persistLocked rename 失败注入（路径变成目录）+ 重复 ID ----

func swapPathToDir(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestGapJSONRenameFailureRollback(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "accounts.json")
	st, err := New(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Create(gapAcc("a")); err != nil {
		t.Fatal(err)
	}
	swapPathToDir(t, p) // rename 目标是目录 → rename 失败
	if _, err := st.Create(gapAcc("b")); err == nil {
		t.Fatal("expected rename failure")
	}
	list, _ := st.List()
	if len(list) != 1 {
		t.Fatalf("rollback after rename failure, list = %d", len(list))
	}
	// 重复 ID
	st2, err := New(filepath.Join(t.TempDir(), "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := st2.Create(gapAcc("dup"))
	dup := gapAcc("dup")
	dup.ID = a.ID
	if _, err := st2.Create(dup); err == nil {
		t.Fatal("duplicate id must fail")
	}
}

func TestGapEncryptedRenameFailureAndMaskedNoop(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "accounts.json.enc")
	st, err := NewEncrypted(path, "pw-long-enough")
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.Create(gapAcc("e"))
	if err != nil {
		t.Fatal(err)
	}
	swapPathToDir(t, path)
	if _, err := st.Create(gapAcc("x")); err == nil {
		t.Fatal("expected rename failure")
	}
	// 已存在 id 的 Update 在持久化失败时也要报错（非 NotFound）
	if _, err := st.Update(a.ID, gapAcc("e2")); err == nil {
		t.Fatal("expected update persist failure")
	}
	list, _ := st.List()
	if len(list) != 1 {
		t.Fatalf("rollback after rename failure, list = %d", len(list))
	}
}

func TestGapOpenMkdirAllFails(t *testing.T) {
	dir := t.TempDir()
	fileAsDir := filepath.Join(dir, "occupied")
	if err := os.WriteFile(fileAsDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(fileAsDir, "sub")
	if _, err := Open(dataDir, "json", ""); err == nil {
		t.Fatal("Open must fail when data dir cannot be created")
	}
}

// TestGapSQLiteTableDroppedErrors DROP TABLE 后各方法必须报错而非伪装成功/NotFound。
func TestGapSQLiteTableDroppedErrors(t *testing.T) {
	st, err := openSQLite(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`DROP TABLE accounts`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.List(); err == nil {
		t.Fatal("List must error after drop")
	}
	if _, err := st.Get("x"); err == nil || err == ErrNotFound {
		t.Fatalf("Get must error after drop, got %v", err)
	}
	if _, err := st.Create(gapAcc("x")); err == nil {
		t.Fatal("Create must error after drop")
	}
	if _, err := st.Update("x", gapAcc("y")); err == nil || err == ErrNotFound {
		t.Fatalf("Update must error after drop, got %v", err)
	}
	if err := st.Delete("x"); err == nil {
		t.Fatal("Delete must error after drop")
	}
}

// ---- 末批：NotFound 系、重复主键、NULL 扫描、MkdirAll 失败、空文件 ----

func TestGapJSONNotFoundBranches(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Update("nope", gapAcc("u")); err != ErrNotFound {
		t.Fatalf("update missing = %v", err)
	}
	if err := st.Delete("nope"); err != ErrNotFound {
		t.Fatalf("delete missing = %v", err)
	}
	if _, err := st.Get("nope"); err != ErrNotFound {
		t.Fatalf("get missing = %v", err)
	}
}

func TestGapEncryptedNotFoundAndDup(t *testing.T) {
	dir := t.TempDir()
	st, err := NewEncrypted(filepath.Join(dir, "a.enc"), "pw-long")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get("nope"); err != ErrNotFound {
		t.Fatalf("get missing = %v", err)
	}
	if _, err := st.Update("nope", gapAcc("u")); err != ErrNotFound {
		t.Fatalf("update missing = %v", err)
	}
	if err := st.Delete("nope"); err != ErrNotFound {
		t.Fatalf("delete missing = %v", err)
	}
	a, err := st.Create(gapAcc("d"))
	if err != nil {
		t.Fatal(err)
	}
	dup := gapAcc("d")
	dup.ID = a.ID
	if _, err := st.Create(dup); err == nil {
		t.Fatal("duplicate id must fail")
	}
}

func TestGapEncryptedEmptyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.enc")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := NewEncrypted(p, "pw-long")
	if err != nil {
		t.Fatalf("empty encrypted file should init fresh: %v", err)
	}
	list, _ := st.List()
	if len(list) != 0 {
		t.Fatalf("empty file list = %d", len(list))
	}
}

func TestGapNewMkdirAllFails(t *testing.T) {
	dir := t.TempDir()
	occupied := filepath.Join(dir, "occupied")
	if err := os.WriteFile(occupied, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(filepath.Join(occupied, "sub", "accounts.json")); err == nil {
		t.Fatal("New must fail when data dir cannot be created")
	}
}

func TestGapOpenSQLiteMkdirAllFails(t *testing.T) {
	dir := t.TempDir()
	occupied := filepath.Join(dir, "occupied")
	if err := os.WriteFile(occupied, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openSQLite(filepath.Join(occupied, "sub", "accounts.db")); err == nil {
		t.Fatal("openSQLite must fail when dir cannot be created")
	}
}

func TestGapSQLiteDuplicateIDConstraint(t *testing.T) {
	st, err := openSQLite(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a, err := st.Create(gapAcc("one"))
	if err != nil {
		t.Fatal(err)
	}
	dup := gapAcc("one")
	dup.ID = a.ID
	if _, err := st.Create(dup); err == nil {
		t.Fatal("duplicate primary key must fail")
	}
	// 第二次正常 Create：nextSortOrder 走 max.Valid 分支
	if _, err := st.Create(gapAcc("two")); err != nil {
		t.Fatalf("second create = %v", err)
	}
}

func TestGapSQLiteScanNullRow(t *testing.T) {
	st, err := openSQLite(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// path_style 存 BLOB：BLOB→int 转换失败 → 扫描错误路径
	if _, err := st.db.Exec(`INSERT INTO accounts (id, name, endpoint, public_endpoint, region, access_key, secret_key, bucket, path_style, use_ssl, created_at, updated_at, sort_order)
		VALUES ('blobrow', 'n', 'e', '', 'r', 'ak', 'sk', 'b', X'FF', 1, '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z', 1)`); err != nil {
		t.Fatalf("insert blob row: %v", err)
	}
	if _, err := st.List(); err == nil {
		t.Fatal("blob created_at row must cause scan error")
	}
}

// TestAtomicWriteFile 通过注入 OS 操作覆盖原子写全部分支。
// 真实 OS 上无法触发「写一半失败 / close 报错 / rename 失败」等错误路径，
// 借助包级钩子在测试里精确制造这些场景。
func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()

	// 备份并恢复注入点
	origOpen, origWrite, origClose, origRename, origRemove :=
		atomicOpenTmp, atomicWrite, atomicClose, atomicRename, atomicRemove
	t.Cleanup(func() {
		atomicOpenTmp, atomicWrite, atomicClose, atomicRename, atomicRemove =
			origOpen, origWrite, origClose, origRename, origRemove
	})

	t.Run("happy", func(t *testing.T) {
		p := filepath.Join(dir, "happy.bin")
		if err := atomicWriteFile(p, []byte("hello")); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != "hello" {
			t.Fatalf("got %q", got)
		}
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("perm = %v err %v", fi.Mode().Perm(), err)
		}
	})

	t.Run("open fail", func(t *testing.T) {
		atomicOpenTmp = func(string) (*os.File, error) { return nil, errors.New("boom") }
		if err := atomicWriteFile(filepath.Join(dir, "x"), []byte("a")); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("write fail", func(t *testing.T) {
		atomicOpenTmp = origOpen
		atomicWrite = func(*os.File, []byte) (int, error) { return 0, errors.New("disk full") }
		atomicRemove = origRemove
		p := filepath.Join(dir, "wf.bin")
		if err := atomicWriteFile(p, []byte("x")); err == nil {
			t.Fatal("expected write error")
		}
		if _, err := os.Stat(p + ".tmp"); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("tmp should be cleaned, stat err = %v", err)
		}
	})

	t.Run("short write", func(t *testing.T) {
		atomicOpenTmp = origOpen
		atomicWrite = func(_ *os.File, data []byte) (int, error) { return len(data) - 1, nil }
		if err := atomicWriteFile(filepath.Join(dir, "sw.bin"), []byte("xyz")); err == nil {
			t.Fatal("expected short write")
		}
	})

	t.Run("close fail", func(t *testing.T) {
		atomicOpenTmp = origOpen
		atomicWrite = origWrite
		atomicClose = func(*os.File) error { return errors.New("close err") }
		if err := atomicWriteFile(filepath.Join(dir, "cf.bin"), []byte("x")); err == nil {
			t.Fatal("expected close error")
		}
		if _, err := os.Stat(filepath.Join(dir, "cf.bin.tmp")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("tmp should be cleaned, stat err = %v", err)
		}
	})

	t.Run("rename fail", func(t *testing.T) {
		atomicOpenTmp = origOpen
		atomicWrite = origWrite
		atomicClose = origClose
		atomicRename = func(_, _ string) error { return errors.New("rename fail") }
		if err := atomicWriteFile(filepath.Join(dir, "rf.bin"), []byte("x")); err == nil {
			t.Fatal("expected rename error")
		}
		if _, err := os.Stat(filepath.Join(dir, "rf.bin.tmp")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("tmp should be cleaned, stat err = %v", err)
		}
	})
}

// TestGapEncryptedDeleteSuccess 正常 Delete 走通（既有的 Delete 测试都建立在 nuke 之上，
// 覆盖的是「持久化失败」分支；此处补正常成功路径）。
func TestGapEncryptedDeleteSuccess(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := NewEncrypted(filepath.Join(dir, "a.enc"), "pw-very-long")
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.Create(gapAcc("to-delete"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Delete(a.ID); err != nil {
		t.Fatalf("Delete success: %v", err)
	}
	if _, err := st.Get(a.ID); err != ErrNotFound {
		t.Fatalf("Get after Delete = %v, want NotFound", err)
	}
	list, _ := st.List()
	if len(list) != 0 {
		t.Fatalf("list after delete = %d", len(list))
	}
}

// TestGapSQLiteUpdatePreservesSecretKey 显式设置非掩码 SecretKey 时应原样保留。
func TestGapSQLiteUpdatePreservesSecretKey(t *testing.T) {
	st, err := openSQLite(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a, err := st.Create(gapAcc("u"))
	if err != nil {
		t.Fatal(err)
	}
	upd := gapAcc("u2")
	upd.SecretKey = "new-secret-value"
	if _, err := st.Update(a.ID, upd); err != nil {
		t.Fatal(err)
	}
	raw, err := st.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if raw.SecretKey != "new-secret-value" {
		t.Fatalf("SecretKey persisted = %q, want new-secret-value", raw.SecretKey)
	}
}

// TestGapEncryptedUpdateRollbackOnPersistFail 持久化失败时内存须回滚到原值，
// 避免「磁盘旧值、内存新值」的读写漂移（与 JSON Store Update 行为对齐）。
func TestGapEncryptedUpdateRollbackOnPersistFail(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := NewEncrypted(filepath.Join(dir, "a.enc"), "pw-very-long-123")
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.Create(gapAcc("orig"))
	if err != nil {
		t.Fatal(err)
	}

	// 备份并恢复注入点
	origRename := atomicRename
	t.Cleanup(func() { atomicRename = origRename })
	atomicRename = func(_, _ string) error { return errors.New("rename fail") }

	upd := gapAcc("updated")
	if _, err := st.Update(a.ID, upd); err == nil {
		t.Fatal("Update should fail when persist fails")
	}

	// 内存应回滚：原值仍可读出
	raw, err := st.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Name != "orig" {
		t.Fatalf("after failed Update, Name = %q, want orig (in-memory rollback)", raw.Name)
	}
}

// TestGapJSONUpdateRollbackOnPersistFail 与 EncryptedStore 行为对齐：JSON Store
// 原本已支持回滚，此处显式补测以防回归。
func TestGapJSONUpdateRollbackOnPersistFail(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := New(filepath.Join(dir, "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.Create(gapAcc("orig"))
	if err != nil {
		t.Fatal(err)
	}

	origRename := atomicRename
	t.Cleanup(func() { atomicRename = origRename })
	atomicRename = func(_, _ string) error { return errors.New("rename fail") }

	upd := gapAcc("updated")
	if _, err := st.Update(a.ID, upd); err == nil {
		t.Fatal("Update should fail when persist fails")
	}

	raw, err := st.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Name != "orig" {
		t.Fatalf("after failed Update, Name = %q, want orig (in-memory rollback)", raw.Name)
	}
}
