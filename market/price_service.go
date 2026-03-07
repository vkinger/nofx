// Package market: 实时价/标记价服务（P3-2），供风控与交易员使用；内存 map，由交易所拉取或 WebSocket 写入
package market

import (
	"sync"
)

// PriceSnapshot 某标的的实时价与标记价
type PriceSnapshot struct {
	LastPrice float64 // 最新成交价
	MarkPrice float64 // 标记价（合约用，现货可等于 LastPrice）
}

// PriceService 实时价/标记价读写
type PriceService struct {
	mu    sync.RWMutex
	prices map[string]PriceSnapshot // symbol -> snapshot
}

// NewPriceService 创建实时价服务
func NewPriceService() *PriceService {
	return &PriceService{prices: make(map[string]PriceSnapshot)}
}

// Set 写入某标的的 last/mark 价
func (s *PriceService) Set(symbol string, lastPrice, markPrice float64) {
	symbol = Normalize(symbol)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prices == nil {
		s.prices = make(map[string]PriceSnapshot)
	}
	s.prices[symbol] = PriceSnapshot{LastPrice: lastPrice, MarkPrice: markPrice}
}

// SetLast 仅更新最新价（mark 沿用旧值或等于 last）
func (s *PriceService) SetLast(symbol string, lastPrice float64) {
	symbol = Normalize(symbol)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prices == nil {
		s.prices = make(map[string]PriceSnapshot)
	}
	cur := s.prices[symbol]
	cur.LastPrice = lastPrice
	if cur.MarkPrice == 0 {
		cur.MarkPrice = lastPrice
	}
	s.prices[symbol] = cur
}

// GetLastPrice 获取最新价，无则返回 0
func (s *PriceService) GetLastPrice(symbol string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.prices[Normalize(symbol)]; ok {
		return p.LastPrice
	}
	return 0
}

// GetMarkPrice 获取标记价，无则返回 0
func (s *PriceService) GetMarkPrice(symbol string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.prices[Normalize(symbol)]; ok {
		return p.MarkPrice
	}
	return 0
}

// Get 获取 last 与 mark
func (s *PriceService) Get(symbol string) (last, mark float64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.prices[Normalize(symbol)]; ok {
		return p.LastPrice, p.MarkPrice
	}
	return 0, 0
}
