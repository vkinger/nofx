package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"nofx/logger"
	"nofx/market"
	"nofx/mcp"
	"nofx/provider/coinank"
	"nofx/provider/coinank/coinank_enum"
	"nofx/provider/nofxos"
	"nofx/security"
	"nofx/store"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// ============================================================================
// Pre-compiled regular expressions (performance optimization)
// ============================================================================

var (
	// Safe regex: precisely match ```json code blocks
	reJSONFence      = regexp.MustCompile(`(?is)` + "```json\\s*(\\[\\s*\\{.*?\\}\\s*\\])\\s*```")
	reJSONArray      = regexp.MustCompile(`(?is)\[\s*\{.*?\}\s*\]`)
	reArrayHead      = regexp.MustCompile(`^\[\s*\{`)
	reArrayOpenSpace = regexp.MustCompile(`^\[\s+\{`)
	reInvisibleRunes = regexp.MustCompile("[\u200B\u200C\u200D\uFEFF]")

	// XML tag extraction: use "thinking" for top-level 思维分析 to avoid conflict with decision-level "reasoning"
	reThinkingTag  = regexp.MustCompile(`(?s)<thinking>(.*?)</thinking>`)
	reReasoningTag = regexp.MustCompile(`(?s)<reasoning>(.*?)</reasoning>`) // legacy
	reDecisionTag  = regexp.MustCompile(`(?s)<decision>(.*?)</decision>`)
)

// ============================================================================
// Token Estimation
// ============================================================================

// EstimateTokenCount 估算文本的token数量
// 使用简化的估算方法：
// - 中文字符：约1.5字符=1token
// - 英文字符和数字：约4字符=1token
// - 混合文本：使用加权平均估算
func EstimateTokenCount(text string) int {
	if len(text) == 0 {
		return 0
	}

	// 计算中文字符数量（UTF-8中中文字符通常占3字节）
	chineseCharCount := 0
	totalRunes := utf8.RuneCountInString(text)

	for _, r := range text {
		// 中文字符Unicode范围：0x4E00-0x9FFF（基本），0x3400-0x4DBF（扩展A）
		if r >= 0x4E00 && r <= 0x9FFF {
			chineseCharCount++
		}
	}

	// 估算token数
	// 中文字符：1.5字符/1token，非中文字符：4字符/1token
	nonChineseCount := totalRunes - chineseCharCount
	estimatedTokens := int(float64(chineseCharCount)/1.5 + float64(nonChineseCount)/4.0)

	// 至少返回总字符数的1/3（保守估算）
	minTokens := totalRunes / 3
	if estimatedTokens < minTokens {
		estimatedTokens = minTokens
	}

	return estimatedTokens
}

// ============================================================================
// Type Definitions
// ============================================================================

// PositionInfo position information
type PositionInfo struct {
	Symbol           string  `json:"symbol"`
	Side             string  `json:"side"` // "long" or "short"
	EntryPrice       float64 `json:"entry_price"`
	MarkPrice        float64 `json:"mark_price"`
	Quantity         float64 `json:"quantity"`
	Leverage         int     `json:"leverage"`
	UnrealizedPnL    float64 `json:"unrealized_pnl"`
	UnrealizedPnLPct float64 `json:"unrealized_pnl_pct"`
	PeakPnLPct       float64 `json:"peak_pnl_pct"` // Historical peak profit percentage
	LiquidationPrice float64 `json:"liquidation_price"`
	MarginUsed       float64 `json:"margin_used"`
	UpdateTime       int64   `json:"update_time"` // Position update timestamp (milliseconds)
}

// AccountInfo account information
type AccountInfo struct {
	TotalEquity      float64 `json:"total_equity"`      // Account equity
	AvailableBalance float64 `json:"available_balance"` // Available balance
	UnrealizedPnL    float64 `json:"unrealized_pnl"`    // Unrealized profit/loss
	TotalPnL         float64 `json:"total_pnl"`         // Total profit/loss
	TotalPnLPct      float64 `json:"total_pnl_pct"`     // Total profit/loss percentage
	MarginUsed       float64 `json:"margin_used"`       // Used margin
	MarginUsedPct    float64 `json:"margin_used_pct"`   // Margin usage rate
	PositionCount    int     `json:"position_count"`    // Number of positions
}

// CandidateCoin candidate coin (from coin pool)
type CandidateCoin struct {
	Symbol  string   `json:"symbol"`
	Sources []string `json:"sources"` // Sources: "ai500" and/or "oi_top"
}

// OITopData open interest growth top data (for AI decision reference)
type OITopData struct {
	Rank              int     // OI Top ranking
	OIDeltaPercent    float64 // Open interest change percentage (1 hour)
	OIDeltaValue      float64 // Open interest change value
	PriceDeltaPercent float64 // Price change percentage
}

// TradingStats trading statistics (for AI input)
type TradingStats struct {
	TotalTrades    int     `json:"total_trades"`     // Total number of trades (closed)
	WinRate        float64 `json:"win_rate"`         // Win rate (%)
	ProfitFactor   float64 `json:"profit_factor"`    // Profit factor
	SharpeRatio    float64 `json:"sharpe_ratio"`     // Sharpe ratio
	TotalPnL       float64 `json:"total_pnl"`        // Total profit/loss
	AvgWin         float64 `json:"avg_win"`          // Average win
	AvgLoss        float64 `json:"avg_loss"`         // Average loss
	MaxDrawdownPct float64 `json:"max_drawdown_pct"` // Maximum drawdown (%)
}

// RecentOrder recently completed order (for AI input)
type RecentOrder struct {
	Symbol       string  `json:"symbol"`        // Trading pair
	Side         string  `json:"side"`          // long/short
	EntryPrice   float64 `json:"entry_price"`   // Entry price
	ExitPrice    float64 `json:"exit_price"`    // Exit price
	RealizedPnL  float64 `json:"realized_pnl"`  // Realized profit/loss
	PnLPct       float64 `json:"pnl_pct"`       // Profit/loss percentage
	EntryTime    string  `json:"entry_time"`    // Entry time
	ExitTime     string  `json:"exit_time"`     // Exit time
	HoldDuration string  `json:"hold_duration"` // Hold duration, e.g. "2h30m"
}

// Context trading context (complete information passed to AI)
type Context struct {
	CurrentTime         string                             `json:"current_time"`
	RuntimeMinutes      int                                `json:"runtime_minutes"`
	CallCount           int                                `json:"call_count"`
	Account             AccountInfo                        `json:"account"`
	Positions           []PositionInfo                     `json:"positions"`
	CandidateCoins      []CandidateCoin                    `json:"candidate_coins"`
	PromptVariant       string                             `json:"prompt_variant,omitempty"`
	TradingStats        *TradingStats                      `json:"trading_stats,omitempty"`
	RecentOrders        []RecentOrder                      `json:"recent_orders,omitempty"`
	MarketDataMap       map[string]*market.Data            `json:"-"`
	MultiTFMarket       map[string]map[string]*market.Data `json:"-"`
	OITopDataMap        map[string]*OITopData              `json:"-"`
	QuantDataMap        map[string]*QuantData              `json:"-"`
	OIRankingData       *nofxos.OIRankingData              `json:"-"` // Market-wide OI ranking data
	NetFlowRankingData  *nofxos.NetFlowRankingData         `json:"-"` // Market-wide fund flow ranking data
	PriceRankingData    *nofxos.PriceRankingData           `json:"-"` // Market-wide price gainers/losers
	BTCETHLeverage      int                                `json:"-"`
	AltcoinLeverage     int                                `json:"-"`
	Timeframes          []string                           `json:"-"`
	ExchangeCredentials *market.ExchangeCredentials        `json:"-"` // Optional exchange credentials for fetching accurate trading fees
	Exchange            string                             `json:"-"` // Exchange type for market data (binance, bybit, okx, hyperliquid, etc.); used so K-line/funding/OI match trading venue
	RealtimePriceGetter func(symbol string) (float64, error) `json:"-"` // Optional: if set, used to fill RealtimePrice after market data fetch (e.g. exchange ticker for execution reference)
	RealtimePrice       map[string]float64                  `json:"-"` // Symbol -> live ticker price; filled when RealtimePriceGetter is set (for prompt: "实时价")
	// P3-5 可选：同一 base 现货+合约双流价差/基差，key=base（如 "BTC"），value=perp_price - spot_price
	SpotPerpBasis map[string]float64 `json:"-"`
}

// Decision AI trading decision
//
// 开仓(open_long/open_short)必填：symbol, action, reasoning, leverage, position_size_usd, stop_loss, take_profit, confidence；
// 平仓(close_long/close_short)/hold/wait 必填：symbol, action, reasoning。
// quantity/price 为网格或限价用，开仓市价单可不填（后端会用 position_size_usd 与 stop_loss/take_profit 推导展示用 quantity/price）。
// 若开仓缺少必填或为 0，validateDecision 会直接报错拒绝。
type Decision struct {
	Symbol string `json:"symbol"`
	Action string `json:"action"` // Standard: "open_long", "open_short", "close_long", "close_short", "hold", "wait"
	// Grid actions: "place_buy_limit", "place_sell_limit", "cancel_order", "cancel_all_orders", "pause_grid", "resume_grid", "adjust_grid"

	// Opening position parameters (REQUIRED when action is open_long or open_short)
	Leverage        int     `json:"leverage,omitempty"`
	PositionSizeUSD float64 `json:"position_size_usd,omitempty"`
	StopLoss        float64 `json:"stop_loss,omitempty"`
	TakeProfit      float64 `json:"take_profit,omitempty"`

	// Grid trading parameters
	Price      float64 `json:"price,omitempty"`       // Limit order price (for grid)
	Quantity   float64 `json:"quantity,omitempty"`   // Order quantity (for grid); for open_* market order often 0, backend may derive from position_size_usd
	LevelIndex int     `json:"level_index,omitempty"` // Grid level index
	OrderID    string  `json:"order_id,omitempty"`    // Order ID (for cancel)

	// Common parameters
	Confidence int     `json:"confidence,omitempty"` // Confidence level (0-100)
	RiskUSD    float64 `json:"risk_usd,omitempty"`   // Maximum USD risk
	Reasoning  string  `json:"reasoning"`
}

// FullDecision AI's complete decision (including chain of thought)
type FullDecision struct {
	SystemPrompt        string     `json:"system_prompt"`
	UserPrompt          string     `json:"user_prompt"`
	CoTTrace            string     `json:"cot_trace"`
	Decisions           []Decision `json:"decisions"`
	RawResponse         string     `json:"raw_response"`
	Timestamp           time.Time  `json:"timestamp"`
	AIRequestDurationMs int64      `json:"ai_request_duration_ms,omitempty"`
}

// QuantData quantitative data structure (fund flow, position changes, price changes)
type QuantData struct {
	Symbol      string             `json:"symbol"`
	Price       float64            `json:"price"`
	Netflow     *NetflowData       `json:"netflow,omitempty"`
	OI          map[string]*OIData `json:"oi,omitempty"`
	PriceChange map[string]float64 `json:"price_change,omitempty"`
}

type NetflowData struct {
	Institution *FlowTypeData `json:"institution,omitempty"`
	Personal    *FlowTypeData `json:"personal,omitempty"`
}

type FlowTypeData struct {
	Future map[string]float64 `json:"future,omitempty"`
	Spot   map[string]float64 `json:"spot,omitempty"`
}

type OIData struct {
	CurrentOI float64                 `json:"current_oi"`
	Delta     map[string]*OIDeltaData `json:"delta,omitempty"`
}

type OIDeltaData struct {
	OIDelta        float64 `json:"oi_delta"`
	OIDeltaValue   float64 `json:"oi_delta_value"`
	OIDeltaPercent float64 `json:"oi_delta_percent"`
}

// ============================================================================
// StrategyEngine - Core Strategy Execution Engine
// ============================================================================

// StrategyEngine strategy execution engine
type StrategyEngine struct {
	config       *store.StrategyConfig
	nofxosClient *nofxos.Client
}

// NewStrategyEngine creates strategy execution engine
func NewStrategyEngine(config *store.StrategyConfig) *StrategyEngine {
	// Create NofxOS client with API key from config
	apiKey := config.Indicators.NofxOSAPIKey
	if apiKey == "" {
		apiKey = nofxos.DefaultAuthKey
	}
	client := nofxos.NewClient(nofxos.DefaultBaseURL, apiKey)

	return &StrategyEngine{
		config:       config,
		nofxosClient: client,
	}
}

// GetRiskControlConfig gets risk control configuration
func (e *StrategyEngine) GetRiskControlConfig() store.RiskControlConfig {
	return e.config.RiskControl
}

// GetLanguage returns the language from config or falls back to auto-detection
func (e *StrategyEngine) GetLanguage() Language {
	switch e.config.Language {
	case "zh":
		return LangChinese
	case "en":
		return LangEnglish
	default:
		// Fall back to auto-detection from prompt content for backward compatibility
		return detectLanguage(e.config.PromptSections.RoleDefinition)
	}
}

// GetConfig gets complete strategy configuration
func (e *StrategyEngine) GetConfig() *store.StrategyConfig {
	return e.config
}

// ============================================================================
// Entry Functions - Main API
// ============================================================================

// GetFullDecision gets AI's complete trading decision (batch analysis of all coins and positions)
// Uses default strategy configuration - for production use GetFullDecisionWithStrategy with explicit config
func GetFullDecision(ctx *Context, mcpClient mcp.AIClient) (*FullDecision, error) {
	defaultConfig := store.GetDefaultStrategyConfig("en")
	engine := NewStrategyEngine(&defaultConfig)
	return GetFullDecisionWithStrategy(ctx, mcpClient, engine, "")
}

// GetFullDecisionWithStrategy uses StrategyEngine to get AI decision (unified prompt generation).
func GetFullDecisionWithStrategy(ctx *Context, mcpClient mcp.AIClient, engine *StrategyEngine, variant string) (*FullDecision, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	if engine == nil {
		defaultConfig := store.GetDefaultStrategyConfig("en")
		engine = NewStrategyEngine(&defaultConfig)
	}

	// 1. Fetch market data using strategy config
	if len(ctx.MarketDataMap) == 0 {
		if err := fetchMarketDataWithStrategy(ctx, engine); err != nil {
			return nil, fmt.Errorf("failed to fetch market data: %w", err)
		}
	}

	// 1.5. Fill realtime prices from exchange (for prompt: 实时价) when getter is provided
	if ctx.RealtimePriceGetter != nil {
		ctx.RealtimePrice = make(map[string]float64)
		for symbol := range ctx.MarketDataMap {
			price, err := ctx.RealtimePriceGetter(symbol)
			if err == nil && price > 0 {
				ctx.RealtimePrice[symbol] = price
			}
		}
	}

	// Ensure OITopDataMap is initialized
	if ctx.OITopDataMap == nil {
		ctx.OITopDataMap = make(map[string]*OITopData)
		oiPositions, err := engine.nofxosClient.GetOITopPositions()
		if err == nil {
			for _, pos := range oiPositions {
				ctx.OITopDataMap[pos.Symbol] = &OITopData{
					Rank:              pos.Rank,
					OIDeltaPercent:    pos.OIDeltaPercent,
					OIDeltaValue:      pos.OIDeltaValue,
					PriceDeltaPercent: pos.PriceDeltaPercent,
				}
			}
		}
	}

	// 2. Build System Prompt
	systemPrompt := engine.BuildSystemPrompt(ctx.Account.TotalEquity, variant, mcpClient)

	// 3. Build User Prompt
	userPrompt := engine.BuildUserPrompt(ctx)

	riskConfig := engine.GetRiskControlConfig()

	// 3.5. 按当前 Agent 角色设置 JSON Schema（交易员），支持 json_schema 的模型会收到对应输出格式
	PrepareClientForRole(mcpClient, "trader", engine.GetLanguage())

	// Calculate estimated token count
	systemTokens := EstimateTokenCount(systemPrompt)
	userTokens := EstimateTokenCount(userPrompt)
	totalTokens := systemTokens + userTokens

	// Log token estimation
	logger.Infof("📊 [Token Estimation] System prompt: ~%d tokens, User prompt: ~%d tokens, Total: ~%d tokens",
		systemTokens, userTokens, totalTokens)

	if totalTokens > 50000 {
		logger.Warnf("⚠️  [Token Warning] Estimated token count (%d) is very high, may exceed model context limit", totalTokens)
	}

	// 4. Call AI API
	aiCallStart := time.Now()
	aiResponse, err := mcpClient.CallWithMessages(systemPrompt, userPrompt)
	aiCallDuration := time.Since(aiCallStart)
	if err != nil {
		return nil, fmt.Errorf("AI API call failed: %w", err)
	}

	// 5. Parse AI response
	decision, err := parseFullDecisionResponse(
		aiResponse,
		ctx.Account.TotalEquity,
		riskConfig.BTCETHMaxLeverage,
		riskConfig.AltcoinMaxLeverage,
		riskConfig.BTCETHMaxPositionValueRatio,
		riskConfig.AltcoinMaxPositionValueRatio,
		engine.GetConfig().CoinSource.ExcludedCoins,
	)

	if decision != nil {
		decision.Timestamp = time.Now()
		decision.SystemPrompt = systemPrompt
		decision.UserPrompt = userPrompt
		decision.AIRequestDurationMs = aiCallDuration.Milliseconds()
		decision.RawResponse = aiResponse
	}

	if err != nil {
		return decision, fmt.Errorf("failed to parse AI response: %w", err)
	}

	return decision, nil
}

// ============================================================================
// Market Data Fetching
// ============================================================================

// fetchMarketDataWithStrategy fetches market data using strategy config (multiple timeframes)
func fetchMarketDataWithStrategy(ctx *Context, engine *StrategyEngine) error {
	config := engine.GetConfig()
	ctx.MarketDataMap = make(map[string]*market.Data)

	timeframes := config.Indicators.Klines.SelectedTimeframes
	primaryTimeframe := config.Indicators.Klines.PrimaryTimeframe
	klineCount := config.Indicators.Klines.PrimaryCount

	// Compatible with old configuration
	if len(timeframes) == 0 {
		if primaryTimeframe != "" {
			timeframes = append(timeframes, primaryTimeframe)
		} else {
			timeframes = append(timeframes, "3m")
		}
		if config.Indicators.Klines.LongerTimeframe != "" {
			timeframes = append(timeframes, config.Indicators.Klines.LongerTimeframe)
		}
	}
	if primaryTimeframe == "" {
		primaryTimeframe = timeframes[0]
	}
	if klineCount <= 0 {
		klineCount = 50
	}

	logger.Infof("📊 Strategy timeframes: %v, Primary: %s, Kline count: %d", timeframes, primaryTimeframe, klineCount)

	exchange := ctx.Exchange
	if exchange == "" {
		exchange = "binance"
	}
	// 1. First fetch data for position coins (must fetch), using trader's exchange
	for _, pos := range ctx.Positions {
		data, err := market.GetWithTimeframes(pos.Symbol, exchange, timeframes, primaryTimeframe, klineCount)
		if err != nil {
			logger.Infof("⚠️  Failed to fetch market data for position %s: %v", pos.Symbol, err)
			continue
		}
		ctx.MarketDataMap[pos.Symbol] = data
	}

	// 2. Fetch data for all candidate coins
	positionSymbols := make(map[string]bool)
	for _, pos := range ctx.Positions {
		positionSymbols[pos.Symbol] = true
	}

	const minOIThresholdMillions = 15.0 // 15M USD minimum open interest value

	for _, coin := range ctx.CandidateCoins {
		if _, exists := ctx.MarketDataMap[coin.Symbol]; exists {
			continue
		}

		data, err := market.GetWithTimeframes(coin.Symbol, exchange, timeframes, primaryTimeframe, klineCount)
		if err != nil {
			logger.Infof("⚠️  Failed to fetch market data for %s: %v", coin.Symbol, err)
			continue
		}

		// Liquidity filter (skip for xyz dex assets - they don't have OI data from Binance)
		isExistingPosition := positionSymbols[coin.Symbol]
		isXyzAsset := market.IsXyzDexAsset(coin.Symbol)
		if !isExistingPosition && !isXyzAsset && data.OpenInterest != nil && data.CurrentPrice > 0 {
			oiValue := data.OpenInterest.Latest * data.CurrentPrice
			oiValueInMillions := oiValue / 1_000_000
			if oiValueInMillions < minOIThresholdMillions {
				logger.Infof("⚠️  %s OI value too low (%.2fM USD < %.1fM), skipping coin",
					coin.Symbol, oiValueInMillions, minOIThresholdMillions)
				continue
			}
		}

		ctx.MarketDataMap[coin.Symbol] = data
	}

	// 3. If exchange credentials are provided, update trading fees with accurate data from exchange
	if ctx.ExchangeCredentials != nil && ctx.ExchangeCredentials.APIKey != "" {
		logger.Infof("🔐 Using exchange credentials (%s) to fetch accurate trading fees", ctx.ExchangeCredentials.ExchangeType)
		for symbol, data := range ctx.MarketDataMap {
			makerRate, takerRate, source := market.FetchTradingFeeRates(symbol, ctx.ExchangeCredentials)
			data.MakerFeeRate = makerRate
			data.TakerFeeRate = takerRate
			data.FeeSource = source
		}
	}

	// 4. Fetch contract-specific data: liquidation + long/short ratio (via CoinAnk authenticated API)
	apiKey := engine.config.Indicators.NofxOSAPIKey
	if apiKey == "" {
		apiKey = nofxos.DefaultAuthKey
	}
	if apiKey != "" {
		coinankClient := coinank.NewCoinankClient(coinank_enum.MainUrl, apiKey)
		fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer fetchCancel()

		// Batch fetch long/short rank (one call for all symbols)
		lsItems, lsErr := coinankClient.LongShortRank(fetchCtx, coinank_enum.LongShortPerson, coinank_enum.Desc, 1, 100)
		lsMap := make(map[string]*market.LongShortInfo)
		if lsErr == nil {
			for _, item := range lsItems {
				lsMap[strings.ToUpper(item.BaseCoin)] = &market.LongShortInfo{
					Ratio:  item.LongShortPerson,
					Chg5m:  item.LsPersonChg5M,
					Chg15m: item.LsPersonChg15M,
					Chg1h:  item.LsPersonChg1H,
					Chg4h:  item.LsPersonChg4H,
				}
			}
		}

		for symbol, data := range ctx.MarketDataMap {
			if market.IsXyzDexAsset(symbol) {
				continue
			}
			baseCoin := strings.TrimSuffix(strings.ToUpper(symbol), "USDT")

			// Long/Short ratio (from batch result)
			if ls, ok := lsMap[baseCoin]; ok {
				data.LongShortRatio = ls
			}

			// Liquidation data (per-symbol)
			liqData, liqErr := coinankClient.LiquidationExchangeStatistics(fetchCtx, baseCoin)
			if liqErr == nil && liqData != nil {
				data.LiquidationData = &market.LiquidationInfo{
					Liq1hTotal:  liqData.OneH.TotalTurnover,
					Liq1hLong:   liqData.OneH.LongTurnover,
					Liq1hShort:  liqData.OneH.ShortTurnover,
					Liq24hTotal: liqData.Two4H.TotalTurnover,
					Liq24hLong:  liqData.Two4H.LongTurnover,
					Liq24hShort: liqData.Two4H.ShortTurnover,
				}
			}
		}
	}

	logger.Infof("📊 Successfully fetched multi-timeframe market data for %d coins", len(ctx.MarketDataMap))
	return nil
}

// ============================================================================
// Candidate Coins
// ============================================================================

// GetCandidateCoins gets candidate coins based on strategy configuration
func (e *StrategyEngine) GetCandidateCoins() ([]CandidateCoin, error) {
	var candidates []CandidateCoin
	symbolSources := make(map[string][]string)

	coinSource := e.config.CoinSource

	switch coinSource.SourceType {
	case "static":
		for _, symbol := range coinSource.StaticCoins {
			symbol = market.Normalize(symbol)
			candidates = append(candidates, CandidateCoin{
				Symbol:  symbol,
				Sources: []string{"static"},
			})
		}

		return e.filterExcludedCoins(candidates), nil

	case "ai500":
		// 检查 use_ai500 标志，如果为 false 则回退到静态币种
		if !coinSource.UseAI500 {
			logger.Infof("⚠️  source_type is 'ai500' but use_ai500 is false, falling back to static coins")
			for _, symbol := range coinSource.StaticCoins {
				symbol = market.Normalize(symbol)
				candidates = append(candidates, CandidateCoin{
					Symbol:  symbol,
					Sources: []string{"static"},
				})
			}
			return e.filterExcludedCoins(candidates), nil
		}
		coins, err := e.getAI500Coins(coinSource.AI500Limit)
		if err != nil {
			return nil, err
		}
		// 空列表是正常情况，直接返回
		return e.filterExcludedCoins(coins), nil

	case "oi_top":
		// 检查 use_oi_top 标志，如果为 false 则回退到静态币种
		if !coinSource.UseOITop {
			logger.Infof("⚠️  source_type is 'oi_top' but use_oi_top is false, falling back to static coins")
			for _, symbol := range coinSource.StaticCoins {
				symbol = market.Normalize(symbol)
				candidates = append(candidates, CandidateCoin{
					Symbol:  symbol,
					Sources: []string{"static"},
				})
			}
			return e.filterExcludedCoins(candidates), nil
		}
		coins, err := e.getOITopCoins(coinSource.OITopLimit)
		if err != nil {
			return nil, err
		}
		// 空列表是正常情况，直接返回
		return e.filterExcludedCoins(coins), nil

	case "oi_low":
		// 持仓减少榜，适合做空
		if !coinSource.UseOILow {
			logger.Infof("⚠️  source_type is 'oi_low' but use_oi_low is false, falling back to static coins")
			for _, symbol := range coinSource.StaticCoins {
				symbol = market.Normalize(symbol)
				candidates = append(candidates, CandidateCoin{
					Symbol:  symbol,
					Sources: []string{"static"},
				})
			}
			return e.filterExcludedCoins(candidates), nil
		}
		coins, err := e.getOILowCoins(coinSource.OILowLimit)
		if err != nil {
			return nil, err
		}
		// 空列表是正常情况，直接返回
		return e.filterExcludedCoins(coins), nil

	case "mixed":
		if coinSource.UseAI500 {
			poolCoins, err := e.getAI500Coins(coinSource.AI500Limit)
			if err != nil {
				logger.Infof("⚠️  Failed to get AI500 coins: %v", err)
			} else {
				for _, coin := range poolCoins {
					symbolSources[coin.Symbol] = append(symbolSources[coin.Symbol], "ai500")
				}
			}
		}

		if coinSource.UseOITop {
			oiCoins, err := e.getOITopCoins(coinSource.OITopLimit)
			if err != nil {
				logger.Infof("⚠️  Failed to get OI Top: %v", err)
			} else {
				for _, coin := range oiCoins {
					symbolSources[coin.Symbol] = append(symbolSources[coin.Symbol], "oi_top")
				}
			}
		}

		if coinSource.UseOILow {
			oiLowCoins, err := e.getOILowCoins(coinSource.OILowLimit)
			if err != nil {
				logger.Infof("⚠️  Failed to get OI Low: %v", err)
			} else {
				for _, coin := range oiLowCoins {
					symbolSources[coin.Symbol] = append(symbolSources[coin.Symbol], "oi_low")
				}
			}
		}

		for _, symbol := range coinSource.StaticCoins {
			symbol = market.Normalize(symbol)
			if _, exists := symbolSources[symbol]; !exists {
				symbolSources[symbol] = []string{"static"}
			} else {
				symbolSources[symbol] = append(symbolSources[symbol], "static")
			}
		}

		for symbol, sources := range symbolSources {
			candidates = append(candidates, CandidateCoin{
				Symbol:  symbol,
				Sources: sources,
			})
		}
		return e.filterExcludedCoins(candidates), nil

	default:
		return nil, fmt.Errorf("unknown coin source type: %s", coinSource.SourceType)
	}
}

// filterExcludedCoins removes excluded coins from the candidates list
func (e *StrategyEngine) filterExcludedCoins(candidates []CandidateCoin) []CandidateCoin {
	if len(e.config.CoinSource.ExcludedCoins) == 0 {
		return candidates
	}

	// Build excluded set for O(1) lookup
	excluded := make(map[string]bool)
	for _, coin := range e.config.CoinSource.ExcludedCoins {
		normalized := market.Normalize(coin)
		excluded[normalized] = true
	}

	// Filter out excluded coins
	filtered := make([]CandidateCoin, 0, len(candidates))
	for _, c := range candidates {
		if !excluded[c.Symbol] {
			filtered = append(filtered, c)
		} else {
			logger.Infof("🚫 Excluded coin: %s", c.Symbol)
		}
	}

	return filtered
}

func (e *StrategyEngine) getAI500Coins(limit int) ([]CandidateCoin, error) {
	if limit <= 0 {
		limit = 30
	}

	symbols, err := e.nofxosClient.GetTopRatedCoins(limit)
	if err != nil {
		return nil, err
	}

	var candidates []CandidateCoin
	for _, symbol := range symbols {
		candidates = append(candidates, CandidateCoin{
			Symbol:  symbol,
			Sources: []string{"ai500"},
		})
	}
	return candidates, nil
}

func (e *StrategyEngine) getOITopCoins(limit int) ([]CandidateCoin, error) {
	if limit <= 0 {
		limit = 10
	}

	positions, err := e.nofxosClient.GetOITopPositions()
	if err != nil {
		return nil, err
	}

	var candidates []CandidateCoin
	for i, pos := range positions {
		if i >= limit {
			break
		}
		symbol := market.Normalize(pos.Symbol)
		candidates = append(candidates, CandidateCoin{
			Symbol:  symbol,
			Sources: []string{"oi_top"},
		})
	}
	return candidates, nil
}

func (e *StrategyEngine) getOILowCoins(limit int) ([]CandidateCoin, error) {
	if limit <= 0 {
		limit = 10
	}

	positions, err := e.nofxosClient.GetOILowPositions()
	if err != nil {
		return nil, err
	}

	var candidates []CandidateCoin
	for i, pos := range positions {
		if i >= limit {
			break
		}
		symbol := market.Normalize(pos.Symbol)
		candidates = append(candidates, CandidateCoin{
			Symbol:  symbol,
			Sources: []string{"oi_low"},
		})
	}
	return candidates, nil
}

// ============================================================================
// External & Quant Data
// ============================================================================

// FetchMarketData fetches market data based on strategy configuration
func (e *StrategyEngine) FetchMarketData(symbol string) (*market.Data, error) {
	return market.Get(symbol)
}

// FetchExternalData fetches external data sources
func (e *StrategyEngine) FetchExternalData() (map[string]interface{}, error) {
	externalData := make(map[string]interface{})

	for _, source := range e.config.Indicators.ExternalDataSources {
		data, err := e.fetchSingleExternalSource(source)
		if err != nil {
			logger.Infof("⚠️  Failed to fetch external data source [%s]: %v", source.Name, err)
			continue
		}
		externalData[source.Name] = data
	}

	return externalData, nil
}

func (e *StrategyEngine) fetchSingleExternalSource(source store.ExternalDataSource) (interface{}, error) {
	// SSRF Protection: Validate URL before making request
	if err := security.ValidateURL(source.URL); err != nil {
		return nil, fmt.Errorf("external source URL validation failed: %w", err)
	}

	timeout := time.Duration(source.RefreshSecs) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	// Use SSRF-safe HTTP client
	client := security.SafeHTTPClient(timeout)

	req, err := http.NewRequest(source.Method, source.URL, nil)
	if err != nil {
		return nil, err
	}

	for k, v := range source.Headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if source.DataPath != "" {
		result = extractJSONPath(result, source.DataPath)
	}

	return result, nil
}

func extractJSONPath(data interface{}, path string) interface{} {
	parts := strings.Split(path, ".")
	current := data

	for _, part := range parts {
		if m, ok := current.(map[string]interface{}); ok {
			current = m[part]
		} else {
			return nil
		}
	}

	return current
}

// FetchQuantData fetches quantitative data for a single coin
func (e *StrategyEngine) FetchQuantData(symbol string) (*QuantData, error) {
	if !e.config.Indicators.EnableQuantData {
		return nil, nil
	}

	// Use nofxos client with unified API key
	include := "oi,price"
	if e.config.Indicators.EnableQuantNetflow {
		include = "netflow,oi,price"
	}

	nofxosData, err := e.nofxosClient.GetCoinData(symbol, include)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch quant data: %w", err)
	}

	if nofxosData == nil {
		return nil, nil
	}

	// Convert nofxos.QuantData to kernel.QuantData
	quantData := &QuantData{
		Symbol:      nofxosData.Symbol,
		Price:       nofxosData.Price,
		PriceChange: nofxosData.PriceChange,
	}

	// Convert OI data
	if nofxosData.OI != nil {
		quantData.OI = make(map[string]*OIData)
		for exchange, oiData := range nofxosData.OI {
			if oiData != nil {
				kData := &OIData{
					CurrentOI: oiData.CurrentOI,
				}
				if oiData.Delta != nil {
					kData.Delta = make(map[string]*OIDeltaData)
					for dur, delta := range oiData.Delta {
						if delta != nil {
							kData.Delta[dur] = &OIDeltaData{
								OIDelta:        delta.OIDelta,
								OIDeltaValue:   delta.OIDeltaValue,
								OIDeltaPercent: delta.OIDeltaPercent,
							}
						}
					}
				}
				quantData.OI[exchange] = kData
			}
		}
	}

	// Convert Netflow data
	if nofxosData.Netflow != nil {
		quantData.Netflow = &NetflowData{}
		if nofxosData.Netflow.Institution != nil {
			quantData.Netflow.Institution = &FlowTypeData{
				Future: nofxosData.Netflow.Institution.Future,
				Spot:   nofxosData.Netflow.Institution.Spot,
			}
		}
		if nofxosData.Netflow.Personal != nil {
			quantData.Netflow.Personal = &FlowTypeData{
				Future: nofxosData.Netflow.Personal.Future,
				Spot:   nofxosData.Netflow.Personal.Spot,
			}
		}
	}

	return quantData, nil
}

// FetchQuantDataBatch batch fetches quantitative data
func (e *StrategyEngine) FetchQuantDataBatch(symbols []string) map[string]*QuantData {
	result := make(map[string]*QuantData)

	if !e.config.Indicators.EnableQuantData {
		return result
	}

	for _, symbol := range symbols {
		data, err := e.FetchQuantData(symbol)
		if err != nil {
			logger.Infof("⚠️  Failed to fetch quantitative data for %s: %v", symbol, err)
			continue
		}
		if data != nil {
			result[symbol] = data
		}
	}

	return result
}

// FetchOIRankingData fetches market-wide OI ranking data
func (e *StrategyEngine) FetchOIRankingData() *nofxos.OIRankingData {
	indicators := e.config.Indicators
	if !indicators.EnableOIRanking {
		return nil
	}

	duration := indicators.OIRankingDuration
	if duration == "" {
		duration = "1h"
	}

	limit := indicators.OIRankingLimit
	if limit <= 0 {
		limit = 15
	}

	logger.Infof("📊 Fetching OI ranking data (duration: %s, limit: %d)", duration, limit)

	data, err := e.nofxosClient.GetOIRanking(duration, limit)
	if err != nil {
		logger.Warnf("⚠️  Failed to fetch OI ranking data: %v", err)
		return nil
	}

	logger.Infof("✓ OI ranking data ready: %d top, %d low positions",
		len(data.TopPositions), len(data.LowPositions))

	return data
}

// FetchNetFlowRankingData fetches market-wide NetFlow ranking data
func (e *StrategyEngine) FetchNetFlowRankingData() *nofxos.NetFlowRankingData {
	indicators := e.config.Indicators
	if !indicators.EnableNetFlowRanking {
		return nil
	}

	duration := indicators.NetFlowRankingDuration
	if duration == "" {
		duration = "1h"
	}

	limit := indicators.NetFlowRankingLimit
	if limit <= 0 {
		limit = 15
	}

	logger.Infof("💰 Fetching NetFlow ranking data (duration: %s, limit: %d)", duration, limit)

	data, err := e.nofxosClient.GetNetFlowRanking(duration, limit)
	if err != nil {
		logger.Warnf("⚠️  Failed to fetch NetFlow ranking data: %v", err)
		return nil
	}

	logger.Infof("✓ NetFlow ranking data ready: inst_in=%d, inst_out=%d, retail_in=%d, retail_out=%d",
		len(data.InstitutionFutureTop), len(data.InstitutionFutureLow),
		len(data.PersonalFutureTop), len(data.PersonalFutureLow))

	return data
}

// FetchPriceRankingData fetches market-wide price ranking data (gainers/losers)
func (e *StrategyEngine) FetchPriceRankingData() *nofxos.PriceRankingData {
	indicators := e.config.Indicators
	if !indicators.EnablePriceRanking {
		return nil
	}

	durations := indicators.PriceRankingDuration
	if durations == "" {
		durations = "1h"
	}

	limit := indicators.PriceRankingLimit
	if limit <= 0 {
		limit = 15
	}

	logger.Infof("📈 Fetching Price ranking data (durations: %s, limit: %d)", durations, limit)

	data, err := e.nofxosClient.GetPriceRanking(durations, limit)
	if err != nil {
		logger.Warnf("⚠️  Failed to fetch Price ranking data: %v", err)
		return nil
	}

	logger.Infof("✓ Price ranking data ready for %d durations", len(data.Durations))

	return data
}

// ============================================================================
// Prompt Building - System Prompt
// ============================================================================

// BuildSystemPrompt builds System Prompt according to strategy configuration.
// mcpClient: 用于检查模型是否支持JSON Schema（API级别），如果为nil则使用提示词集成方式
func (e *StrategyEngine) BuildSystemPrompt(accountEquity float64, variant string, mcpClient mcp.AIClient) string {
	var sb strings.Builder
	riskControl := e.config.RiskControl
	promptSections := e.config.PromptSections

	// 0. Data Dictionary & Schema (ensure AI understands all fields)
	lang := e.GetLanguage()
	modelName := getModelNameFromClient(mcpClient)
	schemaPrompt := GetSchemaPrompt(lang, modelName)
	sb.WriteString(schemaPrompt)
	sb.WriteString("\n\n")
	sb.WriteString("---\n\n")

	// 1. Role definition (editable)
	if promptSections.RoleDefinition != "" {
		sb.WriteString(promptSections.RoleDefinition)
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("# You are a professional cryptocurrency trading AI\n\n")
		sb.WriteString("Your task is to make trading decisions based on provided market data.\n\n")
	}

	// 2. Trading mode variant
	switch strings.ToLower(strings.TrimSpace(variant)) {
	case "aggressive":
		sb.WriteString("## Mode: Aggressive\n- Prioritize capturing trend breakouts, can build positions in batches when confidence ≥ 70\n- Allow higher positions, but must strictly set stop-loss and explain risk-reward ratio\n\n")
	case "conservative":
		sb.WriteString("## Mode: Conservative\n- Only open positions when multiple signals resonate\n- Prioritize cash preservation, must pause for multiple periods after consecutive losses\n\n")
	case "scalping":
		sb.WriteString("## Mode: Scalping\n- Focus on short-term momentum, smaller profit targets but require quick action\n- If price doesn't move as expected within two bars, immediately reduce position or stop-loss\n\n")
	}

	// 3. Hard constraints (risk control)
	btcEthPosValueRatio := riskControl.BTCETHMaxPositionValueRatio
	if btcEthPosValueRatio <= 0 {
		btcEthPosValueRatio = 5.0
	}
	altcoinPosValueRatio := riskControl.AltcoinMaxPositionValueRatio
	if altcoinPosValueRatio <= 0 {
		altcoinPosValueRatio = 1.0
	}

	sb.WriteString("# Hard Constraints (Risk Control)\n\n")
	sb.WriteString("## CODE ENFORCED (Backend validation, cannot be bypassed):\n")
	sb.WriteString(fmt.Sprintf("- Max Positions: %d coins simultaneously\n", riskControl.MaxPositions))
	sb.WriteString(fmt.Sprintf("- Position Value Limit (Altcoins): max %.0f USDT (= equity %.0f × %.1fx)\n",
		accountEquity*altcoinPosValueRatio, accountEquity, altcoinPosValueRatio))
	sb.WriteString(fmt.Sprintf("- Position Value Limit (BTC/ETH): max %.0f USDT (= equity %.0f × %.1fx)\n",
		accountEquity*btcEthPosValueRatio, accountEquity, btcEthPosValueRatio))
	sb.WriteString(fmt.Sprintf("- Max Margin Usage: ≤%.0f%%\n", riskControl.MaxMarginUsage*100))
	sb.WriteString(fmt.Sprintf("- Min Position Size: ≥%.0f USDT\n\n", riskControl.MinPositionSize))

	sb.WriteString("## AI GUIDED (Recommended, you should follow):\n")
	sb.WriteString(fmt.Sprintf("- Trading Leverage: Altcoins max %dx | BTC/ETH max %dx\n",
		riskControl.AltcoinMaxLeverage, riskControl.BTCETHMaxLeverage))
	sb.WriteString(fmt.Sprintf("- Risk-Reward Ratio: ≥1:%.1f (take_profit / stop_loss)\n", riskControl.MinRiskRewardRatio))
	sb.WriteString(fmt.Sprintf("- Min Confidence: ≥%d to open position\n\n", riskControl.MinConfidence))

	// Position sizing guidance
	sb.WriteString("## Position Sizing Guidance\n")
	sb.WriteString("Calculate `position_size_usd` based on your confidence and the Position Value Limits above:\n")
	sb.WriteString("- High confidence (≥85): Use 80-100%% of max position value limit\n")
	sb.WriteString("- Medium confidence (70-84): Use 50-80%% of max position value limit\n")
	sb.WriteString("- Low confidence (60-69): Use 30-50%% of max position value limit\n")
	sb.WriteString(fmt.Sprintf("- Example: With equity %.0f and BTC/ETH ratio %.1fx, max is %.0f USDT\n",
		accountEquity, btcEthPosValueRatio, accountEquity*btcEthPosValueRatio))
	sb.WriteString("- **DO NOT** just use available_balance as position_size_usd. Use the Position Value Limits!\n\n")

	// Dynamic position size validation rule
	sb.WriteString("### ⚠️ Dynamic Position Size Validation (CRITICAL - 关键验证):\n")
	sb.WriteString("**Before outputting `position_size_usd`, you MUST validate and adjust if necessary:**\n\n")
	sb.WriteString(fmt.Sprintf("1. **Calculate max allowed position value**:\n"))
	sb.WriteString(fmt.Sprintf("   - For BTC/ETH: max = equity × %.1fx = %.0f USDT\n", btcEthPosValueRatio, accountEquity*btcEthPosValueRatio))
	sb.WriteString(fmt.Sprintf("   - For Altcoins: max = equity × %.1fx = %.0f USDT\n", altcoinPosValueRatio, accountEquity*altcoinPosValueRatio))
	sb.WriteString("2. **Compare your calculated `position_size_usd` with the max limit**\n")
	sb.WriteString("3. **If `position_size_usd` > max limit, you MUST adjust it to max limit**\n")
	sb.WriteString("   - ❌ WRONG: Outputting `position_size_usd` that exceeds the limit (will be rejected by backend)\n")
	sb.WriteString("   - ✅ CORRECT: Adjust `position_size_usd` to max limit if your calculation exceeds it\n")
	sb.WriteString(fmt.Sprintf("   - Example: If you calculate 6000 USDT for BTC but max is %.0f USDT, use %.0f USDT instead\n",
		accountEquity*btcEthPosValueRatio, accountEquity*btcEthPosValueRatio))
	sb.WriteString("4. **This validation is CODE ENFORCED - backend will reject decisions exceeding limits**\n\n")

	// 4. Trading frequency (editable)
	if promptSections.TradingFrequency != "" {
		sb.WriteString(promptSections.TradingFrequency)
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("# ⏱️ Trading Frequency Awareness\n\n")
		sb.WriteString("- Excellent traders: 2-4 trades/day ≈ 0.1-0.2 trades/hour\n")
		sb.WriteString("- >2 trades/hour = Overtrading\n")
		sb.WriteString("- Single position hold time ≥ 30-60 minutes\n")
		sb.WriteString("If you find yourself trading every period → standards too low; if closing positions < 30 minutes → too impatient.\n\n")
	}

	// 5. Entry standards (editable)
	if promptSections.EntryStandards != "" {
		sb.WriteString(promptSections.EntryStandards)
		sb.WriteString("\n\nYou have the following indicator data:\n")
		e.writeAvailableIndicators(&sb)
		sb.WriteString(fmt.Sprintf("\n**Confidence ≥ %d** required to open positions.\n\n", riskControl.MinConfidence))
	} else {
		sb.WriteString("# 🎯 Entry Standards (Strict)\n\n")
		sb.WriteString("Only open positions when multiple signals resonate. You have:\n")
		e.writeAvailableIndicators(&sb)
		sb.WriteString(fmt.Sprintf("\nFeel free to use any effective analysis method, but **confidence ≥ %d** required to open positions; avoid low-quality behaviors such as single indicators, contradictory signals, sideways consolidation, reopening immediately after closing, etc.\n\n", riskControl.MinConfidence))
	}

	// 5.5 Win rate & cost awareness (improve win rate by fewer low-quality opens)
	if lang == LangChinese {
		sb.WriteString("# 📊 胜率与成本意识\n\n")
		sb.WriteString("- 当**历史胜率**或**近期胜率**偏低时，优先输出 WAIT/hold，少开新仓；仅在多信号共振、高置信度时开仓。\n")
		sb.WriteString("- 开仓前确认**预期波动**明显大于**往返手续费**（数据中会给出 round-trip 成本）；否则易被成本吃掉利润。\n\n")
	} else {
		sb.WriteString("# 📊 Win Rate & Cost Awareness\n\n")
		sb.WriteString("- When **historical** or **recent win rate** is low, prefer WAIT/hold; only open when multiple signals align and confidence is high.\n")
		sb.WriteString("- Before opening, ensure **expected price move** clearly exceeds **round-trip cost** (shown in data); otherwise profit is eroded by fees.\n\n")
	}

	// 6. Decision process (editable)
	if promptSections.DecisionProcess != "" {
		sb.WriteString(promptSections.DecisionProcess)
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("# 📋 Decision Process\n\n")
		sb.WriteString("1. Check positions → Should we take profit/stop-loss\n")
		sb.WriteString("2. Scan candidate coins + multi-timeframe → Are there strong signals\n")
		sb.WriteString("3. Write chain of thought first, then output structured JSON\n\n")
	}

	// 7. Output format (STRICT - 严格格式要求)
	// 使用新的输出格式方法（支持JSON Schema和提示词集成两种方式，传入mcpClient以支持JSON Schema API级别检查）
	outputFormat := e.buildOutputFormat(accountEquity, btcEthPosValueRatio, riskControl, mcpClient)
	sb.WriteString(outputFormat)

	// ========== 旧版本输出格式（已提取为方法，方便回滚）==========
	// 如需回滚，取消下面的注释，并注释掉上面的 buildOutputFormat调用
	// outputFormatLegacy := e.buildOutputFormatLegacy(accountEquity, btcEthPosValueRatio, riskControl)
	// sb.WriteString(outputFormatLegacy)

	// 8. Custom Prompt
	if e.config.CustomPrompt != "" {
		sb.WriteString("# 📌 Personalized Trading Strategy\n\n")
		sb.WriteString(e.config.CustomPrompt)
		sb.WriteString("\n\n")
		sb.WriteString("Note: The above personalized strategy is a supplement to the basic rules and cannot violate the basic risk control principles.\n")
	}

	return sb.String()
}

// ============================================================================
// Output Format Building - 输出格式构建
// ============================================================================

// buildOutputFormatLegacy 构建输出格式（旧版本，使用XML标签+JSON数组格式）
func (e *StrategyEngine) buildOutputFormatLegacy(accountEquity float64, btcEthPosValueRatio float64, riskControl store.RiskControlConfig) string {
	var sb strings.Builder

	sb.WriteString("# ⚠️ Output Format (STRICTLY ENFORCED - 严格强制执行)\n\n")
	sb.WriteString("**CRITICAL: You MUST follow this format exactly. Any deviation will cause parsing errors.**\n\n")

	sb.WriteString("## Format Structure (Required)\n\n")
	sb.WriteString("You MUST use XML tags to separate thinking and decision:\n\n")
	sb.WriteString("<thinking>\n")
	sb.WriteString("Your analysis process (brief, no excessive verbosity)\n")
	sb.WriteString("</thinking>\n\n")
	sb.WriteString("<decision>\n")
	sb.WriteString("JSON array here (see format template below)\n")
	sb.WriteString("</decision>\n\n")

	sb.WriteString("## Action Field (CRITICAL - 关键字段)\n\n")
	sb.WriteString("**The `action` field MUST be EXACTLY one of these 6 values (case-sensitive, no variations):**\n\n")
	sb.WriteString("1. `\"open_long\"` - Open a long position (buy)\n")
	sb.WriteString("2. `\"open_short\"` - Open a short position (sell)\n")
	sb.WriteString("3. `\"close_long\"` - Close an existing long position\n")
	sb.WriteString("4. `\"close_short\"` - Close an existing short position\n")
	sb.WriteString("5. `\"hold\"` - Hold existing position(s), no action\n")
	sb.WriteString("6. `\"wait\"` - Wait, no positions, no action\n\n")

	sb.WriteString("### Example Format (Values are placeholders - 数值仅为占位符)\n\n")
	sb.WriteString("**Note: The values below are FORMAT EXAMPLES only. Replace ALL values with your calculated decisions.**\n\n")
	examplePositionSize := accountEquity * btcEthPosValueRatio
	sb.WriteString("<thinking>\n")
	sb.WriteString("Example analysis: Market shows bearish signals. RSI overbought. OI decreasing.\n")
	sb.WriteString("</thinking>\n\n")
	sb.WriteString("<decision>\n")
	sb.WriteString("```json\n[\n")
	sb.WriteString(fmt.Sprintf("  {\"symbol\": \"BTCUSDT\", \"action\": \"open_short\", \"leverage\": %d, \"position_size_usd\": %.0f, \"stop_loss\": 97000, \"take_profit\": 91000, \"confidence\": 85, \"risk_usd\": 300},\n",
		riskControl.BTCETHMaxLeverage, examplePositionSize))
	sb.WriteString("  {\"symbol\": \"ETHUSDT\", \"action\": \"close_long\"}\n")
	sb.WriteString("]\n```\n")
	sb.WriteString("</decision>\n\n")

	sb.WriteString("**⚠️ CRITICAL REMINDER:**\n")
	sb.WriteString("- The example above shows FORMAT STRUCTURE only\n")
	sb.WriteString("- You MUST replace symbol, action, prices, sizes with YOUR actual analysis\n")
	sb.WriteString("- DO NOT use the example BTCUSDT/ETHUSDT decisions unless they match your analysis\n")
	sb.WriteString("- Calculate position_size_usd, stop_loss, take_profit based on actual market data\n")
	sb.WriteString("- Use actual symbols from the candidate coins or existing positions provided\n\n")

	sb.WriteString("## Field Requirements\n\n")
	sb.WriteString("### Required for ALL decisions:\n")
	sb.WriteString("- `symbol`: Trading pair symbol from provided data (e.g., \"BTCUSDT\", \"ETHUSDT\")\n")
	sb.WriteString(fmt.Sprintf("- `action`: EXACTLY one of: open_long, open_short, close_long, close_short, hold, wait (case-sensitive)\n"))
	sb.WriteString(fmt.Sprintf("- `confidence`: Integer 0-100 (opening positions require ≥ %d)\n\n", riskControl.MinConfidence))
	sb.WriteString("- **Open position (open_long/open_short) REQUIRED fields** (backend rejects if missing or zero): leverage, position_size_usd, stop_loss, take_profit, confidence. Optional: risk_usd. Do NOT omit or use 0 for these.\n")
	sb.WriteString("- **Stop Loss and Take Profit validation** (CRITICAL - 关键验证):\n")
	sb.WriteString("  - For `open_long`: `stop_loss` MUST be LOWER than `take_profit` (止损必须低于止盈)\n")
	sb.WriteString("  - For `open_short`: `take_profit` MUST be LOWER than `stop_loss` (止盈必须低于止损)\n")
	sb.WriteString("  - Example for long: Entry 100, stop_loss 95, take_profit 110 ✓ (stop_loss < take_profit)\n")
	sb.WriteString("  - Example for short: Entry 100, stop_loss 110, take_profit 95 ✓ (take_profit < stop_loss)\n\n")

	sb.WriteString("- **Trading Fees (交易手续费)** (CRITICAL - 关键信息):\n")
	sb.WriteString("  - **Dynamic fee rates are provided in the user prompt per coin** (交易手续费会在用户提示词中按币种动态提供)\n")
	sb.WriteString("  - **Funding rate is also provided per coin in the user prompt** (资金费率也会在用户提示词中按币种提供)\n")
	sb.WriteString("  - If fee data is not available, use defaults: maker ~0.02-0.04%, taker ~0.04-0.05% (若无法获取则使用默认值)\n")
	sb.WriteString("  - **IMPORTANT**: Fee buffer for stop_loss/take_profit differs by position direction:\n")
	sb.WriteString("    - **For LONG positions** (止损在下方，止盈在上方):\n")
	sb.WriteString("      - stop_loss: Set HIGHER (closer to entry) by ~0.1% (e.g., target -5% → set at -4.9%)\n")
	sb.WriteString("      - take_profit: Set LOWER (closer to entry) by ~0.1% (e.g., target +8% → set at +7.9%)\n")
	sb.WriteString("    - **For SHORT positions** (止损在上方，止盈在下方):\n")
	sb.WriteString("      - stop_loss: Set LOWER (closer to entry) by ~0.1% (e.g., target +5% → set at +4.9%)\n")
	sb.WriteString("      - take_profit: Set HIGHER (closer to entry) by ~0.1% (e.g., target -8% → set at -7.9%)\n")
	sb.WriteString("    - **Principle**: Always make SL/TP trigger slightly EARLIER to ensure actual PnL meets target after fees\n\n")

	sb.WriteString("- **IMPORTANT**: All numeric values must be calculated numbers, NOT formulas/expressions (e.g., use `27.76` not `3000 * 0.01`)\n\n")

	sb.WriteString("## Validation Rules (Backend will reject invalid formats)\n\n")
	sb.WriteString("1. **Action validation**: If `action` is not one of the 6 exact values above, the decision will be REJECTED\n")
	sb.WriteString("2. **JSON format**: Must be valid JSON array, each element is an object\n")
	sb.WriteString("3. **Numeric values**: Must be actual numbers, NOT formulas (e.g., use `27.76` not `3000 * 0.01`)\n")
	sb.WriteString("4. **Required fields**: For open_long/open_short you MUST output non-zero: leverage, position_size_usd, stop_loss, take_profit, confidence. Missing or zero causes validation rejection.\n")
	sb.WriteString("5. **Price validation**: stop_loss and take_profit must be valid price levels from actual market data\n")
	sb.WriteString("6. **Stop Loss/Take Profit relationship** (CRITICAL):\n")
	sb.WriteString("   - For `open_long`: stop_loss MUST be < take_profit (止损必须低于止盈), otherwise REJECTED\n")
	sb.WriteString("   - For `open_short`: take_profit MUST be < stop_loss (止盈必须低于止损), otherwise REJECTED\n")
	sb.WriteString("7. **Risk/reward**: Risk-reward ratio must be ≥ 3.0:1\n\n")

	sb.WriteString("## Common Mistakes to Avoid\n\n")
	sb.WriteString("- ❌ Copying example values without analyzing actual market data\n")
	sb.WriteString("- ❌ Missing required fields when opening positions\n")
	sb.WriteString("- ❌ Using formulas in numeric fields instead of calculated values\n")
	sb.WriteString("- ❌ Invalid JSON structure (missing brackets, commas, quotes)\n\n")

	sb.WriteString("**Remember: Follow the format structure, but generate decisions based on actual market analysis.**\n\n")

	return sb.String()
}

// ============================================================================
// 支持两种方式：
// 1. JSON Schema集成（如果模型支持API级别的结构化输出）
// 2. 提示词集成（在提示词中嵌入JSON Schema，兼容所有模型）
// ============================================================================

// buildOutputFormat 构建输出格式部分
// mcpClient: 用于检查模型是否支持JSON Schema（API级别），如果为nil则使用提示词集成方式
func (e *StrategyEngine) buildOutputFormat(accountEquity float64, btcEthPosValueRatio float64, riskControl store.RiskControlConfig, mcpClient mcp.AIClient) string {
	// 检查模型是否支持JSON Schema（API级别）
	supportsJSONSchema := e.checkModelSupportsJSONSchema(mcpClient)

	if supportsJSONSchema {
		// 方法1：模型支持JSON Schema（API级别）
		// 注意：JSON Schema会在API调用时通过response_format参数传递
		// 这里只提供简化的格式说明
		return e.buildOutputFormatWithJSONSchemaAPI(accountEquity, btcEthPosValueRatio, riskControl)
	} else {
		// 方法2：提示词集成JSON Schema（兼容所有模型）
		return e.buildOutputFormatWithPromptIntegration(accountEquity, btcEthPosValueRatio, riskControl)
	}
}

// buildOutputFormatWithPromptIntegration 构建输出格式（提示词集成JSON Schema）
// 当模型不支持API级别的JSON Schema时，使用此方法
// 在提示词中嵌入完整的JSON Schema说明
func (e *StrategyEngine) buildOutputFormatWithPromptIntegration(accountEquity float64, btcEthPosValueRatio float64, riskControl store.RiskControlConfig) string {
	var sb strings.Builder
	lang := e.GetLanguage()

	sb.WriteString("# ⚠️ Output Format (STRICTLY ENFORCED - 严格强制执行)\n\n")

	if lang == LangChinese {
		sb.WriteString("**CRITICAL: You MUST follow this format exactly. Any deviation will cause parsing errors.**\n\n")

		sb.WriteString("## 输出格式要求\n\n")
		sb.WriteString("**必须**使用以下JSON对象格式输出（包含思维链和决策数组）：\n\n")
		sb.WriteString("```json\n")
		sb.WriteString("{\n")
		sb.WriteString("  \"thinking\": \"思维链分析过程...\",\n")
		sb.WriteString("  \"decisions\": [\n")
		sb.WriteString("    {\n")
		sb.WriteString("      \"symbol\": \"BTCUSDT\",\n")
		sb.WriteString("      \"action\": \"open_long\",\n")
		sb.WriteString("      \"leverage\": 3,\n")
		sb.WriteString("      \"position_size_usd\": 1000,\n")
		sb.WriteString("      \"stop_loss\": 42000,\n")
		sb.WriteString("      \"take_profit\": 48000,\n")
		sb.WriteString("      \"confidence\": 85,\n")
		sb.WriteString("      \"reasoning\": \"详细的推理过程...\"\n")
		sb.WriteString("    }\n")
		sb.WriteString("  ]\n")
		sb.WriteString("}\n")
		sb.WriteString("```\n\n")

		// 嵌入JSON Schema（紧凑版）
		schema := GetDecisionJSONSchemaCompact(lang)
		sb.WriteString("## JSON Schema（必须严格遵守）\n\n")
		sb.WriteString("**以下JSON Schema定义了输出格式的所有约束，请严格按照Schema输出：**\n\n")
		sb.WriteString("```json\n")
		sb.WriteString(schema)
		sb.WriteString("\n```\n\n")

		sb.WriteString("### 关键字段说明\n\n")
		sb.WriteString(fmt.Sprintf("- **thinking**: 思维链分析（必需，至少50字符），详细说明分析思路、市场判断、风险评估\n"))
		sb.WriteString("- **decisions**: 决策数组（必需，0-10个决策对象）\n")
		sb.WriteString(fmt.Sprintf("- **action**: 必须是以下之一：open_long, open_short, close_long, close_short, hold, wait, partial_close, full_close, add_position\n"))
		sb.WriteString(fmt.Sprintf("- **confidence**: 0-100整数（开新仓时要求≥%d）\n", riskControl.MinConfidence))
		sb.WriteString("- **开新仓必需字段**: leverage, position_size_usd, stop_loss, take_profit, confidence（缺一或为 0 将导致校验拒绝）\n")
		sb.WriteString("- **价格精度**: 根据实际市场价格动态确定（价格<0.0001用8位小数，<0.001用6位小数，<0.01用6位小数，<1.0用4位小数，<100用4位小数，≥100用2位小数）\n")
		sb.WriteString("- **止盈止损关系**: 做多时stop_loss必须 < take_profit，做空时stop_loss必须 > take_profit\n")
		sb.WriteString("- **风险回报比**: 必须≥3:1（止盈空间至少是止损空间的3倍）\n")
		sb.WriteString("- **手续费考虑**: 做多时止损价调高约0.1%，止盈价调低约0.1%；做空时止损价调低约0.1%，止盈价调高约0.1%\n\n")

		sb.WriteString("### 输出示例\n\n")
		examplePositionSize := accountEquity * btcEthPosValueRatio
		sb.WriteString("```json\n")
		sb.WriteString("{\n")
		sb.WriteString("  \"thinking\": \"分析账户状态：当前保证金使用率25%，在安全范围内。分析持仓：BTCUSDT当前PnL +2.96%，接近历史峰值+2.99%，回撤仅0.03%。5分钟K线显示价格接近短期阻力位，成交量开始萎缩，上涨动能减弱。建议部分平仓锁定利润。\",\n")
		sb.WriteString("  \"decisions\": [\n")
		sb.WriteString(fmt.Sprintf("    {\"symbol\": \"BTCUSDT\", \"action\": \"open_short\", \"leverage\": %d, \"position_size_usd\": %.0f, \"stop_loss\": 97000, \"take_profit\": 91000, \"confidence\": 85, \"reasoning\": \"市场显示看跌信号，RSI超买，OI下降\"},\n",
			riskControl.BTCETHMaxLeverage, examplePositionSize))
		sb.WriteString("    {\"symbol\": \"ETHUSDT\", \"action\": \"close_long\", \"confidence\": 80, \"reasoning\": \"达到止盈目标\"}\n")
		sb.WriteString("  ]\n")
		sb.WriteString("}\n")
		sb.WriteString("```\n\n")

		sb.WriteString("**⚠️ 重要提醒：**\n")
		sb.WriteString("- 所有数值必须是实际数字，不能是公式或表达式\n")
		sb.WriteString("- 必须严格按照JSON Schema的约束输出\n")
		sb.WriteString("- 开新仓时必须提供所有必需字段\n")
		sb.WriteString("- 价格精度根据实际市场价格动态确定\n\n")
	} else {
		sb.WriteString("**CRITICAL: You MUST follow this format exactly. Any deviation will cause parsing errors.**\n\n")

		sb.WriteString("## Output Format Requirements\n\n")
		sb.WriteString("**Must** use the following JSON object format (containing thinking chain and decisions array):\n\n")
		sb.WriteString("```json\n")
		sb.WriteString("{\n")
		sb.WriteString("  \"thinking\": \"Chain of thought analysis...\",\n")
		sb.WriteString("  \"decisions\": [\n")
		sb.WriteString("    {\n")
		sb.WriteString("      \"symbol\": \"BTCUSDT\",\n")
		sb.WriteString("      \"action\": \"open_long\",\n")
		sb.WriteString("      \"leverage\": 3,\n")
		sb.WriteString("      \"position_size_usd\": 1000,\n")
		sb.WriteString("      \"stop_loss\": 42000,\n")
		sb.WriteString("      \"take_profit\": 48000,\n")
		sb.WriteString("      \"confidence\": 85,\n")
		sb.WriteString("      \"reasoning\": \"Detailed reasoning...\"\n")
		sb.WriteString("    }\n")
		sb.WriteString("  ]\n")
		sb.WriteString("}\n")
		sb.WriteString("```\n\n")

		// 嵌入JSON Schema（紧凑版）
		schema := GetDecisionJSONSchemaCompact(lang)
		sb.WriteString("## JSON Schema (Must Strictly Follow)\n\n")
		sb.WriteString("**The following JSON Schema defines all constraints for output format. Please output strictly according to the Schema:**\n\n")
		sb.WriteString("```json\n")
		sb.WriteString(schema)
		sb.WriteString("\n```\n\n")

		sb.WriteString("### Key Field Descriptions\n\n")
		sb.WriteString(fmt.Sprintf("- **thinking**: Chain of thought analysis (required, min 50 chars), detailing analysis approach, market judgment, risk assessment\n"))
		sb.WriteString("- **decisions**: Decisions array (required, 0-10 decision objects)\n")
		sb.WriteString(fmt.Sprintf("- **action**: Must be one of: open_long, open_short, close_long, close_short, hold, wait, partial_close, full_close, add_position\n"))
		sb.WriteString(fmt.Sprintf("- **confidence**: Integer 0-100 (opening positions require ≥%d)\n", riskControl.MinConfidence))
		sb.WriteString("- **Required for new positions**: leverage, position_size_usd, stop_loss, take_profit (missing or zero causes validation rejection)\n")
		sb.WriteString("- **Price precision**: Dynamically determined based on actual market price (<0.0001 use 8 decimals, <0.001 use 6 decimals, <0.01 use 6 decimals, <1.0 use 4 decimals, <100 use 4 decimals, ≥100 use 2 decimals)\n")
		sb.WriteString("- **SL/TP relationship**: For LONG: stop_loss must < take_profit, For SHORT: stop_loss must > take_profit\n")
		sb.WriteString("- **Risk-reward ratio**: Must be ≥3:1 (take profit space must be at least 3x stop loss space)\n")
		sb.WriteString("- **Fee consideration**: For LONG: set SL ~0.1% higher, TP ~0.1% lower; For SHORT: set SL ~0.1% lower, TP ~0.1% higher\n\n")

		sb.WriteString("### Output Example\n\n")
		examplePositionSize := accountEquity * btcEthPosValueRatio
		sb.WriteString("```json\n")
		sb.WriteString("{\n")
		sb.WriteString("  \"thinking\": \"Analyze account status: Current margin usage 25%, within safe range. Analyze positions: BTCUSDT current PnL +2.96%, near historical peak +2.99%, only 0.03% pullback. 5M chart shows price approaching short-term resistance, volume declining, upward momentum weakening. Suggest partial close to lock profits.\",\n")
		sb.WriteString("  \"decisions\": [\n")
		sb.WriteString(fmt.Sprintf("    {\"symbol\": \"BTCUSDT\", \"action\": \"open_short\", \"leverage\": %d, \"position_size_usd\": %.0f, \"stop_loss\": 97000, \"take_profit\": 91000, \"confidence\": 85, \"reasoning\": \"Market shows bearish signals, RSI overbought, OI decreasing\"},\n",
			riskControl.BTCETHMaxLeverage, examplePositionSize))
		sb.WriteString("    {\"symbol\": \"ETHUSDT\", \"action\": \"close_long\", \"confidence\": 80, \"reasoning\": \"Reached take-profit target\"}\n")
		sb.WriteString("  ]\n")
		sb.WriteString("}\n")
		sb.WriteString("```\n\n")

		sb.WriteString("**⚠️ Important Reminders:**\n")
		sb.WriteString("- All numeric values must be actual numbers, not formulas or expressions\n")
		sb.WriteString("- Must strictly follow JSON Schema constraints\n")
		sb.WriteString("- All required fields must be provided when opening positions\n")
		sb.WriteString("- Price precision dynamically determined based on actual market price\n\n")
	}

	return sb.String()
}

// getModelNameFromClient 从mcpClient获取模型名称
// 如果无法获取，返回空字符串（将使用默认值）
func getModelNameFromClient(mcpClient mcp.AIClient) string {
	if mcpClient == nil {
		return ""
	}

	// 使用反射访问嵌入的 *Client 结构体中的 Model 字段
	// 所有 mcp 客户端（OpenAIClient, ClaudeClient等）都嵌入了 *Client
	val := reflect.ValueOf(mcpClient)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	// 遍历所有字段，查找嵌入的 Client
	for i := 0; i < val.Type().NumField(); i++ {
		field := val.Type().Field(i)
		fieldVal := val.Field(i)

		// 检查是否是嵌入字段（Anonymous）且类型是 *Client
		if field.Anonymous {
			// 处理指针类型
			if fieldVal.Kind() == reflect.Ptr {
				if !fieldVal.IsNil() {
					fieldVal = fieldVal.Elem()
				} else {
					continue
				}
			}

			// 检查字段类型名是否包含 "Client"（匹配 mcp.Client）
			typeName := field.Type.String()
			if strings.Contains(typeName, "Client") && fieldVal.IsValid() {
				// 尝试访问 Model 字段
				modelField := fieldVal.FieldByName("Model")
				if modelField.IsValid() && modelField.Kind() == reflect.String {
					modelName := modelField.String()
					if modelName != "" {
						return modelName
					}
				}
			}
		}
	}

	// 如果反射失败，返回空字符串（将使用默认策略）
	return ""
}

// checkModelSupportsJSONSchema 检查模型是否支持JSON Schema（API级别）
// 支持的模型：
//   - OpenAI: GPT-4o系列, GPT-4-turbo系列, GPT-4o-mini, o1系列, o3系列, GPT-4系列（2024年后版本）
//   - Claude: Claude Sonnet 4.5+, Claude Opus 4.1+, Claude Opus 4.5+（不支持Claude 3.x旧版本）
func (e *StrategyEngine) checkModelSupportsJSONSchema(mcpClient mcp.AIClient) bool {
	if mcpClient == nil {
		logger.Infof("🔍 [JSON Schema Check] mcpClient is nil, returning false (no JSON Schema support)")
		return false
	}

	// 获取模型名称和Provider
	modelName := getModelNameFromClient(mcpClient)
	provider := getProviderFromClient(mcpClient)

	// 如果无法获取模型信息，使用保守策略（不支持）
	if modelName == "" && provider == "" {
		logger.Warnf("⚠️  [JSON Schema Check] Cannot get model name and provider from mcpClient, returning false (no JSON Schema support)")
		return false
	}

	modelNameLower := strings.ToLower(modelName)
	providerLower := strings.ToLower(provider)

	logger.Infof("🔍 [JSON Schema Check] Checking model: provider=%s, modelName=%s", provider, modelName)

	// 调用 schema.go 中的统一检查函数
	supportsJSONSchema := CheckModelSupportsJSONSchema(providerLower, modelNameLower)

	if supportsJSONSchema {
		// 进一步检查是否支持高级特性
		supportsAdvanced := CheckModelSupportsAdvancedJSONSchemaFeatures(providerLower, modelNameLower)
		if supportsAdvanced {
			logger.Infof("✅ [JSON Schema Check] Model %s/%s supports JSON Schema with ADVANCED features (allOf, pattern, exclusiveMinimum) - will use FULL schema version", provider, modelName)
		} else {
			logger.Infof("✅ [JSON Schema Check] Model %s/%s supports JSON Schema with BASIC features only - will use SIMPLIFIED schema version", provider, modelName)
		}
	} else {
		logger.Infof("❌ [JSON Schema Check] Model %s/%s does NOT support JSON Schema - will use prompt integration mode (legacy format)", provider, modelName)
	}

	return supportsJSONSchema
}

// checkModelSupportsJSONSchemaByProvider 根据provider和modelName检查是否支持JSON Schema
// 这个方法可以被mcp包的回调函数调用，避免需要mcpClient参数
func (e *StrategyEngine) checkModelSupportsJSONSchemaByProvider(providerLower, modelNameLower string) bool {
	// 调用 schema.go 中的统一检查函数
	return CheckModelSupportsJSONSchema(providerLower, modelNameLower)
}

// getProviderFromClient 从mcpClient获取Provider名称
// 如果无法获取，返回空字符串
func getProviderFromClient(mcpClient mcp.AIClient) string {
	if mcpClient == nil {
		return ""
	}

	// 使用反射访问嵌入的 *Client 结构体中的 Provider 字段
	val := reflect.ValueOf(mcpClient)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	// 遍历所有字段，查找嵌入的 Client
	for i := 0; i < val.Type().NumField(); i++ {
		field := val.Type().Field(i)
		fieldVal := val.Field(i)

		// 检查是否是嵌入字段（Anonymous）且类型是 *Client
		if field.Anonymous {
			// 处理指针类型
			if fieldVal.Kind() == reflect.Ptr {
				if !fieldVal.IsNil() {
					fieldVal = fieldVal.Elem()
				} else {
					continue
				}
			}

			// 检查字段类型名是否包含 "Client"（匹配 mcp.Client）
			typeName := field.Type.String()
			if strings.Contains(typeName, "Client") && fieldVal.IsValid() {
				// 尝试访问 Provider 字段
				providerField := fieldVal.FieldByName("Provider")
				if providerField.IsValid() && providerField.Kind() == reflect.String {
					provider := providerField.String()
					if provider != "" {
						return provider
					}
				}
			}
		}
	}

	// 如果反射失败，返回空字符串
	return ""
}

// 各 Agent 角色推荐的请求参数（对齐 Qwen3.5-35B-A3B 等模型最佳实践，按角色职责定制）
const (
	TraderMaxTokens      = 2048
	AnalystMaxTokens     = 4096
	ComplianceMaxTokens  = 2048
	TraderTemperature    = 0.5   // 技术+决策：稳定可执行
	AnalystTemperature   = 0.75  // 宏观分析：适度多样性
	ComplianceTemperature = 0.0  // 审计：极度确定性
	TraderTopP           = 0.85
	AnalystTopP          = 0.95
	ComplianceTopP       = 0.1
	TraderPresencePenalty   = 0.5
	AnalystPresencePenalty  = 1.0
	CompliancePresencePenalty = 0.0
	TraderFrequencyPenalty   = 0.0
	AnalystFrequencyPenalty  = 0.0
	ComplianceFrequencyPenalty = 0.0
)

// PrepareClientForRole 根据当前 Agent 角色设置 client 的 JSON Schema 与请求参数（MaxTokens、Temperature、TopP、PresencePenalty、FrequencyPenalty），
// 以适配不同角色并确保输出质量。单一流程/多 Agent 在调用模型前应调用此函数。
// role: "trader" | "analyst" | "compliance"
func PrepareClientForRole(mcpClient mcp.AIClient, role string, lang Language) {
	if mcpClient == nil {
		return
	}
	modelName := getModelNameFromClient(mcpClient)
	provider := getProviderFromClient(mcpClient)

	// 按角色设置四类请求参数（对齐 Qwen3.5 等官方实践 + 分析师宏观/交易员技术/风控审计）
	switch role {
	case "trader":
		mcpClient.SetMaxTokens(TraderMaxTokens)
		mcpClient.SetTemperature(TraderTemperature)
		mcpClient.SetTopP(TraderTopP)
		mcpClient.SetPresencePenalty(TraderPresencePenalty)
		mcpClient.SetFrequencyPenalty(TraderFrequencyPenalty)
	case "analyst":
		mcpClient.SetMaxTokens(AnalystMaxTokens)
		mcpClient.SetTemperature(AnalystTemperature)
		mcpClient.SetTopP(AnalystTopP)
		mcpClient.SetPresencePenalty(AnalystPresencePenalty)
		mcpClient.SetFrequencyPenalty(AnalystFrequencyPenalty)
	case "compliance":
		mcpClient.SetMaxTokens(ComplianceMaxTokens)
		mcpClient.SetTemperature(ComplianceTemperature)
		mcpClient.SetTopP(ComplianceTopP)
		mcpClient.SetPresencePenalty(CompliancePresencePenalty)
		mcpClient.SetFrequencyPenalty(ComplianceFrequencyPenalty)
	default:
		// 未知角色仅清 Schema，不改请求参数
	}

	// 注册 mcp 包使用的 JSON Schema 检查回调
	mcp.JSONSchemaChecker = func(p, m string) bool {
		return CheckModelSupportsJSONSchema(strings.ToLower(p), strings.ToLower(m))
	}

	if modelName == "" && provider == "" {
		logger.Warnf("⚠️ [JSON Schema] Cannot determine model (provider=%s, modelName=%s), clearing schema for role=%s", provider, modelName, role)
		mcpClient.SetJSONSchema("")
		return
	}
	providerLower := strings.ToLower(provider)
	modelNameLower := strings.ToLower(modelName)
	supportsJSONSchema := CheckModelSupportsJSONSchema(providerLower, modelNameLower)

	if !supportsJSONSchema {
		logger.Infof("📝 [JSON Schema] Model %s/%s does not support JSON Schema API, role=%s uses prompt-only format", provider, modelName, role)
		mcpClient.SetJSONSchema("")
		return
	}

	var jsonSchema string
	switch role {
	case "trader":
		supportsAdvanced := CheckModelSupportsAdvancedJSONSchemaFeatures(providerLower, modelNameLower)
		schemaType := "SIMPLIFIED"
		if supportsAdvanced {
			schemaType = "FULL"
		}
		jsonSchema = GetDecisionJSONSchemaForModel(lang, provider, modelName)
		logger.Infof("🔧 [JSON Schema] role=trader, model %s/%s, %s schema", provider, modelName, schemaType)
	case "analyst", "compliance":
		jsonSchema = ""
		logger.Infof("🔧 [JSON Schema] role=%s, model %s/%s (no schema)", role, provider, modelName)
	default:
		logger.Warnf("⚠️ [JSON Schema] Unknown role=%s, clearing schema", role)
		mcpClient.SetJSONSchema("")
		return
	}
	mcpClient.SetJSONSchema(jsonSchema)
}

// buildOutputFormatWithJSONSchemaAPI 构建输出格式（模型支持JSON Schema API级别）
// 当模型支持API级别的JSON Schema时，使用此方法
// JSON Schema会通过API的response_format参数传递，提示词中只需简要说明
func (e *StrategyEngine) buildOutputFormatWithJSONSchemaAPI(accountEquity float64, btcEthPosValueRatio float64, riskControl store.RiskControlConfig) string {
	var sb strings.Builder
	lang := e.GetLanguage()

	sb.WriteString("# ⚠️ Output Format (JSON Schema Enforced - JSON Schema强制执行)\n\n")

	if lang == LangChinese {
		sb.WriteString("**重要：模型已启用JSON Schema结构化输出，输出格式将严格按照Schema验证。**\n\n")
		sb.WriteString("## 输出格式要求\n\n")
		sb.WriteString("**必须**使用以下JSON对象格式输出（包含思维链和决策数组）：\n\n")
		sb.WriteString("⚠️ **关键提醒**：最外层的 `thinking` 字段是JSON Schema中的必需字段（required），绝对不能省略！即使 `decisions` 数组为空，也必须提供 `thinking` 字段。\n\n")
		sb.WriteString("```json\n")
		sb.WriteString("{\n")
		sb.WriteString("  \"thinking\": \"思维链分析过程...\",\n")
		sb.WriteString("  \"decisions\": [\n")
		sb.WriteString("    {\n")
		sb.WriteString("      \"symbol\": \"BTCUSDT\",\n")
		sb.WriteString("      \"action\": \"open_long\",\n")
		sb.WriteString("      \"leverage\": 3,\n")
		sb.WriteString("      \"position_size_usd\": 1000,\n")
		sb.WriteString("      \"stop_loss\": 42000,\n")
		sb.WriteString("      \"take_profit\": 48000,\n")
		sb.WriteString("      \"confidence\": 85,\n")
		sb.WriteString("      \"reasoning\": \"详细的推理过程...\"\n")
		sb.WriteString("    }\n")
		sb.WriteString("  ]\n")
		sb.WriteString("}\n")
		sb.WriteString("```\n\n")

		sb.WriteString("### 关键要求\n\n")
		sb.WriteString(fmt.Sprintf("- **thinking**: 思维链分析（必需，至少50字符）\n"))
		sb.WriteString("- **decisions**: 决策数组（必需，0-10个决策）\n")
		sb.WriteString(fmt.Sprintf("- **action**: 必须是以下之一：open_long, open_short, close_long, close_short, hold, wait\n"))
		sb.WriteString(fmt.Sprintf("- **confidence**: 0-100整数（开新仓时要求≥%d）\n", riskControl.MinConfidence))
		sb.WriteString("- **开新仓必需字段**: leverage, position_size_usd, stop_loss, take_profit, confidence（缺一或为 0 将导致校验拒绝）\n")
		sb.WriteString("- **价格精度**: 根据实际市场价格动态确定\n")
		sb.WriteString("- **止盈止损关系**: 做多时stop_loss < take_profit，做空时stop_loss > take_profit\n")
		sb.WriteString("- **风险回报比**: 必须≥3:1\n\n")

		sb.WriteString("**注意：JSON Schema会在API调用时自动验证，确保输出格式完全符合要求。**\n\n")
	} else {
		sb.WriteString("**Important: Model has JSON Schema structured output enabled. Output format will be strictly validated against Schema.**\n\n")
		sb.WriteString("## Output Format Requirements\n\n")
		sb.WriteString("**Must** use the following JSON object format (containing thinking chain and decisions array):\n\n")
		sb.WriteString("```json\n")
		sb.WriteString("{\n")
		sb.WriteString("  \"thinking\": \"Chain of thought analysis...\",\n")
		sb.WriteString("  \"decisions\": [\n")
		sb.WriteString("    {\n")
		sb.WriteString("      \"symbol\": \"BTCUSDT\",\n")
		sb.WriteString("      \"action\": \"open_long\",\n")
		sb.WriteString("      \"leverage\": 3,\n")
		sb.WriteString("      \"position_size_usd\": 1000,\n")
		sb.WriteString("      \"stop_loss\": 42000,\n")
		sb.WriteString("      \"take_profit\": 48000,\n")
		sb.WriteString("      \"confidence\": 85,\n")
		sb.WriteString("      \"reasoning\": \"Detailed reasoning...\"\n")
		sb.WriteString("    }\n")
		sb.WriteString("  ]\n")
		sb.WriteString("}\n")
		sb.WriteString("```\n\n")

		sb.WriteString("### Key Requirements\n\n")
		sb.WriteString(fmt.Sprintf("- **thinking**: Chain of thought analysis (required, min 50 chars)\n"))
		sb.WriteString("- **decisions**: Decisions array (required, 0-10 decisions)\n")
		sb.WriteString(fmt.Sprintf("- **action**: Must be one of: open_long, open_short, close_long, close_short, hold, wait\n"))
		sb.WriteString(fmt.Sprintf("- **confidence**: Integer 0-100 (opening positions require ≥%d)\n", riskControl.MinConfidence))
		sb.WriteString("- **Required for new positions**: leverage, position_size_usd, stop_loss, take_profit (missing or zero causes validation rejection)\n")
		sb.WriteString("- **Price precision**: Dynamically determined based on actual market price\n")
		sb.WriteString("- **SL/TP relationship**: For LONG: stop_loss < take_profit, For SHORT: stop_loss > take_profit\n")
		sb.WriteString("- **Risk-reward ratio**: Must be ≥3:1\n\n")

		sb.WriteString("**Note: JSON Schema will automatically validate output format during API call to ensure full compliance.**\n\n")
	}

	return sb.String()
}

func (e *StrategyEngine) writeAvailableIndicators(sb *strings.Builder) {
	indicators := e.config.Indicators
	kline := indicators.Klines

	sb.WriteString(fmt.Sprintf("- %s price series", kline.PrimaryTimeframe))
	if kline.EnableMultiTimeframe {
		sb.WriteString(fmt.Sprintf(" + %s K-line series\n", kline.LongerTimeframe))
	} else {
		sb.WriteString("\n")
	}

	if indicators.EnableEMA {
		sb.WriteString("- EMA indicators")
		if len(indicators.EMAPeriods) > 0 {
			sb.WriteString(fmt.Sprintf(" (periods: %v)", indicators.EMAPeriods))
		}
		sb.WriteString("\n")
	}

	if indicators.EnableMACD {
		sb.WriteString("- MACD indicators\n")
	}

	if indicators.EnableRSI {
		sb.WriteString("- RSI indicators")
		if len(indicators.RSIPeriods) > 0 {
			sb.WriteString(fmt.Sprintf(" (periods: %v)", indicators.RSIPeriods))
		}
		sb.WriteString("\n")
	}

	if indicators.EnableATR {
		sb.WriteString("- ATR indicators")
		if len(indicators.ATRPeriods) > 0 {
			sb.WriteString(fmt.Sprintf(" (periods: %v)", indicators.ATRPeriods))
		}
		sb.WriteString("\n")
	}

	if indicators.EnableBOLL {
		sb.WriteString("- Bollinger Bands (BOLL) - Upper/Middle/Lower bands")
		if len(indicators.BOLLPeriods) > 0 {
			sb.WriteString(fmt.Sprintf(" (periods: %v)", indicators.BOLLPeriods))
		}
		sb.WriteString("\n")
	}

	if indicators.EnableVolume {
		sb.WriteString("- Volume data\n")
	}

	if indicators.EnableOI {
		sb.WriteString("- Open Interest (OI) data\n")
	}

	if indicators.EnableFundingRate {
		sb.WriteString("- Funding rate\n")
	}

	if len(e.config.CoinSource.StaticCoins) > 0 || e.config.CoinSource.UseAI500 || e.config.CoinSource.UseOITop {
		sb.WriteString("- AI500 / OI_Top filter tags (if available)\n")
	}

	if indicators.EnableQuantData {
		sb.WriteString("- Quantitative data (institutional/retail fund flow, position changes, multi-period price changes)\n")
	}
}

// ============================================================================
// Prompt Building - User Prompt
// ============================================================================

// BuildUserPrompt builds User Prompt based on strategy configuration
func (e *StrategyEngine) BuildUserPrompt(ctx *Context) string {
	var sb strings.Builder

	// System status
	sb.WriteString(fmt.Sprintf("Time: %s | Period: #%d | Runtime: %d minutes\n\n",
		ctx.CurrentTime, ctx.CallCount, ctx.RuntimeMinutes))

	// BTC market
	if btcData, hasBTC := ctx.MarketDataMap["BTCUSDT"]; hasBTC {
		sb.WriteString(fmt.Sprintf("BTC: %.2f", btcData.CurrentPrice))
		if ctx.RealtimePrice != nil {
			if rt, ok := ctx.RealtimePrice["BTCUSDT"]; ok && rt > 0 {
				sb.WriteString(fmt.Sprintf(" (实时价 %.2f)", rt))
			}
		}
		sb.WriteString(fmt.Sprintf(" (1h: %+.2f%%, 4h: %+.2f%%) | MACD: %.4f | RSI: %.2f\n\n",
			btcData.PriceChange1h, btcData.PriceChange4h,
			btcData.CurrentMACD, btcData.CurrentRSI7))
	}

	// Account information
	sb.WriteString(fmt.Sprintf("Account: Equity %.2f | Balance %.2f (%.1f%%) | PnL %+.2f%% | Margin %.1f%% | Positions %d\n\n",
		ctx.Account.TotalEquity,
		ctx.Account.AvailableBalance,
		(ctx.Account.AvailableBalance/ctx.Account.TotalEquity)*100,
		ctx.Account.TotalPnLPct,
		ctx.Account.MarginUsedPct,
		ctx.Account.PositionCount))

	// Recently completed orders (placed before positions to ensure visibility)
	if len(ctx.RecentOrders) > 0 {
		sb.WriteString("## Recent Completed Trades\n")
		wins := 0
		for _, order := range ctx.RecentOrders {
			if order.RealizedPnL > 0 {
				wins++
			}
		}
		n := len(ctx.RecentOrders)
		recentWinRate := 0.0
		if n > 0 {
			recentWinRate = float64(wins) / float64(n) * 100
		}
		if e.GetLanguage() == LangChinese {
			sb.WriteString(fmt.Sprintf("近期%d笔: %d胜 (胜率 %.1f%%) — 胜率偏低时建议少开仓、提高开仓门槛\n",
				n, wins, recentWinRate))
		} else {
			sb.WriteString(fmt.Sprintf("Recent %d trades: %d wins (%.1f%% win rate) — when low, reduce frequency and only open on high conviction\n",
				n, wins, recentWinRate))
		}
		for i, order := range ctx.RecentOrders {
			resultStr := "Profit"
			if order.RealizedPnL < 0 {
				resultStr = "Loss"
			}
			sb.WriteString(fmt.Sprintf("%d. %s %s | Entry %.4f Exit %.4f | %s: %+.2f USDT (%+.2f%%) | %s→%s (%s)\n",
				i+1, order.Symbol, order.Side,
				order.EntryPrice, order.ExitPrice,
				resultStr, order.RealizedPnL, order.PnLPct,
				order.EntryTime, order.ExitTime, order.HoldDuration))
		}
		sb.WriteString("\n")
	}

	// Historical trading statistics (helps AI understand past performance)
	if ctx.TradingStats != nil && ctx.TradingStats.TotalTrades > 0 {
		// Get language from strategy config
		lang := e.GetLanguage()

		// Win/Loss ratio
		var winLossRatio float64
		if ctx.TradingStats.AvgLoss > 0 {
			winLossRatio = ctx.TradingStats.AvgWin / ctx.TradingStats.AvgLoss
		}

		if lang == LangChinese {
			sb.WriteString("## 历史交易统计\n")
			sb.WriteString(fmt.Sprintf("总交易: %d 笔 | 胜率: %.1f%% | 盈利因子: %.2f | 夏普比率: %.2f | 盈亏比: %.2f\n",
				ctx.TradingStats.TotalTrades,
				ctx.TradingStats.WinRate,
				ctx.TradingStats.ProfitFactor,
				ctx.TradingStats.SharpeRatio,
				winLossRatio))
			sb.WriteString(fmt.Sprintf("总盈亏: %+.2f USDT | 平均盈利: +%.2f | 平均亏损: -%.2f | 最大回撤: %.1f%%\n",
				ctx.TradingStats.TotalPnL,
				ctx.TradingStats.AvgWin,
				ctx.TradingStats.AvgLoss,
				ctx.TradingStats.MaxDrawdownPct))

			// Performance hints based on profit factor, sharpe, drawdown, win rate
			if ctx.TradingStats.WinRate > 0 && ctx.TradingStats.WinRate < 50 {
				sb.WriteString("表现: 胜率偏低 - 建议减少开仓频率，仅在多信号共振、高置信度时开仓\n")
			} else if ctx.TradingStats.ProfitFactor >= 1.5 && ctx.TradingStats.SharpeRatio >= 1 {
				sb.WriteString("表现: 良好 - 保持当前策略\n")
			} else if ctx.TradingStats.ProfitFactor < 1 {
				sb.WriteString("表现: 需改进 - 提高盈亏比，优化止盈止损\n")
			} else if ctx.TradingStats.MaxDrawdownPct > 30 {
				sb.WriteString("表现: 风险偏高 - 减少仓位，控制回撤\n")
			} else {
				sb.WriteString("表现: 正常 - 有优化空间\n")
			}
		} else {
			sb.WriteString("## Historical Trading Statistics\n")
			sb.WriteString(fmt.Sprintf("Total Trades: %d | Win Rate: %.1f%% | Profit Factor: %.2f | Sharpe: %.2f | Win/Loss Ratio: %.2f\n",
				ctx.TradingStats.TotalTrades,
				ctx.TradingStats.WinRate,
				ctx.TradingStats.ProfitFactor,
				ctx.TradingStats.SharpeRatio,
				winLossRatio))
			sb.WriteString(fmt.Sprintf("Total PnL: %+.2f USDT | Avg Win: +%.2f | Avg Loss: -%.2f | Max Drawdown: %.1f%%\n",
				ctx.TradingStats.TotalPnL,
				ctx.TradingStats.AvgWin,
				ctx.TradingStats.AvgLoss,
				ctx.TradingStats.MaxDrawdownPct))

			// Performance hints based on win rate, profit factor, sharpe, drawdown
			if ctx.TradingStats.WinRate > 0 && ctx.TradingStats.WinRate < 50 {
				sb.WriteString("Performance: LOW WIN RATE - reduce trade frequency, only open when signals align and confidence is high\n")
			} else if ctx.TradingStats.ProfitFactor >= 1.5 && ctx.TradingStats.SharpeRatio >= 1 {
				sb.WriteString("Performance: GOOD - maintain current strategy\n")
			} else if ctx.TradingStats.ProfitFactor < 1 {
				sb.WriteString("Performance: NEEDS IMPROVEMENT - improve win/loss ratio, optimize TP/SL\n")
			} else if ctx.TradingStats.MaxDrawdownPct > 30 {
				sb.WriteString("Performance: HIGH RISK - reduce position size, control drawdown\n")
			} else {
				sb.WriteString("Performance: NORMAL - room for optimization\n")
			}
		}
		sb.WriteString("\n")
	}

	// Position information
	if len(ctx.Positions) > 0 {
		sb.WriteString("## Current Positions\n")
		for i, pos := range ctx.Positions {
			sb.WriteString(e.formatPositionInfo(i+1, pos, ctx))
		}
	} else {
		sb.WriteString("Current Positions: None\n\n")
	}

	// Candidate coins (exclude coins already in positions to avoid duplicate data)
	positionSymbols := make(map[string]bool)
	for _, pos := range ctx.Positions {
		// Normalize symbol to handle both "ETH" and "ETHUSDT" formats
		normalizedSymbol := market.Normalize(pos.Symbol)
		positionSymbols[normalizedSymbol] = true
	}

	// 方案4：优先使用策略配置的候选币种数量，优化作为兜底
	coinSource := e.config.CoinSource
	maxCandidateCoins := 0

	// 根据配置的币种来源确定最大数量
	switch coinSource.SourceType {
	case "static":
		maxCandidateCoins = len(coinSource.StaticCoins)
	case "ai500":
		maxCandidateCoins = coinSource.AI500Limit
		if maxCandidateCoins <= 0 {
			maxCandidateCoins = 10 // 默认值
		}
	case "oi_top":
		maxCandidateCoins = coinSource.OITopLimit
		if maxCandidateCoins <= 0 {
			maxCandidateCoins = 20 // 默认值
		}
	case "mixed":
		// 混合模式：取两者之和，但设置上限
		ai500Limit := coinSource.AI500Limit
		if ai500Limit <= 0 {
			ai500Limit = 10
		}
		oiTopLimit := coinSource.OITopLimit
		if oiTopLimit <= 0 {
			oiTopLimit = 20
		}
		maxCandidateCoins = ai500Limit + oiTopLimit
	default:
		maxCandidateCoins = 10 // 默认值
	}

	// 方案3：动态上限 - 根据最大持仓数计算合理上限，同时设置绝对上限
	// 公式：候选币种数 = MaxPositions × 2 + 1（提供足够选择空间）
	riskControl := e.config.RiskControl
	maxPositions := riskControl.MaxPositions
	if maxPositions <= 0 {
		maxPositions = 3 // 默认值
	}

	// 计算动态上限
	dynamicLimit := maxPositions*2 + 1

	// 设置绝对上限（防止配置过大）
	absoluteMaxLimit := 10

	// 取三者最小值：用户配置、动态上限、绝对上限
	if maxCandidateCoins > dynamicLimit {
		maxCandidateCoins = dynamicLimit
	}
	if maxCandidateCoins > absoluteMaxLimit {
		maxCandidateCoins = absoluteMaxLimit
	}

	candidateCoins := ctx.CandidateCoins
	if len(candidateCoins) > maxCandidateCoins {
		candidateCoins = candidateCoins[:maxCandidateCoins]
	}

	totalCandidateCount := len(ctx.CandidateCoins)
	sb.WriteString(fmt.Sprintf("## Candidate Coins (%d total, showing top %d)\n\n", totalCandidateCount, len(candidateCoins)))
	displayedCount := 0
	for _, coin := range candidateCoins {
		// Skip if this coin is already a position (data already shown in positions section)
		normalizedCoinSymbol := market.Normalize(coin.Symbol)
		if positionSymbols[normalizedCoinSymbol] {
			continue
		}

		marketData, hasData := ctx.MarketDataMap[coin.Symbol]
		if !hasData {
			continue
		}
		displayedCount++

		sourceTags := e.formatCoinSourceTag(coin.Sources)
		sb.WriteString(fmt.Sprintf("### %d. %s%s\n\n", displayedCount, coin.Symbol, sourceTags))
		sb.WriteString(e.formatMarketData(marketData))

		if ctx.QuantDataMap != nil {
			if quantData, hasQuant := ctx.QuantDataMap[coin.Symbol]; hasQuant {
				sb.WriteString(e.formatQuantData(quantData))
			}
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// Get language for market data formatting
	nofxosLang := nofxos.LangEnglish
	if e.GetLanguage() == LangChinese {
		nofxosLang = nofxos.LangChinese
	}

	// OI Ranking data (market-wide open interest changes)
	// 优化：显示 Top 5，平衡信息完整性与token消耗（Top 5提供更全面的市场信号）
	if ctx.OIRankingData != nil {
		limitedOIRanking := limitOIRankingData(ctx.OIRankingData, AnalystRankingsTopN)
		sb.WriteString(nofxos.FormatOIRankingForAI(limitedOIRanking, nofxosLang))
	}

	// NetFlow Ranking data (market-wide fund flow)
	// 优化：显示 Top 5，平衡信息完整性与token消耗（Top 5提供更全面的市场信号）
	if ctx.NetFlowRankingData != nil {
		limitedNetFlowRanking := limitNetFlowRankingData(ctx.NetFlowRankingData, AnalystRankingsTopN)
		sb.WriteString(nofxos.FormatNetFlowRankingForAI(limitedNetFlowRanking, nofxosLang))
	}

	// Price Ranking data (market-wide gainers/losers)
	// 优化：显示 Top 5，平衡信息完整性与token消耗（Top 5提供更全面的市场信号）
	if ctx.PriceRankingData != nil {
		limitedPriceRanking := limitPriceRankingData(ctx.PriceRankingData, AnalystRankingsTopN)
		sb.WriteString(nofxos.FormatPriceRankingForAI(limitedPriceRanking, nofxosLang))
	}

	sb.WriteString("---\n\n")
	sb.WriteString("Now please analyze and output your decision (Chain of Thought + JSON)\n")

	return sb.String()
}

func (e *StrategyEngine) formatPositionInfo(index int, pos PositionInfo, ctx *Context) string {
	var sb strings.Builder

	holdingDuration := ""
	if pos.UpdateTime > 0 {
		durationMs := time.Now().UnixMilli() - pos.UpdateTime
		durationMin := durationMs / (1000 * 60)
		if durationMin < 60 {
			holdingDuration = fmt.Sprintf(" | Holding Duration %d min", durationMin)
		} else {
			durationHour := durationMin / 60
			durationMinRemainder := durationMin % 60
			holdingDuration = fmt.Sprintf(" | Holding Duration %dh %dm", durationHour, durationMinRemainder)
		}
	}

	positionValue := pos.Quantity * pos.MarkPrice
	if positionValue < 0 {
		positionValue = -positionValue
	}

	sb.WriteString(fmt.Sprintf("%d. %s %s | Entry %.4f Current %.4f | Qty %.4f | Position Value %.2f USDT | PnL%+.2f%% | PnL Amount%+.2f USDT | Peak PnL%.2f%% | Leverage %dx | Margin %.0f | Liq Price %.4f%s\n\n",
		index, pos.Symbol, strings.ToUpper(pos.Side),
		pos.EntryPrice, pos.MarkPrice, pos.Quantity, positionValue, pos.UnrealizedPnLPct, pos.UnrealizedPnL, pos.PeakPnLPct,
		pos.Leverage, pos.MarginUsed, pos.LiquidationPrice, holdingDuration))

	if marketData, ok := ctx.MarketDataMap[pos.Symbol]; ok {
		sb.WriteString(e.formatMarketData(marketData))

		if ctx.QuantDataMap != nil {
			if quantData, hasQuant := ctx.QuantDataMap[pos.Symbol]; hasQuant {
				sb.WriteString(e.formatQuantData(quantData))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func (e *StrategyEngine) formatCoinSourceTag(sources []string) string {
	if len(sources) > 1 {
		// 多信号源组合
		hasAI500 := false
		hasOITop := false
		hasOILow := false
		for _, s := range sources {
			switch s {
			case "ai500":
				hasAI500 = true
			case "oi_top":
				hasOITop = true
			case "oi_low":
				hasOILow = true
			}
		}
		if hasAI500 && hasOITop {
			return " (AI500+OI_Top dual signal)"
		}
		if hasAI500 && hasOILow {
			return " (AI500+OI_Low dual signal)"
		}
		if hasOITop && hasOILow {
			return " (OI_Top+OI_Low)"
		}
		return " (Multiple sources)"
	} else if len(sources) == 1 {
		switch sources[0] {
		case "ai500":
			return " (AI500)"
		case "oi_top":
			return " (OI_Top 持仓增加)"
		case "oi_low":
			return " (OI_Low 持仓减少)"
		case "static":
			return " (Manual selection)"
		}
	}
	return ""
}

// ============================================================================
// Market Data Formatting
// ============================================================================

func (e *StrategyEngine) formatMarketData(data *market.Data) string {
	var sb strings.Builder
	indicators := e.config.Indicators
	lang := e.GetLanguage()

	// 明确标注币种
	sb.WriteString(fmt.Sprintf("=== %s Market Data ===\n\n", data.Symbol))

	// 市场状态标签（基于 1h/4h 涨跌幅），便于 AI 判断是否适合开仓
	var regime string
	if lang == LangChinese {
		if data.PriceChange1h > 0.005 && data.PriceChange4h > 0.01 {
			regime = "上升趋势"
		} else if data.PriceChange1h < -0.005 && data.PriceChange4h < -0.01 {
			regime = "下降趋势"
		} else {
			regime = "震荡"
		}
		sb.WriteString(fmt.Sprintf("市场状态(1h/4h): %s\n", regime))
	} else {
		if data.PriceChange1h > 0.005 && data.PriceChange4h > 0.01 {
			regime = "UPTREND"
		} else if data.PriceChange1h < -0.005 && data.PriceChange4h < -0.01 {
			regime = "DOWNTREND"
		} else {
			regime = "RANGING"
		}
		sb.WriteString(fmt.Sprintf("Regime (1h/4h): %s\n", regime))
	}

	sb.WriteString(fmt.Sprintf("current_price = %s", market.FormatPriceWithDynamicPrecision(data.CurrentPrice)))

	if indicators.EnableEMA {
		sb.WriteString(fmt.Sprintf(", current_ema20 = %s", market.FormatPriceWithDynamicPrecision(data.CurrentEMA20)))
	}

	if indicators.EnableMACD {
		sb.WriteString(fmt.Sprintf(", current_macd = %.6f", data.CurrentMACD))
	}

	if indicators.EnableRSI {
		sb.WriteString(fmt.Sprintf(", current_rsi7 = %.2f", data.CurrentRSI7))
	}

	sb.WriteString("\n\n")

	// Format contract info (funding rate + trading fees) in a unified section
	hasFundingRate := indicators.EnableFundingRate
	hasTradingFee := data.MakerFeeRate > 0 || data.TakerFeeRate > 0
	hasOI := indicators.EnableOI && data.OpenInterest != nil

	if hasOI || hasFundingRate || hasTradingFee {
		sb.WriteString(fmt.Sprintf("--- %s Contract Info ---\n", data.Symbol))

		if hasOI {
			// 计算OI变化百分比
			oiChange := float64(0)
			if data.OpenInterest.Average > 0 {
				oiChange = ((data.OpenInterest.Latest - data.OpenInterest.Average) / data.OpenInterest.Average) * 100
			}

			// OI-价格配合分析
			// | 价格 | OI | 含义 |
			// | ↑ | ↑ | 多头强势 (新资金做多) |
			// | ↑ | ↓ | 空头平仓 (弱反弹) |
			// | ↓ | ↑ | 空头强势 (新资金做空) |
			// | ↓ | ↓ | 多头平仓 (弱下跌) |
			priceUp := data.PriceChange1h > 0.001 || data.PriceChange4h > 0.005     // 价格上涨
			priceDown := data.PriceChange1h < -0.001 || data.PriceChange4h < -0.005 // 价格下跌
			oiUp := oiChange > 1                                                    // OI增加
			oiDown := oiChange < -1                                                 // OI减少

			oiSignal := ""
			if priceUp && oiUp {
				oiSignal = " [LONG_DOMINANT: new money buying]"
			} else if priceUp && oiDown {
				oiSignal = " [SHORT_COVERING: weak rally]"
			} else if priceDown && oiUp {
				oiSignal = " [SHORT_DOMINANT: new money selling]"
			} else if priceDown && oiDown {
				oiSignal = " [LONG_LIQUIDATION: weak decline]"
			}

			sb.WriteString(fmt.Sprintf("OI: %s (%+.1f%% vs avg)%s\n",
				formatFlowValue(data.OpenInterest.Latest), oiChange, oiSignal))
		}

		// Unified funding rate and trading fee display，显式量化成本便于决策
		if hasFundingRate || hasTradingFee {
			var costParts []string
			if hasFundingRate {
				fundingDir := "→"
				if data.FundingRate > 0 {
					fundingDir = "Long→Short"
				} else if data.FundingRate < 0 {
					fundingDir = "Short→Long"
				}
				costParts = append(costParts, fmt.Sprintf("Funding=%.4f%% (%s)",
					data.FundingRate*100, fundingDir))
			}
			if hasTradingFee {
				roundTripFee := (data.MakerFeeRate + data.TakerFeeRate) * 100
				costParts = append(costParts, fmt.Sprintf("Fee(maker/taker)=%.4f%%/%.4f%% [round-trip≈%.3f%%]",
					data.MakerFeeRate*100, data.TakerFeeRate*100, roundTripFee))
			}
			sb.WriteString("Cost Structure: ")
			sb.WriteString(strings.Join(costParts, ", "))
			if data.FeeSource != "" && data.FeeSource != "default" {
				sb.WriteString(fmt.Sprintf(" (source: %s)", data.FeeSource))
			}
			sb.WriteString("\n")

			// 显式成本提示：开仓前预期波动需覆盖成本
			roundTripPct := (data.MakerFeeRate + data.TakerFeeRate) * 100
			funding8hPct := data.FundingRate * 100
			if hasTradingFee && roundTripPct > 0 {
				if lang == LangChinese {
					sb.WriteString(fmt.Sprintf("成本提示: 往返手续费约 %.3f%%，开仓前预期波动需明显大于此值；", roundTripPct))
				} else {
					sb.WriteString(fmt.Sprintf("Cost note: Round-trip fee ≈ %.3f%%, ensure expected move > this before opening; ", roundTripPct))
				}
			}
			if hasFundingRate {
				if lang == LangChinese {
					sb.WriteString(fmt.Sprintf("资金费率 8h≈%.4f%%\n", funding8hPct))
				} else {
					sb.WriteString(fmt.Sprintf("Funding 8h≈%.4f%%\n", funding8hPct))
				}
			} else if hasTradingFee {
				sb.WriteString("\n")
			}
		}

		sb.WriteString("\n")
	}

	// Liquidation data (contract-specific, high signal value for reversals)
	if data.LiquidationData != nil && (data.LiquidationData.Liq1hTotal > 0 || data.LiquidationData.Liq24hTotal > 0) {
		liq := data.LiquidationData
		sb.WriteString(fmt.Sprintf("--- %s Liquidation ---\n", data.Symbol))
		if liq.Liq1hTotal > 0 {
			liqSignal := ""
			if liq.Liq1hLong > liq.Liq1hShort*3 {
				liqSignal = " [LONG_FLUSH: potential bounce]"
			} else if liq.Liq1hShort > liq.Liq1hLong*3 {
				liqSignal = " [SHORT_SQUEEZE: potential pullback]"
			}
			sb.WriteString(fmt.Sprintf("1h: Total=%s (Long=%s, Short=%s)%s\n",
				formatFlowValue(liq.Liq1hTotal), formatFlowValue(liq.Liq1hLong), formatFlowValue(liq.Liq1hShort), liqSignal))
		}
		if liq.Liq24hTotal > 0 {
			sb.WriteString(fmt.Sprintf("24h: Total=%s (Long=%s, Short=%s)\n",
				formatFlowValue(liq.Liq24hTotal), formatFlowValue(liq.Liq24hLong), formatFlowValue(liq.Liq24hShort)))
		}
		sb.WriteString("\n")
	}

	// Long/Short ratio (contract-specific, contrarian indicator at extremes)
	if data.LongShortRatio != nil && data.LongShortRatio.Ratio > 0 {
		ls := data.LongShortRatio
		lsSignal := ""
		if ls.Ratio > 2.5 {
			lsSignal = " [EXTREME_LONG: contrarian bearish]"
		} else if ls.Ratio < 0.5 {
			lsSignal = " [EXTREME_SHORT: contrarian bullish]"
		} else if ls.Ratio > 1.8 {
			lsSignal = " [CROWDED_LONG: caution]"
		} else if ls.Ratio < 0.7 {
			lsSignal = " [CROWDED_SHORT: caution]"
		}
		sb.WriteString(fmt.Sprintf("--- %s Long/Short Person Ratio ---\n", data.Symbol))
		sb.WriteString(fmt.Sprintf("Ratio=%.3f%s | Chg: 5m=%+.2f%%, 15m=%+.2f%%, 1h=%+.2f%%, 4h=%+.2f%%\n\n",
			ls.Ratio, lsSignal, ls.Chg5m*100, ls.Chg15m*100, ls.Chg1h*100, ls.Chg4h*100))
	}

	if len(data.TimeframeData) > 0 {
		// 优先使用策略配置的时间框架
		timeframes := indicators.Klines.SelectedTimeframes
		// 显示配置的时间框架（已优化）
		for _, tf := range timeframes {
			if tfData, ok := data.TimeframeData[tf]; ok {
				sb.WriteString(fmt.Sprintf("=== %s Timeframe (oldest → latest) ===\n\n", strings.ToUpper(tf)))
				e.formatTimeframeSeriesData(&sb, tfData, indicators, tf, data)
			}
		}

		// Multi-timeframe resonance analysis (weighted: 4h > 1h > 15m)
		if len(timeframes) >= 2 {
			var bullWeight, bearWeight, totalWeight float64
			bullish, bearish, total := 0, 0, 0
			for _, tf := range timeframes {
				tfData, ok := data.TimeframeData[tf]
				if !ok || len(tfData.Klines) < 3 {
					continue
				}
				w := TimeframeResonanceWeight(tf)
				totalWeight += w
				total++
				kls := tfData.Klines
				latest := kls[len(kls)-1].Close
				oldest := kls[0].Close
				emaBull := false
				if len(tfData.EMA20Values) > 0 {
					emaBull = latest > tfData.EMA20Values[len(tfData.EMA20Values)-1]
				}
				priceBull := latest > oldest
				if priceBull && emaBull {
					bullish++
					bullWeight += w
				} else if !priceBull && !emaBull {
					bearish++
					bearWeight += w
				}
			}
			if total >= 2 {
				resonance := ""
				if totalWeight > 0 {
					if bullWeight >= totalWeight*0.7 {
						resonance = fmt.Sprintf("BULLISH_RESONANCE (weighted %.0f/%.0f, 4h>1h>15m)", bullWeight, totalWeight)
					} else if bearWeight >= totalWeight*0.7 {
						resonance = fmt.Sprintf("BEARISH_RESONANCE (weighted %.0f/%.0f, 4h>1h>15m)", bearWeight, totalWeight)
					} else if bullWeight > 0 && bearWeight > 0 {
						resonance = fmt.Sprintf("DIVERGENCE (bull:%.0f bear:%.0f of %.0f - conflicting, caution)", bullWeight, bearWeight, totalWeight)
					} else {
						resonance = fmt.Sprintf("MIXED (bull:%d bear:%d neutral:%d)", bullish, bearish, total-bullish-bearish)
					}
				} else {
					if bullish == total {
						resonance = fmt.Sprintf("BULLISH_RESONANCE (%d/%d TF aligned bullish)", bullish, total)
					} else if bearish == total {
						resonance = fmt.Sprintf("BEARISH_RESONANCE (%d/%d TF aligned bearish)", bearish, total)
					} else if bullish > 0 && bearish > 0 {
						resonance = fmt.Sprintf("DIVERGENCE (bull:%d bear:%d of %d TF - conflicting, caution)", bullish, bearish, total)
					} else {
						resonance = fmt.Sprintf("MIXED (bull:%d bear:%d neutral:%d)", bullish, bearish, total-bullish-bearish)
					}
				}
				if lang == LangChinese {
					sb.WriteString(fmt.Sprintf("多周期共振: %s\n\n", resonance))
				} else {
					sb.WriteString(fmt.Sprintf("Multi-TF Resonance: %s\n\n", resonance))
				}
			}
		}
	} else {
		// Compatible with old data format
		if data.IntradaySeries != nil {
			klineConfig := indicators.Klines
			sb.WriteString(fmt.Sprintf("Intraday series (%s intervals, oldest → latest):\n\n", klineConfig.PrimaryTimeframe))

			if len(data.IntradaySeries.MidPrices) > 0 {
				sb.WriteString(fmt.Sprintf("Mid prices: %s\n\n", formatFloatSlice(data.IntradaySeries.MidPrices)))
			}

			if indicators.EnableEMA && len(data.IntradaySeries.EMA20Values) > 0 {
				sb.WriteString(fmt.Sprintf("EMA indicators (20-period): %s\n\n", formatFloatSlice(data.IntradaySeries.EMA20Values)))
			}

			if indicators.EnableMACD && len(data.IntradaySeries.MACDValues) > 0 {
				sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.IntradaySeries.MACDValues)))
			}

			if indicators.EnableRSI {
				if len(data.IntradaySeries.RSI7Values) > 0 {
					sb.WriteString(fmt.Sprintf("RSI indicators (7-Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI7Values)))
				}
				if len(data.IntradaySeries.RSI14Values) > 0 {
					sb.WriteString(fmt.Sprintf("RSI indicators (14-Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI14Values)))
				}
			}

			if indicators.EnableVolume && len(data.IntradaySeries.Volume) > 0 {
				sb.WriteString(fmt.Sprintf("Volume: %s\n\n", formatFloatSlice(data.IntradaySeries.Volume)))
			}

			if indicators.EnableATR {
				sb.WriteString(fmt.Sprintf("3m ATR (14-period): %.3f\n\n", data.IntradaySeries.ATR14))
			}
		}

		if data.LongerTermContext != nil && indicators.Klines.EnableMultiTimeframe {
			sb.WriteString(fmt.Sprintf("Longer-term context (%s timeframe):\n\n", indicators.Klines.LongerTimeframe))

			if indicators.EnableEMA {
				sb.WriteString(fmt.Sprintf("20-Period EMA: %.3f vs. 50-Period EMA: %.3f\n\n",
					data.LongerTermContext.EMA20, data.LongerTermContext.EMA50))
			}

			if indicators.EnableATR {
				sb.WriteString(fmt.Sprintf("3-Period ATR: %.3f vs. 14-Period ATR: %.3f\n\n",
					data.LongerTermContext.ATR3, data.LongerTermContext.ATR14))
			}

			if indicators.EnableVolume {
				sb.WriteString(fmt.Sprintf("Current Volume: %.3f vs. Average Volume: %.3f\n\n",
					data.LongerTermContext.CurrentVolume, data.LongerTermContext.AverageVolume))
			}

			if indicators.EnableMACD && len(data.LongerTermContext.MACDValues) > 0 {
				sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.LongerTermContext.MACDValues)))
			}

			if indicators.EnableRSI && len(data.LongerTermContext.RSI14Values) > 0 {
				sb.WriteString(fmt.Sprintf("RSI indicators (14-Period): %s\n\n", formatFloatSlice(data.LongerTermContext.RSI14Values)))
			}
		}
	}

	return sb.String()
}

func (e *StrategyEngine) formatTimeframeSeriesData(sb *strings.Builder, data *market.TimeframeSeriesData, indicators store.IndicatorConfig, timeframe string, fullData *market.Data) {
	lang := e.GetLanguage()
	// 优化版摘要：平衡决策质量与token消耗
	klines := data.Klines
	if len(klines) > 0 {
		latest := klines[len(klines)-1]
		oldest := klines[0]

		// 计算价格统计
		var minPrice, maxPrice, totalVolume float64
		minPrice = klines[0].Low
		maxPrice = klines[0].High
		for _, k := range klines {
			if k.Low < minPrice {
				minPrice = k.Low
			}
			if k.High > maxPrice {
				maxPrice = k.High
			}
			totalVolume += k.Volume
		}
		avgVolume := totalVolume / float64(len(klines))

		// 支撑/阻力预处理（前高前低、斐波那契、整数关、量能密集区），供下方关键价位与 Key Levels 使用
		sr := ComputeSupportResistance(klines, latest.Close)

		// 计算价格变化
		priceChange := ((latest.Close - oldest.Close) / oldest.Close) * 100

		// 改进趋势判断：分段分析（首1/3 vs 中1/3 vs 尾1/3）
		// 优化：使用更精确的分段边界计算，确保三段尽可能均匀
		trend := "sideways"
		trendStrength := ""
		if len(klines) >= 9 {
			n := len(klines)
			// 优化分段边界：使用浮点数计算后取整，确保三段长度尽可能均匀
			seg1End := int(float64(n) / 3.0)
			seg2End := int(float64(n) * 2.0 / 3.0)
			// 确保边界有效
			if seg1End < 1 {
				seg1End = 1
			}
			if seg2End <= seg1End {
				seg2End = seg1End + 1
			}
			if seg2End >= n {
				seg2End = n - 1
			}

			// 计算各段平均价（使用实际段长度，避免除零）
			var seg1Sum, seg2Sum, seg3Sum float64
			seg1Count := seg1End
			seg2Count := seg2End - seg1End
			seg3Count := n - seg2End

			for i := 0; i < seg1End; i++ {
				seg1Sum += klines[i].Close
			}
			for i := seg1End; i < seg2End; i++ {
				seg2Sum += klines[i].Close
			}
			for i := seg2End; i < n; i++ {
				seg3Sum += klines[i].Close
			}
			seg1Avg := seg1Sum / float64(seg1Count)
			seg2Avg := seg2Sum / float64(seg2Count)
			seg3Avg := seg3Sum / float64(seg3Count)

			// 判断趋势类型
			if seg3Avg > seg2Avg && seg2Avg > seg1Avg {
				trend = "uptrend"
				if seg3Avg > seg1Avg*1.02 {
					trendStrength = " (strong)"
				}
			} else if seg3Avg < seg2Avg && seg2Avg < seg1Avg {
				trend = "downtrend"
				if seg3Avg < seg1Avg*0.98 {
					trendStrength = " (strong)"
				}
			} else if seg3Avg > seg1Avg && seg2Avg > seg1Avg && seg2Avg > seg3Avg {
				trend = "up-then-pullback"
			} else if seg3Avg < seg1Avg && seg2Avg < seg1Avg && seg2Avg < seg3Avg {
				trend = "down-then-bounce"
			}
		} else if len(klines) >= 3 {
			// 简化判断
			if latest.Close > oldest.Close*1.01 {
				trend = "uptrend"
			} else if latest.Close < oldest.Close*0.99 {
				trend = "downtrend"
			}
		}

		// 使用动态精度格式化价格
		fmtPrice := market.FormatPriceWithDynamicPrecision

		// 显示摘要（包含关键价位）
		sb.WriteString(fmt.Sprintf("Summary: Close %s | High %s | Low %s | Change %+.2f%% | Trend: %s%s\n",
			fmtPrice(latest.Close), fmtPrice(maxPrice), fmtPrice(minPrice),
			priceChange, trend, trendStrength))

		// 近期关键价位（支撑/阻力）：区间最高/最低 + 细档位（前高前低、斐波那契、整数关、量能密集区）
		if lang == LangChinese {
			sb.WriteString(fmt.Sprintf("关键价位(近期区间): 阻力 %s | 支撑 %s\n",
				fmtPrice(sr.RangeHigh), fmtPrice(sr.RangeLow)))
		} else {
			sb.WriteString(fmt.Sprintf("Key levels (recent range): Resistance %s | Support %s\n",
				fmtPrice(sr.RangeHigh), fmtPrice(sr.RangeLow)))
		}
		if len(sr.Resistances) > 0 || len(sr.Supports) > 0 {
			var rParts, sParts []string
			for _, r := range sr.Resistances {
				rParts = append(rParts, fmt.Sprintf("%s(%s)", fmtPrice(r.Price), r.Label))
			}
			for _, s := range sr.Supports {
				sParts = append(sParts, fmt.Sprintf("%s(%s)", fmtPrice(s.Price), s.Label))
			}
			if lang == LangChinese {
				sb.WriteString("关键价位(细): ")
			} else {
				sb.WriteString("Key levels (detail): ")
			}
			if len(rParts) > 0 {
				sb.WriteString("R: " + strings.Join(rParts, " "))
			}
			if len(sParts) > 0 {
				if len(rParts) > 0 {
					sb.WriteString(" | ")
				}
				sb.WriteString("S: " + strings.Join(sParts, " "))
			}
			sb.WriteString("\n")
		}

		// 显示最近10根K线（提供完整形态与量价上下文，便于买卖点判断）
		recentCount := 10
		if len(klines) < recentCount {
			recentCount = len(klines)
		}
		sb.WriteString("Recent candles (oldest→newest):\n")
		for i := recentCount; i > 0; i-- {
			k := klines[len(klines)-i]
			t := time.Unix(k.Time/1000, 0).UTC()
			timeStr := t.Format("01-02 15:04")
			candleTypeCode := SingleCandleType(k)
			candleTypeDisplay := GetSignalDisplayName("SingleCandleTypes", candleTypeCode, lang)
			volRatio := k.Volume / avgVolume
			volLabel := ""
			if volRatio > 2.0 {
				volLabel = " [HIGH VOL]"
			} else if volRatio > 1.5 {
				volLabel = " [+vol]"
			} else if volRatio < 0.5 {
				volLabel = " [-vol]"
			}
			// Taker Buy Ratio: >0.6 bullish (buyers aggressive), <0.4 bearish (sellers aggressive)
			tbLabel := ""
			if k.TakerBuyRatio > 0 {
				if k.TakerBuyRatio > 0.65 {
					tbLabel = fmt.Sprintf(" TB:%.0f%%↑", k.TakerBuyRatio*100)
				} else if k.TakerBuyRatio < 0.35 {
					tbLabel = fmt.Sprintf(" TB:%.0f%%↓", k.TakerBuyRatio*100)
				} else if k.TakerBuyRatio > 0.55 || k.TakerBuyRatio < 0.45 {
					tbLabel = fmt.Sprintf(" TB:%.0f%%", k.TakerBuyRatio*100)
				}
			}
			sb.WriteString(fmt.Sprintf("  %s: O:%s H:%s L:%s C:%s (%s)%s%s\n",
				timeStr, fmtPrice(k.Open), fmtPrice(k.High), fmtPrice(k.Low), fmtPrice(k.Close), candleTypeDisplay, volLabel, tbLabel))
		}

		// Taker Buy Ratio summary for recent N candles (aggregate signal)
		{
			var tbSum float64
			var tbCount int
			for i := recentCount; i > 0; i-- {
				k := klines[len(klines)-i]
				if k.TakerBuyRatio > 0 {
					tbSum += k.TakerBuyRatio
					tbCount++
				}
			}
			if tbCount > 0 {
				avgTB := tbSum / float64(tbCount)
				tbSignal := ""
				if avgTB > 0.60 {
					tbSignal = " [BUYERS_DOMINANT]"
				} else if avgTB < 0.40 {
					tbSignal = " [SELLERS_DOMINANT]"
				}
				if tbSignal != "" || avgTB > 0.55 || avgTB < 0.45 {
					sb.WriteString(fmt.Sprintf("Taker Buy Avg (recent %d): %.1f%%%s\n", tbCount, avgTB*100, tbSignal))
				}
			}
		}

		// K线组合形态识别（完整清单见 kernel/candlestick.go），提示词双语且形态名与字典对应；4h>1h>15m 权重降短周期噪音
		patterns := DetectCandlestickPatterns(klines, trend)
		if len(patterns) > 0 {
			displayNames := make([]string, len(patterns))
			for i, code := range patterns {
				displayNames[i] = GetSignalDisplayName("CandlestickPatterns", code, lang)
			}
			weightHint := TimeframePatternWeightLabel(timeframe, lang == LangChinese)
			if lang == LangChinese {
				sb.WriteString(fmt.Sprintf("形态: %s%s\n", strings.Join(displayNames, ", "), weightHint))
			} else {
				sb.WriteString(fmt.Sprintf("Patterns: %s%s\n", strings.Join(displayNames, ", "), weightHint))
			}
		}

		// === 量价配合分析（全部量价类型，参考量价交易宝典），提示词双语 ===
		if len(klines) >= 3 {
			vpSignals := DetectVolumePriceSignals(klines)
			if len(vpSignals) > 0 {
				if lang == LangChinese {
					sb.WriteString(fmt.Sprintf("量价: %s\n", strings.Join(vpSignals, ", ")))
				} else {
					sb.WriteString(fmt.Sprintf("Volume-Price: %s\n", strings.Join(vpSignals, ", ")))
				}
			}
		}

		// === 分时摘要（短周期：多 lookback 5/10/20 + 斜率/量价/多空 TB 区分），提示词双语 ===
		if IsShortTimeframe(timeframe) && len(klines) >= 2 {
			atr := float64(0)
			if data != nil && data.ATR14 > 0 {
				atr = data.ATR14
			}
			for _, lb := range []int{5, 10, 20} {
				if len(klines) < lb {
					continue
				}
				if summary, ok := ComputeIntradaySummary(klines, lb, timeframe, atr); ok {
					if lang == LangChinese {
						sb.WriteString(fmt.Sprintf("分时(L%d): ", lb))
					} else {
						sb.WriteString(fmt.Sprintf("Intraday(L%d): ", lb))
					}
					sb.WriteString(FormatIntradaySummaryForPrompt(summary, lang == LangChinese))
					if summary.StrongBuy {
						sb.WriteString(" [STRONG_BUY]")
					}
					sb.WriteString("\n")
				}
			}
			// 方案A：分时旁补充合约语境（距下次资金费、OI/爆仓/多空比一句），便于综合判断
			if fullData != nil {
				var parts []string
				if fullData.NextFundingTimeMs > 0 {
					nowMs := time.Now().UTC().UnixMilli()
					minLeft := (fullData.NextFundingTimeMs - nowMs) / 60000
					if minLeft < 0 {
						minLeft = 0
					}
					if lang == LangChinese {
						parts = append(parts, fmt.Sprintf("距下次资金费 %d 分钟", minLeft))
					} else {
						parts = append(parts, fmt.Sprintf("%d min to next funding", minLeft))
					}
				}
				if fullData.OpenInterest != nil && fullData.OpenInterest.Latest > 0 {
					oiChg := 0.0
					if fullData.OpenInterest.Average > 0 {
						oiChg = ((fullData.OpenInterest.Latest - fullData.OpenInterest.Average) / fullData.OpenInterest.Average) * 100
					}
					if lang == LangChinese {
						parts = append(parts, fmt.Sprintf("OI %+.1f%%", oiChg))
					} else {
						parts = append(parts, fmt.Sprintf("OI %+.1f%%", oiChg))
					}
				}
				if fullData.LiquidationData != nil && (fullData.LiquidationData.Liq1hTotal > 0 || fullData.LiquidationData.Liq24hTotal > 0) {
					if lang == LangChinese {
						parts = append(parts, "有爆仓数据")
					} else {
						parts = append(parts, "liquidation data available")
					}
				}
				if fullData.LongShortRatio != nil && fullData.LongShortRatio.Ratio > 0 {
					if lang == LangChinese {
						parts = append(parts, fmt.Sprintf("多空比 %.2f", fullData.LongShortRatio.Ratio))
					} else {
						parts = append(parts, fmt.Sprintf("long/short ratio %.2f", fullData.LongShortRatio.Ratio))
					}
				}
				if len(parts) > 0 {
					if lang == LangChinese {
						sb.WriteString("合约语境：" + strings.Join(parts, "；") + "；上述分时为短周期动量，需结合本段综合判断。\n")
					} else {
						sb.WriteString("Contract context: " + strings.Join(parts, "; ") + "; intraday above is short-TF momentum, use with contract data.\n")
					}
				}
			}
		}

		sb.WriteString("\n")
	}

	// === 指标摘要优化：添加趋势信息和关键信号 ===
	fmtPrice := market.FormatPriceWithDynamicPrecision
	currentPrice := float64(0)
	if len(klines) > 0 {
		currentPrice = klines[len(klines)-1].Close
	}

	if indicators.EnableEMA {
		var ema20Val, ema50Val float64
		var ema20Trend, ema50Trend string
		hasEMA20, hasEMA50 := false, false

		if len(data.EMA20Values) > 0 {
			hasEMA20 = true
			ema20 := data.EMA20Values
			ema20Val = ema20[len(ema20)-1]
			ema20Trend = "→"
			if len(ema20) >= 3 {
				v1, v2, v3 := ema20[len(ema20)-3], ema20[len(ema20)-2], ema20[len(ema20)-1]
				if v3 > v2 && v2 > v1 {
					ema20Trend = "↑"
				} else if v3 < v2 && v2 < v1 {
					ema20Trend = "↓"
				}
			}
		}

		if len(data.EMA50Values) > 0 {
			hasEMA50 = true
			ema50 := data.EMA50Values
			ema50Val = ema50[len(ema50)-1]
			ema50Trend = "→"
			if len(ema50) >= 3 {
				v1, v2, v3 := ema50[len(ema50)-3], ema50[len(ema50)-2], ema50[len(ema50)-1]
				if v3 > v2 && v2 > v1 {
					ema50Trend = "↑"
				} else if v3 < v2 && v2 < v1 {
					ema50Trend = "↓"
				}
			}
		}

		// 合并输出
		if hasEMA20 && hasEMA50 {
			// 判断金叉/死叉状态和价格位置
			crossState := "BEARISH"
			if ema20Val > ema50Val {
				crossState = "BULLISH"
			}

			pricePos := "between"
			if currentPrice > 0 {
				if currentPrice > ema20Val && currentPrice > ema50Val {
					pricePos = "above both"
				} else if currentPrice < ema20Val && currentPrice < ema50Val {
					pricePos = "below both"
				} else if currentPrice > ema50Val && currentPrice < ema20Val {
					pricePos = "EMA50<Price<EMA20"
				} else {
					pricePos = "EMA20<Price<EMA50"
				}
			}

			sb.WriteString(fmt.Sprintf("EMA: 20=%s(%s) | 50=%s(%s) | %s | Price %s\n",
				fmtPrice(ema20Val), ema20Trend, fmtPrice(ema50Val), ema50Trend, crossState, pricePos))
		} else if hasEMA20 {
			pricePos := "at"
			if currentPrice > ema20Val*1.005 {
				pricePos = "above"
			} else if currentPrice < ema20Val*0.995 {
				pricePos = "below"
			}
			sb.WriteString(fmt.Sprintf("EMA20: %s (%s) | Price %s\n", fmtPrice(ema20Val), ema20Trend, pricePos))
		} else if hasEMA50 {
			pricePos := "at"
			if currentPrice > ema50Val*1.005 {
				pricePos = "above"
			} else if currentPrice < ema50Val*0.995 {
				pricePos = "below"
			}
			sb.WriteString(fmt.Sprintf("EMA50: %s (%s) | Price %s\n", fmtPrice(ema50Val), ema50Trend, pricePos))
		}
	}

	if indicators.EnableMACD && len(data.MACDValues) > 0 {
		macd := data.MACDValues
		current := macd[len(macd)-1]

		// 计算Signal线 (MACD的9周期SMA近似)
		signalPeriod := 9
		if len(macd) < signalPeriod {
			signalPeriod = len(macd)
		}
		var signalSum float64
		for i := len(macd) - signalPeriod; i < len(macd); i++ {
			signalSum += macd[i]
		}
		signal := signalSum / float64(signalPeriod)

		// 计算Histogram (MACD - Signal)
		histogram := current - signal

		// 计算前一个Histogram用于判断趋势
		var prevHistogram float64
		if len(macd) >= 2 {
			var prevSignalSum float64
			prevEnd := len(macd) - 1
			prevStart := prevEnd - signalPeriod
			if prevStart < 0 {
				prevStart = 0
			}
			for i := prevStart; i < prevEnd; i++ {
				prevSignalSum += macd[i]
			}
			prevSignal := prevSignalSum / float64(prevEnd-prevStart)
			prevHistogram = macd[len(macd)-2] - prevSignal
		}

		// 判断Histogram趋势
		histTrend := "flat"
		if histogram > prevHistogram*1.1 {
			histTrend = "expanding"
		} else if histogram < prevHistogram*0.9 {
			histTrend = "contracting"
		}

		// 检测金叉/死叉
		crossSignal := ""
		if len(macd) >= 2 {
			prevMACD := macd[len(macd)-2]
			// 计算前一个signal
			var prevSignalSum float64
			prevEnd := len(macd) - 1
			prevStart := prevEnd - signalPeriod
			if prevStart < 0 {
				prevStart = 0
			}
			for i := prevStart; i < prevEnd; i++ {
				prevSignalSum += macd[i]
			}
			prevSignal := prevSignalSum / float64(prevEnd-prevStart)

			// 金叉: MACD从下穿上Signal
			if prevMACD <= prevSignal && current > signal {
				crossSignal = " [GOLDEN_CROSS]"
			}
			// 死叉: MACD从上穿下Signal
			if prevMACD >= prevSignal && current < signal {
				crossSignal = " [DEATH_CROSS]"
			}
		}

		// 动量判断
		momentum := "neutral"
		if histogram > 0 && histTrend == "expanding" {
			momentum = "bullish_strengthening"
		} else if histogram > 0 && histTrend == "contracting" {
			momentum = "bullish_weakening"
		} else if histogram < 0 && histTrend == "expanding" {
			momentum = "bearish_strengthening"
		} else if histogram < 0 && histTrend == "contracting" {
			momentum = "bearish_weakening"
		}

		sb.WriteString(fmt.Sprintf("MACD: %.6f | Signal: %.6f | Hist: %+.6f (%s) [%s]%s\n",
			current, signal, histogram, histTrend, momentum, crossSignal))
	}

	if indicators.EnableRSI {
		// RSI背离检测函数
		detectDivergence := func(rsiValues []float64, klines []market.KlineBar) string {
			if len(rsiValues) < 10 || len(klines) < 10 {
				return ""
			}
			// 取最近10个数据点进行分析
			n := 10
			if len(rsiValues) < n {
				n = len(rsiValues)
			}
			if len(klines) < n {
				n = len(klines)
			}
			rsi := rsiValues[len(rsiValues)-n:]
			prices := make([]float64, n)
			for i := 0; i < n; i++ {
				prices[i] = klines[len(klines)-n+i].Close
			}

			// 找价格和RSI的局部高低点
			// 简化：比较前半段和后半段的高低点
			half := n / 2
			var priceHigh1, priceHigh2, priceLow1, priceLow2 float64
			var rsiHigh1, rsiHigh2, rsiLow1, rsiLow2 float64

			priceHigh1, priceLow1 = prices[0], prices[0]
			rsiHigh1, rsiLow1 = rsi[0], rsi[0]
			for i := 0; i < half; i++ {
				if prices[i] > priceHigh1 {
					priceHigh1 = prices[i]
				}
				if prices[i] < priceLow1 {
					priceLow1 = prices[i]
				}
				if rsi[i] > rsiHigh1 {
					rsiHigh1 = rsi[i]
				}
				if rsi[i] < rsiLow1 {
					rsiLow1 = rsi[i]
				}
			}

			priceHigh2, priceLow2 = prices[half], prices[half]
			rsiHigh2, rsiLow2 = rsi[half], rsi[half]
			for i := half; i < n; i++ {
				if prices[i] > priceHigh2 {
					priceHigh2 = prices[i]
				}
				if prices[i] < priceLow2 {
					priceLow2 = prices[i]
				}
				if rsi[i] > rsiHigh2 {
					rsiHigh2 = rsi[i]
				}
				if rsi[i] < rsiLow2 {
					rsiLow2 = rsi[i]
				}
			}

			// 顶背离：价格新高但RSI未新高 (bearish signal)
			if priceHigh2 > priceHigh1*1.005 && rsiHigh2 < rsiHigh1*0.98 {
				return " [BEARISH_DIVERGENCE: price higher but RSI lower]"
			}
			// 底背离：价格新低但RSI未新低 (bullish signal)
			if priceLow2 < priceLow1*0.995 && rsiLow2 > rsiLow1*1.02 {
				return " [BULLISH_DIVERGENCE: price lower but RSI higher]"
			}
			return ""
		}

		if len(data.RSI7Values) > 0 {
			rsi7 := data.RSI7Values
			current := rsi7[len(rsi7)-1]
			signal := "neutral"
			if current > 70 {
				signal = "overbought"
			} else if current > 60 {
				signal = "bullish"
			} else if current < 30 {
				signal = "oversold"
			} else if current < 40 {
				signal = "bearish"
			}
			// RSI趋势
			rsiTrend := ""
			if len(rsi7) >= 3 {
				v1, v2, v3 := rsi7[len(rsi7)-3], rsi7[len(rsi7)-2], rsi7[len(rsi7)-1]
				if v3 > v2 && v2 > v1 {
					rsiTrend = ", rising"
				} else if v3 < v2 && v2 < v1 {
					rsiTrend = ", falling"
				}
			}
			// 背离检测
			divergence := detectDivergence(rsi7, klines)
			sb.WriteString(fmt.Sprintf("RSI7: %.1f (%s%s)%s\n", current, signal, rsiTrend, divergence))
		}
		if len(data.RSI14Values) > 0 {
			rsi14 := data.RSI14Values
			current := rsi14[len(rsi14)-1]
			signal := "neutral"
			if current > 70 {
				signal = "overbought"
			} else if current > 60 {
				signal = "bullish"
			} else if current < 30 {
				signal = "oversold"
			} else if current < 40 {
				signal = "bearish"
			}
			// RSI14也检测背离
			divergence := detectDivergence(rsi14, klines)
			sb.WriteString(fmt.Sprintf("RSI14: %.1f (%s)%s\n", current, signal, divergence))
		}
	}

	if indicators.EnableATR && data.ATR14 > 0 {
		// ATR占价格百分比，便于理解波动率
		atrPct := float64(0)
		if currentPrice > 0 {
			atrPct = (data.ATR14 / currentPrice) * 100
		}

		// 波动率状态判断
		volStatus := "normal"
		if atrPct < 1.5 {
			volStatus = "LOW_VOL (consolidation, breakout likely)"
		} else if atrPct < 3.0 {
			volStatus = "normal"
		} else if atrPct < 6.0 {
			volStatus = "HIGH_VOL (trending)"
		} else {
			volStatus = "EXTREME_VOL (caution!)"
		}

		// 尝试从K线数据判断波动率趋势
		volTrend := ""
		if len(klines) >= 10 {
			// 比较最近5根K线的平均波动与前5根
			var recent5Range, prev5Range float64
			for i := len(klines) - 5; i < len(klines); i++ {
				recent5Range += klines[i].High - klines[i].Low
			}
			for i := len(klines) - 10; i < len(klines)-5; i++ {
				prev5Range += klines[i].High - klines[i].Low
			}
			recent5Range /= 5
			prev5Range /= 5
			if recent5Range > prev5Range*1.2 {
				volTrend = ", expanding"
			} else if recent5Range < prev5Range*0.8 {
				volTrend = ", contracting"
			}
		}

		// ATR percentile context
		atrPctile := ""
		if data.ATR14Percentile > 0 {
			if data.ATR14Percentile >= 80 {
				atrPctile = fmt.Sprintf(" P%d(high vs history→may contract)", int(data.ATR14Percentile))
			} else if data.ATR14Percentile <= 20 {
				atrPctile = fmt.Sprintf(" P%d(low vs history→breakout brewing)", int(data.ATR14Percentile))
			} else {
				atrPctile = fmt.Sprintf(" P%d", int(data.ATR14Percentile))
			}
		}
		sb.WriteString(fmt.Sprintf("ATR14: %s (%.2f%%) [%s%s]%s\n", fmtPrice(data.ATR14), atrPct, volStatus, volTrend, atrPctile))
	}

	if indicators.EnableBOLL && len(data.BOLLUpper) > 0 {
		bollUpper := data.BOLLUpper
		bollMiddle := data.BOLLMiddle
		bollLower := data.BOLLLower
		if len(bollUpper) > 0 {
			upper := bollUpper[len(bollUpper)-1]
			middle := bollMiddle[len(bollMiddle)-1]
			lower := bollLower[len(bollLower)-1]

			// 价格位置
			position := "middle"
			if currentPrice > upper {
				position = "ABOVE upper (overbought)"
			} else if currentPrice < lower {
				position = "BELOW lower (oversold)"
			} else if currentPrice > middle {
				pctToUpper := ((upper - currentPrice) / (upper - middle)) * 100
				position = fmt.Sprintf("upper half (%.0f%% to upper)", pctToUpper)
			} else {
				pctToLower := ((currentPrice - lower) / (middle - lower)) * 100
				position = fmt.Sprintf("lower half (%.0f%% to lower)", pctToLower)
			}

			// 布林带宽度（波动率指标）
			bandwidth := ((upper - lower) / middle) * 100
			bandwidthLabel := "normal"
			if bandwidth < 2 {
				bandwidthLabel = "SQUEEZE (low volatility, breakout likely)"
			} else if bandwidth > 8 {
				bandwidthLabel = "WIDE (high volatility)"
			}

			sb.WriteString(fmt.Sprintf("BOLL: [%s | %s | %s] Width: %.2f%% (%s) | Price: %s\n",
				fmtPrice(lower), fmtPrice(middle), fmtPrice(upper), bandwidth, bandwidthLabel, position))
		}
	}

	// Key Price Levels: consolidate range, pivot/Fib/round/VP, EMA, BOLL into structured support/resistance
	if currentPrice > 0 && len(klines) > 0 {
		type priceLevel struct {
			price float64
			label string
		}
		var supports, resistances []priceLevel
		srLevels := ComputeSupportResistance(klines, currentPrice)

		// 近期区间
		if srLevels.RangeHigh > currentPrice {
			resistances = append(resistances, priceLevel{srLevels.RangeHigh, "Range High"})
		}
		if srLevels.RangeLow < currentPrice && srLevels.RangeLow > 0 {
			supports = append(supports, priceLevel{srLevels.RangeLow, "Range Low"})
		}
		// 前高前低、斐波那契、整数关、量能密集区（预处理已去重）
		for _, r := range srLevels.Resistances {
			resistances = append(resistances, priceLevel{r.Price, r.Label})
		}
		for _, s := range srLevels.Supports {
			supports = append(supports, priceLevel{s.Price, s.Label})
		}

		// EMA levels
		if indicators.EnableEMA && len(data.EMA20Values) > 0 {
			ema20 := data.EMA20Values[len(data.EMA20Values)-1]
			if ema20 > currentPrice {
				resistances = append(resistances, priceLevel{ema20, "EMA20"})
			} else if ema20 < currentPrice {
				supports = append(supports, priceLevel{ema20, "EMA20"})
			}
		}
		if indicators.EnableEMA && len(data.EMA50Values) > 0 {
			ema50 := data.EMA50Values[len(data.EMA50Values)-1]
			if ema50 > currentPrice {
				resistances = append(resistances, priceLevel{ema50, "EMA50"})
			} else if ema50 < currentPrice {
				supports = append(supports, priceLevel{ema50, "EMA50"})
			}
		}

		// BOLL levels
		if indicators.EnableBOLL && len(data.BOLLUpper) > 0 {
			upper := data.BOLLUpper[len(data.BOLLUpper)-1]
			lower := data.BOLLLower[len(data.BOLLLower)-1]
			if upper > currentPrice {
				resistances = append(resistances, priceLevel{upper, "BOLL Upper"})
			}
			if lower < currentPrice && lower > 0 {
				supports = append(supports, priceLevel{lower, "BOLL Lower"})
			}
		}

		// 合并相近价位（0.2% 容差），避免重复显示
		dedupeNear := func(levels []priceLevel, desc bool) []priceLevel {
			if len(levels) <= 1 {
				return levels
			}
			if desc {
				sort.Slice(levels, func(i, j int) bool { return levels[i].price > levels[j].price })
			} else {
				sort.Slice(levels, func(i, j int) bool { return levels[i].price < levels[j].price })
			}
			out := levels[:1]
			for i := 1; i < len(levels); i++ {
				last := out[len(out)-1].price
				if last <= 0 || math.Abs(levels[i].price-last)/last <= 0.002 {
					continue
				}
				out = append(out, levels[i])
			}
			return out
		}
		supports = dedupeNear(supports, true)
		resistances = dedupeNear(resistances, false)

		// Sort and format (supports descending = nearest first, resistances ascending = nearest first)
		if len(supports) > 0 || len(resistances) > 0 {
			sort.Slice(supports, func(i, j int) bool { return supports[i].price > supports[j].price })
			sort.Slice(resistances, func(i, j int) bool { return resistances[i].price < resistances[j].price })

			if lang == LangChinese {
				sb.WriteString("关键价位: ")
			} else {
				sb.WriteString("Key Levels: ")
			}
			if len(resistances) > 0 {
				parts := make([]string, 0, len(resistances))
				for _, r := range resistances {
					parts = append(parts, fmt.Sprintf("%s(%s)", fmtPrice(r.price), r.label))
				}
				sb.WriteString("R: " + strings.Join(parts, " > "))
			}
			if len(supports) > 0 {
				if len(resistances) > 0 {
					sb.WriteString(" | ")
				}
				parts := make([]string, 0, len(supports))
				for _, s := range supports {
					parts = append(parts, fmt.Sprintf("%s(%s)", fmtPrice(s.price), s.label))
				}
				sb.WriteString("S: " + strings.Join(parts, " > "))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
}

func (e *StrategyEngine) formatQuantData(data *QuantData) string {
	if data == nil {
		return ""
	}

	indicators := e.config.Indicators
	if !indicators.EnableQuantOI && !indicators.EnableQuantNetflow {
		return ""
	}

	var sb strings.Builder

	// === 摘要式输出：用信号替代详细列表 ===

	// 1. 价格动量摘要
	if len(data.PriceChange) > 0 {
		// 取关键时间框架
		price1h, has1h := data.PriceChange["1h"]
		price24h, has24h := data.PriceChange["24h"]

		momentum := "NEUTRAL"
		if has1h && has24h {
			if price1h > 0.01 && price24h > 0.03 {
				momentum = "STRONG_BULLISH"
			} else if price1h > 0.005 && price24h > 0.01 {
				momentum = "BULLISH"
			} else if price1h < -0.01 && price24h < -0.03 {
				momentum = "STRONG_BEARISH"
			} else if price1h < -0.005 && price24h < -0.01 {
				momentum = "BEARISH"
			}
		}
		sb.WriteString(fmt.Sprintf("Price: 1h %+.2f%% | 24h %+.2f%% [%s]\n",
			price1h*100, price24h*100, momentum))
	}

	// 2. 资金流摘要
	if indicators.EnableQuantNetflow && data.Netflow != nil {
		var instTotal, retailTotal float64
		var instSignal, retailSignal string

		// 计算机构总流入（取24h或最大可用时间框架）
		if data.Netflow.Institution != nil {
			if data.Netflow.Institution.Future != nil {
				if v, ok := data.Netflow.Institution.Future["24h"]; ok {
					instTotal += v
				} else if v, ok := data.Netflow.Institution.Future["4h"]; ok {
					instTotal += v
				}
			}
			if data.Netflow.Institution.Spot != nil {
				if v, ok := data.Netflow.Institution.Spot["24h"]; ok {
					instTotal += v
				} else if v, ok := data.Netflow.Institution.Spot["4h"]; ok {
					instTotal += v
				}
			}
		}

		// 计算散户总流入
		if data.Netflow.Personal != nil {
			if data.Netflow.Personal.Future != nil {
				if v, ok := data.Netflow.Personal.Future["24h"]; ok {
					retailTotal += v
				} else if v, ok := data.Netflow.Personal.Future["4h"]; ok {
					retailTotal += v
				}
			}
			if data.Netflow.Personal.Spot != nil {
				if v, ok := data.Netflow.Personal.Spot["24h"]; ok {
					retailTotal += v
				} else if v, ok := data.Netflow.Personal.Spot["4h"]; ok {
					retailTotal += v
				}
			}
		}

		// 判断机构信号
		if instTotal > 1e6 {
			instSignal = "INFLOW"
		} else if instTotal < -1e6 {
			instSignal = "OUTFLOW"
		} else {
			instSignal = "NEUTRAL"
		}

		// 判断散户信号
		if retailTotal > 1e6 {
			retailSignal = "INFLOW"
		} else if retailTotal < -1e6 {
			retailSignal = "OUTFLOW"
		} else {
			retailSignal = "NEUTRAL"
		}

		// 综合判断市场状态
		marketState := ""
		if instSignal == "INFLOW" && retailSignal == "OUTFLOW" {
			marketState = " → SMART_MONEY_ACCUMULATION"
		} else if instSignal == "OUTFLOW" && retailSignal == "INFLOW" {
			marketState = " → DISTRIBUTION_WARNING"
		} else if instSignal == "INFLOW" && retailSignal == "INFLOW" {
			marketState = " → BROAD_BUYING"
		} else if instSignal == "OUTFLOW" && retailSignal == "OUTFLOW" {
			marketState = " → BROAD_SELLING"
		}

		sb.WriteString(fmt.Sprintf("Flow: Inst %s(%s) | Retail %s(%s)%s\n",
			formatFlowValue(instTotal), instSignal,
			formatFlowValue(retailTotal), retailSignal,
			marketState))
	}

	// 3. OI摘要
	if indicators.EnableQuantOI && len(data.OI) > 0 {
		for exchange, oiData := range data.OI {
			if len(oiData.Delta) > 0 {
				// 取24h或4h的OI变化
				var oiPct float64
				var oiVal float64
				if d, ok := oiData.Delta["24h"]; ok {
					oiPct = d.OIDeltaPercent
					oiVal = d.OIDeltaValue
				} else if d, ok := oiData.Delta["4h"]; ok {
					oiPct = d.OIDeltaPercent
					oiVal = d.OIDeltaValue
				}

				oiSignal := "STABLE"
				if oiPct > 5 {
					oiSignal = "STRONG_INCREASE"
				} else if oiPct > 2 {
					oiSignal = "INCREASING"
				} else if oiPct < -5 {
					oiSignal = "STRONG_DECREASE"
				} else if oiPct < -2 {
					oiSignal = "DECREASING"
				}

				sb.WriteString(fmt.Sprintf("OI(%s): %+.2f%% (%s) [%s]\n",
					exchange, oiPct, formatFlowValue(oiVal), oiSignal))
			}
			break // 只显示第一个交易所
		}
	}

	return sb.String()
}

func formatFlowValue(v float64) string {
	sign := ""
	if v >= 0 {
		sign = "+"
	}
	absV := v
	if absV < 0 {
		absV = -absV
	}
	if absV >= 1e9 {
		return fmt.Sprintf("%s%.2fB", sign, v/1e9)
	} else if absV >= 1e6 {
		return fmt.Sprintf("%s%.2fM", sign, v/1e6)
	} else if absV >= 1e3 {
		return fmt.Sprintf("%s%.2fK", sign, v/1e3)
	}
	return fmt.Sprintf("%s%.2f", sign, v)
}

// formatFloatSlice 格式化浮点数切片（方案7：压缩数据格式）
func formatFloatSlice(values []float64) string {
	if len(values) == 0 {
		return "[]"
	}

	strValues := make([]string, len(values))
	for i, v := range values {
		// 优化：根据数值大小动态调整精度，减少 token 消耗
		// 绝对值 > 1000 使用 2 位小数，> 100 使用 3 位，否则使用 4 位
		precision := 4
		absV := v
		if absV < 0 {
			absV = -absV
		}
		if absV > 1000 {
			precision = 2
		} else if absV > 100 {
			precision = 3
		}
		strValues[i] = fmt.Sprintf("%.*f", precision, v)
	}
	return "[" + strings.Join(strValues, ", ") + "]"
}

// ============================================================================
// AI Response Parsing
// ============================================================================

func parseFullDecisionResponse(aiResponse string, accountEquity float64, btcEthLeverage, altcoinLeverage int, btcEthPosRatio, altcoinPosRatio float64, excludedCoins []string) (*FullDecision, error) {
	cotTrace := extractCoTTrace(aiResponse)

	decisions, err := extractDecisions(aiResponse)
	if err != nil {
		return &FullDecision{
			CoTTrace:  cotTrace,
			Decisions: []Decision{},
		}, fmt.Errorf("failed to extract decisions: %w", err)
	}

	if err := validateDecisions(decisions, accountEquity, btcEthLeverage, altcoinLeverage, btcEthPosRatio, altcoinPosRatio, excludedCoins); err != nil {
		return &FullDecision{
			CoTTrace:  cotTrace,
			Decisions: decisions,
		}, fmt.Errorf("decision validation failed: %w", err)
	}

	return &FullDecision{
		CoTTrace:  cotTrace,
		Decisions: decisions,
	}, nil
}

// extractThinkingFromObject 从 JSON 对象字符串中提取 thinking 或 reasoning 字段（思维链）
func extractThinkingFromObject(jsonStr string) string {
	var v struct {
		Thinking  string `json:"thinking"`
		Reasoning string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &v); err != nil {
		return ""
	}
	chain := strings.TrimSpace(v.Thinking)
	if chain == "" {
		chain = strings.TrimSpace(v.Reasoning)
	}
	return chain
}

func extractCoTTrace(response string) string {
	s := removeInvisibleRunes(response)
	s = strings.TrimSpace(s)
	s = fixMissingQuotes(s)

	// 查找JSON对象：支持 thinking 或 reasoning（thinking 优先）
	var jsonObjStart int = -1
	if idx := strings.Index(s, `{"thinking"`); idx >= 0 {
		jsonObjStart = idx
	} else if idx := strings.Index(s, `{"reasoning"`); idx >= 0 {
		jsonObjStart = idx
	} else if idx := strings.Index(s, `{"decisions"`); idx >= 0 {
		jsonObjStart = idx
	}

	if jsonObjStart >= 0 {
		jsonObjEnd := findMatchingBrace(s, jsonObjStart)
		if jsonObjEnd > jsonObjStart {
			jsonObjStr := s[jsonObjStart : jsonObjEnd+1]
			if chain := extractThinkingFromObject(jsonObjStr); chain != "" {
				logger.Infof("✓ Extracted reasoning chain using new JSON format (thinking/reasoning field)")
				return chain
			}
		}
	}

	// 兼容格式化 JSON：{ 后带换行/空格再跟 "decisions" 或 "thinking" 等，精确子串匹配不到时从第一个 { 起解析
	if firstBrace := strings.Index(s, "{"); firstBrace >= 0 {
		jsonObjEnd := findMatchingBrace(s, firstBrace)
		if jsonObjEnd > firstBrace {
			jsonObjStr := s[firstBrace : jsonObjEnd+1]
			if chain := extractThinkingFromObject(jsonObjStr); chain != "" {
				logger.Infof("✓ Extracted reasoning chain from pretty-printed JSON (thinking/reasoning field)")
				return chain
			}
		}
	}

	// 尝试从 ```json 代码块内解析对象中的 thinking/reasoning
	if codeBlock := extractCodeFenceContent(s); codeBlock != "" {
		codeBlock = fixMissingQuotes(codeBlock)
		if objStart := strings.Index(codeBlock, "{"); objStart >= 0 {
			if objEnd := findMatchingBrace(codeBlock, objStart); objEnd > objStart {
				if chain := extractThinkingFromObject(codeBlock[objStart : objEnd+1]); chain != "" {
					logger.Infof("✓ Extracted reasoning chain from code fence (thinking/reasoning)")
					return chain
				}
			}
		}
	}

	// 回退到 XML 标签：优先 <thinking>，再 <reasoning>
	if match := reThinkingTag.FindStringSubmatch(response); match != nil && len(match) > 1 {
		logger.Infof("✓ Extracted reasoning chain using <thinking> tag")
		return strings.TrimSpace(match[1])
	}
	if match := reReasoningTag.FindStringSubmatch(response); match != nil && len(match) > 1 {
		logger.Infof("✓ Extracted reasoning chain using <reasoning> tag (legacy format)")
		return strings.TrimSpace(match[1])
	}

	if decisionIdx := strings.Index(response, "<decision>"); decisionIdx > 0 {
		logger.Infof("✓ Extracted content before <decision> tag as reasoning chain (legacy format)")
		return strings.TrimSpace(response[:decisionIdx])
	}

	// 回退到旧格式：JSON数组前的文本
	jsonStart := strings.Index(response, "[")
	if jsonStart > 0 {
		logger.Infof("⚠️  Extracted reasoning chain using old format ([ character separator)")
		return strings.TrimSpace(response[:jsonStart])
	}

	return strings.TrimSpace(response)
}

// findMatchingBrace 找到与开始位置匹配的右花括号位置
func findMatchingBrace(s string, start int) int {
	if start < 0 || start >= len(s) || s[start] != '{' {
		return -1
	}

	depth := 0
	inString := false
	escape := false

	for i := start; i < len(s); i++ {
		char := s[i]

		if escape {
			escape = false
			continue
		}

		if char == '\\' {
			escape = true
			continue
		}

		if char == '"' {
			inString = !inString
			continue
		}

		if inString {
			continue
		}

		if char == '{' {
			depth++
		} else if char == '}' {
			depth--
			if depth == 0 {
				return i
			}
		}
	}

	return -1
}

// findMatchingBracket 找到与 start 处 '[' 匹配的 ']' 位置（考虑字符串与嵌套）
func findMatchingBracket(s string, start int) int {
	if start < 0 || start >= len(s) || s[start] != '[' {
		return -1
	}
	depth := 0
	inString := false
	escape := false
	var quote byte
	for i := start; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' && inString {
			escape = true
			continue
		}
		if (c == '"' || c == '\'') && !inString {
			inString = true
			quote = c
			continue
		}
		if c == quote {
			inString = false
			continue
		}
		if inString {
			continue
		}
		if c == '[' {
			depth++
		} else if c == ']' {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// extractCodeFenceContent 从响应中提取第一个 ```json 或 ``` 代码块内容（不含标记行）
func extractCodeFenceContent(s string) string {
	const fence = "```"
	idx := strings.Index(s, fence)
	if idx < 0 {
		return ""
	}
	rest := s[idx+len(fence):]
	rest = strings.TrimLeft(rest, " \t\r\n")
	if strings.HasPrefix(strings.ToLower(rest), "json") {
		rest = rest[4:]
		rest = strings.TrimLeft(rest, " \t\r\n")
	}
	end := strings.Index(rest, fence)
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

// tryRepairTrailingComma 尝试修复 JSON 中常见的尾逗号（在 ] 或 } 前）
func tryRepairTrailingComma(in string) string {
	// 移除 ] 前的尾逗号：, ] -> ]
	s := regexp.MustCompile(`,\s*\]`).ReplaceAllString(in, "]")
	// 移除 } 前的尾逗号：, } -> }
	s = regexp.MustCompile(`,\s*}`).ReplaceAllString(s, "}")
	return s
}

// tryUnmarshalDecisionArray 尝试解析 JSON 为 []Decision，失败时尝试修复尾逗号后重试
func tryUnmarshalDecisionArray(jsonContent string) ([]Decision, error) {
	jsonContent = strings.TrimSpace(jsonContent)
	if jsonContent == "" {
		return nil, fmt.Errorf("empty json content")
	}
	var decisions []Decision
	if err := json.Unmarshal([]byte(jsonContent), &decisions); err == nil {
		return decisions, nil
	}
	repaired := tryRepairTrailingComma(jsonContent)
	if err := json.Unmarshal([]byte(repaired), &decisions); err != nil {
		return nil, err
	}
	return decisions, nil
}

// normalizeDecisionAction 将 action 规范为小写且下划线形式（兼容 "Open Long" / "OPEN_LONG" 等）
func normalizeDecisionAction(d *Decision) {
	if d == nil {
		return
	}
	d.Action = strings.TrimSpace(d.Action)
	d.Action = strings.ToLower(d.Action)
	d.Action = strings.ReplaceAll(d.Action, " ", "_")
}

func normalizeDecisionActions(list []Decision) {
	for i := range list {
		normalizeDecisionAction(&list[i])
	}
}

// parseNewFormatObject 解析 {"thinking"/"reasoning", "decisions": [...]} 格式，返回 decisions 与是否成功（空数组也视为成功）
func parseNewFormatObject(jsonStr string) ([]Decision, bool) {
	var v struct {
		Thinking  string     `json:"thinking"`
		Reasoning string     `json:"reasoning"`
		Decisions []Decision `json:"decisions"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &v); err != nil {
		return nil, false
	}
	// 空数组为合法结果（表示 wait，无需新开/平仓）
	return v.Decisions, true
}

func extractDecisions(response string) ([]Decision, error) {
	s := removeInvisibleRunes(response)
	s = strings.TrimSpace(s)
	s = fixMissingQuotes(s)

	// ========== 优先尝试新格式：JSON对象（包含 thinking/reasoning 和 decisions 字段）==========
	type newFormatResponse struct {
		Thinking  string     `json:"thinking"`
		Reasoning string     `json:"reasoning"` // legacy
		Decisions []Decision `json:"decisions"`
	}

	var jsonObjStart int = -1
	if idx := strings.Index(s, `{"thinking"`); idx >= 0 {
		jsonObjStart = idx
	} else if idx := strings.Index(s, `{"reasoning"`); idx >= 0 {
		jsonObjStart = idx
	} else if idx := strings.Index(s, `{"decisions"`); idx >= 0 {
		jsonObjStart = idx
	}

	if jsonObjStart >= 0 {
		jsonObjEnd := findMatchingBrace(s, jsonObjStart)
		if jsonObjEnd > jsonObjStart {
			jsonObjStr := s[jsonObjStart : jsonObjEnd+1]
			if decisions, ok := parseNewFormatObject(jsonObjStr); ok {
				normalizeDecisionActions(decisions)
				logger.Infof("✓ Extracted decisions using new JSON format (decisions field)")
				return decisions, nil
			}
		}
	}

	// 兼容格式化 JSON：{ 后带换行/空格再跟 "decisions"/"thinking" 等时，从第一个 { 起解析
	if firstBrace := strings.Index(s, "{"); firstBrace >= 0 {
		jsonObjEnd := findMatchingBrace(s, firstBrace)
		if jsonObjEnd > firstBrace {
			jsonObjStr := s[firstBrace : jsonObjEnd+1]
			if decisions, ok := parseNewFormatObject(jsonObjStr); ok {
				normalizeDecisionActions(decisions)
				logger.Infof("✓ Extracted decisions from pretty-printed JSON (decisions field)")
				return decisions, nil
			}
		}
	}

	// 从 ```json 代码块中提取完整对象（避免正则截断大内容）
	if codeBlock := extractCodeFenceContent(s); codeBlock != "" {
		codeBlock = fixMissingQuotes(codeBlock)
		if objStart := strings.Index(codeBlock, "{"); objStart >= 0 {
			if objEnd := findMatchingBrace(codeBlock, objStart); objEnd > objStart {
				jsonObjStr := codeBlock[objStart : objEnd+1]
				if decisions, ok := parseNewFormatObject(jsonObjStr); ok {
					normalizeDecisionActions(decisions)
					logger.Infof("✓ Extracted decisions using new JSON format from code fence")
					return decisions, nil
				}
			}
		}
		// 代码块内可能是纯数组
		if arrStart := strings.Index(codeBlock, "["); arrStart >= 0 {
			if arrEnd := findMatchingBracket(codeBlock, arrStart); arrEnd > arrStart {
				arrStr := codeBlock[arrStart : arrEnd+1]
				if decisions, err := tryUnmarshalDecisionArray(arrStr); err == nil && len(decisions) > 0 {
					normalizeDecisionActions(decisions)
					logger.Infof("✓ Extracted decisions from code fence (array)")
					return decisions, nil
				}
			}
		}
	}

	// ========== 回退到旧格式：XML 标签 + JSON 数组 ==========
	var jsonPart string
	if match := reDecisionTag.FindStringSubmatch(s); match != nil && len(match) > 1 {
		jsonPart = strings.TrimSpace(match[1])
		logger.Infof("✓ Extracted JSON using <decision> tag (legacy format)")
	} else {
		jsonPart = s
		logger.Infof("⚠️  <decision> tag not found, searching JSON in full text")
	}

	jsonPart = fixMissingQuotes(jsonPart)

	// 尝试从代码块或正文中提取 JSON 数组（括号匹配，避免正则截断）
	tryExtractArray := func(text string) ([]Decision, bool) {
		for i := 0; i < len(text); i++ {
			if text[i] == '[' {
				arrEnd := findMatchingBracket(text, i)
				if arrEnd > i {
					arrStr := text[i : arrEnd+1]
					arrStr = compactArrayOpen(arrStr)
					if err := validateJSONFormat(arrStr); err != nil {
						continue
					}
					decisions, err := tryUnmarshalDecisionArray(arrStr)
					if err == nil && len(decisions) > 0 {
						return decisions, true
					}
				}
			}
		}
		return nil, false
	}

	// 1) 代码块内的数组
	if codeBlock := extractCodeFenceContent(jsonPart); codeBlock != "" {
		codeBlock = fixMissingQuotes(codeBlock)
		if decisions, ok := tryExtractArray(codeBlock); ok {
			normalizeDecisionActions(decisions)
			logger.Infof("✓ Extracted decisions from code fence (array)")
			return decisions, nil
		}
	}
	// 2) 正则代码块（兼容旧响应）
	if m := reJSONFence.FindStringSubmatch(jsonPart); m != nil && len(m) > 1 {
		jsonContent := compactArrayOpen(strings.TrimSpace(m[1]))
		jsonContent = fixMissingQuotes(jsonContent)
		if err := validateJSONFormat(jsonContent); err == nil {
			if decisions, err := tryUnmarshalDecisionArray(jsonContent); err == nil && len(decisions) > 0 {
				normalizeDecisionActions(decisions)
				logger.Infof("✓ Extracted decisions from JSON code fence (legacy format)")
				return decisions, nil
			}
		}
	}
	// 3) 括号匹配提取数组（全文或 jsonPart）
	if decisions, ok := tryExtractArray(jsonPart); ok {
		normalizeDecisionActions(decisions)
		logger.Infof("✓ Extracted decisions from JSON array (bracket match)")
		return decisions, nil
	}

	// 4) 单条决策对象兼容：{"symbol":"BTCUSDT","action":"wait",...}
	for i := 0; i < len(jsonPart); i++ {
		if jsonPart[i] == '{' {
			objEnd := findMatchingBrace(jsonPart, i)
			if objEnd > i {
				objStr := jsonPart[i : objEnd+1]
				var single Decision
				if err := json.Unmarshal([]byte(tryRepairTrailingComma(objStr)), &single); err == nil {
					if single.Symbol != "" || single.Action != "" {
						normalizeDecisionAction(&single)
						if single.Action == "" {
							single.Action = "wait"
						}
						logger.Infof("✓ Extracted single decision object (wrapped as array)")
						return []Decision{single}, nil
					}
				}
			}
		}
	}

	// 5) 无法解析时安全回退
	logger.Infof("⚠️  [SafeFallback] AI didn't output structured JSON decision, entering safe wait mode")
	cotSummary := jsonPart
	if len(cotSummary) > 240 {
		cotSummary = cotSummary[:240] + "..."
	}
	return []Decision{{
		Symbol:    "ALL",
		Action:    "wait",
		Reasoning: fmt.Sprintf("Model didn't output structured JSON decision, entering safe wait; summary: %s", cotSummary),
	}}, nil
}

func fixMissingQuotes(jsonStr string) string {
	jsonStr = strings.ReplaceAll(jsonStr, "\u201c", "\"")
	jsonStr = strings.ReplaceAll(jsonStr, "\u201d", "\"")
	jsonStr = strings.ReplaceAll(jsonStr, "\u2018", "'")
	jsonStr = strings.ReplaceAll(jsonStr, "\u2019", "'")

	jsonStr = strings.ReplaceAll(jsonStr, "［", "[")
	jsonStr = strings.ReplaceAll(jsonStr, "］", "]")
	jsonStr = strings.ReplaceAll(jsonStr, "｛", "{")
	jsonStr = strings.ReplaceAll(jsonStr, "｝", "}")
	jsonStr = strings.ReplaceAll(jsonStr, "：", ":")
	jsonStr = strings.ReplaceAll(jsonStr, "，", ",")

	jsonStr = strings.ReplaceAll(jsonStr, "【", "[")
	jsonStr = strings.ReplaceAll(jsonStr, "】", "]")
	jsonStr = strings.ReplaceAll(jsonStr, "〔", "[")
	jsonStr = strings.ReplaceAll(jsonStr, "〕", "]")
	jsonStr = strings.ReplaceAll(jsonStr, "、", ",")

	jsonStr = strings.ReplaceAll(jsonStr, "　", " ")

	return jsonStr
}

func validateJSONFormat(jsonStr string) error {
	trimmed := strings.TrimSpace(jsonStr)

	if !reArrayHead.MatchString(trimmed) {
		if strings.HasPrefix(trimmed, "[") && !strings.Contains(trimmed[:min(20, len(trimmed))], "{") {
			return fmt.Errorf("not a valid decision array (must contain objects {}), actual content: %s", trimmed[:min(50, len(trimmed))])
		}
		return fmt.Errorf("JSON must start with [{ (whitespace allowed), actual: %s", trimmed[:min(20, len(trimmed))])
	}

	if strings.Contains(jsonStr, "~") {
		return fmt.Errorf("JSON cannot contain range symbol ~, all numbers must be precise single values")
	}

	for i := 0; i < len(jsonStr)-4; i++ {
		if jsonStr[i] >= '0' && jsonStr[i] <= '9' &&
			jsonStr[i+1] == ',' &&
			jsonStr[i+2] >= '0' && jsonStr[i+2] <= '9' &&
			jsonStr[i+3] >= '0' && jsonStr[i+3] <= '9' &&
			jsonStr[i+4] >= '0' && jsonStr[i+4] <= '9' {
			return fmt.Errorf("JSON numbers cannot contain thousand separator comma, found: %s", jsonStr[i:min(i+10, len(jsonStr))])
		}
	}

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func removeInvisibleRunes(s string) string {
	return reInvisibleRunes.ReplaceAllString(s, "")
}

func compactArrayOpen(s string) string {
	return reArrayOpenSpace.ReplaceAllString(strings.TrimSpace(s), "[{")
}

// ============================================================================
// Decision Validation
// ============================================================================

func validateDecisions(decisions []Decision, accountEquity float64, btcEthLeverage, altcoinLeverage int, btcEthPosRatio, altcoinPosRatio float64, excludedCoins []string) error {
	for i := range decisions {
		if err := validateDecision(&decisions[i], accountEquity, btcEthLeverage, altcoinLeverage, btcEthPosRatio, altcoinPosRatio, excludedCoins); err != nil {
			return fmt.Errorf("decision #%d validation failed: %w", i+1, err)
		}
	}
	return nil
}

func validateDecision(d *Decision, accountEquity float64, btcEthLeverage, altcoinLeverage int, btcEthPosRatio, altcoinPosRatio float64, excludedCoins []string) error {
	validActions := map[string]bool{
		"open_long":   true,
		"open_short":  true,
		"close_long":  true,
		"close_short": true,
		"hold":        true,
		"wait":        true,
	}

	if !validActions[d.Action] {
		return fmt.Errorf("invalid action: %s", d.Action)
	}

	// 检查币种是否在排除列表中（仅对开仓操作进行检查）
	if (d.Action == "open_long" || d.Action == "open_short") && len(excludedCoins) > 0 {
		normalizedSymbol := market.Normalize(d.Symbol)
		for _, excludedCoin := range excludedCoins {
			normalizedExcluded := market.Normalize(excludedCoin)
			if normalizedSymbol == normalizedExcluded {
				return fmt.Errorf("symbol %s is in the excluded coins list, cannot open new position", d.Symbol)
			}
		}
	}

	if d.Action == "open_long" || d.Action == "open_short" {
		maxLeverage := altcoinLeverage
		posRatio := altcoinPosRatio
		maxPositionValue := accountEquity * posRatio
		if d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT" {
			maxLeverage = btcEthLeverage
			posRatio = btcEthPosRatio
			maxPositionValue = accountEquity * posRatio
		}

		if d.Leverage <= 0 {
			return fmt.Errorf("leverage must be greater than 0: %d", d.Leverage)
		}
		if d.Leverage > maxLeverage {
			logger.Infof("⚠️  [Leverage Fallback] %s leverage exceeded (%dx > %dx), auto-adjusting to limit %dx",
				d.Symbol, d.Leverage, maxLeverage, maxLeverage)
			d.Leverage = maxLeverage
		}
		if d.PositionSizeUSD <= 0 {
			return fmt.Errorf("position size must be greater than 0: %.2f", d.PositionSizeUSD)
		}

		const minPositionSizeGeneral = 12.0
		const minPositionSizeBTCETH = 60.0

		if d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT" {
			if d.PositionSizeUSD < minPositionSizeBTCETH {
				return fmt.Errorf("%s opening amount too small (%.2f USDT), must be ≥%.2f USDT", d.Symbol, d.PositionSizeUSD, minPositionSizeBTCETH)
			}
		} else {
			if d.PositionSizeUSD < minPositionSizeGeneral {
				return fmt.Errorf("opening amount too small (%.2f USDT), must be ≥%.2f USDT", d.PositionSizeUSD, minPositionSizeGeneral)
			}
		}

		tolerance := maxPositionValue * 0.01
		if d.PositionSizeUSD > maxPositionValue+tolerance {
			if d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT" {
				return fmt.Errorf("BTC/ETH single coin position value cannot exceed %.0f USDT (%.1fx account equity), actual: %.0f", maxPositionValue, posRatio, d.PositionSizeUSD)
			} else {
				return fmt.Errorf("altcoin single coin position value cannot exceed %.0f USDT (%.1fx account equity), actual: %.0f", maxPositionValue, posRatio, d.PositionSizeUSD)
			}
		}
		if d.StopLoss <= 0 || d.TakeProfit <= 0 {
			return fmt.Errorf("stop loss and take profit must be greater than 0")
		}
		if d.Confidence <= 0 || d.Confidence > 100 {
			return fmt.Errorf("open position requires confidence (1-100), got %d", d.Confidence)
		}

		if d.Action == "open_long" {
			if d.StopLoss >= d.TakeProfit {
				return fmt.Errorf("for long positions, stop loss price must be less than take profit price")
			}
		} else {
			if d.StopLoss <= d.TakeProfit {
				return fmt.Errorf("for short positions, stop loss price must be greater than take profit price")
			}
		}

		var entryPrice float64
		if d.Action == "open_long" {
			entryPrice = d.StopLoss + (d.TakeProfit-d.StopLoss)*0.2
		} else {
			entryPrice = d.StopLoss - (d.StopLoss-d.TakeProfit)*0.2
		}

		var riskPercent, rewardPercent, riskRewardRatio float64
		if d.Action == "open_long" {
			riskPercent = (entryPrice - d.StopLoss) / entryPrice * 100
			rewardPercent = (d.TakeProfit - entryPrice) / entryPrice * 100
			if riskPercent > 0 {
				riskRewardRatio = rewardPercent / riskPercent
			}
		} else {
			riskPercent = (d.StopLoss - entryPrice) / entryPrice * 100
			rewardPercent = (entryPrice - d.TakeProfit) / entryPrice * 100
			if riskPercent > 0 {
				riskRewardRatio = rewardPercent / riskPercent
			}
		}

		if riskRewardRatio < 3.0 {
			return fmt.Errorf("risk/reward ratio too low (%.2f:1), must be ≥3.0:1 [risk: %.2f%% reward: %.2f%%] [stop loss: %.2f take profit: %.2f]",
				riskRewardRatio, riskPercent, rewardPercent, d.StopLoss, d.TakeProfit)
		}
	}

	return nil
}

// ============================================================================
// Helper Functions
// ============================================================================

// detectLanguage detects language from text content
// Returns LangChinese if text contains Chinese characters, otherwise LangEnglish
func detectLanguage(text string) Language {
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF {
			return LangChinese
		}
	}
	return LangEnglish
}

// ============================================================================
// Ranking Data Optimization Functions (减少 Token 消耗)
// ============================================================================

// limitOIRankingData 限制 OI 排名数据，只保留 Top N
func limitOIRankingData(data *nofxos.OIRankingData, limit int) *nofxos.OIRankingData {
	if data == nil {
		return nil
	}

	limited := &nofxos.OIRankingData{
		TimeRange: data.TimeRange,
		Duration:  data.Duration,
		FetchedAt: data.FetchedAt,
	}

	// 限制 TopPositions
	if len(data.TopPositions) > limit {
		limited.TopPositions = data.TopPositions[:limit]
	} else {
		limited.TopPositions = data.TopPositions
	}

	// 限制 LowPositions
	if len(data.LowPositions) > limit {
		limited.LowPositions = data.LowPositions[:limit]
	} else {
		limited.LowPositions = data.LowPositions
	}

	return limited
}

// limitNetFlowRankingData 限制 NetFlow 排名数据，只保留 Top N
func limitNetFlowRankingData(data *nofxos.NetFlowRankingData, limit int) *nofxos.NetFlowRankingData {
	if data == nil {
		return nil
	}

	limited := &nofxos.NetFlowRankingData{
		Duration:  data.Duration,
		TimeRange: data.TimeRange,
		FetchedAt: data.FetchedAt,
	}

	// 限制各个排名列表
	if len(data.InstitutionFutureTop) > limit {
		limited.InstitutionFutureTop = data.InstitutionFutureTop[:limit]
	} else {
		limited.InstitutionFutureTop = data.InstitutionFutureTop
	}

	if len(data.InstitutionFutureLow) > limit {
		limited.InstitutionFutureLow = data.InstitutionFutureLow[:limit]
	} else {
		limited.InstitutionFutureLow = data.InstitutionFutureLow
	}

	if len(data.PersonalFutureTop) > limit {
		limited.PersonalFutureTop = data.PersonalFutureTop[:limit]
	} else {
		limited.PersonalFutureTop = data.PersonalFutureTop
	}

	if len(data.PersonalFutureLow) > limit {
		limited.PersonalFutureLow = data.PersonalFutureLow[:limit]
	} else {
		limited.PersonalFutureLow = data.PersonalFutureLow
	}

	return limited
}

// limitPriceRankingData 限制 Price 排名数据，只保留 Top N
func limitPriceRankingData(data *nofxos.PriceRankingData, limit int) *nofxos.PriceRankingData {
	if data == nil {
		return nil
	}

	limited := &nofxos.PriceRankingData{
		FetchedAt: data.FetchedAt,
		Durations: make(map[string]*nofxos.PriceRankingDuration),
	}

	// 限制每个时间段的 Top 和 Low
	for duration, durationData := range data.Durations {
		if durationData == nil {
			continue
		}

		limitedDuration := &nofxos.PriceRankingDuration{}

		if len(durationData.Top) > limit {
			limitedDuration.Top = durationData.Top[:limit]
		} else {
			limitedDuration.Top = durationData.Top
		}

		if len(durationData.Low) > limit {
			limitedDuration.Low = durationData.Low[:limit]
		} else {
			limitedDuration.Low = durationData.Low
		}

		limited.Durations[duration] = limitedDuration
	}

	return limited
}

// AnalystRankingsTopN 分析师用全市场排名条数（与 OI movers 一致，便于宏观宽度判断）
const AnalystRankingsTopN = 10

// FormatMarketRankingsForAnalyst 为分析师生成全市场排名摘要（OI / 资金流 / 涨跌榜），Top 10，与交易员同源
// 供分析师做宏观宽度与资金流向判断；无数据时返回空字符串，由调用方决定是否显示占位
func FormatMarketRankingsForAnalyst(ctx *Context, lang Language) string {
	if ctx == nil {
		return ""
	}
	nofxosLang := nofxos.LangEnglish
	if lang == LangChinese {
		nofxosLang = nofxos.LangChinese
	}
	var sb strings.Builder
	if ctx.OIRankingData != nil {
		limited := limitOIRankingData(ctx.OIRankingData, AnalystRankingsTopN)
		sb.WriteString(nofxos.FormatOIRankingForAI(limited, nofxosLang))
	}
	if ctx.NetFlowRankingData != nil {
		limited := limitNetFlowRankingData(ctx.NetFlowRankingData, AnalystRankingsTopN)
		sb.WriteString(nofxos.FormatNetFlowRankingForAI(limited, nofxosLang))
	}
	if ctx.PriceRankingData != nil {
		limited := limitPriceRankingData(ctx.PriceRankingData, AnalystRankingsTopN)
		sb.WriteString(nofxos.FormatPriceRankingForAI(limited, nofxosLang))
	}
	if sb.Len() == 0 {
		return ""
	}
	return sb.String()
}
