package store

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// PaperAccount 虚拟盘账户（按 user_id + trader_id 唯一）
type PaperAccount struct {
	ID             int64   `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID         string  `gorm:"column:user_id;not null;uniqueIndex:idx_paper_account_user_trader" json:"user_id"`
	TraderID       string  `gorm:"column:trader_id;not null;uniqueIndex:idx_paper_account_user_trader" json:"trader_id"`
	InitialBalance float64 `gorm:"column:initial_balance;not null" json:"initial_balance"`
	Balance        float64 `gorm:"column:balance;not null" json:"balance"` // 可用余额（不含持仓占用保证金）
	Currency       string  `gorm:"column:currency;default:USDT" json:"currency"`
	CreatedAt      int64   `gorm:"column:created_at" json:"created_at"`
	UpdatedAt      int64   `gorm:"column:updated_at" json:"updated_at"`
}

// TableName returns the table name
func (PaperAccount) TableName() string {
	return "paper_accounts"
}

// PaperPosition 虚拟盘持仓（仅存 open 的持仓；平仓后可保留一条 CLOSED 记录或另表，此处仅 open）
type PaperPosition struct {
	ID           int64   `gorm:"primaryKey;autoIncrement" json:"id"`
	AccountID    int64   `gorm:"column:account_id;not null;index" json:"account_id"`
	Symbol       string  `gorm:"column:symbol;not null;index:idx_paper_pos_account_symbol_side" json:"symbol"`
	Side         string  `gorm:"column:side;not null;index:idx_paper_pos_account_symbol_side" json:"side"` // long / short
	Quantity     float64 `gorm:"column:quantity;not null" json:"quantity"`
	EntryPrice   float64 `gorm:"column:entry_price;not null" json:"entry_price"`
	Leverage     int     `gorm:"column:leverage;default:1" json:"leverage"`
	EntryOrderID string  `gorm:"column:entry_order_id;default:''" json:"entry_order_id"`
	EntryTime    int64   `gorm:"column:entry_time;not null" json:"entry_time"`
	Status       string  `gorm:"column:status;default:OPEN;index" json:"status"` // OPEN / CLOSED
	CreatedAt    int64   `gorm:"column:created_at" json:"created_at"`
	UpdatedAt    int64   `gorm:"column:updated_at" json:"updated_at"`
}

// TableName returns the table name
func (PaperPosition) TableName() string {
	return "paper_positions"
}

// PaperTrade 虚拟盘成交记录（平仓时写入，便于统计与对账）
type PaperTrade struct {
	ID            int64   `gorm:"primaryKey;autoIncrement" json:"id"`
	AccountID     int64   `gorm:"column:account_id;not null;index" json:"account_id"`
	PositionID    int64   `gorm:"column:position_id;not null" json:"position_id"`
	Symbol        string  `gorm:"column:symbol;not null" json:"symbol"`
	Side          string  `gorm:"column:side;not null" json:"side"`
	Quantity      float64 `gorm:"column:quantity;not null" json:"quantity"`
	EntryPrice    float64 `gorm:"column:entry_price;not null" json:"entry_price"`
	ExitPrice     float64 `gorm:"column:exit_price;not null" json:"exit_price"`
	RealizedPnL   float64 `gorm:"column:realized_pnl;not null" json:"realized_pnl"`
	Fee           float64 `gorm:"column:fee;default:0" json:"fee"`
	Leverage      int     `gorm:"column:leverage;default:1" json:"leverage"`
	EntryTime     int64   `gorm:"column:entry_time;default:0" json:"entry_time"` // 开仓时间 ms
	ExitTime      int64   `gorm:"column:exit_time;not null" json:"exit_time"`
	CloseReason   string  `gorm:"column:close_reason;default:''" json:"close_reason"`
	CreatedAt     int64   `gorm:"column:created_at" json:"created_at"`
}

// TableName returns the table name
func (PaperTrade) TableName() string {
	return "paper_trades"
}

// PaperStore 虚拟盘存储
type PaperStore struct {
	db *gorm.DB
}

// NewPaperStore creates paper store instance
func NewPaperStore(db *gorm.DB) *PaperStore {
	return &PaperStore{db: db}
}

// InitTables initializes paper tables
func (s *PaperStore) InitTables() error {
	return s.db.AutoMigrate(&PaperAccount{}, &PaperPosition{}, &PaperTrade{})
}

// GetOrCreateAccount 获取或创建虚拟盘账户
func (s *PaperStore) GetOrCreateAccount(userID, traderID string, initialBalance float64) (*PaperAccount, error) {
	var acc PaperAccount
	err := s.db.Where("user_id = ? AND trader_id = ?", userID, traderID).First(&acc).Error
	if err == nil {
		return &acc, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	now := time.Now().UTC().UnixMilli()
	acc = PaperAccount{
		UserID:         userID,
		TraderID:       traderID,
		InitialBalance: initialBalance,
		Balance:        initialBalance,
		Currency:       "USDT",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.db.Create(&acc).Error; err != nil {
		return nil, err
	}
	return &acc, nil
}

// UpdateBalance 更新账户可用余额
func (s *PaperStore) UpdateBalance(accountID int64, newBalance float64) error {
	now := time.Now().UTC().UnixMilli()
	return s.db.Model(&PaperAccount{}).Where("id = ?", accountID).Updates(map[string]interface{}{
		"balance":    newBalance,
		"updated_at": now,
	}).Error
}

// GetAccountByUserAndTrader 根据 user_id 和 trader_id 获取账户
func (s *PaperStore) GetAccountByUserAndTrader(userID, traderID string) (*PaperAccount, error) {
	var acc PaperAccount
	err := s.db.Where("user_id = ? AND trader_id = ?", userID, traderID).First(&acc).Error
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

// GetOpenPositions 获取某账户下所有 OPEN 持仓
func (s *PaperStore) GetOpenPositions(accountID int64) ([]*PaperPosition, error) {
	var list []*PaperPosition
	err := s.db.Where("account_id = ? AND status = ?", accountID, "OPEN").Order("id ASC").Find(&list).Error
	return list, err
}

// AddPosition 新增一条 OPEN 持仓
func (s *PaperStore) AddPosition(pos *PaperPosition) error {
	pos.Status = "OPEN"
	now := time.Now().UTC().UnixMilli()
	pos.CreatedAt = now
	pos.UpdatedAt = now
	return s.db.Create(pos).Error
}

// ClosePosition 将持仓标记为 CLOSED，并写入 PaperTrade 记录
func (s *PaperStore) ClosePosition(positionID int64, exitPrice, realizedPnL, fee float64, closeReason string) error {
	var pos PaperPosition
	if err := s.db.First(&pos, positionID).Error; err != nil {
		return err
	}
	if pos.Status != "OPEN" {
		return fmt.Errorf("position %d is not OPEN", positionID)
	}
	now := time.Now().UTC().UnixMilli()
	if err := s.db.Model(&PaperPosition{}).Where("id = ?", positionID).Updates(map[string]interface{}{
		"status":     "CLOSED",
		"updated_at": now,
	}).Error; err != nil {
		return err
	}
	trade := &PaperTrade{
		AccountID:   pos.AccountID,
		PositionID:  pos.ID,
		Symbol:      pos.Symbol,
		Side:        pos.Side,
		Quantity:    pos.Quantity,
		EntryPrice:  pos.EntryPrice,
		ExitPrice:   exitPrice,
		RealizedPnL: realizedPnL,
		Fee:         fee,
		Leverage:    pos.Leverage,
		EntryTime:   pos.EntryTime,
		ExitTime:    now,
		CloseReason: closeReason,
		CreatedAt:   now,
	}
	return s.db.Create(trade).Error
}

// ReducePosition 部分平仓：减少 quantity，若为 0 则标记 CLOSED 并写 PaperTrade
func (s *PaperStore) ReducePosition(positionID int64, closeQty, exitPrice, realizedPnL, fee float64, closeReason string) (remainingQty float64, closed bool, err error) {
	var pos PaperPosition
	if err = s.db.First(&pos, positionID).Error; err != nil {
		return 0, false, err
	}
	if pos.Status != "OPEN" {
		return 0, false, fmt.Errorf("position %d is not OPEN", positionID)
	}
	if closeQty <= 0 || closeQty > pos.Quantity {
		return pos.Quantity, false, fmt.Errorf("invalid close quantity %f for position quantity %f", closeQty, pos.Quantity)
	}
	now := time.Now().UTC().UnixMilli()
	remainingQty = pos.Quantity - closeQty
	if remainingQty <= 1e-12 {
		// 全平
		if err = s.db.Model(&PaperPosition{}).Where("id = ?", positionID).Updates(map[string]interface{}{
			"quantity":   0,
			"status":     "CLOSED",
			"updated_at": now,
		}).Error; err != nil {
			return 0, false, err
		}
		trade := &PaperTrade{
			AccountID:   pos.AccountID,
			PositionID:  pos.ID,
			Symbol:      pos.Symbol,
			Side:        pos.Side,
			Quantity:    closeQty,
			EntryPrice:  pos.EntryPrice,
			ExitPrice:   exitPrice,
			RealizedPnL: realizedPnL,
			Fee:         fee,
			Leverage:    pos.Leverage,
			EntryTime:   pos.EntryTime,
			ExitTime:    now,
			CloseReason: closeReason,
			CreatedAt:   now,
		}
		if err = s.db.Create(trade).Error; err != nil {
			return 0, false, err
		}
		return 0, true, nil
	}
	// 部分平仓：只更新 quantity，并写一条 PaperTrade
	if err = s.db.Model(&PaperPosition{}).Where("id = ?", positionID).Updates(map[string]interface{}{
		"quantity":   remainingQty,
		"updated_at": now,
	}).Error; err != nil {
		return 0, false, err
	}
	trade := &PaperTrade{
		AccountID:   pos.AccountID,
		PositionID:  pos.ID,
		Symbol:      pos.Symbol,
		Side:        pos.Side,
		Quantity:    closeQty,
		EntryPrice:  pos.EntryPrice,
		ExitPrice:   exitPrice,
		RealizedPnL: realizedPnL,
		Fee:         fee,
		Leverage:    pos.Leverage,
		EntryTime:   pos.EntryTime,
		ExitTime:    now,
		CloseReason: closeReason,
		CreatedAt:   now,
	}
	if err = s.db.Create(trade).Error; err != nil {
		return 0, false, err
	}
	return remainingQty, false, nil
}

// UpdatePositionQuantity 部分平仓后更新持仓数量（已由 ReducePosition 处理，此处仅备用）
func (s *PaperStore) UpdatePositionQuantity(positionID int64, newQuantity float64) error {
	now := time.Now().UTC().UnixMilli()
	updates := map[string]interface{}{"quantity": newQuantity, "updated_at": now}
	if newQuantity <= 0 {
		updates["status"] = "CLOSED"
	}
	return s.db.Model(&PaperPosition{}).Where("id = ?", positionID).Updates(updates).Error
}

// ListTrades 按平仓时间倒序返回最近平仓记录，用于 GetClosedPnL / 最近交易
func (s *PaperStore) ListTrades(userID, traderID string, sinceTimeUnixMs int64, limit int) ([]*PaperTrade, error) {
	acc, err := s.GetAccountByUserAndTrader(userID, traderID)
	if err != nil {
		return nil, err
	}
	var list []*PaperTrade
	q := s.db.Where("account_id = ? AND exit_time >= ?", acc.ID, sinceTimeUnixMs).
		Order("exit_time DESC").Limit(limit).Find(&list)
	if q.Error != nil {
		return nil, q.Error
	}
	return list, nil
}
