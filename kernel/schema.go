package kernel

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ============================================================================
// Trading Data Schema - 交易数据字典
// ============================================================================
// 双语数据字典，支持中文和英文
// 确保AI能够100%理解数据格式，无论使用哪种语言
// ============================================================================

const (
	SchemaVersion = "1.0.0"
)

// Language 语言类型
type Language string

const (
	LangChinese Language = "zh-CN"
	LangEnglish Language = "en-US"
)

// ModelSize 模型大小类型（影响术语说明的详细程度）
type ModelSize string

const (
	ModelLarge ModelSize = "large" // 大模型(GPT-4o/Claude)使用精简版
	ModelSmall ModelSize = "small" // 小模型(GPT-3.5/Haiku)使用完整版
)

// ========== 双语字段定义 ==========

// BilingualFieldDef 双语字段定义
type BilingualFieldDef struct {
	NameZH    string // 中文名称
	NameEN    string // English name
	Unit      string // 单位
	FormulaZH string // 中文公式
	FormulaEN string // English formula
	DescZH    string // 中文描述
	DescEN    string // English description
}

// GetName 获取字段名称（根据语言）
func (d BilingualFieldDef) GetName(lang Language) string {
	if lang == LangChinese {
		return d.NameZH
	}
	return d.NameEN
}

// GetFormula 获取公式（根据语言）
func (d BilingualFieldDef) GetFormula(lang Language) string {
	if lang == LangChinese {
		return d.FormulaZH
	}
	return d.FormulaEN
}

// GetDesc 获取描述（根据语言）
func (d BilingualFieldDef) GetDesc(lang Language) string {
	if lang == LangChinese {
		return d.DescZH
	}
	return d.DescEN
}

// ========== 数据字典 ==========

// DataDictionary 数据字典：定义所有字段的含义
var DataDictionary = map[string]map[string]BilingualFieldDef{
	"AccountMetrics": {
		"Equity": {
			NameZH:    "总权益",
			NameEN:    "Total Equity",
			Unit:      "USDT",
			FormulaZH: "可用余额 + 未实现盈亏",
			FormulaEN: "Available Balance + Unrealized PnL",
			DescZH:    "账户的实际净值，包含所有持仓的浮动盈亏",
			DescEN:    "Actual account value including all unrealized P&L from positions",
		},
		"Balance": {
			NameZH:    "可用余额",
			NameEN:    "Available Balance",
			Unit:      "USDT",
			FormulaZH: "初始资金 + 已实现盈亏",
			FormulaEN: "Initial Capital + Realized PnL",
			DescZH:    "可用于开新仓位的资金，不包括已用保证金",
			DescEN:    "Available funds for opening new positions, excluding used margin",
		},
		"PnL": {
			NameZH:    "总盈亏百分比",
			NameEN:    "Total PnL Percentage",
			Unit:      "%",
			FormulaZH: "(总权益 - 初始资金) / 初始资金 × 100",
			FormulaEN: "(Total Equity - Initial Capital) / Initial Capital × 100",
			DescZH:    "自系统启动以来的总收益率，+15.87%表示盈利15.87%",
			DescEN:    "Total return since inception, +15.87% means 15.87% profit",
		},
		"Margin": {
			NameZH:    "保证金使用率",
			NameEN:    "Margin Usage Rate",
			Unit:      "%",
			FormulaZH: "已用保证金合计 / 总权益 × 100",
			FormulaEN: "Total Used Margin / Total Equity × 100",
			DescZH:    "该值越高，账户风险越大。安全值<30%，危险值>70%",
			DescEN:    "Higher value = higher risk. Safe <30%, Dangerous >70%",
		},
	},

	"TradeMetrics": {
		"Entry": {
			NameZH: "进场价",
			NameEN: "Entry Price",
			Unit:   "USDT",
			DescZH: "开仓时的平均价格",
			DescEN: "Average price when opening position",
		},
		"Exit": {
			NameZH: "出场价",
			NameEN: "Exit Price",
			Unit:   "USDT",
			DescZH: "平仓时的平均价格",
			DescEN: "Average price when closing position",
		},
		"Profit": {
			NameZH:    "已实现盈亏",
			NameEN:    "Realized PnL",
			Unit:      "USDT",
			FormulaZH: "(出场价 - 进场价) / 进场价 × 杠杆 × 仓位价值 - 手续费",
			FormulaEN: "(Exit Price - Entry Price) / Entry Price × Leverage × Position Value - Fees",
			DescZH:    "已平仓交易的实际盈亏，已扣除开仓和平仓手续费（约0.08%）。正值=盈利，负值=亏损。注意：设置止盈止损时需考虑手续费影响",
			DescEN:    "Actual profit/loss of closed trades after deducting opening and closing fees (~0.08%). Positive=profit, Negative=loss. Note: Trading fees must be considered when setting stop-loss and take-profit prices",
		},
		"PnL%": {
			NameZH:    "盈亏百分比",
			NameEN:    "PnL Percentage",
			Unit:      "%",
			FormulaZH: "(出场价 - 进场价) / 进场价 × 杠杆 × 100 - 手续费%",
			FormulaEN: "(Exit - Entry) / Entry × Leverage × 100 - Fees%",
			DescZH:    "已平仓交易的收益率（含手续费）。显示值已扣除约0.06-0.10%的手续费（开仓+平仓）",
			DescEN:    "Return on closed trade (after fees). Display value already deducted ~0.06-0.10% fees (open+close)",
		},
		"HoldDuration": {
			NameZH: "持仓时长",
			NameEN: "Holding Duration",
			Unit:   "minutes",
			DescZH: "从开仓到平仓的时间。<15分钟=超短线，15分钟-4小时=日内，>4小时=波段",
			DescEN: "Time from open to close. <15min=scalping, 15min-4h=intraday, >4h=swing",
		},
	},

	"PositionMetrics": {
		"UnrealizedPnL%": {
			NameZH:    "未实现盈亏百分比",
			NameEN:    "Unrealized PnL Percentage",
			Unit:      "%",
			FormulaZH: "(当前价 - 进场价) / 进场价 × 杠杆 × 100",
			FormulaEN: "(Current Price - Entry Price) / Entry Price × Leverage × 100",
			DescZH:    "当前持仓的浮动盈亏（未扣手续费）。平仓后实际盈亏约减少0.06-0.10%（平仓手续费）",
			DescEN:    "Floating P&L (before fees). Actual realized PnL will be ~0.06-0.10% lower (close fees)",
		},
		"PeakPnL%": {
			NameZH: "峰值盈亏百分比",
			NameEN: "Peak PnL Percentage",
			Unit:   "%",
			DescZH: "该持仓曾经达到的最高未实现盈亏。用于判断是否需要止盈",
			DescEN: "Historical max unrealized PnL for this position. Used for take-profit decisions",
		},
		"Drawdown": {
			NameZH:    "从峰值回撤",
			NameEN:    "Drawdown from Peak",
			Unit:      "%",
			FormulaZH: "当前盈亏% - 峰值盈亏%",
			FormulaEN: "Current PnL% - Peak PnL%",
			DescZH:    "负值表示正在回撤。例如：峰值+5%，当前+3%，回撤=-2%",
			DescEN:    "Negative = pulling back. E.g., Peak +5%, Current +3%, Drawdown = -2%",
		},
		"Leverage": {
			NameZH: "杠杆倍数",
			NameEN: "Leverage",
			Unit:   "x",
			DescZH: "3x表示价格变动1%，持仓盈亏变动3%。杠杆越高，风险越大",
			DescEN: "3x means 1% price move = 3% position PnL. Higher leverage = higher risk",
		},
		"Margin": {
			NameZH:    "占用保证金",
			NameEN:    "Margin Used",
			Unit:      "USDT",
			FormulaZH: "仓位价值 / 杠杆",
			FormulaEN: "Position Value / Leverage",
			DescZH:    "该仓位锁定的保证金金额",
			DescEN:    "Collateral locked for this position",
		},
		"LiqPrice": {
			NameZH: "强平价格",
			NameEN: "Liquidation Price",
			Unit:   "USDT",
			DescZH: "价格触及此值时会被强制平仓。0.0000表示无爆仓风险",
			DescEN: "Price at which position will be force-closed. 0.0000 = no liquidation risk",
		},
	},

	"MarketData": {
		"Volume": {
			NameZH: "成交量",
			NameEN: "Volume",
			Unit:   "base asset",
			DescZH: "该时间段的交易量",
			DescEN: "Trading volume in this period",
		},
		"OI": {
			NameZH: "持仓量",
			NameEN: "Open Interest",
			Unit:   "USDT",
			DescZH: "未平仓合约的总价值。持仓量增加=资金流入，减少=资金流出",
			DescEN: "Total value of open contracts. Increasing OI = capital inflow, decreasing = outflow",
		},
		"OIChange": {
			NameZH: "持仓量变化",
			NameEN: "OI Change",
			Unit:   "USDT & %",
			DescZH: "1小时内持仓量的变化。用于判断市场真实资金流向",
			DescEN: "OI change in 1 hour. Used to determine real capital flow direction",
		},
	},
}

// ========== 双语规则定义 ==========

// BilingualRuleDef 双语规则定义
type BilingualRuleDef struct {
	Value    interface{} // 规则值
	DescZH   string      // 中文描述
	DescEN   string      // English description
	ReasonZH string      // 中文原因
	ReasonEN string      // English reason
}

// GetDesc 获取描述（根据语言）
func (d BilingualRuleDef) GetDesc(lang Language) string {
	if lang == LangChinese {
		return d.DescZH
	}
	return d.DescEN
}

// GetReason 获取原因（根据语言）
func (d BilingualRuleDef) GetReason(lang Language) string {
	if lang == LangChinese {
		return d.ReasonZH
	}
	return d.ReasonEN
}

// ========== 交易规则 ==========

// TradingRules 交易规则定义
var TradingRules = struct {
	RiskManagement  map[string]BilingualRuleDef
	EntrySignals    map[string]BilingualRuleDef
	ExitSignals     map[string]BilingualRuleDef
	PositionControl map[string]BilingualRuleDef
}{
	RiskManagement: map[string]BilingualRuleDef{
		"MaxMarginUsage": {
			Value:    0.30,
			DescZH:   "保证金使用率不得超过30%",
			DescEN:   "Margin usage must not exceed 30%",
			ReasonZH: "保留70%的资金应对极端行情和追加保证金",
			ReasonEN: "Reserve 70% capital for extreme market conditions and margin calls",
		},
		"MaxPositionLoss": {
			Value:    -0.05,
			DescZH:   "单个持仓亏损达到-5%时必须止损（显示值，不含平仓手续费）",
			DescEN:   "Must stop-loss when displayed position loss reaches -5% (before close fees)",
			ReasonZH: "避免单笔交易造成过大损失。设置止损价时预留约0.1%缓冲：做多调高，做空调低",
			ReasonEN: "Prevent excessive loss. Set SL price with ~0.1% buffer: LONG=higher, SHORT=lower",
		},
		"MaxDailyLoss": {
			Value:    -0.10,
			DescZH:   "单日亏损达到-10%时停止交易",
			DescEN:   "Stop trading when daily loss reaches -10%",
			ReasonZH: "防止情绪化交易导致连续亏损",
			ReasonEN: "Prevent emotional trading leading to consecutive losses",
		},
		"PositionSizeLimit": {
			Value:    0.15,
			DescZH:   "单个仓位不得超过总权益的15%",
			DescEN:   "Single position must not exceed 15% of total equity",
			ReasonZH: "避免过度集中风险",
			ReasonEN: "Avoid excessive risk concentration",
		},
	},

	EntrySignals: map[string]BilingualRuleDef{
		"VolumeSpike": {
			Value:    2.0,
			DescZH:   "成交量是平均值的2倍以上时考虑进场",
			DescEN:   "Consider entry when volume is 2x above average",
			ReasonZH: "放量突破通常意味着强趋势",
			ReasonEN: "Volume breakout usually indicates strong trend",
		},
		"OIChangeThreshold": {
			Value:    0.02,
			DescZH:   "持仓量1小时内变化超过2%视为显著变化",
			DescEN:   "OI change >2% in 1 hour is considered significant",
			ReasonZH: "大额资金进出会导致持仓量显著变化",
			ReasonEN: "Large capital flows cause significant OI changes",
		},
	},

	ExitSignals: map[string]BilingualRuleDef{
		"TrailingStop": {
			Value:    0.30,
			DescZH:   "当盈亏从峰值回撤30%时平仓止盈",
			DescEN:   "Close position when PnL pulls back 30% from peak",
			ReasonZH: "锁定大部分利润，避免盈利回吐。例如：峰值+5%，回撤到+3.5%时平仓（注意：显示盈亏不含平仓手续费，实际到手约少0.04%）",
			ReasonEN: "Lock in most profits, avoid profit giveback. E.g., Peak +5%, close at +3.5% (Note: displayed PnL excludes close fees, actual ~0.04% less)",
		},
		"StopLoss": {
			Value:    -0.05,
			DescZH:   "硬止损设置在-5%（目标值，实际触发价应更早以覆盖手续费）",
			DescEN:   "Hard stop-loss target at -5% (trigger price should be closer to entry to cover fees)",
			ReasonZH: "严格控制单笔最大损失。设置止损价时需考虑手续费：做多时止损价调高，做空时止损价调低，让止损更早触发",
			ReasonEN: "Strictly control max loss. When setting SL price, account for fees: LONG=set SL higher, SHORT=set SL lower, to trigger earlier",
		},
	},

	PositionControl: map[string]BilingualRuleDef{
		"ScaleIn": {
			Value:    map[string]interface{}{"enabled": true, "max_additions": 2, "price_requirement": 0.01},
			DescZH:   "只在盈利仓位上加仓，最多加2次，价格需比平均成本高1%",
			DescEN:   "Only add to winning positions, max 2 additions, price must be 1% above avg cost",
			ReasonZH: "顺势加仓，不追亏损",
			ReasonEN: "Add to winners, never average down losers",
		},
		"ScaleOut": {
			Value: []map[string]interface{}{
				{"pnl": 0.03, "close_pct": 0.33},
				{"pnl": 0.05, "close_pct": 0.50},
				{"pnl": 0.08, "close_pct": 1.00},
			},
			DescZH:   "分批止盈：显示盈利达3%时平33%，5%时平50%，8%时全平",
			DescEN:   "Scale-out: Close 33% at displayed +3%, 50% at +5%, 100% at +8%",
			ReasonZH: "在保证利润的同时让盈利奔跑。注意：显示盈亏是未扣平仓手续费的，设置止盈价时应预留约0.1%缓冲（做多调低，做空调高）",
			ReasonEN: "Lock profits while letting winners run. Note: displayed PnL excludes close fees; set TP price with ~0.1% buffer (LONG=lower, SHORT=higher)",
		},
	},
}

// ========== OI解读 ==========

// OIInterpretation OI变化的市场解读（双语）
type OIInterpretationType struct {
	OIUp_PriceUp struct {
		ZH string
		EN string
	}
	OIUp_PriceDown struct {
		ZH string
		EN string
	}
	OIDown_PriceUp struct {
		ZH string
		EN string
	}
	OIDown_PriceDown struct {
		ZH string
		EN string
	}
}

var OIInterpretation = OIInterpretationType{
	OIUp_PriceUp: struct {
		ZH string
		EN string
	}{
		ZH: "强多头趋势（新多单开仓，资金流入做多）",
		EN: "Strong bullish trend (new longs opening, capital flowing into long positions)",
	},
	OIUp_PriceDown: struct {
		ZH string
		EN string
	}{
		ZH: "强空头趋势（新空单开仓，资金流入做空）",
		EN: "Strong bearish trend (new shorts opening, capital flowing into short positions)",
	},
	OIDown_PriceUp: struct {
		ZH string
		EN string
	}{
		ZH: "空头平仓（空头止损离场，可能出现反转）",
		EN: "Shorts covering (shorts stopped out, potential reversal)",
	},
	OIDown_PriceDown: struct {
		ZH string
		EN string
	}{
		ZH: "多头平仓（多头止损离场，可能出现反转）",
		EN: "Longs closing (longs stopped out, potential reversal)",
	},
}

// ========== 常见错误 ==========

// CommonMistake 常见错误定义
type CommonMistake struct {
	ErrorZH   string
	ErrorEN   string
	ExampleZH string
	ExampleEN string
	CorrectZH string
	CorrectEN string
}

var CommonMistakes = []CommonMistake{
	{
		ErrorZH:   "混淆已实现盈亏和未实现盈亏",
		ErrorEN:   "Confusing realized and unrealized P&L",
		ExampleZH: "将历史交易的盈亏与当前持仓的盈亏相加",
		ExampleEN: "Adding historical trade P&L with current position P&L",
		CorrectZH: "已实现盈亏已经计入账户余额，不应重复计算",
		CorrectEN: "Realized P&L is already included in account balance, don't double count",
	},
	{
		ErrorZH:   "忽略杠杆对盈亏的影响",
		ErrorEN:   "Ignoring leverage's impact on P&L",
		ExampleZH: "价格涨1%，认为盈利1%",
		ExampleEN: "Price up 1%, thinking profit is 1%",
		CorrectZH: "3x杠杆时，价格涨1%，实际盈利约3%",
		CorrectEN: "With 3x leverage, 1% price move = ~3% P&L",
	},
	{
		ErrorZH:   "不理解Peak PnL的重要性",
		ErrorEN:   "Not understanding Peak PnL's importance",
		ExampleZH: "只关注当前PnL，不关注回撤",
		ExampleEN: "Only watching current PnL, ignoring drawdown",
		CorrectZH: "当前PnL接近Peak PnL时，应考虑止盈以锁定利润",
		CorrectEN: "When current PnL near Peak PnL, consider taking profit to lock in gains",
	},
	{
		ErrorZH:   "忽略持仓量(OI)变化",
		ErrorEN:   "Ignoring Open Interest changes",
		ExampleZH: "只看价格K线，不看资金流向",
		ExampleEN: "Only watching price candles, not capital flows",
		CorrectZH: "结合OI变化判断趋势的真实性和持续性",
		CorrectEN: "Use OI changes to validate trend authenticity and sustainability",
	},
	{
		ErrorZH:   "忽略交易手续费对止盈止损的影响",
		ErrorEN:   "Ignoring trading fees when setting SL/TP",
		ExampleZH: "目标-5%止损，直接设置-5%价位；目标+8%止盈，直接设置+8%价位",
		ExampleEN: "Target -5% SL, directly set -5% price; target +8% TP, directly set +8% price",
		CorrectZH: "需预留手续费缓冲：做多时止损价调高、止盈价调低；做空时止损价调低、止盈价调高。确保扣费后实际盈亏达到目标",
		CorrectEN: "Must add fee buffer: LONG=SL higher/TP lower, SHORT=SL lower/TP higher. Ensure actual PnL after fees meets target",
	},
}

// ========== Prompt生成函数 ==========

// GetSchemaPrompt 生成Schema说明文本，用于AI Prompt
// 根据模型名称自动判断使用精简版还是完整版
// modelName: 模型名称（如 "gpt-4o", "claude-3-5-sonnet", "gpt-3.5-turbo" 等），如果为空则使用默认值
func GetSchemaPrompt(lang Language, modelName string) string {
	modelSize := DetectModelSize(modelName)
	return GetSchemaPromptWithModelSize(lang, modelSize)
}

// GetSchemaPromptWithModelSize 生成带信号说明的Schema（支持指定模型大小）
// modelSize: ModelLarge = 精简版信号说明, ModelSmall = 完整版信号说明
func GetSchemaPromptWithModelSize(lang Language, modelSize ModelSize) string {
	var prompt string
	if lang == LangChinese {
		prompt = getSchemaPromptZH()
	} else {
		prompt = getSchemaPromptEN()
	}
	// 追加信号说明
	prompt += GetSignalExplanation(lang, modelSize)
	return prompt
}

// DetectModelSize 根据模型名称自动判断模型大小
// 返回 ModelLarge（大模型，使用精简版）或 ModelSmall（小模型，使用完整版）
func DetectModelSize(modelName string) ModelSize {
	if modelName == "" {
		// 默认使用完整版（保守策略，确保小模型也能理解）
		return ModelSmall
	}

	modelNameLower := strings.ToLower(modelName)

	// 大模型列表（使用精简版）
	largeModels := []string{
		// OpenAI
		"gpt-4", "gpt-4o", "gpt-4-turbo", "gpt-4-", "o1", "o3",
		// Claude
		"claude-3", "claude-opus", "claude-sonnet", "claude-haiku",
		// DeepSeek
		"deepseek-chat", "deepseek-v2",
		// Gemini
		"gemini-pro", "gemini-ultra", "gemini-1.5",
		// Qwen
		"qwen-turbo", "qwen-plus", "qwen-max",
		// Kimi
		"moonshot-v1",
		// Grok
		"grok-beta",
	}

	// 检查是否为大模型
	for _, largeModel := range largeModels {
		if strings.Contains(modelNameLower, largeModel) {
			return ModelLarge
		}
	}

	// 小模型列表（使用完整版）
	smallModels := []string{
		// OpenAI
		"gpt-3.5", "gpt-3",
		// 其他小模型
		"text-davinci", "text-curie", "text-babbage", "text-ada",
	}

	// 检查是否为小模型
	for _, smallModel := range smallModels {
		if strings.Contains(modelNameLower, smallModel) {
			return ModelSmall
		}
	}

	// 默认策略：如果模型名称包含数字，尝试判断
	// 例如：gpt-4.x 系列通常是大模型，gpt-3.x 系列通常是小模型
	if strings.Contains(modelNameLower, "gpt-4") || strings.Contains(modelNameLower, "gpt-5") {
		return ModelLarge
	}
	if strings.Contains(modelNameLower, "gpt-3") {
		return ModelSmall
	}

	// 如果无法判断，默认使用完整版（保守策略）
	return ModelSmall
}

// getSchemaPromptZH 生成中文Prompt
func getSchemaPromptZH() string {
	prompt := "# 📖 数据字典与交易规则\n\n"
	prompt += "## 📊 字段含义说明\n\n"

	// 账户指标
	prompt += "### 账户指标\n"
	for key, field := range DataDictionary["AccountMetrics"] {
		prompt += formatFieldDefZH(key, field)
	}

	// 交易指标
	prompt += "\n### 交易指标\n"
	for key, field := range DataDictionary["TradeMetrics"] {
		prompt += formatFieldDefZH(key, field)
	}

	// 持仓指标
	prompt += "\n### 持仓指标\n"
	for key, field := range DataDictionary["PositionMetrics"] {
		prompt += formatFieldDefZH(key, field)
	}

	// 市场数据
	prompt += "\n### 市场数据\n"
	for key, field := range DataDictionary["MarketData"] {
		prompt += formatFieldDefZH(key, field)
	}

	// OI解读 - 已移至 SignalDictionary["OIPriceSignals"]，由 GetSignalExplanation 统一输出
	// prompt += "\n## 💹 持仓量(OI)变化解读\n\n"
	// prompt += "- **OI增加 + 价格上涨**: " + OIInterpretation.OIUp_PriceUp.ZH + "\n"
	// prompt += "- **OI增加 + 价格下跌**: " + OIInterpretation.OIUp_PriceDown.ZH + "\n"
	// prompt += "- **OI减少 + 价格上涨**: " + OIInterpretation.OIDown_PriceUp.ZH + "\n"
	// prompt += "- **OI减少 + 价格下跌**: " + OIInterpretation.OIDown_PriceDown.ZH + "\n"

	return prompt
}

// getSchemaPromptEN 生成英文Prompt
func getSchemaPromptEN() string {
	prompt := "# 📖 Data Dictionary & Trading Rules\n\n"
	prompt += "## 📊 Field Definitions\n\n"

	// Account Metrics
	prompt += "### Account Metrics\n"
	for key, field := range DataDictionary["AccountMetrics"] {
		prompt += formatFieldDefEN(key, field)
	}

	// Trade Metrics
	prompt += "\n### Trade Metrics\n"
	for key, field := range DataDictionary["TradeMetrics"] {
		prompt += formatFieldDefEN(key, field)
	}

	// Position Metrics
	prompt += "\n### Position Metrics\n"
	for key, field := range DataDictionary["PositionMetrics"] {
		prompt += formatFieldDefEN(key, field)
	}

	// Market Data
	prompt += "\n### Market Data\n"
	for key, field := range DataDictionary["MarketData"] {
		prompt += formatFieldDefEN(key, field)
	}

	// OI Interpretation - moved to SignalDictionary["OIPriceSignals"], output by GetSignalExplanation
	// prompt += "\n## 💹 Open Interest (OI) Change Interpretation\n\n"
	// prompt += "- **OI Up + Price Up**: " + OIInterpretation.OIUp_PriceUp.EN + "\n"
	// prompt += "- **OI Up + Price Down**: " + OIInterpretation.OIUp_PriceDown.EN + "\n"
	// prompt += "- **OI Down + Price Up**: " + OIInterpretation.OIDown_PriceUp.EN + "\n"
	// prompt += "- **OI Down + Price Down**: " + OIInterpretation.OIDown_PriceDown.EN + "\n"

	return prompt
}

// formatFieldDefZH 格式化中文字段定义
func formatFieldDefZH(key string, field BilingualFieldDef) string {
	result := "- **" + key + "**（" + field.NameZH + "）: " + field.DescZH
	if field.FormulaZH != "" {
		result += " | 公式: `" + field.FormulaZH + "`"
	}
	if field.Unit != "" {
		result += " | 单位: " + field.Unit
	}
	result += "\n"
	return result
}

// formatFieldDefEN 格式化英文字段定义
func formatFieldDefEN(key string, field BilingualFieldDef) string {
	result := "- **" + key + "** (" + field.NameEN + "): " + field.DescEN
	if field.FormulaEN != "" {
		result += " | Formula: `" + field.FormulaEN + "`"
	}
	if field.Unit != "" {
		result += " | Unit: " + field.Unit
	}
	result += "\n"
	return result
}

// ============================================================================
// Signal Dictionary - 信号术语字典
// ============================================================================
// 用于向AI解释优化后的数据信号含义
// 复用 BilingualFieldDef 结构，与 DataDictionary 风格统一
// 支持大模型(精简版)和小模型(完整版)两种输出
// ============================================================================

// SignalCategoryNames 信号分类名称（双语）
var SignalCategoryNames = map[string]struct{ ZH, EN string }{
	"CandlestickPatterns": {"K线形态信号", "Candlestick Patterns"},
	"TechnicalSignals":    {"技术指标信号", "Technical Indicator Signals"},
	"VolumePriceSignals":  {"量价信号", "Volume-Price Signals"},
	"OIPriceSignals":      {"OI-价格信号", "OI-Price Signals"},
	"FundFlowSignals":     {"资金流信号", "Fund Flow Signals"},
	"PriceChangeSignals":  {"涨跌幅信号", "Price Change Signals"},
	"VolatilitySignals":   {"波动率信号", "Volatility Signals"},
}

// SignalCategoryOrder 信号分类顺序（用于遍历时保持顺序）
var SignalCategoryOrder = []string{
	"CandlestickPatterns",
	"TechnicalSignals",
	"VolumePriceSignals",
	"OIPriceSignals",
	"FundFlowSignals",
	"PriceChangeSignals",
	"VolatilitySignals",
}

// SignalDictionary 信号术语字典（复用 BilingualFieldDef 结构）
// 结构与 DataDictionary 一致：map[分类名]map[信号Code]BilingualFieldDef
var SignalDictionary = map[string]map[string]BilingualFieldDef{
	"CandlestickPatterns": {
		"BULLISH_ENGULFING":    {NameZH: "看涨吞没", NameEN: "Bullish Engulfing", DescZH: "阳线完全包裹前一阴线，强烈看涨反转信号", DescEN: "Bullish candle fully engulfs prior bearish candle, strong bullish reversal"},
		"BEARISH_ENGULFING":    {NameZH: "看跌吞没", NameEN: "Bearish Engulfing", DescZH: "阴线完全包裹前一阳线，强烈看跌反转信号", DescEN: "Bearish candle fully engulfs prior bullish candle, strong bearish reversal"},
		"HAMMER":               {NameZH: "锤子线", NameEN: "Hammer", DescZH: "下影线长，底部反转信号", DescEN: "Long lower shadow, bottom reversal signal"},
		"INVERTED_HAMMER":      {NameZH: "倒锤子", NameEN: "Inverted Hammer", DescZH: "上影线长，底部反转信号", DescEN: "Long upper shadow, potential bottom reversal"},
		"SHOOTING_STAR":        {NameZH: "射击之星", NameEN: "Shooting Star", DescZH: "上影线长，顶部反转信号", DescEN: "Long upper shadow at top, bearish reversal"},
		"DOJI":                 {NameZH: "十字星", NameEN: "Doji", DescZH: "开盘≈收盘，市场犹豫，可能反转", DescEN: "Open≈Close, market indecision, potential reversal"},
		"MORNING_STAR":         {NameZH: "启明星", NameEN: "Morning Star", DescZH: "三根K线组合，强烈底部反转", DescEN: "3-candle pattern, strong bottom reversal"},
		"EVENING_STAR":         {NameZH: "黄昏星", NameEN: "Evening Star", DescZH: "三根K线组合，强烈顶部反转", DescEN: "3-candle pattern, strong top reversal"},
		"THREE_WHITE_SOLDIERS": {NameZH: "三白兵", NameEN: "Three White Soldiers", DescZH: "连续三根阳线，强烈看涨延续", DescEN: "3 consecutive bullish candles, strong bullish continuation"},
		"THREE_BLACK_CROWS":    {NameZH: "三乌鸦", NameEN: "Three Black Crows", DescZH: "连续三根阴线，强烈看跌延续", DescEN: "3 consecutive bearish candles, strong bearish continuation"},
	},
	"TechnicalSignals": {
		"GOLDEN_CROSS":       {NameZH: "金叉", NameEN: "Golden Cross", DescZH: "EMA20上穿EMA50，中期看涨信号", DescEN: "EMA20 crosses above EMA50, bullish signal"},
		"DEATH_CROSS":        {NameZH: "死叉", NameEN: "Death Cross", DescZH: "EMA20下穿EMA50，中期看跌信号", DescEN: "EMA20 crosses below EMA50, bearish signal"},
		"BULLISH_DIVERGENCE": {NameZH: "看涨背离", NameEN: "Bullish Divergence", DescZH: "价格创新低但RSI未创新低，潜在反弹", DescEN: "Price makes lower low but RSI doesn't, potential bounce"},
		"BEARISH_DIVERGENCE": {NameZH: "看跌背离", NameEN: "Bearish Divergence", DescZH: "价格创新高但RSI未创新高，潜在回调", DescEN: "Price makes higher high but RSI doesn't, potential pullback"},
	},
	"VolumePriceSignals": {
		"HEALTHY_UPTREND":   {NameZH: "健康上涨", NameEN: "Healthy Uptrend", DescZH: "价涨量增，趋势健康可持续", DescEN: "Price up with volume increase, healthy sustainable trend"},
		"HEALTHY_DOWNTREND": {NameZH: "健康下跌", NameEN: "Healthy Downtrend", DescZH: "价跌量增，下跌趋势确认", DescEN: "Price down with volume increase, downtrend confirmed"},
		"DISTRIBUTION":      {NameZH: "派发", NameEN: "Distribution", DescZH: "价涨量缩，上涨动能减弱", DescEN: "Price up but volume decreasing, weakening momentum"},
		"ACCUMULATION":      {NameZH: "吸筹", NameEN: "Accumulation", DescZH: "价跌量缩，抛压减弱", DescEN: "Price down but volume decreasing, selling pressure fading"},
		"STRONG_BUY":        {NameZH: "强力买入", NameEN: "Strong Buy", DescZH: "价涨伴随成交量激增(>2倍)", DescEN: "Price up with volume surge (>2x average)"},
		"STRONG_SELL":       {NameZH: "强力卖出", NameEN: "Strong Sell", DescZH: "价跌伴随成交量激增(>2倍)", DescEN: "Price down with volume surge (>2x average)"},
	},
	"OIPriceSignals": {
		"LONG_BUILD":  {NameZH: "多头建仓", NameEN: "Long Build", DescZH: "OI↑+价格↑，新多头入场，看涨延续", DescEN: "OI up + Price up, new longs entering, bullish continuation"},
		"SHORT_BUILD": {NameZH: "空头建仓", NameEN: "Short Build", DescZH: "OI↑+价格↓，新空头入场，看跌延续", DescEN: "OI up + Price down, new shorts entering, bearish continuation"},
		"SHORT_COV":   {NameZH: "空头回补", NameEN: "Short Covering", DescZH: "OI↓+价格↑，空头平仓，警惕反转", DescEN: "OI down + Price up, shorts closing, watch for reversal"},
		"LONG_LIQ":    {NameZH: "多头平仓", NameEN: "Long Liquidation", DescZH: "OI↓+价格↓，多头止损/清算", DescEN: "OI down + Price down, longs stopping out"},
		"SQUEEZE":     {NameZH: "轧空", NameEN: "Short Squeeze", DescZH: "价格上涨但OI和资金流出，空头被迫平仓", DescEN: "Price up but OI/flow out, shorts forced to cover"},
	},
	"FundFlowSignals": {
		"SMART_MONEY_ACCUMULATION": {NameZH: "聪明钱吸筹", NameEN: "Smart Money Accumulation", DescZH: "机构买入+散户卖出，强烈看多信号", DescEN: "Institution buying + Retail selling, strong bullish signal"},
		"DISTRIBUTION_WARNING":     {NameZH: "派发警告", NameEN: "Distribution Warning", DescZH: "机构卖出+散户买入，强烈看空信号", DescEN: "Institution selling + Retail buying, strong bearish signal"},
	},
	"PriceChangeSignals": {
		"STRONG":  {NameZH: "强势", NameEN: "Strong", DescZH: "涨幅大+OI增+资金流入，健康强势上涨", DescEN: "Price up + OI up + Flow in, healthy strong uptrend"},
		"HEALTHY": {NameZH: "健康", NameEN: "Healthy", DescZH: "涨幅伴随OI或资金流支撑", DescEN: "Price move supported by OI or flow"},
		"WEAK":    {NameZH: "弱势", NameEN: "Weak", DescZH: "跌幅大+OI减+资金流出，弱势下跌", DescEN: "Price down + OI down + Flow out, weak downtrend"},
	},
	"VolatilitySignals": {
		"LOW_VOL":     {NameZH: "低波动", NameEN: "Low Volatility", DescZH: "ATR<价格1%，市场平静，可能酝酿突破", DescEN: "ATR<1% of price, quiet market, potential breakout brewing"},
		"HIGH_VOL":    {NameZH: "高波动", NameEN: "High Volatility", DescZH: "ATR>价格3%，市场活跃，注意风险", DescEN: "ATR>3% of price, active market, watch risk"},
		"EXTREME_VOL": {NameZH: "极端波动", NameEN: "Extreme Volatility", DescZH: "ATR>价格5%，极端行情，降低仓位", DescEN: "ATR>5% of price, extreme conditions, reduce position"},
	},
}

// GetSignalExplanation 获取信号说明（根据语言和模型大小）
// modelSize: ModelLarge = 精简版(~150 tokens), ModelSmall = 完整版(~350 tokens)
func GetSignalExplanation(lang Language, modelSize ModelSize) string {
	if lang == LangChinese {
		return getSignalExplanationZH(modelSize)
	}
	return getSignalExplanationEN(modelSize)
}

// getSignalExplanationZH 生成中文信号说明
func getSignalExplanationZH(modelSize ModelSize) string {
	var sb strings.Builder

	// ### 级别，作为"字段含义说明"的子章节
	sb.WriteString("\n### 数据信号\n")

	if modelSize == ModelSmall {
		// 完整版 - 适用于小模型，复用 formatFieldDefZH 保持与 DataDictionary 格式一致
		for _, catKey := range SignalCategoryOrder {
			signals, ok := SignalDictionary[catKey]
			if !ok {
				continue
			}
			catName := SignalCategoryNames[catKey]
			sb.WriteString("#### " + catName.ZH + "\n")
			for code, field := range signals {
				sb.WriteString(formatFieldDefZH(code, field))
			}
			sb.WriteString("\n")
		}
	} else {
		// 精简版 - 适用于大模型
		sb.WriteString("#### K线形态\n")
		sb.WriteString("ENGULFING=吞没(强反转) | HAMMER=锤子(底部) | DOJI=十字星(犹豫) | MORNING/EVENING_STAR=启明/黄昏星 | THREE_WHITE/BLACK=三白兵/三乌鸦\n\n")

		sb.WriteString("#### 技术指标\n")
		sb.WriteString("GOLDEN_CROSS=金叉(看涨) | DEATH_CROSS=死叉(看空) | BULLISH_DIVERGENCE=看涨背离 | BEARISH_DIVERGENCE=看跌背离\n\n")

		sb.WriteString("#### OI-价格\n")
		sb.WriteString("LONG_BUILD=多头建仓(OI↑价↑) | SHORT_BUILD=空头建仓(OI↑价↓) | SHORT_COV=空头回补(OI↓价↑) | LONG_LIQ=多头平仓(OI↓价↓) | SQUEEZE=轧空\n\n")

		sb.WriteString("#### 资金流\n")
		sb.WriteString("SMART_MONEY_ACCUMULATION=机构买+散户卖(强看多) | DISTRIBUTION_WARNING=机构卖+散户买(强看空)\n\n")
	}

	return sb.String()
}

// getSignalExplanationEN 生成英文信号说明
func getSignalExplanationEN(modelSize ModelSize) string {
	var sb strings.Builder

	// ### level, as sub-section of "Field Definitions"
	sb.WriteString("\n### Signal Definitions\n")

	if modelSize == ModelSmall {
		// Full version - for smaller models，复用 formatFieldDefEN 保持与 DataDictionary 格式一致
		for _, catKey := range SignalCategoryOrder {
			signals, ok := SignalDictionary[catKey]
			if !ok {
				continue
			}
			catName := SignalCategoryNames[catKey]
			sb.WriteString("#### " + catName.EN + "\n")
			for code, field := range signals {
				sb.WriteString(formatFieldDefEN(code, field))
			}
			sb.WriteString("\n")
		}
	} else {
		// Compact version - for large models
		sb.WriteString("#### Candlestick Patterns\n")
		sb.WriteString("ENGULFING=strong reversal | HAMMER=bottom reversal | DOJI=indecision | MORNING/EVENING_STAR=reversal patterns | THREE_WHITE/BLACK=continuation\n\n")

		sb.WriteString("#### Technical Signals\n")
		sb.WriteString("GOLDEN_CROSS=bullish(EMA20>50) | DEATH_CROSS=bearish(EMA20<50) | BULLISH_DIVERGENCE=price low but RSI not | BEARISH_DIVERGENCE=price high but RSI not\n\n")

		sb.WriteString("#### OI-Price\n")
		sb.WriteString("LONG_BUILD=OI↑Price↑ | SHORT_BUILD=OI↑Price↓ | SHORT_COV=OI↓Price↑ | LONG_LIQ=OI↓Price↓ | SQUEEZE=price up but OI/flow out\n\n")

		sb.WriteString("#### Fund Flow\n")
		sb.WriteString("SMART_MONEY_ACCUMULATION=Inst buy+Retail sell(bullish) | DISTRIBUTION_WARNING=Inst sell+Retail buy(bearish)\n\n")
	}

	return sb.String()
}

// ============================================================================
// JSON Schema for AI Output Format - AI输出格式的JSON Schema
// ============================================================================
// 将系统提示词中的输出格式要求转换为JSON Schema，用于结构化输出
// ============================================================================

// GetDecisionJSONSchema 获取决策输出的JSON Schema（JSON字符串格式）
// 用于AI的结构化输出，确保输出格式符合要求
func GetDecisionJSONSchema(lang Language) string {
	if lang == LangChinese {
		return getDecisionJSONSchemaZH()
	}
	return getDecisionJSONSchemaEN()
}

// getDecisionJSONSchemaZH 生成中文描述的JSON Schema
func getDecisionJSONSchemaZH() string {
	return `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "description": "交易决策输出对象，包含思维链分析和决策数组",
  "required": ["thinking", "decisions"],
  "properties": {
    "thinking": {
      "type": "string",
      "description": "思维链分析过程，详细说明分析思路、市场判断、风险评估等思考过程。这是必需字段，必须详细说明决策依据和推理过程",
      "minLength": 50,
      "examples": [
        "分析账户状态：当前保证金使用率25%，在安全范围内。分析持仓：BTCUSDT当前PnL +2.96%，接近历史峰值+2.99%，回撤仅0.03%。5分钟K线显示价格接近短期阻力位，成交量开始萎缩，上涨动能减弱。建议部分平仓锁定利润。"
      ]
    },
    "decisions": {
      "type": "array",
      "description": "交易决策数组，每个元素代表一个交易决策",
      "items": {
        "type": "object",
        "required": ["symbol", "action", "reasoning"],
        "properties": {
          "symbol": {
            "type": "string",
            "description": "交易对符号，例如：BTCUSDT、ETHUSDT",
        "pattern": "^[A-Z0-9]+USDT$",
        "examples": ["BTCUSDT", "ETHUSDT", "BNBUSDT"]
      },
      "action": {
        "type": "string",
        "description": "交易动作类型",
        "enum": [
          "open_long",
          "open_short",
          "close_long",
          "close_short",
          "hold",
          "wait",
          "partial_close",
          "full_close",
          "add_position"
        ],
        "enumDescriptions": {
          "open_long": "开多仓",
          "open_short": "开空仓",
          "close_long": "平多仓",
          "close_short": "平空仓",
          "hold": "持有当前仓位，不进行任何操作",
          "wait": "等待，不采取任何行动",
          "partial_close": "部分平仓，平掉部分持仓",
          "full_close": "全部平仓，平掉所有持仓",
          "add_position": "在现有仓位上加仓"
        }
      },
      "leverage": {
        "type": "integer",
        "description": "杠杆倍数，开新仓时必需。BTC/ETH最大20倍，其他币种最大5倍",
        "minimum": 1,
        "maximum": 20,
        "examples": [3, 5, 10, 20]
      },
      "position_size_usd": {
        "type": "number",
        "description": "仓位大小（USDT），开新仓时必需。必须大于等于最小开仓金额（一般币种≥12 USDT，BTC/ETH≥60 USDT）",
        "minimum": 12,
        "examples": [100, 500, 1000, 5000]
      },
      "stop_loss": {
        "type": "number",
        "description": "止损价格（必需数值，不能是公式或表达式）。开新仓时强烈建议提供。格式要求：1) 必须是正数(>0)；2) 必须是实际价格数值，不能是表达式如'3000*0.01'；3) 价格精度：根据实际市场价格动态确定（价格<0.0001用8位小数，<0.001用6位小数，<0.01用6位小数，<1.0用4位小数，<100用4位小数，≥100用2位小数）。方向要求：做多(open_long)时止损在下方(stop_loss < take_profit)，做空(open_short)时止损在上方(stop_loss > take_profit)。手续费考虑：做多时止损价应调高约0.1%（更接近入场价），做空时止损价应调低约0.1%（更接近入场价），确保扣除手续费后实际亏损不超过-5%。风险回报比：止损空间与止盈空间的比例应≥1:3（即止盈空间至少是止损空间的3倍）",
        "minimum": 0.0001,
        "exclusiveMinimum": true,
        "examples": [42000, 42000.5, 0.1560, 0.15605, 0.00002070]
      },
      "take_profit": {
        "type": "number",
        "description": "止盈价格（必需数值，不能是公式或表达式）。开新仓时强烈建议提供。格式要求：1) 必须是正数(>0)；2) 必须是实际价格数值，不能是表达式如'3000*0.01'；3) 价格精度：根据实际市场价格动态确定（价格<0.0001用8位小数，<0.001用6位小数，<0.01用6位小数，<1.0用4位小数，<100用4位小数，≥100用2位小数）。方向要求：做多(open_long)时止盈在上方(take_profit > stop_loss)，做空(open_short)时止盈在下方(take_profit < stop_loss)。手续费考虑：做多时止盈价应调低约0.1%（更接近入场价），做空时止盈价应调高约0.1%（更接近入场价），确保扣除手续费后实际盈利达到目标。风险回报比：止盈空间与止损空间的比例应≥3:1（即止盈空间至少是止损空间的3倍）",
        "minimum": 0.0001,
        "exclusiveMinimum": true,
        "examples": [48000, 48000.5, 0.1720, 0.17205, 0.00002070]
      },
      "confidence": {
        "type": "integer",
        "description": "信心度（0-100），表示对该决策的把握程度",
        "minimum": 0,
        "maximum": 100,
        "examples": [75, 85, 90]
      },
      "reasoning": {
        "type": "string",
        "description": "详细的推理过程，必须详细说明决策依据。这是必需字段，不能为空",
        "minLength": 10,
        "examples": [
          "当前PnL +2.96%，接近历史峰值+2.99%（回撤仅0.03%）。建议部分平仓锁定利润，因为：1) 持仓时间仅11分钟，已获得3%收益；2) 5分钟K线显示价格接近短期阻力位；3) 成交量开始萎缩，上涨动能减弱。"
        ]
      },
      "risk_usd": {
        "type": "number",
        "description": "最大风险金额（USDT），可选字段",
        "minimum": 0,
        "examples": [20, 50, 100]
      },
      "price": {
        "type": "number",
        "description": "限价单价格（用于网格交易）",
        "minimum": 0
      },
      "quantity": {
        "type": "number",
        "description": "订单数量（用于网格交易）",
        "minimum": 0
      },
      "level_index": {
        "type": "integer",
        "description": "网格层级索引（用于网格交易）",
        "minimum": 0
      },
      "order_id": {
        "type": "string",
        "description": "订单ID（用于取消订单）"
      }
    },
    "allOf": [
      {
        "if": {
          "properties": {
            "action": {
              "enum": ["open_long", "open_short"]
            }
          }
        },
        "then": {
          "required": ["leverage", "position_size_usd", "stop_loss", "take_profit"],
          "properties": {
            "stop_loss": {
              "description": "开新仓时必需。做多时：stop_loss必须 < take_profit（止损在下方，止盈在上方）。做空时：stop_loss必须 > take_profit（止损在上方，止盈在下方）。必须考虑手续费和风险回报比≥1:3。价格精度根据实际市场价格动态确定。"
            },
            "take_profit": {
              "description": "开新仓时必需。做多时：take_profit必须 > stop_loss（止盈在上方，止损在下方）。做空时：take_profit必须 < stop_loss（止盈在下方，止损在上方）。必须考虑手续费和风险回报比≥3:1。价格精度根据实际市场价格动态确定。"
            }
          }
        }
      },
      {
        "if": {
          "properties": {
            "action": {
              "const": "open_long"
            }
          }
        },
        "then": {
          "properties": {
            "stop_loss": {
              "description": "做多时：stop_loss必须 < take_profit（止损在下方，止盈在上方）。示例：入场价45000，止损42000，止盈54000（风险回报比=9000/3000=3:1）。价格精度根据实际市场价格动态确定。"
            },
            "take_profit": {
              "description": "做多时：take_profit必须 > stop_loss（止盈在上方，止损在下方）。示例：入场价45000，止损42000，止盈54000（风险回报比=9000/3000=3:1）。价格精度根据实际市场价格动态确定。"
            }
          }
        }
      },
      {
        "if": {
          "properties": {
            "action": {
              "const": "open_short"
            }
          }
        },
        "then": {
          "properties": {
            "stop_loss": {
              "description": "做空时：stop_loss必须 > take_profit（止损在上方，止盈在下方）。示例：入场价45000，止损48000，止盈36000（风险回报比=9000/3000=3:1）。价格精度根据实际市场价格动态确定。"
            },
            "take_profit": {
              "description": "做空时：take_profit必须 < stop_loss（止盈在下方，止损在上方）。示例：入场价45000，止损48000，止盈36000（风险回报比=9000/3000=3:1）。价格精度根据实际市场价格动态确定。"
            }
          }
        }
      }
    ],
    "minItems": 0,
    "maxItems": 10
    }
  }
}`
}

// getDecisionJSONSchemaEN 生成英文描述的JSON Schema
func getDecisionJSONSchemaEN() string {
	return `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "description": "Trading decision output object, containing thinking chain and decisions array",
  "required": ["thinking", "decisions"],
  "properties": {
    "thinking": {
      "type": "string",
      "description": "Chain of thought analysis process, detailing analysis approach, market judgment, risk assessment, and other thinking processes. This is a required field and must explain decision basis and reasoning in detail",
      "minLength": 50,
      "examples": [
        "Analyze account status: Current margin usage 25%, within safe range. Analyze positions: BTCUSDT current PnL +2.96%, near historical peak +2.99%, only 0.03% pullback. 5M chart shows price approaching short-term resistance, volume declining, upward momentum weakening. Suggest partial close to lock profits."
      ]
    },
    "decisions": {
      "type": "array",
      "description": "Array of trading decisions, each element represents one trading decision",
      "items": {
        "type": "object",
        "required": ["symbol", "action", "reasoning"],
        "properties": {
          "symbol": {
            "type": "string",
            "description": "Trading pair symbol, e.g., BTCUSDT, ETHUSDT",
        "pattern": "^[A-Z0-9]+USDT$",
        "examples": ["BTCUSDT", "ETHUSDT", "BNBUSDT"]
      },
      "action": {
        "type": "string",
        "description": "Trading action type",
        "enum": [
          "open_long",
          "open_short",
          "close_long",
          "close_short",
          "hold",
          "wait",
          "partial_close",
          "full_close",
          "add_position"
        ],
        "enumDescriptions": {
          "open_long": "Open long position",
          "open_short": "Open short position",
          "close_long": "Close long position",
          "close_short": "Close short position",
          "hold": "Hold current position, no action",
          "wait": "Wait, take no action",
          "partial_close": "Partially close position",
          "full_close": "Fully close position",
          "add_position": "Add to existing position"
        }
      },
      "leverage": {
        "type": "integer",
        "description": "Leverage multiplier, required for new positions. Max 20x for BTC/ETH, max 5x for other coins",
        "minimum": 1,
        "maximum": 20,
        "examples": [3, 5, 10, 20]
      },
      "position_size_usd": {
        "type": "number",
        "description": "Position size in USDT, required for new positions. Must be >= minimum opening amount (≥12 USDT for general coins, ≥60 USDT for BTC/ETH)",
        "minimum": 12,
        "examples": [100, 500, 1000, 5000]
      },
      "stop_loss": {
        "type": "number",
        "description": "Stop-loss price (required numeric value, not formula or expression). Strongly recommended for new positions. Format requirements: 1) Must be positive (>0); 2) Must be actual price value, not expression like '3000*0.01'; 3) Price precision: Dynamically determined based on actual market price (<0.0001 use 8 decimals, <0.001 use 6 decimals, <0.01 use 6 decimals, <1.0 use 4 decimals, <100 use 4 decimals, ≥100 use 2 decimals). Direction requirements: For LONG (open_long) stop loss below (stop_loss < take_profit), for SHORT (open_short) stop loss above (stop_loss > take_profit). Fee consideration: For LONG, set SL ~0.1% higher (closer to entry); for SHORT, set SL ~0.1% lower (closer to entry), ensuring actual loss after fees does not exceed -5%. Risk-reward ratio: Stop loss space to take profit space ratio should be ≥1:3 (i.e., take profit space must be at least 3x stop loss space)",
        "minimum": 0.0001,
        "exclusiveMinimum": true,
        "examples": [42000, 42000.5, 0.1560, 0.15605, 0.00002070]
      },
      "take_profit": {
        "type": "number",
        "description": "Take-profit price (required numeric value, not formula or expression). Strongly recommended for new positions. Format requirements: 1) Must be positive (>0); 2) Must be actual price value, not expression like '3000*0.01'; 3) Price precision: Dynamically determined based on actual market price (<0.0001 use 8 decimals, <0.001 use 6 decimals, <0.01 use 6 decimals, <1.0 use 4 decimals, <100 use 4 decimals, ≥100 use 2 decimals). Direction requirements: For LONG (open_long) take profit above (take_profit > stop_loss), for SHORT (open_short) take profit below (take_profit < stop_loss). Fee consideration: For LONG, set TP ~0.1% lower (closer to entry); for SHORT, set TP ~0.1% higher (closer to entry), ensuring actual profit after fees meets target. Risk-reward ratio: Take profit space to stop loss space ratio should be ≥3:1 (i.e., take profit space must be at least 3x stop loss space)",
        "minimum": 0.0001,
        "exclusiveMinimum": true,
        "examples": [48000, 48000.5, 0.1720, 0.17205, 0.00002070]
      },
      "confidence": {
        "type": "integer",
        "description": "Confidence level (0-100), indicating certainty of this decision",
        "minimum": 0,
        "maximum": 100,
        "examples": [75, 85, 90]
      },
      "reasoning": {
        "type": "string",
        "description": "Detailed reasoning process, must explain decision basis in detail. This is a required field and cannot be empty",
        "minLength": 10,
        "examples": [
          "Current PnL +2.96%, near historical peak +2.99% (only 0.03% pullback). Suggest partial close to lock profits because: 1) Only 11 minutes holding time with 3% gain; 2) 5M chart shows price approaching short-term resistance; 3) Volume declining, upward momentum weakening."
        ]
      },
      "risk_usd": {
        "type": "number",
        "description": "Maximum risk amount in USDT, optional field",
        "minimum": 0,
        "examples": [20, 50, 100]
      },
      "price": {
        "type": "number",
        "description": "Limit order price (for grid trading)",
        "minimum": 0
      },
      "quantity": {
        "type": "number",
        "description": "Order quantity (for grid trading)",
        "minimum": 0
      },
      "level_index": {
        "type": "integer",
        "description": "Grid level index (for grid trading)",
        "minimum": 0
      },
      "order_id": {
        "type": "string",
        "description": "Order ID (for canceling orders)"
      }
    },
    "allOf": [
      {
        "if": {
          "properties": {
            "action": {
              "enum": ["open_long", "open_short"]
            }
          }
        },
        "then": {
          "required": ["leverage", "position_size_usd", "stop_loss", "take_profit"],
          "properties": {
            "stop_loss": {
              "description": "Required for new positions. For LONG: stop_loss must < take_profit (SL below, TP above). For SHORT: stop_loss must > take_profit (SL above, TP below). Must consider fees and risk-reward ratio ≥1:3. Price precision dynamically determined based on actual market price."
            },
            "take_profit": {
              "description": "Required for new positions. For LONG: take_profit must > stop_loss (TP above, SL below). For SHORT: take_profit must < stop_loss (TP below, SL above). Must consider fees and risk-reward ratio ≥3:1. Price precision dynamically determined based on actual market price."
            }
          }
        }
      },
      {
        "if": {
          "properties": {
            "action": {
              "const": "open_long"
            }
          }
        },
        "then": {
          "properties": {
            "stop_loss": {
              "description": "For LONG: stop_loss must < take_profit (SL below, TP above). Example: Entry 45000, SL 42000, TP 54000 (risk-reward ratio=9000/3000=3:1). Price precision dynamically determined based on actual market price."
            },
            "take_profit": {
              "description": "For LONG: take_profit must > stop_loss (TP above, SL below). Example: Entry 45000, SL 42000, TP 54000 (risk-reward ratio=9000/3000=3:1). Price precision dynamically determined based on actual market price."
            }
          }
        }
      },
      {
        "if": {
          "properties": {
            "action": {
              "const": "open_short"
            }
          }
        },
        "then": {
          "properties": {
            "stop_loss": {
              "description": "For SHORT: stop_loss must > take_profit (SL above, TP below). Example: Entry 45000, SL 48000, TP 36000 (risk-reward ratio=9000/3000=3:1). Price precision dynamically determined based on actual market price."
            },
            "take_profit": {
              "description": "For SHORT: take_profit must < stop_loss (TP below, SL above). Example: Entry 45000, SL 48000, TP 36000 (risk-reward ratio=9000/3000=3:1). Price precision dynamically determined based on actual market price."
            }
          }
        }
      }
    ],
    "minItems": 0,
    "maxItems": 10
    }
  }
}`
}

// getDecisionJSONSchemaSimplifiedEN 生成简化版英文 JSON Schema
// 移除：enumDescriptions, examples, exclusiveMinimum, pattern, allOf
// 保留：基础字段和描述
func getDecisionJSONSchemaSimplifiedEN() string {
	return `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "description": "Trading decision output object, containing thinking chain and decisions array",
  "required": ["thinking", "decisions"],
  "properties": {
    "thinking": {
      "type": "string",
      "description": "Chain of thought analysis process, detailing analysis approach, market judgment, risk assessment, and other thinking processes. This is a required field and must explain decision basis and reasoning in detail",
      "minLength": 50
    },
    "decisions": {
      "type": "array",
      "description": "Array of trading decisions, each element represents one trading decision",
      "minItems": 0,
      "maxItems": 10,
      "items": {
        "type": "object",
        "required": ["symbol", "action", "reasoning"],
        "properties": {
          "symbol": {
            "type": "string",
            "description": "Trading pair symbol, e.g., BTCUSDT, ETHUSDT"
          },
          "action": {
            "type": "string",
            "description": "Trading action type",
            "enum": [
              "open_long",
              "open_short",
              "close_long",
              "close_short",
              "hold",
              "wait",
              "partial_close",
              "full_close",
              "add_position"
            ]
          },
          "leverage": {
            "type": "integer",
            "description": "Leverage multiplier, required for new positions. Max 20x for BTC/ETH, max 5x for other coins",
            "minimum": 1,
            "maximum": 20
          },
          "position_size_usd": {
            "type": "number",
            "description": "Position size in USDT, required for new positions. Must be >= minimum opening amount (≥12 USDT for general coins, ≥60 USDT for BTC/ETH)",
            "minimum": 12
          },
          "stop_loss": {
            "type": "number",
            "description": "Stop-loss price (required numeric value, not formula or expression). Strongly recommended for new positions. Format requirements: 1) Must be positive (>0); 2) Must be actual price value, not expression like '3000*0.01'; 3) Price precision: Dynamically determined based on actual market price (<0.0001 use 8 decimals, <0.001 use 6 decimals, <0.01 use 6 decimals, <1.0 use 4 decimals, <100 use 4 decimals, ≥100 use 2 decimals). Direction requirements: For LONG (open_long) stop loss below (stop_loss < take_profit), for SHORT (open_short) stop loss above (stop_loss > take_profit). Fee consideration: For LONG, set SL ~0.1% higher (closer to entry); for SHORT, set SL ~0.1% lower (closer to entry), ensuring actual loss after fees does not exceed -5%. Risk-reward ratio: Stop loss space to take profit space ratio should be ≥1:3 (i.e., take profit space must be at least 3x stop loss space)",
            "minimum": 0.0001
          },
          "take_profit": {
            "type": "number",
            "description": "Take-profit price (required numeric value, not formula or expression). Strongly recommended for new positions. Format requirements: 1) Must be positive (>0); 2) Must be actual price value, not expression like '3000*0.01'; 3) Price precision: Dynamically determined based on actual market price (<0.0001 use 8 decimals, <0.001 use 6 decimals, <0.01 use 6 decimals, <1.0 use 4 decimals, <100 use 4 decimals, ≥100 use 2 decimals). Direction requirements: For LONG (open_long) take profit above (take_profit > stop_loss), for SHORT (open_short) take profit below (take_profit < stop_loss). Fee consideration: For LONG, set TP ~0.1% lower (closer to entry); for SHORT, set TP ~0.1% higher (closer to entry), ensuring actual profit after fees meets target. Risk-reward ratio: Take profit space to stop loss space ratio should be ≥3:1 (i.e., take profit space must be at least 3x stop loss space)",
            "minimum": 0.0001
          },
          "confidence": {
            "type": "integer",
            "description": "Confidence level (0-100), indicating certainty of this decision",
            "minimum": 0,
            "maximum": 100
          },
          "reasoning": {
            "type": "string",
            "description": "Detailed reasoning process, must explain decision basis in detail. This is a required field and cannot be empty",
            "minLength": 10
          },
          "risk_usd": {
            "type": "number",
            "description": "Maximum risk amount in USDT, optional field",
            "minimum": 0
          },
          "price": {
            "type": "number",
            "description": "Limit order price (for grid trading)",
            "minimum": 0
          },
          "quantity": {
            "type": "number",
            "description": "Order quantity (for grid trading)",
            "minimum": 0
          },
          "level_index": {
            "type": "integer",
            "description": "Grid level index (for grid trading)",
            "minimum": 0
          },
          "order_id": {
            "type": "string",
            "description": "Order ID (for canceling orders)"
          }
        }
      }
    }
  }
}`
}

// getDecisionJSONSchemaSimplifiedZH 生成简化版中文 JSON Schema
func getDecisionJSONSchemaSimplifiedZH() string {
	return `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "description": "交易决策输出对象，包含思维链分析和决策数组",
  "required": ["thinking", "decisions"],
  "properties": {
    "thinking": {
      "type": "string",
      "description": "思维链分析过程，详细说明分析思路、市场判断、风险评估等思考过程。这是必需字段，必须详细说明决策依据和推理过程",
      "minLength": 50
    },
    "decisions": {
      "type": "array",
      "description": "交易决策数组，每个元素代表一个交易决策",
      "minItems": 0,
      "maxItems": 10,
      "items": {
        "type": "object",
        "required": ["symbol", "action", "reasoning"],
        "properties": {
          "symbol": {
            "type": "string",
            "description": "交易对符号，例如：BTCUSDT、ETHUSDT"
          },
          "action": {
            "type": "string",
            "description": "交易动作类型",
            "enum": [
              "open_long",
              "open_short",
              "close_long",
              "close_short",
              "hold",
              "wait",
              "partial_close",
              "full_close",
              "add_position"
            ]
          },
          "leverage": {
            "type": "integer",
            "description": "杠杆倍数，开新仓时必需。BTC/ETH最大20倍，其他币种最大5倍",
            "minimum": 1,
            "maximum": 20
          },
          "position_size_usd": {
            "type": "number",
            "description": "仓位大小（USDT），开新仓时必需。必须大于等于最小开仓金额（一般币种≥12 USDT，BTC/ETH≥60 USDT）",
            "minimum": 12
          },
          "stop_loss": {
            "type": "number",
            "description": "止损价格（必需数值，不能是公式或表达式）。开新仓时强烈建议提供。格式要求：1) 必须是正数(>0)；2) 必须是实际价格数值，不能是表达式如'3000*0.01'；3) 价格精度：根据实际市场价格动态确定（价格<0.0001用8位小数，<0.001用6位小数，<0.01用6位小数，<1.0用4位小数，<100用4位小数，≥100用2位小数）。方向要求：做多(open_long)时止损在下方(stop_loss < take_profit)，做空(open_short)时止损在上方(stop_loss > take_profit)。手续费考虑：做多时止损价应调高约0.1%（更接近入场价），做空时止损价应调低约0.1%（更接近入场价），确保扣除手续费后实际亏损不超过-5%。风险回报比：止损空间与止盈空间的比例应≥1:3（即止盈空间至少是止损空间的3倍）",
            "minimum": 0.0001
          },
          "take_profit": {
            "type": "number",
            "description": "止盈价格（必需数值，不能是公式或表达式）。开新仓时强烈建议提供。格式要求：1) 必须是正数(>0)；2) 必须是实际价格数值，不能是表达式如'3000*0.01'；3) 价格精度：根据实际市场价格动态确定（价格<0.0001用8位小数，<0.001用6位小数，<0.01用6位小数，<1.0用4位小数，<100用4位小数，≥100用2位小数）。方向要求：做多(open_long)时止盈在上方(take_profit > stop_loss)，做空(open_short)时止盈在下方(take_profit < stop_loss)。手续费考虑：做多时止盈价应调低约0.1%（更接近入场价），做空时止盈价应调高约0.1%（更接近入场价），确保扣除手续费后实际盈利达到目标。风险回报比：止盈空间与止损空间的比例应≥3:1（即止盈空间至少是止损空间的3倍）",
            "minimum": 0.0001
          },
          "confidence": {
            "type": "integer",
            "description": "信心度（0-100），表示对该决策的把握程度",
            "minimum": 0,
            "maximum": 100
          },
          "reasoning": {
            "type": "string",
            "description": "详细的推理过程，必须详细说明决策依据。这是必需字段，不能为空",
            "minLength": 10
          },
          "risk_usd": {
            "type": "number",
            "description": "最大风险金额（USDT），可选字段",
            "minimum": 0
          },
          "price": {
            "type": "number",
            "description": "限价单价格（用于网格交易）",
            "minimum": 0
          },
          "quantity": {
            "type": "number",
            "description": "订单数量（用于网格交易）",
            "minimum": 0
          },
          "level_index": {
            "type": "integer",
            "description": "网格层级索引（用于网格交易）",
            "minimum": 0
          },
          "order_id": {
            "type": "string",
            "description": "订单ID（用于取消订单）"
          }
        }
      }
    }
  }
}`
}

// GetDecisionJSONSchemaCompact 获取紧凑版的JSON Schema（用于AI提示词）
// 返回压缩后的JSON字符串，移除不必要的空白字符以节省token
func GetDecisionJSONSchemaCompact(lang Language) string {
	schema := GetDecisionJSONSchema(lang)
	return compactJSONSchema(schema)
}

// compactJSONSchema 压缩JSON Schema，移除不必要的空白字符
// 优先使用标准JSON序列化，确保JSON有效性
func compactJSONSchema(schema string) string {
	// 首先尝试解析为JSON对象并重新序列化为紧凑格式
	// 这是最可靠的方法，可以确保JSON有效性
	var jsonObj interface{}
	if err := json.Unmarshal([]byte(schema), &jsonObj); err == nil {
		// 成功解析，使用紧凑格式序列化（无缩进，无多余空格）
		compact, err := json.Marshal(jsonObj)
		if err == nil {
			return string(compact)
		}
	}

	// 如果JSON解析失败（理论上不应该发生，因为schema是有效的JSON）
	// 使用正则表达式进行备用压缩，但这种方法可能不够精确
	compacted := schema

	// 移除行尾空格和制表符
	compacted = regexp.MustCompile(`[ \t]+(\n|\r\n?)`).ReplaceAllString(compacted, "$1")

	// 移除JSON结构符号周围的空格（但要小心不要破坏字符串内容）
	// 这些正则表达式只匹配结构符号，不匹配字符串内的内容
	compacted = regexp.MustCompile(`:\s+`).ReplaceAllString(compacted, ":")
	compacted = regexp.MustCompile(`,\s+`).ReplaceAllString(compacted, ",")
	compacted = regexp.MustCompile(`\{\s+`).ReplaceAllString(compacted, "{")
	compacted = regexp.MustCompile(`\s+\}`).ReplaceAllString(compacted, "}")
	compacted = regexp.MustCompile(`\[\s+`).ReplaceAllString(compacted, "[")
	compacted = regexp.MustCompile(`\s+\]`).ReplaceAllString(compacted, "]")

	// 移除连续的空白行
	compacted = regexp.MustCompile(`\n\s*\n\s*\n+`).ReplaceAllString(compacted, "")

	// 移除所有换行和行首空格，生成单行JSON
	lines := strings.Split(compacted, "\n")
	var result strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			result.WriteString(trimmed)
		}
	}

	return result.String()
}

// ============================================================================
// JSON Schema 支持检查 - 统一入口
// ============================================================================

// CheckModelSupportsJSONSchema 检查模型是否支持 JSON Schema（API级别）
// 这是统一的入口函数，供 engine.go 和 schema.go 使用
// 
// 支持的模型（支持 JSON Schema API）：
//   - OpenAI: GPT-4o系列, GPT-4-turbo系列, GPT-4o-mini, o1系列, o3系列, GPT-4系列（2024年后版本）
//   - Claude: Claude Sonnet 4.5+, Claude Opus 4.1+, Claude Opus 4.5+
//   - Qwen: 支持基础 JSON Schema
//   - Kimi: 支持基础 JSON Schema
//   - DeepSeek: 支持基础 JSON Schema
// 
// 不支持的模型：
//   - OpenAI: GPT-3.5系列, GPT-3系列, GPT-4旧版本（2023年3月及以前）
//   - Claude: Claude 3.x系列（包括3.5）, Claude Haiku 4.5（即将支持但当前不支持）
//   - 其他: Gemini, Grok等
func CheckModelSupportsJSONSchema(provider, modelName string) bool {
	providerLower := strings.ToLower(provider)
	modelNameLower := strings.ToLower(modelName)

	// 1. 优先通过 modelName 判断（更准确，支持自定义 API 场景）
	if modelNameLower != "" {
		// OpenAI 模型（gpt-*, o1-*, o3-*）
		if strings.HasPrefix(modelNameLower, "gpt-") ||
			strings.HasPrefix(modelNameLower, "o1-") ||
			strings.HasPrefix(modelNameLower, "o3-") {
			return checkOpenAISupportsJSONSchema(modelNameLower)
		}
		// Claude 模型（claude-*、opus、sonnet、haiku）
		if strings.HasPrefix(modelNameLower, "claude-") ||
			strings.Contains(modelNameLower, "opus") ||
			strings.Contains(modelNameLower, "sonnet") ||
			strings.Contains(modelNameLower, "haiku") {
			return checkClaudeSupportsJSONSchema(modelNameLower)
		}
		// Qwen 模型（qwen-*, qwq-*）
		if strings.HasPrefix(modelNameLower, "qwen") ||
			strings.HasPrefix(modelNameLower, "qwq") {
			return checkQwenSupportsJSONSchema(modelNameLower)
		}
		// Kimi 模型（moonshot-*, kimi-*）
		if strings.HasPrefix(modelNameLower, "moonshot") ||
			strings.HasPrefix(modelNameLower, "kimi") {
			return checkKimiSupportsJSONSchema(modelNameLower)
		}
		// DeepSeek 模型（deepseek-*）
		if strings.HasPrefix(modelNameLower, "deepseek") {
			return checkDeepSeekSupportsJSONSchema(modelNameLower)
		}
		// Gemini 模型（gemini-*）
		if strings.HasPrefix(modelNameLower, "gemini") {
			return false
		}
		// Grok 模型（grok-*）
		if strings.HasPrefix(modelNameLower, "grok") {
			return false
		}
	}

	// 2. modelName 无法识别时，通过 provider 判断
	if strings.Contains(providerLower, "openai") {
		return checkOpenAISupportsJSONSchema(modelNameLower)
	} else if strings.Contains(providerLower, "claude") {
		return checkClaudeSupportsJSONSchema(modelNameLower)
	} else if strings.Contains(providerLower, "qwen") {
		return checkQwenSupportsJSONSchema(modelNameLower)
	} else if strings.Contains(providerLower, "kimi") {
		return checkKimiSupportsJSONSchema(modelNameLower)
	} else if strings.Contains(providerLower, "deepseek") {
		return checkDeepSeekSupportsJSONSchema(modelNameLower)
	} else if strings.Contains(providerLower, "gemini") {
		return false // Gemini 目前不支持 JSON Schema
	} else if strings.Contains(providerLower, "grok") {
		return false // Grok 目前不支持 JSON Schema
	}

	// 3. 其他未识别的模型，保守策略返回 false
	return false
}

// checkOpenAISupportsJSONSchema 检查OpenAI模型是否支持JSON Schema
// 支持的模型：GPT-4o系列, GPT-4-turbo系列, GPT-4o-mini, o1系列, o3系列, GPT-4系列（2024年后版本）
// 参考：https://platform.openai.com/docs/guides/structured-outputs
func checkOpenAISupportsJSONSchema(modelNameLower string) bool {
	// 1. 明确支持的模型系列（优先检查，按优先级排序）
	explicitlySupported := []string{
		// GPT-4o 系列（2024年8月后支持，gpt-4o-2024-08-06 及以后）
		"gpt-4o-2024", "gpt-4o-2025", "gpt-4o",
		// GPT-4-turbo 系列（2024年版本）
		"gpt-4-turbo-2024", "gpt-4-turbo-2025", "gpt-4-turbo",
		// GPT-4o-mini
		"gpt-4o-mini",
		// o1 系列（推理模型，支持JSON Schema）
		"o1-preview", "o1-mini", "o1-",
		// o3 系列（推理模型，支持JSON Schema）
		"o3-mini", "o3-",
	}

	for _, supported := range explicitlySupported {
		if strings.Contains(modelNameLower, supported) {
			return true
		}
	}

	// 2. GPT-4 系列（2024年后的版本支持）
	if strings.Contains(modelNameLower, "gpt-4") {
		// 排除明确不支持的旧版本
		unsupportedVersions := []string{
			"gpt-4-0314",     // 2023年3月版本，不支持
			"gpt-4-32k-0314", // 2023年3月版本，不支持
		}
		for _, unsupported := range unsupportedVersions {
			if strings.Contains(modelNameLower, unsupported) {
				return false
			}
		}

		// 检查是否是2024年后的版本（通过日期标识）
		supportedDatePatterns := []string{
			"2024", "2025", // 2024年及以后的版本
			"0125", "1106", "0613", // 2024年的具体版本
			"gpt-4-0125", "gpt-4-1106", "gpt-4-0613", // 完整版本号
		}
		for _, pattern := range supportedDatePatterns {
			if strings.Contains(modelNameLower, pattern) {
				return true
			}
		}

		// 如果没有日期标识，但包含 gpt-4-turbo 或 gpt-4o，也支持
		if strings.Contains(modelNameLower, "turbo") || strings.Contains(modelNameLower, "gpt-4o") {
			return true
		}

		// 其他 GPT-4 变体（如 gpt-4-32k）需要进一步确认
		// 如果包含明确的版本号且不是旧版本，假设支持
		if strings.HasPrefix(modelNameLower, "gpt-4-") {
			// 检查是否包含日期格式的版本号（如 gpt-4-2024-xx-xx）
			if strings.Contains(modelNameLower, "-2024") || strings.Contains(modelNameLower, "-2025") {
				return true
			}
		}
	}

	// 3. GPT-3.5 系列不支持 JSON Schema
	if strings.Contains(modelNameLower, "gpt-3.5") || strings.Contains(modelNameLower, "gpt-3") {
		return false
	}

	// 4. GPT-5 系列（未来模型，假设支持）
	if strings.Contains(modelNameLower, "gpt-5") {
		return true
	}

	// 5. 其他未识别的模型，保守策略返回false
	return false
}

// checkClaudeSupportsJSONSchema 检查Claude模型是否支持JSON Schema
// 支持的模型：Claude Sonnet 4.5+, Claude Opus 4.1+, Claude Opus 4.5+
// 不支持的模型：Claude 3.x系列（包括3.5）, Claude Haiku 4.5（即将支持但当前不支持）
// 参考：https://platform.claude.com/docs/en/build-with-claude/structured-outputs
func checkClaudeSupportsJSONSchema(modelNameLower string) bool {
	// 重要：Claude 3.x 系列（包括 3.5）不支持 JSON Schema
	// 只有 Claude 4.x 系列支持
	if strings.Contains(modelNameLower, "claude-3") {
		return false
	}

	// Claude 4.x 系列明确支持
	// 支持的模型标识（按优先级排序，覆盖所有可能的命名格式）：
	supportedPatterns := []string{
		// Claude Opus 4.5（默认模型格式，如 claude-opus-4-5-20251101）- 最高优先级
		"claude-opus-4-5-2025", // 匹配 claude-opus-4-5-20251101 等
		"claude-opus-4-5-2024",
		"claude-opus-4-5", // 不带日期后缀的格式
		// Claude Opus 4.1+（明确支持）
		"claude-opus-4.1", "claude-opus-4-1",
		"claude-opus-4.5", "claude-opus-4-5",
		"opus-4.1", "opus-4-1",
		"opus-4.5", "opus-4-5",
		"opus-4-", // Opus 4.x 系列（通用匹配，但需要 >= 4.1）
		// Claude Sonnet 4.5+（明确支持）
		"claude-sonnet-4.5", "claude-sonnet-4-5",
		"sonnet-4.5", "sonnet-4-5",
		"sonnet-4-", // Sonnet 4.x 系列（通用匹配，但需要 >= 4.5）
	}

	for _, pattern := range supportedPatterns {
		if strings.Contains(modelNameLower, pattern) {
			return true
		}
	}

	// 检查是否是 Claude 4.x 系列（但不包括 Haiku）
	if strings.Contains(modelNameLower, "claude-4") || strings.Contains(modelNameLower, "claude-4.") {
		// 排除 Haiku（当前不支持，但即将支持）
		if strings.Contains(modelNameLower, "haiku") {
			return false
		}
		// Sonnet 和 Opus 4.x 支持
		if strings.Contains(modelNameLower, "sonnet") || strings.Contains(modelNameLower, "opus") {
			return true
		}
	}

	// 检查 Opus 4.x 或 Sonnet 4.x 系列（不带 claude- 前缀的情况）
	if strings.Contains(modelNameLower, "opus-4") || strings.Contains(modelNameLower, "sonnet-4") {
		// 排除 Haiku（当前不支持）
		if strings.Contains(modelNameLower, "haiku") {
			return false
		}
		// Opus 4.1+ 支持
		if strings.Contains(modelNameLower, "opus-4") {
			// 检查版本号，4.1+ 支持
			if strings.Contains(modelNameLower, "opus-4.1") ||
				strings.Contains(modelNameLower, "opus-4-1") ||
				strings.Contains(modelNameLower, "opus-4.5") ||
				strings.Contains(modelNameLower, "opus-4-5") ||
				strings.Contains(modelNameLower, "opus-4-") {
				return true
			}
		}
		// Sonnet 4.5+ 支持（注意：Sonnet 需要 >= 4.5，不是 4.1）
		if strings.Contains(modelNameLower, "sonnet-4") {
			// 检查版本号，4.5+ 支持
			if strings.Contains(modelNameLower, "sonnet-4.5") ||
				strings.Contains(modelNameLower, "sonnet-4-5") ||
				strings.Contains(modelNameLower, "sonnet-4-") {
				// 需要进一步确认版本号 >= 4.5
				// 如果包含明确的 4.5 或更高版本，返回 true
				// 如果只有 "sonnet-4-"，需要检查后续版本号
				return true // 保守策略：如果包含 sonnet-4-，假设是 4.5+
			}
		}
	}

	// 如果模型名称只包含 "claude" 但没有明确的版本信息，保守策略返回false
	// 因为需要明确的版本号（4.x）才能确定是否支持
	return false
}

// checkQwenSupportsJSONSchema 检查Qwen模型是否支持JSON Schema
// 支持的模型：qwen-turbo, qwen-plus, qwen-max, qwen2.5 系列, qwq 系列
// 不支持的模型：qwen-vl（视觉模型）, qwen-audio（音频模型）
func checkQwenSupportsJSONSchema(modelNameLower string) bool {
	// 排除不支持的模型类型
	unsupportedPatterns := []string{
		"qwen-vl",    // 视觉模型，不支持结构化输出
		"qwen-audio", // 音频模型，不支持结构化输出
		"qwen-coder", // 代码模型，JSON Schema 支持待确认
	}
	for _, pattern := range unsupportedPatterns {
		if strings.Contains(modelNameLower, pattern) {
			return false
		}
	}

	// 支持的模型系列
	supportedPatterns := []string{
		"qwen-turbo",  // Qwen Turbo 系列
		"qwen-plus",   // Qwen Plus 系列
		"qwen-max",    // Qwen Max 系列
		"qwen-long",   // Qwen Long 系列
		"qwen2.5",     // Qwen 2.5 系列
		"qwen2-",      // Qwen 2 系列
		"qwen1.5",     // Qwen 1.5 系列
		"qwq",         // QwQ 推理模型
	}
	for _, pattern := range supportedPatterns {
		if strings.Contains(modelNameLower, pattern) {
			return true
		}
	}

	// 通用 qwen 模型（无明确版本号），假设支持
	if strings.HasPrefix(modelNameLower, "qwen") {
		return true
	}

	return false
}

// checkKimiSupportsJSONSchema 检查Kimi模型是否支持JSON Schema
// 支持的模型：moonshot-v1 系列, kimi 系列
func checkKimiSupportsJSONSchema(modelNameLower string) bool {
	// 支持的模型系列
	supportedPatterns := []string{
		"moonshot-v1-8k",    // Moonshot v1 8K
		"moonshot-v1-32k",   // Moonshot v1 32K
		"moonshot-v1-128k",  // Moonshot v1 128K
		"moonshot-v1",       // Moonshot v1 系列
		"kimi-",             // Kimi 系列
	}
	for _, pattern := range supportedPatterns {
		if strings.Contains(modelNameLower, pattern) {
			return true
		}
	}

	// 通用 moonshot/kimi 模型，假设支持
	if strings.HasPrefix(modelNameLower, "moonshot") || strings.HasPrefix(modelNameLower, "kimi") {
		return true
	}

	return false
}

// checkDeepSeekSupportsJSONSchema 检查DeepSeek模型是否支持JSON Schema
// 支持的模型：deepseek-chat, deepseek-coder, deepseek-reasoner
// 不支持的模型：deepseek-vl（视觉模型）
func checkDeepSeekSupportsJSONSchema(modelNameLower string) bool {
	// 排除不支持的模型类型
	unsupportedPatterns := []string{
		"deepseek-vl", // 视觉模型，不支持结构化输出
	}
	for _, pattern := range unsupportedPatterns {
		if strings.Contains(modelNameLower, pattern) {
			return false
		}
	}

	// 支持的模型系列
	supportedPatterns := []string{
		"deepseek-chat",     // DeepSeek Chat
		"deepseek-coder",    // DeepSeek Coder
		"deepseek-reasoner", // DeepSeek Reasoner (R1)
		"deepseek-v2",       // DeepSeek V2 系列
		"deepseek-v3",       // DeepSeek V3 系列
	}
	for _, pattern := range supportedPatterns {
		if strings.Contains(modelNameLower, pattern) {
			return true
		}
	}

	// 通用 deepseek 模型，假设支持
	if strings.HasPrefix(modelNameLower, "deepseek") {
		return true
	}

	return false
}

// ============================================================================
// 根据模型类型动态选择 JSON Schema 版本
// ============================================================================

// CheckModelSupportsAdvancedJSONSchemaFeatures 检查模型是否支持高级 JSON Schema 特性
// 
// 高级特性包括：allOf（条件验证）, pattern（正则表达式）, exclusiveMinimum（严格最小值）等
// 
// 支持的模型（支持高级特性）：
//   - OpenAI: GPT-4o, GPT-4-turbo, o1, o3 系列
//   - Claude: Sonnet 4.5+, Opus 4.1+
// 
// 不支持高级特性的模型（仅支持基础 JSON Schema）：
//   - Qwen: 仅支持基础 JSON Schema（不支持 allOf, pattern, exclusiveMinimum）
//   - Kimi: 仅支持基础 JSON Schema（不支持 allOf, pattern, exclusiveMinimum）
//   - DeepSeek: 仅支持基础 JSON Schema（不支持 allOf, pattern, exclusiveMinimum）
// 
// 注意：
//   - 支持高级特性的模型一定支持 JSON Schema
//   - 支持 JSON Schema 的模型不一定支持高级特性
//   - 如果模型不支持 JSON Schema，此函数返回 false
func CheckModelSupportsAdvancedJSONSchemaFeatures(provider, modelName string) bool {
	// 首先检查是否支持 JSON Schema（基础要求）
	// 如果不支持 JSON Schema，则肯定不支持高级特性
	if !CheckModelSupportsJSONSchema(provider, modelName) {
		return false
	}

	providerLower := strings.ToLower(provider)
	modelNameLower := strings.ToLower(modelName)

	// 1. 优先通过 modelName 判断（更准确）
	if modelNameLower != "" {
		// OpenAI 模型（gpt-*, o1-*, o3-*）支持高级特性
		if strings.HasPrefix(modelNameLower, "gpt-") ||
			strings.HasPrefix(modelNameLower, "o1-") ||
			strings.HasPrefix(modelNameLower, "o3-") {
			return true
		}
		// Claude 模型（claude-*、opus、sonnet）
		if strings.HasPrefix(modelNameLower, "claude-") ||
			strings.Contains(modelNameLower, "opus") ||
			strings.Contains(modelNameLower, "sonnet") {
			// Claude 3.x 不支持高级特性
			if strings.Contains(modelNameLower, "claude-3") {
				return false
			}
			return true
		}
		// Qwen, Kimi, DeepSeek 等明确不支持高级特性
		if strings.HasPrefix(modelNameLower, "qwen") ||
			strings.HasPrefix(modelNameLower, "qwq") ||
			strings.HasPrefix(modelNameLower, "moonshot") ||
			strings.HasPrefix(modelNameLower, "kimi") ||
			strings.HasPrefix(modelNameLower, "deepseek") {
			return false
		}
	}

	// 2. modelName 无法识别时，通过 provider 判断
	if strings.Contains(providerLower, "openai") || strings.Contains(providerLower, "claude") {
		return true
	}

	// Qwen, Kimi, DeepSeek 等仅支持基础 JSON Schema，不支持高级特性
	return false
}

// GetDecisionJSONSchemaForModel 根据模型类型获取合适的 JSON Schema 版本
// 
// 返回值：
//   - 如果模型支持高级 JSON Schema 特性（OpenAI/Claude）：
//     返回完整版本（包含 allOf, pattern, exclusiveMinimum 等高级特性）
//   - 如果模型仅支持基础 JSON Schema（Qwen/Kimi/DeepSeek）：
//     返回简化版本（移除高级特性，仅保留基础 JSON Schema）
//   - 如果模型不支持 JSON Schema（其他模型）：
//     返回简化版本（作为兜底，用于提示词集成方式）
// 
// 注意：此函数不检查模型是否支持 JSON Schema，调用者应确保在支持 JSON Schema 的模型上使用
func GetDecisionJSONSchemaForModel(lang Language, provider, modelName string) string {
	providerLower := strings.ToLower(provider)
	modelNameLower := strings.ToLower(modelName)

	// 检查是否支持高级特性
	supportsAdvanced := CheckModelSupportsAdvancedJSONSchemaFeatures(providerLower, modelNameLower)

	if supportsAdvanced {
		// 完整版本（包含所有高级特性）- OpenAI/Claude
		if lang == LangChinese {
			return getDecisionJSONSchemaZH()
		}
		return getDecisionJSONSchemaEN()
	} else {
		// 简化版本（移除高级特性）- Qwen/Kimi/DeepSeek 或其他不支持高级特性的模型
		if lang == LangChinese {
			return getDecisionJSONSchemaSimplifiedZH()
		}
		return getDecisionJSONSchemaSimplifiedEN()
	}
}
