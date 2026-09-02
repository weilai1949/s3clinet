package handler

import (
	"sync"
	"time"

	"github.com/weilai1949/s3clinet/server/internal/model"
	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
)

// clientCache 按账号 ID 缓存 S3 客户端；UpdatedAt 变化时自动重建。
type clientCache struct {
	mu sync.RWMutex
	m  map[string]clientCacheEntry
}

type clientCacheEntry struct {
	updatedAt time.Time
	client    *s3wrap.Client
}

func newClientCache() *clientCache {
	return &clientCache{m: make(map[string]clientCacheEntry)}
}

func (c *clientCache) get(acc *model.Account) (*s3wrap.Client, error) {
	if acc == nil || acc.ID == "" {
		return s3wrap.New(acc)
	}
	c.mu.RLock()
	if e, ok := c.m[acc.ID]; ok && e.updatedAt.Equal(acc.UpdatedAt) {
		client := e.client
		c.mu.RUnlock()
		return client, nil
	}
	c.mu.RUnlock()

	client, err := s3wrap.New(acc)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[acc.ID]; ok && e.updatedAt.Equal(acc.UpdatedAt) {
		return e.client, nil
	}
	c.m[acc.ID] = clientCacheEntry{updatedAt: acc.UpdatedAt, client: client}
	return client, nil
}

func (c *clientCache) evict(id string) {
	if id == "" {
		return
	}
	c.mu.Lock()
	delete(c.m, id)
	c.mu.Unlock()
}
