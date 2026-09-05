package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/weilai1949/s3clinet/server/internal/model"
)

var (
	ErrNotFound = errors.New("account not found")
)

// Store 基于 JSON 文件持久化的账号存储，进程内带读写锁保护。
type Store struct {
	mu       sync.RWMutex
	path     string
	accounts map[string]*model.Account
	order    []string // 保持创建顺序，便于列表展示稳定
}

// New 创建 store 并从 path 加载已有数据。
func New(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	s := &Store{
		path:     path,
		accounts: make(map[string]*model.Account),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read account file: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	var list []*model.Account
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("parse account file: %w", err)
	}
	for _, a := range list {
		s.accounts[a.ID] = a
		s.order = append(s.order, a.ID)
	}
	return nil
}

// List 返回全部账号（脱敏副本），按创建顺序。
func (s *Store) List() ([]*model.Account, error) {
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

// Get 返回指定账号（含密钥，供服务端内部使用）。
func (s *Store) Get(id string) (*model.Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.accounts[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *a
	return &cp, nil
}

// Create 新增账号并持久化。
func (s *Store) Create(a *model.Account) (*model.Account, error) {
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
	s.accounts[a.ID] = a
	s.order = append(s.order, a.ID)
	if err := s.persistLocked(); err != nil {
		// 写盘失败：回滚内存状态保持一致。
		delete(s.accounts, a.ID)
		s.order = s.order[:len(s.order)-1]
		return nil, err
	}
	return a.Sanitized(), nil
}

// Update 更新账号；id 不允许变更。
func (s *Store) Update(id string, a *model.Account) (*model.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.accounts[id]
	if !ok {
		return nil, ErrNotFound
	}
	// 持久化失败时回滚内存状态，保持与磁盘一致。
	prev := *cur
	// 保留 id 与创建时间；未提供的 SecretKey 不覆盖（避免脱敏值回写）
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

// Delete 删除账号并持久化。
func (s *Store) Delete(id string) error {
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

// Close 释放资源；JSON 存储无额外资源需释放。
func (s *Store) Close() error { return nil }

// Ping 探测存储可用性（JSON 文件始终视为可用）。
func (s *Store) Ping() error { return nil }

// persistLocked 假定调用方已持有写锁。详见 atomicWriteFile。
func (s *Store) persistLocked() error {
	list := make([]*model.Account, 0, len(s.order))
	for _, id := range s.order {
		if a, ok := s.accounts[id]; ok {
			list = append(list, a)
		}
	}
	// model.Account 字段全部为基本类型/时间，MarshalIndent 不会失败。
	data, _ := json.MarshalIndent(list, "", "  ")
	return atomicWriteFile(s.path, data)
}
