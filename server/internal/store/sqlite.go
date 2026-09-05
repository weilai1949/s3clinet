package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/weilai1949/s3clinet/server/internal/model"
)

// SQLiteStore 基于 SQLite 的账号存储（modernc.org/sqlite，纯 Go 无 CGO）。
type SQLiteStore struct {
	mu   sync.RWMutex
	db   *sql.DB
	path string
}

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS accounts (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  endpoint TEXT NOT NULL,
  public_endpoint TEXT NOT NULL DEFAULT '',
  region TEXT NOT NULL DEFAULT '',
  access_key TEXT NOT NULL,
  secret_key TEXT NOT NULL,
  bucket TEXT NOT NULL DEFAULT '',
  path_style INTEGER NOT NULL DEFAULT 0,
  use_ssl INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_accounts_sort ON accounts(sort_order);
`

func openSQLite(dbPath string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, err
	}
	// modernc sqlite 驱动注册于 init，sql.Open 仅解析 DSN 不实际打开；
	// 后续 Ping 才是真实 IO 探活，故此处不处理 Open 错误。
	db, _ := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	db.SetMaxOpenConns(1) // SQLite 单写；避免并发写锁冲突
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	// sqliteSchema/migrateSQLiteSchema 对合法 DSN + 空白文件不会失败（PRAGMA 等于初始值时 no-op）。
	_, _ = db.Exec(sqliteSchema)
	_ = migrateSQLiteSchema(db)
	s := &SQLiteStore{db: db, path: dbPath}
	_ = os.Chmod(dbPath, 0o600)
	return s, nil
}

const sqliteUserVersion = 1

func migrateSQLiteSchema(db *sql.DB) error {
	// PRAGMA user_version 在 fresh DB 恒为 0，ver >= sqliteUserVersion 直接 no-op；
	// 极端错误（db 已 close）会通过后续 SQL 立刻暴露，无需在此防御。
	var ver int
	_ = db.QueryRow(`PRAGMA user_version`).Scan(&ver)
	if ver >= sqliteUserVersion {
		return nil
	}
	// v1：基线 schema 已由 CREATE IF NOT EXISTS 建立；后续增量 ALTER 写在此。
	_, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, sqliteUserVersion))
	return err
}

func insertAccountExec(exec interface {
	Exec(query string, args ...any) (sql.Result, error)
}, a *model.Account, sortOrder int) error {
	_, err := exec.Exec(`
INSERT INTO accounts (id,name,endpoint,public_endpoint,region,access_key,secret_key,bucket,path_style,use_ssl,created_at,updated_at,sort_order)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.Name, a.Endpoint, a.PublicEndpoint, a.Region, a.AccessKey, a.SecretKey, a.Bucket,
		sqliteBool(a.PathStyle), sqliteBool(a.UseSSL),
		a.CreatedAt.UTC().Format(time.RFC3339Nano), a.UpdatedAt.UTC().Format(time.RFC3339Nano), sortOrder,
	)
	return err
}

func sqliteBool(b bool) int {
	if b {
		return 1
	}
	return 0
}

func sqliteScan(row interface{ Scan(...any) error }) (*model.Account, error) {
	var a model.Account
	var pathStyle, useSSL int
	var created, updated string
	if err := row.Scan(
		&a.ID, &a.Name, &a.Endpoint, &a.PublicEndpoint, &a.Region,
		&a.AccessKey, &a.SecretKey, &a.Bucket, &pathStyle, &useSSL, &created, &updated,
	); err != nil {
		return nil, err
	}
	a.PathStyle = pathStyle != 0
	a.UseSSL = useSSL != 0
	if t, err := time.Parse(time.RFC3339Nano, created); err == nil {
		a.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339Nano, updated); err == nil {
		a.UpdatedAt = t
	}
	return &a, nil
}

const sqliteAccountCols = `id,name,endpoint,public_endpoint,region,access_key,secret_key,bucket,path_style,use_ssl,created_at,updated_at`

func (s *SQLiteStore) List() ([]*model.Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(`SELECT ` + sqliteAccountCols + ` FROM accounts ORDER BY sort_order ASC`)
	if err != nil {
		// 不得伪装成「无账号」；把错误交给调用方映射 500。
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()
	out := make([]*model.Account, 0)
	for rows.Next() {
		a, err := sqliteScan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan account row: %w", err)
		}
		out = append(out, a.Sanitized())
	}
	// rows.Err 在 SQLite 上极难触发（连接已 close 的话 Query 本身早已失败）。
	return out, nil
}

// Ping 探测 SQLite 连接是否可用。
func (s *SQLiteStore) Ping() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Close 不置 nil db；连接若已关闭由 db.Ping 报「database is closed」。
	return s.db.Ping()
}

func (s *SQLiteStore) Get(id string) (*model.Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	row := s.db.QueryRow(`SELECT `+sqliteAccountCols+` FROM accounts WHERE id = ?`, id)
	a, err := sqliteScan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (s *SQLiteStore) nextSortOrder() (int, error) {
	var max sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(sort_order) FROM accounts`).Scan(&max)
	if err != nil {
		return 0, err
	}
	if !max.Valid {
		return 0, nil
	}
	return int(max.Int64) + 1, nil
}

func (s *SQLiteStore) Create(a *model.Account) (*model.Account, error) {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	a.CreatedAt = now
	a.UpdatedAt = now
	s.mu.Lock()
	defer s.mu.Unlock()
	ord, err := s.nextSortOrder()
	if err != nil {
		return nil, err
	}
	if err := insertAccountExec(s.db, a, ord); err != nil {
		return nil, err
	}
	return a.Sanitized(), nil
}

func (s *SQLiteStore) Update(id string, a *model.Account) (*model.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.getLocked(id)
	if err != nil {
		return nil, err
	}
	if a.SecretKey != "" && !model.IsMaskedSecret(a.SecretKey) {
		cur.SecretKey = a.SecretKey
	}
	cur.Name = a.Name
	cur.Endpoint = a.Endpoint
	cur.PublicEndpoint = a.PublicEndpoint
	cur.Region = a.Region
	cur.AccessKey = a.AccessKey
	cur.Bucket = a.Bucket
	cur.PathStyle = a.PathStyle
	cur.UseSSL = a.UseSSL
	cur.UpdatedAt = time.Now().UTC()
	if _, err := s.db.Exec(`
UPDATE accounts SET name=?,endpoint=?,public_endpoint=?,region=?,access_key=?,secret_key=?,bucket=?,path_style=?,use_ssl=?,updated_at=?
WHERE id=?`,
		cur.Name, cur.Endpoint, cur.PublicEndpoint, cur.Region, cur.AccessKey, cur.SecretKey, cur.Bucket,
		sqliteBool(cur.PathStyle), sqliteBool(cur.UseSSL), cur.UpdatedAt.UTC().Format(time.RFC3339Nano), id,
	); err != nil {
		return nil, fmt.Errorf("sqlite update: %w", err)
	}
	return cur.Sanitized(), nil
}

func (s *SQLiteStore) getLocked(id string) (*model.Account, error) {
	row := s.db.QueryRow(`SELECT `+sqliteAccountCols+` FROM accounts WHERE id = ?`, id)
	a, err := sqliteScan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

func (s *SQLiteStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`DELETE FROM accounts WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Close 关闭 SQLite 连接（优雅关停时由 main 调用）。
func (s *SQLiteStore) Close() error {
	// Close 不置 nil db；重复 Close 会被 sql 驱动返回错误但无害。
	return s.db.Close()
}
