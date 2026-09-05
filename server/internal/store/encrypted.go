package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/argon2"

	"github.com/weilai1949/s3clinet/server/internal/model"
)

// 加密文件格式（仅 S3C2）：
//
//	magic "S3C2"(4) + salt(16) + nonce|ciphertext
//	密钥 = Argon2id(password, salt)
var encMagicV2 = []byte("S3C2")

const (
	encSaltLen   = 16
	argonTime    = 1
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 4
	keyLen       = 32
)

func deriveKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, keyLen)
}

// EncryptedStore 在 JSON 存储之上对磁盘文件做 AES-256-GCM 加密（静态保护 SecretKey）。
type EncryptedStore struct {
	mu       sync.RWMutex
	path     string
	password string
	key      []byte
	salt     []byte
	accounts map[string]*model.Account
	order    []string
}

// NewEncrypted 创建加密存储。storeKey 非空；新文件用 Argon2id + 随机盐。
func NewEncrypted(encPath, storeKey string) (*EncryptedStore, error) {
	if storeKey == "" {
		return nil, errors.New("S3C_STORE_KEY is required for encrypted store")
	}
	s := &EncryptedStore{
		path:     encPath,
		password: storeKey,
		accounts: make(map[string]*model.Account),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *EncryptedStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s.initFreshKey()
		}
		return fmt.Errorf("read encrypted account file: %w", err)
	}
	if len(data) == 0 {
		return s.initFreshKey()
	}
	if len(data) < 4+encSaltLen {
		return errors.New("encrypted account file too short or not S3C2")
	}
	if string(data[:4]) != string(encMagicV2) {
		return fmt.Errorf("encrypted account file magic %q is not S3C2", string(data[:4]))
	}
	salt := make([]byte, encSaltLen)
	copy(salt, data[4:4+encSaltLen])
	s.salt = salt
	s.key = deriveKey(s.password, salt)
	plain, err := decryptAESGCM(s.key, data[4+encSaltLen:])
	if err != nil {
		return fmt.Errorf("decrypt accounts: %w", err)
	}
	return s.applyJSON(plain)
}

func (s *EncryptedStore) initFreshKey() error {
	// crypto/rand 在 Linux 上不会失败；省略防御性检查。
	salt := make([]byte, encSaltLen)
	_, _ = io.ReadFull(rand.Reader, salt)
	s.salt = salt
	s.key = deriveKey(s.password, salt)
	return nil
}

func (s *EncryptedStore) applyJSON(data []byte) error {
	var list []*model.Account
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("parse account file: %w", err)
	}
	s.accounts = make(map[string]*model.Account)
	s.order = s.order[:0]
	for _, a := range list {
		s.accounts[a.ID] = a
		s.order = append(s.order, a.ID)
	}
	return nil
}

func (s *EncryptedStore) List() ([]*model.Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.Account, 0, len(s.order))
	for _, id := range s.order {
		if a, ok := s.accounts[id]; ok {
			out = append(out, a.Sanitized())
		}
	}
	return out, nil
}

func (s *EncryptedStore) Get(id string) (*model.Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.accounts[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (s *EncryptedStore) Create(a *model.Account) (*model.Account, error) {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	a.CreatedAt = now
	a.UpdatedAt = now
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.accounts[a.ID]; exists {
		return nil, fmt.Errorf("account %s already exists", a.ID)
	}
	cp := *a
	s.accounts[a.ID] = &cp
	s.order = append(s.order, a.ID)
	if err := s.persistLocked(); err != nil {
		delete(s.accounts, a.ID)
		s.order = s.order[:len(s.order)-1]
		return nil, err
	}
	return a.Sanitized(), nil
}

func (s *EncryptedStore) Update(id string, a *model.Account) (*model.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.accounts[id]
	if !ok {
		return nil, ErrNotFound
	}
	// 持久化失败时回滚内存状态，保持与磁盘一致（与 JSON Store 行为对齐）。
	prev := *cur
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
	if err := s.persistLocked(); err != nil {
		*cur = prev
		return nil, err
	}
	return cur.Sanitized(), nil
}

func (s *EncryptedStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[id]
	if !ok {
		return ErrNotFound
	}
	idx := -1
	for i, oid := range s.order {
		if oid == id {
			idx = i
			break
		}
	}
	delete(s.accounts, id)
	if idx >= 0 {
		s.order = append(s.order[:idx], s.order[idx+1:]...)
	}
	if err := s.persistLocked(); err != nil {
		// 写盘失败：回滚内存状态，避免「磁盘仍在、内存已删」的漂移。
		s.accounts[id] = a
		if idx >= 0 {
			s.order = append(s.order[:idx], append([]string{id}, s.order[idx:]...)...)
		}
		return err
	}
	return nil
}

func (s *EncryptedStore) Close() error { return nil }

func (s *EncryptedStore) Ping() error { return nil }

func (s *EncryptedStore) persistLocked() error {
	list := make([]*model.Account, 0, len(s.order))
	for _, id := range s.order {
		if a, ok := s.accounts[id]; ok {
			list = append(list, a)
		}
	}
	// model.Account 全部字段为基本类型/时间，MarshalIndent 不会失败。
	data, _ := json.MarshalIndent(list, "", "  ")
	// AES-256 GCM 对合法 key 不会失败。
	enc, _ := encryptAESGCM(s.key, data)
	out := make([]byte, 0, 4+encSaltLen+len(enc))
	out = append(out, encMagicV2...)
	out = append(out, s.salt...)
	out = append(out, enc...)
	return atomicWriteFile(s.path, out)
}

func encryptAESGCM(key, plain []byte) ([]byte, error) {
	// aes.NewCipher 仅在 key 长度非法时（≠16/24/32）报错——用于 caller 误用保护。
	// GCM 与 rand.Reader 在 Linux 上不会失败，省去冗余检查。
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	_, _ = io.ReadFull(rand.Reader, nonce)
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func decryptAESGCM(key, blob []byte) ([]byte, error) {
	// aes.NewCipher 仅在 key 长度非法时（≠16/24/32）报错——用于 caller 误用保护。
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, _ := cipher.NewGCM(block)
	ns := gcm.NonceSize()
	if len(blob) < ns {
		return nil, errors.New("ciphertext too short")
	}
	return gcm.Open(nil, blob[:ns], blob[ns:], nil)
}
