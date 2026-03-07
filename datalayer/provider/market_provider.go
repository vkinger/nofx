// Package provider: 基于现有 market 包实现 DataProvider，并接入 K 线短期缓存
package provider

import (
	"nofx/datalayer/cache"
	"nofx/market"
	"time"
)

// MarketDataProvider 使用 market 包 + 可选 K 线缓存与落库的 DataProvider 实现（P0-2/P0-4/P0-5）
type MarketDataProvider struct {
	cache       *cache.KlineCache
	ttl         time.Duration
	persistence KlineBarPersistence // 可选：REST 成功后落库
}

// NewMarketDataProvider 创建 DataProvider。useCache 为 true 时启用内存缓存，ttl 为缓存时长（0 则用默认）
func NewMarketDataProvider(useCache bool, ttl time.Duration) *MarketDataProvider {
	if ttl <= 0 {
		ttl = 90 * time.Second // 默认 1.5 分钟，适配 1m/5m 周期
	}
	p := &MarketDataProvider{ttl: ttl}
	if useCache {
		p.cache = cache.NewKlineCache(ttl, 300)
	}
	return p
}

// SetKlineBarPersistence 设置可选 K 线落库（如 store.KlineBarStore.AsKlineBarPersistence()），REST 成功后写入
func (p *MarketDataProvider) SetKlineBarPersistence(persistence KlineBarPersistence) {
	p.persistence = persistence
}

// GetWithTimeframes 读路径：先缓存，未命中则 REST；REST 成功后写缓存并可选落库（P0-5）
func (p *MarketDataProvider) GetWithTimeframes(symbol, exchange string, timeframes []string, primaryTimeframe string, count int) (*market.Data, error) {
	if p.cache != nil {
		key := p.cache.Key(symbol, exchange, timeframes, primaryTimeframe, count)
		if data := p.cache.Get(key); data != nil {
			return data, nil
		}
	}
	data, err := market.GetWithTimeframes(symbol, exchange, timeframes, primaryTimeframe, count)
	if err != nil {
		return nil, err
	}
	if p.cache != nil && data != nil {
		key := p.cache.Key(symbol, exchange, timeframes, primaryTimeframe, count)
		p.cache.Set(key, data)
	}
	if p.persistence != nil && data != nil && data.TimeframeData != nil {
		for tf, series := range data.TimeframeData {
			if series == nil || len(series.Klines) == 0 {
				continue
			}
			bars := make([]KlineBarRow, len(series.Klines))
			for i, k := range series.Klines {
				bars[i] = KlineBarRow{OpenTime: k.Time, Open: k.Open, High: k.High, Low: k.Low, Close: k.Close, Volume: k.Volume}
			}
			_ = p.persistence.UpsertBars(symbol, exchange, tf, bars)
		}
	}
	return data, nil
}
