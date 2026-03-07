// Package store: K 线落库（P0-4 可选表），symbol, exchange, timeframe, open_time, o/h/l/c/v；REST 拉取后 upsert，保留策略按周期
package store

import (
	"time"

	"nofx/datalayer/provider"
	"gorm.io/gorm"
)

// KlineBarDB 单根 K 线落库表
type KlineBarDB struct {
	ID        int64   `gorm:"primaryKey;autoIncrement"`
	Symbol    string  `gorm:"column:symbol;not null;index:idx_kline_lookup"`
	Exchange  string  `gorm:"column:exchange;not null;index:idx_kline_lookup"`
	Timeframe string  `gorm:"column:timeframe;not null;index:idx_kline_lookup"`
	OpenTime  int64   `gorm:"column:open_time;not null;uniqueIndex:uq_kline_bar"` // Unix 毫秒
	O         float64 `gorm:"column:o;not null"`
	H         float64 `gorm:"column:h;not null"`
	L         float64 `gorm:"column:l;not null"`
	C         float64 `gorm:"column:c;not null"`
	V         float64 `gorm:"column:v;default:0"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (KlineBarDB) TableName() string { return "kline_bars" }

// KlineBarRow 对外单根 K 线（与 market.KlineBar 对齐：Time=OpenTime）
type KlineBarRow struct {
	OpenTime int64
	Open     float64
	High     float64
	Low      float64
	Close    float64
	Volume   float64
}

// KlineBarStore K 线落库读写
type KlineBarStore struct {
	db *gorm.DB
}

// NewKlineBarStore 创建 K 线 store
func NewKlineBarStore(db *gorm.DB) *KlineBarStore {
	return &KlineBarStore{db: db}
}

func (s *KlineBarStore) initTables() error {
	if s.db.Dialector.Name() == "postgres" {
		var n int64
		s.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'kline_bars'`).Scan(&n)
		if n > 0 {
			return nil
		}
	}
	return s.db.AutoMigrate(&KlineBarDB{})
}

// UpsertBars 批量 upsert（按 symbol, exchange, timeframe, open_time 唯一），存在则更新 o/h/l/c/v
func (s *KlineBarStore) UpsertBars(symbol, exchange, timeframe string, bars []KlineBarRow) error {
	for _, b := range bars {
		row := &KlineBarDB{
			Symbol:    symbol,
			Exchange:  exchange,
			Timeframe: timeframe,
			OpenTime:  b.OpenTime,
			O:         b.Open,
			H:         b.High,
			L:         b.Low,
			C:         b.Close,
			V:         b.Volume,
		}
		err := s.db.Where("symbol = ? AND exchange = ? AND timeframe = ? AND open_time = ?", symbol, exchange, timeframe, b.OpenTime).
			Assign(map[string]interface{}{"o": b.Open, "h": b.High, "l": b.Low, "c": b.Close, "v": b.Volume}).
			FirstOrCreate(row).Error
		if err != nil {
			return err
		}
	}
	return nil
}

// QueryBars 按时间范围查询（闭区间 [fromTimeMs, toTimeMs]），按 open_time 升序
func (s *KlineBarStore) QueryBars(symbol, exchange, timeframe string, fromTimeMs, toTimeMs int64) ([]KlineBarRow, error) {
	var list []KlineBarDB
	err := s.db.Where("symbol = ? AND exchange = ? AND timeframe = ? AND open_time >= ? AND open_time <= ?",
		symbol, exchange, timeframe, fromTimeMs, toTimeMs).
		Order("open_time ASC").Find(&list).Error
	if err != nil {
		return nil, err
	}
	out := make([]KlineBarRow, len(list))
	for i := range list {
		out[i] = KlineBarRow{
			OpenTime: list[i].OpenTime,
			Open:     list[i].O,
			High:     list[i].H,
			Low:      list[i].L,
			Close:    list[i].C,
			Volume:   list[i].V,
		}
	}
	return out, nil
}

// RetentionDays 建议保留天数：1m→7, 5m/15m→30, 1h/4h→90
func RetentionDaysForTimeframe(tf string) int {
	switch tf {
	case "1m", "3m":
		return 7
	case "5m", "15m":
		return 30
	case "1h", "4h":
		return 90
	default:
		return 30
	}
}

// CleanOlderThan 删除某 timeframe 早于 beforeTime 的数据（beforeTime 为 Unix 毫秒）
func (s *KlineBarStore) CleanOlderThan(timeframe string, beforeTimeMs int64) (int64, error) {
	res := s.db.Where("timeframe = ? AND open_time < ?", timeframe, beforeTimeMs).Delete(&KlineBarDB{})
	return res.RowsAffected, res.Error
}

// klineBarPersistenceAdapter 使 KlineBarStore 实现 provider.KlineBarPersistence（P0-5 落库）
type klineBarPersistenceAdapter struct{ *KlineBarStore }

// AsKlineBarPersistence 返回实现 provider.KlineBarPersistence 的适配器，供 DataProvider 可选落库
func (s *KlineBarStore) AsKlineBarPersistence() provider.KlineBarPersistence {
	return &klineBarPersistenceAdapter{s}
}

func (a *klineBarPersistenceAdapter) UpsertBars(symbol, exchange, timeframe string, bars []provider.KlineBarRow) error {
	rows := make([]KlineBarRow, len(bars))
	for i := range bars {
		rows[i] = KlineBarRow{
			OpenTime: bars[i].OpenTime,
			Open:     bars[i].Open,
			High:     bars[i].High,
			Low:      bars[i].Low,
			Close:    bars[i].Close,
			Volume:   bars[i].Volume,
		}
	}
	return a.KlineBarStore.UpsertBars(symbol, exchange, timeframe, rows)
}
