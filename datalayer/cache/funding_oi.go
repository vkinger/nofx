// Package cache: 资金费率 / OI 短期缓存，与 DataProvider 键统一（symbol+exchange），TTL 1h，避免同一周期内重复请求
package cache

import (
	"fmt"
	"sync"
	"time"

	"nofx/market"
)

// FundingOIEntry 单条资金费率+OI 缓存
type FundingOIEntry struct {
	FundingRate float64
	OI          *market.OIData
	Expires     time.Time
}

// FundingOICache 按 symbol+exchange 缓存的资金费率与 OI，TTL 默认 1h
type FundingOICache struct {
	mu     sync.RWMutex
	store  map[string]*FundingOIEntry
	ttl    time.Duration
	maxLen int
}

const defaultFundingOITTL = time.Hour

// NewFundingOICache 创建资金费率/OI 缓存，ttl 为 0 时使用 1h
func NewFundingOICache(ttl time.Duration, maxEntries int) *FundingOICache {
	if ttl <= 0 {
		ttl = defaultFundingOITTL
	}
	if maxEntries <= 0 {
		maxEntries = 500
	}
	c := &FundingOICache{store: make(map[string]*FundingOIEntry), ttl: ttl, maxLen: maxEntries}
	go c.cleanLoop()
	return c
}

// Key 生成缓存键，与 DataProvider 统一（symbol+exchange）
func (c *FundingOICache) Key(symbol, exchange string) string {
	return fmt.Sprintf("%s|%s", symbol, exchange)
}

// Get 获取缓存，过期或不存在返回 nil
func (c *FundingOICache) Get(key string) (funding float64, oi *market.OIData, ok bool) {
	c.mu.RLock()
	ent, exists := c.store[key]
	c.mu.RUnlock()
	if !exists || ent == nil || time.Now().After(ent.Expires) {
		return 0, nil, false
	}
	return ent.FundingRate, ent.OI, true
}

// Set 写入缓存
func (c *FundingOICache) Set(key string, fundingRate float64, oi *market.OIData) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.store) >= c.maxLen {
		c.evictOneLocked()
	}
	c.store[key] = &FundingOIEntry{
		FundingRate: fundingRate,
		OI:          oi,
		Expires:     time.Now().Add(c.ttl),
	}
}

func (c *FundingOICache) evictOneLocked() {
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
	}
}

func (c *FundingOICache) cleanLoop() {
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
