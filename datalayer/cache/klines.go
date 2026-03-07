// Package cache provides short-term cache for market data (K-lines, etc.) to reduce exchange API calls.
package cache

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"nofx/logger"
	"nofx/market"
)

// KlineCacheEntry 单条缓存项（按 symbol+exchange+timeframes+primary+count 整体缓存一份 Data）
type KlineCacheEntry struct {
	Data    *market.Data
	Expires time.Time
}

// KlineCache 内存 K 线缓存，带 TTL
type KlineCache struct {
	mu     sync.RWMutex
	store  map[string]*KlineCacheEntry
	ttl    time.Duration
	maxLen int
}

// NewKlineCache 创建 K 线缓存。ttl 建议：1m 周期 60s，5m 5min，1h 15min
func NewKlineCache(ttl time.Duration, maxEntries int) *KlineCache {
	if maxEntries <= 0 {
		maxEntries = 500
	}
	c := &KlineCache{store: make(map[string]*KlineCacheEntry), ttl: ttl, maxLen: maxEntries}
	go c.cleanLoop()
	return c
}

// Key 生成缓存键
func (c *KlineCache) Key(symbol, exchange string, timeframes []string, primaryTF string, count int) string {
	return fmt.Sprintf("%s|%s|%s|%s|%d", symbol, exchange, strings.Join(timeframes, ","), primaryTF, count)
}

// Get 获取缓存，过期或不存在返回 nil。过期项在写锁下再次确认后删除或返回刷新后数据，避免竞态。
func (c *KlineCache) Get(key string) *market.Data {
	c.mu.RLock()
	ent, ok := c.store[key]
	c.mu.RUnlock()
	if !ok || ent == nil {
		return nil
	}
	now := time.Now()
	if now.After(ent.Expires) {
		c.mu.Lock()
		ent2 := c.store[key]
		if ent2 == nil {
			c.mu.Unlock()
			return nil
		}
		if now.After(ent2.Expires) {
			delete(c.store, key)
			c.mu.Unlock()
			return nil
		}
		data := ent2.Data
		c.mu.Unlock()
		return data
	}
	return ent.Data
}

// Set 写入缓存
func (c *KlineCache) Set(key string, data *market.Data) {
	if data == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.store) >= c.maxLen {
		c.evictOneLocked()
	}
	c.store[key] = &KlineCacheEntry{Data: data, Expires: time.Now().Add(c.ttl)}
}

func (c *KlineCache) evictOneLocked() {
	var oldest string
	var oldestExp time.Time
	for k, v := range c.store {
		if oldest == "" || v.Expires.Before(oldestExp) {
			oldest = k
			oldestExp = v.Expires
		}
	}
	if oldest != "" {
		delete(c.store, oldest)
		logger.Infof("[KlineCache] evicted key: %s", oldest)
	}
}

func (c *KlineCache) cleanLoop() {
	tick := time.NewTicker(c.ttl / 2)
	defer tick.Stop()
	for range tick.C {
		c.mu.Lock()
		now := time.Now()
		for k, v := range c.store {
			if now.After(v.Expires) {
				delete(c.store, k)
			}
		}
		c.mu.Unlock()
	}
}
