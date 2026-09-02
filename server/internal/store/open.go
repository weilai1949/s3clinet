package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Open 按 driver 打开账号存储。
//   - json（默认）：明文 accounts.json（0600）
//   - sqlite：SQLite accounts.db（纯 Go modernc driver）
//   - encrypted：AES-256-GCM 加密 accounts.json.enc（需 S3C_STORE_KEY）
func Open(dataDir, driver, storeKey string) (AccountStore, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "sqlite":
		return openSQLite(filepath.Join(dataDir, "accounts.db"))
	case "encrypted":
		return NewEncrypted(filepath.Join(dataDir, "accounts.json.enc"), storeKey)
	default:
		return New(filepath.Join(dataDir, "accounts.json"))
	}
}
