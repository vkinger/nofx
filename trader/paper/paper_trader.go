package paper

import (
	"fmt"
	"math"
	"nofx/logger"
	"nofx/market"
	"nofx/store"
	"nofx/trader/types"
	"sync"
	"time"
)

const (
	defaultOrderBookDepth = 20
	defaultFeeRate        = 0.0004 // 0.04% taker，与 Binance 合约吃单费率一致
)

// 常见交易所合约吃单费率（taker），便于虚拟盘与实盘费率一致
var defaultTakerFeeByExchange = map[string]float64{
	"binance":  0.0004, // 0.04%
	"bybit":    0.00055,
	"okx":      0.0005,
	"bitget":   0.0006,
	"gate":     0.0005,
	"kucoin":   0.0006,
	"hyperliquid": 0.00035,
	"aster":    0.0005,
	"lighter":  0.0005,
}

// Option 可选配置
type Option func(*Trader)

// WithFeeRate 设置手续费率（如 0.0004 = 0.04%）
func WithFeeRate(rate float64) Option {
	return func(t *Trader) { t.feeRate = rate }
}

// WithFeeRateByExchange 按价格源交易所类型设置费率（若未在 map 中则用 defaultFeeRate）
func WithFeeRateByExchange(exchangeType string) Option {
	rate := defaultTakerFeeByExchange[exchangeType]
	if rate <= 0 {
		rate = defaultFeeRate
	}
	return WithFeeRate(rate)
}

// WithFeeRateFromExchange 从价格源交易所实时获取吃单费率（与实盘一致，有缓存）
func WithFeeRateFromExchange(creds *market.ExchangeCredentials) Option {
	return func(t *Trader) { t.feeCredentials = creds }
}

// Trader 虚拟盘交易实现：使用真实交易所盘口模拟成交，余额与持仓持久化到 DB
type Trader struct {
	priceSource     types.Trader       // 价格与盘口来源（实盘或 testnet）
	gridSource      types.GridTrader   // 若 priceSource 实现了 GridTrader 则用盘口模拟成交
	paperStore      *store.PaperStore
	userID          string
	traderID        string
	initialBal      float64
	feeRate         float64            // 静态费率（当 feeCredentials 为空时使用）
	feeCredentials *market.ExchangeCredentials // 非空时从交易所 API 实时获取 taker 费率（有缓存）
	mu              sync.Mutex
}

// NewTrader 创建虚拟盘 Trader。priceSource 用于 GetMarketPrice（以及若实现 GridTrader 则 GetOrderBook）。可选 WithFeeRate / WithFeeRateByExchange 与交易所费率一致。
func NewTrader(priceSource types.Trader, paperStore *store.PaperStore, userID, traderID string, initialBalance float64, opts ...Option) (*Trader, error) {
	if priceSource == nil || paperStore == nil {
		return nil, fmt.Errorf("paper: priceSource and paperStore required")
	}
	if initialBalance <= 0 {
		return nil, fmt.Errorf("paper: initialBalance must be > 0")
	}
	var gridSource types.GridTrader
	if g, ok := priceSource.(types.GridTrader); ok {
		gridSource = g
	}
	t := &Trader{
		priceSource: priceSource,
		gridSource:  gridSource,
		paperStore:  paperStore,
		userID:      userID,
		traderID:    traderID,
		initialBal:  initialBalance,
		feeRate:     defaultFeeRate,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t, nil
}

// ensureAccount 获取或创建虚拟盘账户
func (t *Trader) ensureAccount() (*store.PaperAccount, error) {
	return t.paperStore.GetOrCreateAccount(t.userID, t.traderID, t.initialBal)
}

// getTakerRate 返回吃单费率：若配置了 feeCredentials 则从交易所实时获取（有缓存），否则用静态 feeRate
func (t *Trader) getTakerRate(symbol string) float64 {
	if t.feeCredentials != nil {
		_, taker, _ := market.FetchTradingFeeRates(symbol, t.feeCredentials)
		if taker > 0 {
			return taker
		}
	}
	return t.feeRate
}

// GetBalance 从 DB 读取余额：availableBalance=可用（不含持仓占用），total_equity=可用+占用保证金+未实现盈亏
func (t *Trader) GetBalance() (map[string]interface{}, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	acc, err := t.ensureAccount()
	if err != nil {
		return nil, err
	}
	positions, err := t.paperStore.GetOpenPositions(acc.ID)
	if err != nil {
		return nil, err
	}
	var totalUnrealized float64
	var totalMarginUsed float64
	for _, pos := range positions {
		lev := pos.Leverage
		if lev <= 0 {
			lev = 1
		}
		totalMarginUsed += (pos.EntryPrice * pos.Quantity) / float64(lev)
		mark, _ := t.priceSource.GetMarketPrice(pos.Symbol)
		if mark <= 0 {
			continue
		}
		if pos.Side == "long" {
			totalUnrealized += (mark - pos.EntryPrice) * pos.Quantity
		} else {
			totalUnrealized += (pos.EntryPrice - mark) * pos.Quantity
		}
	}
	totalEquity := acc.Balance + totalMarginUsed + totalUnrealized
	return map[string]interface{}{
		"totalWalletBalance":    acc.Balance + totalMarginUsed,
		"total_equity":         totalEquity,
		"totalUnrealizedProfit": totalUnrealized,
		"availableBalance":     acc.Balance,
	}, nil
}

// GetPositions 从 DB 读取持仓，并填充 markPrice、unRealizedProfit 等
func (t *Trader) GetPositions() ([]map[string]interface{}, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	acc, err := t.ensureAccount()
	if err != nil {
		return nil, err
	}
	positions, err := t.paperStore.GetOpenPositions(acc.ID)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]interface{}, 0, len(positions))
	for _, pos := range positions {
		markPrice, _ := t.priceSource.GetMarketPrice(pos.Symbol)
		if markPrice <= 0 {
			markPrice = pos.EntryPrice
		}
		var unRealized float64
		var positionAmt float64
		if pos.Side == "long" {
			positionAmt = pos.Quantity
			unRealized = (markPrice - pos.EntryPrice) * pos.Quantity
		} else {
			positionAmt = -pos.Quantity
			unRealized = (pos.EntryPrice - markPrice) * pos.Quantity
		}
		m := map[string]interface{}{
			"symbol":            pos.Symbol,
			"positionAmt":      positionAmt,
			"entryPrice":       pos.EntryPrice,
			"markPrice":        markPrice,
			"unRealizedProfit": unRealized,
			"leverage":         float64(pos.Leverage),
			"side":             pos.Side,
		}
		result = append(result, m)
	}
	return result, nil
}

// simulateBuyFromBook 用盘口 asks 模拟市价买入，返回均价和成交量（可部分成交）
func (t *Trader) simulateBuyFromBook(asks [][]float64, wantQty float64) (avgPrice, filledQty float64) {
	if len(asks) == 0 {
		return 0, 0
	}
	var cost, filled float64
	remaining := wantQty
	for _, level := range asks {
		if remaining <= 0 {
			break
		}
		if len(level) < 2 {
			continue
		}
		price, qty := level[0], level[1]
		if price <= 0 || qty <= 0 {
			continue
		}
		take := math.Min(remaining, qty)
		cost += take * price
		filled += take
		remaining -= take
	}
	if filled <= 0 {
		return 0, 0
	}
	return cost / filled, filled
}

// simulateSellFromBook 用盘口 bids 模拟市价卖出
func (t *Trader) simulateSellFromBook(bids [][]float64, wantQty float64) (avgPrice, filledQty float64) {
	if len(bids) == 0 {
		return 0, 0
	}
	var cost, filled float64
	remaining := wantQty
	for _, level := range bids {
		if remaining <= 0 {
			break
		}
		if len(level) < 2 {
			continue
		}
		price, qty := level[0], level[1]
		if price <= 0 || qty <= 0 {
			continue
		}
		take := math.Min(remaining, qty)
		cost += take * price
		filled += take
		remaining -= take
	}
	if filled <= 0 {
		return 0, 0
	}
	return cost / filled, filled
}

// OpenLong 开多：盘口模拟买入，写入持仓
func (t *Trader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	if quantity <= 0 {
		return nil, fmt.Errorf("quantity must be > 0")
	}
	symbolNorm := market.Normalize(symbol)
	t.mu.Lock()
	defer t.mu.Unlock()
	acc, err := t.ensureAccount()
	if err != nil {
		return nil, err
	}
	var avgPrice float64
	if t.gridSource != nil {
		_, asks, err := t.gridSource.GetOrderBook(symbolNorm, defaultOrderBookDepth)
		if err == nil && len(asks) > 0 {
			avgPrice, _ = t.simulateBuyFromBook(asks, quantity)
		}
	}
	if avgPrice <= 0 {
		avgPrice, err = t.priceSource.GetMarketPrice(symbolNorm)
		if err != nil || avgPrice <= 0 {
			return nil, fmt.Errorf("paper: cannot get price for %s: %w", symbolNorm, err)
		}
	}
	rate := t.getTakerRate(symbolNorm)
	fee := avgPrice * quantity * rate
	lev := leverage
	if lev <= 0 {
		lev = 1
	}
	marginUsed := (avgPrice * quantity) / float64(lev)
	newBalance := acc.Balance - marginUsed - fee
	if newBalance < 0 {
		t.mu.Unlock()
		return nil, fmt.Errorf("paper: insufficient balance: need margin %.2f + fee %.2f, available %.2f", marginUsed, fee, acc.Balance)
	}
	pos := &store.PaperPosition{
		AccountID:    acc.ID,
		Symbol:       symbolNorm,
		Side:         "long",
		Quantity:     quantity,
		EntryPrice:   avgPrice,
		Leverage:     leverage,
		EntryOrderID: fmt.Sprintf("paper-%d", time.Now().UnixNano()),
		EntryTime:    time.Now().UTC().UnixMilli(),
	}
	if err := t.paperStore.AddPosition(pos); err != nil {
		return nil, err
	}
	if err := t.paperStore.UpdateBalance(acc.ID, newBalance); err != nil {
		return nil, err
	}
	logger.Infof("📄 [Paper] OpenLong %s qty=%.6f avgPrice=%.4f margin=%.2f fee=%.4f balance %.2f→%.2f", symbolNorm, quantity, avgPrice, marginUsed, fee, acc.Balance, newBalance)
	return map[string]interface{}{
		"orderId":   pos.EntryOrderID,
		"avgPrice":  avgPrice,
		"executedQty": quantity,
		"commission": fee,
	}, nil
}

// OpenShort 开空：盘口模拟卖出，写入持仓
func (t *Trader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	if quantity <= 0 {
		return nil, fmt.Errorf("quantity must be > 0")
	}
	symbolNorm := market.Normalize(symbol)
	t.mu.Lock()
	defer t.mu.Unlock()
	acc, err := t.ensureAccount()
	if err != nil {
		return nil, err
	}
	var avgPrice float64
	if t.gridSource != nil {
		bids, _, err := t.gridSource.GetOrderBook(symbolNorm, defaultOrderBookDepth)
		if err == nil && len(bids) > 0 {
			avgPrice, _ = t.simulateSellFromBook(bids, quantity)
		}
	}
	if avgPrice <= 0 {
		avgPrice, err = t.priceSource.GetMarketPrice(symbolNorm)
		if err != nil || avgPrice <= 0 {
			return nil, fmt.Errorf("paper: cannot get price for %s: %w", symbolNorm, err)
		}
	}
	rate := t.getTakerRate(symbolNorm)
	fee := avgPrice * quantity * rate
	lev := leverage
	if lev <= 0 {
		lev = 1
	}
	marginUsed := (avgPrice * quantity) / float64(lev)
	newBalance := acc.Balance - marginUsed - fee
	if newBalance < 0 {
		t.mu.Unlock()
		return nil, fmt.Errorf("paper: insufficient balance: need margin %.2f + fee %.2f, available %.2f", marginUsed, fee, acc.Balance)
	}
	pos := &store.PaperPosition{
		AccountID:    acc.ID,
		Symbol:       symbolNorm,
		Side:         "short",
		Quantity:     quantity,
		EntryPrice:   avgPrice,
		Leverage:     leverage,
		EntryOrderID: fmt.Sprintf("paper-%d", time.Now().UnixNano()),
		EntryTime:    time.Now().UTC().UnixMilli(),
	}
	if err := t.paperStore.AddPosition(pos); err != nil {
		return nil, err
	}
	if err := t.paperStore.UpdateBalance(acc.ID, newBalance); err != nil {
		return nil, err
	}
	logger.Infof("📄 [Paper] OpenShort %s qty=%.6f avgPrice=%.4f margin=%.2f fee=%.4f balance %.2f→%.2f", symbolNorm, quantity, avgPrice, marginUsed, fee, acc.Balance, newBalance)
	return map[string]interface{}{
		"orderId":   pos.EntryOrderID,
		"avgPrice":  avgPrice,
		"executedQty": quantity,
		"commission": fee,
	}, nil
}

// CloseLong 平多：盘口模拟卖出，更新余额并关闭/减持仓
func (t *Trader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	symbolNorm := market.Normalize(symbol)
	t.mu.Lock()
	defer t.mu.Unlock()
	acc, err := t.ensureAccount()
	if err != nil {
		return nil, err
	}
	positions, err := t.paperStore.GetOpenPositions(acc.ID)
	if err != nil {
		return nil, err
	}
	var target *store.PaperPosition
	for _, p := range positions {
		if p.Symbol == symbolNorm && p.Side == "long" {
			target = p
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("paper: no long position for %s", symbolNorm)
	}
	closeQty := quantity
	if closeQty <= 0 || closeQty > target.Quantity {
		closeQty = target.Quantity
	}
	var exitPrice float64
	if t.gridSource != nil {
		bids, _, err := t.gridSource.GetOrderBook(symbolNorm, defaultOrderBookDepth)
		if err == nil && len(bids) > 0 {
			exitPrice, _ = t.simulateSellFromBook(bids, closeQty)
		}
	}
	if exitPrice <= 0 {
		exitPrice, err = t.priceSource.GetMarketPrice(symbolNorm)
		if err != nil || exitPrice <= 0 {
			return nil, fmt.Errorf("paper: cannot get price for %s: %w", symbolNorm, err)
		}
	}
	rate := t.getTakerRate(symbolNorm)
	fee := exitPrice * closeQty * rate
	realizedPnL := (exitPrice - target.EntryPrice) * closeQty
	realizedPnL -= fee
	lev := target.Leverage
	if lev <= 0 {
		lev = 1
	}
	marginReleased := (target.EntryPrice * closeQty) / float64(lev)
	_, _, err = t.paperStore.ReducePosition(target.ID, closeQty, exitPrice, realizedPnL, fee, "manual")
	if err != nil {
		return nil, err
	}
	acc, _ = t.paperStore.GetAccountByUserAndTrader(t.userID, t.traderID)
	if acc == nil {
		return nil, fmt.Errorf("paper: account not found")
	}
	newBalance := acc.Balance + marginReleased + realizedPnL
	if err := t.paperStore.UpdateBalance(acc.ID, newBalance); err != nil {
		return nil, err
	}
	logger.Infof("📄 [Paper] CloseLong %s closeQty=%.6f exit=%.4f realizedPnL=%.4f marginReleased=%.2f balance %.2f→%.2f", symbolNorm, closeQty, exitPrice, realizedPnL, marginReleased, acc.Balance, newBalance)
	return map[string]interface{}{
		"orderId":      fmt.Sprintf("paper-close-%d", time.Now().UnixNano()),
		"avgPrice":     exitPrice,
		"executedQty":  closeQty,
		"commission":   fee,
		"realizedPnl":  realizedPnL,
	}, nil
}

// CloseShort 平空：盘口模拟买入，更新余额并关闭/减持仓
func (t *Trader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	symbolNorm := market.Normalize(symbol)
	t.mu.Lock()
	defer t.mu.Unlock()
	acc, err := t.ensureAccount()
	if err != nil {
		return nil, err
	}
	positions, err := t.paperStore.GetOpenPositions(acc.ID)
	if err != nil {
		return nil, err
	}
	var target *store.PaperPosition
	for _, p := range positions {
		if p.Symbol == symbolNorm && p.Side == "short" {
			target = p
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("paper: no short position for %s", symbolNorm)
	}
	closeQty := quantity
	if closeQty <= 0 || closeQty > target.Quantity {
		closeQty = target.Quantity
	}
	var exitPrice float64
	if t.gridSource != nil {
		_, asks, err := t.gridSource.GetOrderBook(symbolNorm, defaultOrderBookDepth)
		if err == nil && len(asks) > 0 {
			exitPrice, _ = t.simulateBuyFromBook(asks, closeQty)
		}
	}
	if exitPrice <= 0 {
		exitPrice, err = t.priceSource.GetMarketPrice(symbolNorm)
		if err != nil || exitPrice <= 0 {
			return nil, fmt.Errorf("paper: cannot get price for %s: %w", symbolNorm, err)
		}
	}
	rate := t.getTakerRate(symbolNorm)
	fee := exitPrice * closeQty * rate
	realizedPnL := (target.EntryPrice - exitPrice) * closeQty
	realizedPnL -= fee
	lev := target.Leverage
	if lev <= 0 {
		lev = 1
	}
	marginReleased := (target.EntryPrice * closeQty) / float64(lev)
	_, _, err = t.paperStore.ReducePosition(target.ID, closeQty, exitPrice, realizedPnL, fee, "manual")
	if err != nil {
		return nil, err
	}
	acc, _ = t.paperStore.GetAccountByUserAndTrader(t.userID, t.traderID)
	if acc == nil {
		return nil, fmt.Errorf("paper: account not found")
	}
	newBalance := acc.Balance + marginReleased + realizedPnL
	if err := t.paperStore.UpdateBalance(acc.ID, newBalance); err != nil {
		return nil, err
	}
	logger.Infof("📄 [Paper] CloseShort %s closeQty=%.6f exit=%.4f realizedPnL=%.4f marginReleased=%.2f balance %.2f→%.2f", symbolNorm, closeQty, exitPrice, realizedPnL, marginReleased, acc.Balance, newBalance)
	return map[string]interface{}{
		"orderId":     fmt.Sprintf("paper-close-%d", time.Now().UnixNano()),
		"avgPrice":    exitPrice,
		"executedQty": closeQty,
		"commission":  fee,
		"realizedPnl": realizedPnL,
	}, nil
}

func (t *Trader) SetLeverage(symbol string, leverage int) error   { return nil }
func (t *Trader) SetMarginMode(symbol string, isCrossMargin bool) error { return nil }
func (t *Trader) SetStopLoss(symbol, positionSide string, quantity, stopPrice float64) error { return nil }
func (t *Trader) SetTakeProfit(symbol, positionSide string, quantity, takeProfitPrice float64) error { return nil }
func (t *Trader) CancelStopLossOrders(symbol string) error   { return nil }
func (t *Trader) CancelTakeProfitOrders(symbol string) error { return nil }
func (t *Trader) CancelAllOrders(symbol string) error       { return nil }
func (t *Trader) CancelStopOrders(symbol string) error       { return nil }
func (t *Trader) InvalidateCache()                           {}

func (t *Trader) GetMarketPrice(symbol string) (float64, error) {
	return t.priceSource.GetMarketPrice(symbol)
}

func (t *Trader) FormatQuantity(symbol string, quantity float64) (string, error) {
	return t.priceSource.FormatQuantity(symbol, quantity)
}

func (t *Trader) GetOrderStatus(symbol, orderID string) (map[string]interface{}, error) {
	return map[string]interface{}{
		"status":     "FILLED",
		"avgPrice":   0,
		"executedQty": 0,
		"commission": 0,
	}, nil
}

// GetClosedPnL 从 paper_trades 表返回近期平仓记录，与实盘接口一致，便于同步余额/盈亏与最近交易展示
func (t *Trader) GetClosedPnL(startTime time.Time, limit int) ([]types.ClosedPnLRecord, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	sinceMs := startTime.UTC().UnixMilli()
	list, err := t.paperStore.ListTrades(t.userID, t.traderID, sinceMs, limit)
	if err != nil {
		return nil, err
	}
	out := make([]types.ClosedPnLRecord, 0, len(list))
	for _, tr := range list {
		entryTime := time.UnixMilli(tr.EntryTime).UTC()
		if tr.EntryTime == 0 {
			entryTime = time.UnixMilli(tr.ExitTime - 1).UTC()
		}
		out = append(out, types.ClosedPnLRecord{
			Symbol:      tr.Symbol,
			Side:        tr.Side,
			EntryPrice:  tr.EntryPrice,
			ExitPrice:   tr.ExitPrice,
			Quantity:    tr.Quantity,
			RealizedPnL: tr.RealizedPnL,
			Fee:         tr.Fee,
			Leverage:    tr.Leverage,
			EntryTime:   entryTime,
			ExitTime:    time.UnixMilli(tr.ExitTime).UTC(),
			OrderID:     fmt.Sprintf("paper-%d", tr.ID),
			CloseType:   tr.CloseReason,
			ExchangeID:  "",
		})
	}
	return out, nil
}

func (t *Trader) GetOpenOrders(symbol string) ([]types.OpenOrder, error) {
	return nil, nil
}
