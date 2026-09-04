package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	if _, ok := s.accounts[id]; !ok {
		return ErrNotFound
	}
	delete(s.accounts, id)
	for i, oid := range s.order {
		if oid == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return s.persistLocked()
}

// Close 释放资源；JSON 存储无额外资源需释放。
func (s *Store) Close() error { return nil }

// Ping 探测存储可用性（JSON 文件始终视为可用）。
func (s *Store) Ping() error { return nil }

// persistLocked 假定调用方已持有写锁。使用临时文件 + rename 原子写，避免崩溃导致文件损坏。
func (s *Store) persistLocked() error {
	list := make([]*model.Account, 0, len(s.order))
	for _, id := range s.order {
		if a, ok := s.accounts[id]; ok {
			list = append(list, a)
		}
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	// 崩溃可能留下 tmp 残骸（写后 rename 前进程退出），先清理，
	// 否则下方 O_EXCL 永久失败。路径固定且由本进程命名，无抢占风险。
	_ = os.Remove(tmp)
	// 文件含明文 SecretKey，权限收紧为仅属主可读写；
	// O_CREATE|O_EXCL 对已存在路径（含符号链接）直接失败，杜绝抢占与链接重定向。
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if n, werr := f.Write(data); werr != nil {
		f.Close()
		os.Remove(tmp)
		return werr
	} else if n != len(data) {
		f.Close()
		os.Remove(tmp)
		return io.ErrShortWrite
	}
	if cerr := f.Close(); cerr != nil {
		os.Remove(tmp)
		return cerr
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
