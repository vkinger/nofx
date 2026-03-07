// Package compliance: 风控官输入输出约定（P2-1）
package compliance

import (
	"nofx/kernel"
	"nofx/store"
)

// ComplianceInput 风控官输入：本轮回测的 thinking + decisions + 账户/持仓快照 + 风控规则
type ComplianceInput struct {
	Thinking   string             `json:"thinking"`   // 交易员 CoT / thinking
	Decisions  []kernel.Decision  `json:"decisions"`  // 待执行决策
	Account    AccountSnapshot    `json:"account"`    // 账户快照
	Positions  []PositionSnapshot `json:"positions"`  // 持仓快照
	Rules      ComplianceRules    `json:"rules"`      // 风控规则（杠杆、仓位占比、排除币种等）
	AnalystBias string            `json:"analyst_bias,omitempty"`  // 可选：本轮分析师偏向，用于高风险判断
	AnalystConfidence int         `json:"analyst_confidence,omitempty"`
	MarketType string             `json:"market_type,omitempty"`   // P3-1: crypto_perpetual | crypto_spot，影响提示与规则侧重
}

// AccountSnapshot 账户快照（与 store.AccountSnapshot 对齐）
type AccountSnapshot struct {
	TotalEquity          float64 `json:"total_equity"`
	AvailableBalance     float64 `json:"available_balance"`
	TotalUnrealizedProfit float64 `json:"total_unrealized_profit"`
	PositionCount        int     `json:"position_count"`
	InitialBalance       float64 `json:"initial_balance,omitempty"`
}

// PositionSnapshot 持仓快照（与 store.PositionSnapshot 对齐）
type PositionSnapshot struct {
	Symbol           string  `json:"symbol"`
	Side             string  `json:"side"`
	PositionAmt      float64 `json:"position_amt"`
	EntryPrice       float64 `json:"entry_price"`
	MarkPrice        float64 `json:"mark_price"`
	UnrealizedProfit float64 `json:"unrealized_profit"`
	Leverage         float64 `json:"leverage"`
	LiquidationPrice float64 `json:"liquidation_price"`
}

// ComplianceRules 风控规则（从策略 RiskControl + 排除币种 等来）
type ComplianceRules struct {
	MaxPositions                    int      `json:"max_positions"`
	BTCETHMaxLeverage               int      `json:"btc_eth_max_leverage"`
	AltcoinMaxLeverage              int      `json:"altcoin_max_leverage"`
	BTCETHMaxPositionValueRatio     float64  `json:"btc_eth_max_position_value_ratio"`
	AltcoinMaxPositionValueRatio    float64  `json:"altcoin_max_position_value_ratio"`
	MaxMarginUsage                  float64  `json:"max_margin_usage"`
	MinPositionSize                 float64  `json:"min_position_size"`
	MinConfidence                   int      `json:"min_confidence"`
	ExcludedCoins                   []string `json:"excluded_coins,omitempty"`
	MaxSinglePositionSizeUSD        float64  `json:"max_single_position_size_usd,omitempty"` // 大额阈值，超过可触发 Debate
}

// FromStrategyRiskControl 从策略配置填充风控规则
func (r *ComplianceRules) FromStrategyRiskControl(cfg *store.StrategyConfig) {
	if cfg == nil {
		return
	}
	rc := cfg.RiskControl
	r.MaxPositions = rc.MaxPositions
	r.BTCETHMaxLeverage = rc.BTCETHMaxLeverage
	r.AltcoinMaxLeverage = rc.AltcoinMaxLeverage
	r.BTCETHMaxPositionValueRatio = rc.BTCETHMaxPositionValueRatio
	r.AltcoinMaxPositionValueRatio = rc.AltcoinMaxPositionValueRatio
	r.MaxMarginUsage = rc.MaxMarginUsage
	r.MinPositionSize = rc.MinPositionSize
	r.MinConfidence = rc.MinConfidence
	if cfg.CoinSource.ExcludedCoins != nil {
		r.ExcludedCoins = cfg.CoinSource.ExcludedCoins
	}
	if r.MaxSinglePositionSizeUSD <= 0 {
		r.MaxSinglePositionSizeUSD = 50000 // 默认 5 万 USDT 单笔视为大额
	}
}

// ApplyMarketTypeToComplianceRules P3-4 环境感知：按 market_type 调整风控规则（现货无杠杆、合约用策略杠杆）
func ApplyMarketTypeToComplianceRules(r *ComplianceRules, marketType string) {
	if r == nil {
		return
	}
	switch marketType {
	case store.MarketTypeCryptoSpot:
		r.BTCETHMaxLeverage = 1
		r.AltcoinMaxLeverage = 1
		// 现货无清算/保证金率概念，杠杆相关规则已置 1
	default:
		// crypto_perpetual 或空：保持策略配置
	}
}

// ComplianceOutput 风控官输出：approved, reason, 可选 violations；P2-6 可选 force_actions
type ComplianceOutput struct {
	Approved    bool     `json:"approved"`
	Reason      string   `json:"reason"`
	Violations  []string `json:"violations,omitempty"`
	ForceActions []ForceAction `json:"force_actions,omitempty"` // P2-6 强制指令：执行层优先执行
}

// ForceAction 风控官强制指令（P2-6，如 reduce_position, close_all）；执行层优先执行
type ForceAction struct {
	Action string  `json:"action"` // reduce_position, close_all, pause_trading, ...
	Symbol string  `json:"symbol,omitempty"`
	Param  float64 `json:"param,omitempty"`
}

// ShouldTriggerDebate P2-5 扩展点：是否应触发 Debate（高风险/大额）；调用方可根据此决定是否创建 Debate 会话再执行
func ShouldTriggerDebate(input *ComplianceInput, maxPositionSizeUSD float64) bool {
	if input == nil || maxPositionSizeUSD <= 0 {
		maxPositionSizeUSD = 50000
	}
	for _, d := range input.Decisions {
		if d.PositionSizeUSD > maxPositionSizeUSD {
			return true
		}
		if (d.Action == "open_long" || d.Action == "open_short") && d.Confidence > 85 {
			if input.AnalystBias != "" && input.AnalystBias != "neutral" {
				return true
			}
		}
	}
	return false
}
