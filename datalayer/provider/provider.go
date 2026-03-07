// Package provider provides a unified data access layer for market data (K-lines, etc.) with optional caching.
package provider

import "nofx/market"

// DataProvider 统一数据访问接口：按 symbol/exchange/timeframes 获取市场数据，内部可接缓存与存储
type DataProvider interface {
	// GetWithTimeframes 获取多周期 K 线及衍生指标，与 market.GetWithTimeframes 语义一致
	// 读路径：先缓存，未命中再 REST；可选 REST 后落库（P0-4/P0-5）
	GetWithTimeframes(symbol, exchange string, timeframes []string, primaryTimeframe string, count int) (*market.Data, error)
}

// KlineBarRow 单根 K 线（与落库表一致），供落库接口使用，不依赖 store 包
type KlineBarRow struct {
	OpenTime int64
	Open     float64
	High     float64
	Low      float64
	Close    float64
	Volume   float64
}

// KlineBarPersistence 可选 K 线落库接口；由调用方注入（如 store.KlineBarStore 适配）
type KlineBarPersistence interface {
	UpsertBars(symbol, exchange, timeframe string, bars []KlineBarRow) error
}
