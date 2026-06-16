// Package cache 提供一个通用的、带 TTL 的内存缓存。
package cache

import (
	"sync"
	"time"
)

type entry struct {
	value     any
	expiresAt time.Time
}

// Cache 是一个并发安全的 TTL 缓存，读时惰性失效。
type Cache struct {
	mu  sync.RWMutex
	m   map[string]entry
	now func() time.Time
}

// New 创建一个空缓存。
func New() *Cache {
	return &Cache{m: make(map[string]entry), now: time.Now}
}

// Get 返回 key 对应的值；不存在或已过期返回 (nil, false)。
func (c *Cache) Get(key string) (any, bool) {
	c.mu.RLock()
	e, ok := c.m[key]
	c.mu.RUnlock()
	if !ok || c.now().After(e.expiresAt) {
		return nil, false
	}
	return e.value, true
}

// Set 写入 key，ttl 后过期。
func (c *Cache) Set(key string, value any, ttl time.Duration) {
	c.mu.Lock()
	c.m[key] = entry{value: value, expiresAt: c.now().Add(ttl)}
	c.mu.Unlock()
}
