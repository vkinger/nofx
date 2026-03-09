// Package analyst implements the Market Insight Analyst agent: reads market context and outputs bias + confidence + report.
package analyst

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"nofx/kernel"
	"nofx/mcp"
	"nofx/store"
)

// AnalystReport 分析师输出：宏观偏向、信心、报告正文，可选 key_risks；含调用时的 system/user prompt 供落库展示
type AnalystReport struct {
	Bias         store.AnalystBias `json:"bias"`
	Confidence   int              `json:"confidence"`
	ReportText   string           `json:"report_text"`
	KeyRisks     []string         `json:"key_risks,omitempty"`
	Raw          string           `json:"-"`
	SystemPrompt string           `json:"-"` // 本次调用的系统提示词（写入黑板供单轮详情展示）
	UserPrompt   string           `json:"-"` // 本次调用的用户提示词
}

// analystRoleAndOutputPrompt 分析师角色与输出格式（参考交易员：STRICTLY ENFORCED + 完整 JSON 示例 + 关键字段说明）
const analystRoleAndOutputZH = `---
# 角色
你是宏观市场观察员。你将看到：账户与持仓摘要、BTC/ETH 快照（含可选资金费率）、近期与历史表现、OI movers（Top 10）、全市场排名（OI/资金流/涨跌榜 Top 10）、候选币种列表。只根据上述**汇总数据**做宏观结论，不做逐币分析，不做交易决策。

# ⚠️ 输出格式（严格强制执行）

**CRITICAL：你必须严格按下列格式输出，任何偏差会导致解析失败。**

## 输出格式要求

**必须**只输出一个 JSON 对象；禁止在回复中使用 markdown 代码块，禁止在 JSON 前后写任何说明文字。你的整段回复应仅为可被 JSON 解析的纯文本。

**必须**使用以下 JSON 对象结构：

{{JSON_STRUCT}}

### 关键字段说明

- **bias**（必需）：字符串，且必须为以下之一："bullish"、"bearish"、"neutral"、"strong_bullish"、"strong_bearish"
- **confidence**（必需）：整数，0-100，表示宏观判断信心
- **report_text**（必需）：字符串，2-5 句话，包含趋势、主要风险、建议立场
- **key_risks**（可选）：字符串数组，1-3 条短语

### 输出示例（格式参考）

{{JSON_EXAMPLE}}

**⚠️ 重要提醒：** 你的回复必须是单一 JSON 对象，可直接被程序解析；不要用代码块包裹或前后加任何文字。`

const analystRoleAndOutputEN = `---
# Role
You are a Macro Market Observer. You will see: account and positions summary, BTC/ETH snapshot (optional funding), recent and historical performance, OI movers (Top 10), market-wide rankings (OI / flow / gainers-losers, Top 10), candidate symbol list. Output a high-level view based only on these **summary data**; no per-coin analysis, no trading decisions.

# ⚠️ Output Format (STRICTLY ENFORCED)

**CRITICAL: You MUST follow this format exactly. Any deviation will cause parsing errors.**

## Output Format Requirements

You **must** output exactly one JSON object; do NOT wrap your response in markdown code fences and do NOT add any text before or after the JSON. Your entire response must be parseable as a single JSON object.

**Must** use the following JSON structure:

{{JSON_STRUCT}}

### Field Requirements

- **bias** (required): string, one of "bullish", "bearish", "neutral", "strong_bullish", "strong_bearish"
- **confidence** (required): integer, 0-100
- **report_text** (required): string, 2-5 sentences (trend, main risk, recommended stance)
- **key_risks** (optional): array of strings, 1-3 short items

### Output Example (format reference)

{{JSON_EXAMPLE}}

**⚠️ Critical:** Your response must be a single JSON object only, with no code fence wrapper or any text before/after.`

var (
	analystJSONStructZH = "```json\n{\n  \"bias\": \"neutral\",\n  \"confidence\": 60,\n  \"report_text\": \"BTC 横盘，全市场资金流分歧。建议观望。\",\n  \"key_risks\": [\"风险1\", \"风险2\"]\n}\n```"
	analystJSONStructEN = "```json\n{\n  \"bias\": \"neutral\",\n  \"confidence\": 60,\n  \"report_text\": \"BTC flat, fund flow mixed. Prefer wait.\",\n  \"key_risks\": [\"risk1\", \"risk2\"]\n}\n```"
	analystJSONExampleZH = "```json\n{\"bias\":\"neutral\",\"confidence\":60,\"report_text\":\"BTC 横盘，全市场资金流分歧。建议观望。\",\"key_risks\":[\"资金流向分歧\",\"波动率偏低\"]}\n```"
	analystJSONExampleEN = "```json\n{\"bias\":\"neutral\",\"confidence\":60,\"report_text\":\"BTC flat, fund flow mixed. Prefer wait.\",\"key_risks\":[\"flow divergence\",\"low volatility\"]}\n```"
)

// buildAnalystSystemPrompt 拼装分析师系统提示：数据字典 + 角色与输出格式（注入 JSON 示例块）
func buildAnalystSystemPrompt(engine *kernel.StrategyEngine) string {
	lang := engine.GetLanguage()
	schema := kernel.GetSchemaPromptForAnalyst(lang)
	base := analystRoleAndOutputEN
	structBlock := analystJSONStructEN
	exampleBlock := analystJSONExampleEN
	if lang == kernel.LangChinese {
		base = analystRoleAndOutputZH
		structBlock = analystJSONStructZH
		exampleBlock = analystJSONExampleZH
	}
	base = strings.ReplaceAll(base, "{{JSON_STRUCT}}", structBlock)
	base = strings.ReplaceAll(base, "{{JSON_EXAMPLE}}", exampleBlock)
	return schema + "\n" + base
}

// RunAnalyst 运行分析师 Agent：根据 ctx 生成报告（不写黑板，由调用方写入）
func RunAnalyst(ctx *kernel.Context, engine *kernel.StrategyEngine, client mcp.AIClient) (*AnalystReport, error) {
	if ctx == nil || engine == nil || client == nil {
		return nil, fmt.Errorf("analyst: ctx, engine and client are required")
	}
	lang := engine.GetLanguage()
	userPrompt := buildAnalystUserPrompt(ctx, lang)
	systemPrompt := buildAnalystSystemPrompt(engine)
	resp, err := client.CallWithMessages(systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("analyst AI call failed: %w", err)
	}
	report, err := parseAnalystResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("analyst parse response: %w", err)
	}
	report.Raw = resp
	report.SystemPrompt = systemPrompt
	report.UserPrompt = userPrompt
	return report, nil
}

// buildAnalystUserPrompt 汇总版：仅给宏观结论所需信息；章节标题按 lang 双语（zh/en）
func buildAnalystUserPrompt(ctx *kernel.Context, lang kernel.Language) string {
	zh := lang == kernel.LangChinese
	var (
		sectAccount, sectPositions, sectSnapshot, sectRecent, sectOI, sectRankings, sectCandidates string
		none, noTrades, noData, footer string
	)
	if zh {
		sectAccount = "## 账户\n"
		sectPositions = "## 持仓摘要\n"
		sectSnapshot = "\n## BTC / 市场快照\n"
		sectRecent = "\n## 近期表现\n"
		sectOI = "\n## 持仓量变化 (Top 10)\n"
		sectRankings = "\n## 全市场排名 (Top 10)\n"
		sectCandidates = "\n## 候选币种（此处无逐币数据）\n"
		none = "(无)\n"
		noTrades = "近期无成交\n"
		noData = "(无数据)\n"
		footer = "\n---\n仅输出一个 JSON 对象（bias, confidence, report_text [, key_risks]），不要其他文字。\n"
	} else {
		sectAccount = "## Account\n"
		sectPositions = "## Positions summary\n"
		sectSnapshot = "\n## BTC / market snapshot\n"
		sectRecent = "\n## Recent performance\n"
		sectOI = "\n## OI movers (top 10)\n"
		sectRankings = "\n## Market-wide rankings (top 10)\n"
		sectCandidates = "\n## Candidate symbols (no per-coin data here)\n"
		none = "(none)\n"
		noTrades = "No recent trades\n"
		noData = "(no data)\n"
		footer = "\n---\nOutput only one JSON object (bias, confidence, report_text [, key_risks]). No other text.\n"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Time: %s | Period #%d | Runtime %d min\n\n", ctx.CurrentTime, ctx.CallCount, ctx.RuntimeMinutes))
	b.WriteString(sectAccount)
	b.WriteString(fmt.Sprintf("Equity: %.2f USDT | Available: %.2f | Positions: %d | Margin: %.1f%%\n\n",
		ctx.Account.TotalEquity, ctx.Account.AvailableBalance, ctx.Account.PositionCount, ctx.Account.MarginUsedPct))
	b.WriteString(sectPositions)
	if len(ctx.Positions) == 0 {
		b.WriteString(none)
	} else {
		totalPnl := 0.0
		for _, p := range ctx.Positions {
			totalPnl += p.UnrealizedPnLPct
			b.WriteString(fmt.Sprintf("- %s %s: PnL %.2f%%, Liq %.0f\n", p.Symbol, p.Side, p.UnrealizedPnLPct, p.LiquidationPrice))
		}
		avgPnl := totalPnl / float64(len(ctx.Positions))
		b.WriteString(fmt.Sprintf("Total %d positions, avg PnL %.2f%%\n", len(ctx.Positions), avgPnl))
	}
	b.WriteString(sectSnapshot)
	if btc, ok := ctx.MarketDataMap["BTCUSDT"]; ok {
		b.WriteString(fmt.Sprintf("BTC: %.2f (1h %+.2f%%, 4h %+.2f%%) | MACD: %.4f | RSI: %.2f",
			btc.CurrentPrice, btc.PriceChange1h, btc.PriceChange4h, btc.CurrentMACD, btc.CurrentRSI7))
		if btc.FundingRate != 0 {
			b.WriteString(fmt.Sprintf(" | funding %.4f%%", btc.FundingRate*100))
		}
		b.WriteString("\n")
	}
	if eth, ok := ctx.MarketDataMap["ETHUSDT"]; ok {
		b.WriteString(fmt.Sprintf("ETH: %.2f (1h %+.2f%%, 4h %+.2f%%) | MACD: %.4f | RSI: %.2f",
			eth.CurrentPrice, eth.PriceChange1h, eth.PriceChange4h, eth.CurrentMACD, eth.CurrentRSI7))
		if eth.FundingRate != 0 {
			b.WriteString(fmt.Sprintf(" | funding %.4f%%", eth.FundingRate*100))
		}
		b.WriteString("\n")
	}
	b.WriteString(sectRecent)
	if len(ctx.RecentOrders) > 0 {
		wins := 0
		for _, o := range ctx.RecentOrders {
			if o.RealizedPnL > 0 {
				wins++
			}
		}
		b.WriteString(fmt.Sprintf("Recent %d trades: %d wins (%.1f%% win rate)\n", len(ctx.RecentOrders), wins, float64(wins)/float64(len(ctx.RecentOrders))*100))
	} else {
		b.WriteString(noTrades)
	}
	if ctx.TradingStats != nil && ctx.TradingStats.TotalTrades > 0 {
		b.WriteString(fmt.Sprintf("Historical: %d trades, win rate %.1f%%, profit factor %.2f, max drawdown %.1f%%\n",
			ctx.TradingStats.TotalTrades, ctx.TradingStats.WinRate, ctx.TradingStats.ProfitFactor, ctx.TradingStats.MaxDrawdownPct))
	}
	b.WriteString(sectOI)
	if ctx.OITopDataMap != nil && len(ctx.OITopDataMap) > 0 {
		n := 0
		for sym, oi := range ctx.OITopDataMap {
			if n >= kernel.AnalystRankingsTopN {
				break
			}
			b.WriteString(fmt.Sprintf("- %s: OI %+.2f%%, price %+.2f%%\n", sym, oi.OIDeltaPercent, oi.PriceDeltaPercent))
			n++
		}
	} else {
		b.WriteString(noData)
	}
	if s := kernel.FormatMarketRankingsForAnalyst(ctx, lang); s != "" {
		b.WriteString(sectRankings)
		b.WriteString(s)
	}
	b.WriteString(sectCandidates)
	if len(ctx.CandidateCoins) == 0 {
		b.WriteString(none)
	} else {
		limit := 10
		if limit > len(ctx.CandidateCoins) {
			limit = len(ctx.CandidateCoins)
		}
		for i := 0; i < limit; i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(ctx.CandidateCoins[i].Symbol)
		}
		if len(ctx.CandidateCoins) > limit {
			b.WriteString(fmt.Sprintf(" ... +%d more", len(ctx.CandidateCoins)-limit))
		}
		b.WriteString("\n")
	}
	b.WriteString(footer)
	return b.String()
}

var reAnalystJSON = regexp.MustCompile(`(?s)\{\s*"bias"\s*:\s*"[^"]*"\s*,\s*"confidence"\s*:\s*\d+\s*,\s*"report_text"\s*:\s*"([^"]*)"\s*\}`)
// report_text 内可能含转义引号 \"，用更宽松的子匹配
var reReportTextLenient = regexp.MustCompile(`"report_text"\s*:\s*"((?:[^"\\]|\\.)*)"`)
// 从 reasoning 文本中推断 bias/confidence（被 max_tokens 截断时取最后一处）
var reInferBias = regexp.MustCompile("(?i)(?:bias|stick to|choose|pick|lean)\\s*[:\\s]*[`\"]?(neutral|bearish|bullish|strong_bullish|strong_bearish)[`\"]?")
var reInferConfidence = regexp.MustCompile(`(?i)confidence\s*[:\s]*(\d+)`)

// repairReportTextNewlines 把 JSON 里 report_text 值中的未转义换行换成 \n，便于解析
func repairReportTextNewlines(jsonStr string) string {
	const key = `"report_text"`
	idx := strings.Index(jsonStr, key)
	if idx < 0 {
		return jsonStr
	}
	idx += len(key)
	for idx < len(jsonStr) && (jsonStr[idx] == ' ' || jsonStr[idx] == '\t') {
		idx++
	}
	if idx >= len(jsonStr) || jsonStr[idx] != ':' {
		return jsonStr
	}
	idx++
	for idx < len(jsonStr) && (jsonStr[idx] == ' ' || jsonStr[idx] == '\t') {
		idx++
	}
	if idx >= len(jsonStr) || jsonStr[idx] != '"' {
		return jsonStr
	}
	idx++
	var buf strings.Builder
	buf.WriteString(jsonStr[:idx])
	for idx < len(jsonStr) {
		c := jsonStr[idx]
		if c == '\\' && idx+1 < len(jsonStr) {
			buf.WriteByte(c)
			buf.WriteByte(jsonStr[idx+1])
			idx += 2
			continue
		}
		if c == '"' {
			buf.WriteString(jsonStr[idx:])
			return buf.String()
		}
		if c == '\r' || c == '\n' {
			buf.WriteString(`\n`)
			if c == '\r' && idx+1 < len(jsonStr) && jsonStr[idx+1] == '\n' {
				idx++
			}
			idx++
			continue
		}
		buf.WriteByte(c)
		idx++
	}
	return jsonStr
}

// inferFromReasoningText 从被截断的 reasoning 文本（如 qwen3.5 reasoning_content）推断 bias/confidence
func inferFromReasoningText(resp string) *AnalystReport {
	if len(resp) < 100 {
		return nil
	}
	var bias string
	if all := reInferBias.FindAllStringSubmatch(resp, -1); len(all) > 0 {
		bias = strings.ToLower(strings.TrimSpace(all[len(all)-1][1]))
	}
	var confidence int
	if all := reInferConfidence.FindAllStringSubmatch(resp, -1); len(all) > 0 {
		for i := len(all) - 1; i >= 0; i-- {
			if n, err := fmt.Sscanf(all[i][1], "%d", &confidence); err == nil && n == 1 && confidence >= 0 && confidence <= 100 {
				break
			}
		}
	}
	if bias == "" && confidence == 0 {
		return nil
	}
	if bias == "" {
		bias = "neutral"
	}
	if confidence == 0 {
		confidence = 50
	}
	out := &analystParseOut{Bias: bias, Confidence: confidence, ReportText: "Analysis truncated; bias/confidence inferred from reasoning."}
	return normalizeReport(out)
}

type analystParseOut struct {
	Bias       string   `json:"bias"`
	Confidence int      `json:"confidence"`
	ReportText string   `json:"report_text"`
	KeyRisks   []string `json:"key_risks"`
}

func parseAnalystResponse(resp string) (*AnalystReport, error) {
	rawResp := resp
	resp = strings.TrimSpace(resp)
	if resp == "" {
		return nil, fmt.Errorf("could not parse analyst response: empty response")
	}
	var out analystParseOut
	// 1. Try raw JSON first
	if err := json.Unmarshal([]byte(resp), &out); err == nil && out.Bias != "" && out.ReportText != "" {
		return normalizeReport(&out), nil
	}
	// 2. Try extract from code block (```json ... ``` or ``` ... ```)
	if idx := strings.Index(resp, "```"); idx >= 0 {
		rest := resp[idx+3:]
		if strings.HasPrefix(strings.ToLower(rest), "json") {
			rest = strings.TrimSpace(rest[4:])
		} else {
			rest = strings.TrimSpace(rest)
		}
		end := strings.Index(rest, "```")
		if end > 0 {
			rest = strings.TrimSpace(rest[:end])
			if rest != "" && json.Unmarshal([]byte(rest), &out) == nil && out.Bias != "" && out.ReportText != "" {
				return normalizeReport(&out), nil
			}
		} else {
			// 2b. 截断时无闭合 ```：从 rest 取首个 { 到最后一个 } 再试解析（reasoning 末尾的 ```json\n {...} 被截断）
			first := strings.Index(rest, "{")
			last := strings.LastIndex(rest, "}")
			if first >= 0 && last > first {
				sub := strings.TrimSpace(rest[first : last+1])
				if json.Unmarshal([]byte(sub), &out) == nil && out.Bias != "" && out.ReportText != "" {
					return normalizeReport(&out), nil
				}
			}
		}
	}
	// 3. Try extract first { ... last } (model 可能输出 "分析如下：\n{...}\n")
	first := strings.Index(resp, "{")
	last := strings.LastIndex(resp, "}")
	if first >= 0 && last > first {
		sub := resp[first : last+1]
		if err := json.Unmarshal([]byte(sub), &out); err == nil && out.Bias != "" && out.ReportText != "" {
			return normalizeReport(&out), nil
		}
		// 3a. 部分模型在 report_text 里写未转义换行，尝试修成 \n 再解析
		if strings.Contains(sub, `"report_text"`) && (strings.Contains(sub, "\n") || strings.Contains(sub, "\r")) {
			repaired := repairReportTextNewlines(sub)
			if repaired != sub && json.Unmarshal([]byte(repaired), &out) == nil && out.Bias != "" && out.ReportText != "" {
				return normalizeReport(&out), nil
			}
		}
		// 3b. 尝试用 map 解析（键顺序任意、可含多余键），再转成 struct
		var m map[string]interface{}
		if json.Unmarshal([]byte(sub), &m) == nil {
			if v, _ := m["bias"]; v != nil {
				if s, ok := v.(string); ok {
					out.Bias = s
				}
			}
			if v, _ := m["confidence"]; v != nil {
				switch n := v.(type) {
				case float64:
					out.Confidence = int(n)
				case int:
					out.Confidence = n
				}
			}
			if v, _ := m["report_text"]; v != nil {
				if s, ok := v.(string); ok {
					out.ReportText = s
				}
			}
			if v, _ := m["key_risks"]; v != nil {
				if arr, ok := v.([]interface{}); ok {
					for _, it := range arr {
						if s, ok := it.(string); ok {
							out.KeyRisks = append(out.KeyRisks, s)
						}
					}
				}
			}
			if out.Bias != "" && out.ReportText != "" {
				return normalizeReport(&out), nil
			}
		}
	}
	// 4. Fallback: 宽松 regex（report_text 内可含 \"）
	if m := reReportTextLenient.FindStringSubmatch(resp); len(m) >= 2 {
		out.ReportText = strings.ReplaceAll(m[1], `\"`, `"`)
		if i := strings.Index(resp, `"bias"`); i >= 0 {
			blob := resp[i:]
			if j := strings.Index(blob, `"`); j >= 0 {
				blob = blob[j+1:]
				if end := strings.Index(blob, `"`); end >= 0 {
					out.Bias = blob[:end]
				}
			}
		}
		if i := strings.Index(resp, `"confidence"`); i >= 0 {
			blob := resp[i:]
			_, _ = fmt.Sscanf(blob, `"confidence":%d`, &out.Confidence)
		}
		if out.Bias != "" || out.ReportText != "" {
			return normalizeReport(&out), nil
		}
	}
	// 5. 原严格 regex
	if m := reAnalystJSON.FindStringSubmatch(resp); len(m) >= 2 {
		out.ReportText = m[1]
		if i := strings.Index(resp, `"bias"`); i >= 0 {
			blob := resp[i:]
			if j := strings.Index(blob, `"`); j >= 0 {
				blob = blob[j+1:]
				if end := strings.Index(blob, `"`); end >= 0 {
					out.Bias = blob[:end]
				}
			}
		}
		if i := strings.Index(resp, `"confidence"`); i >= 0 {
			blob := resp[i:]
			_, _ = fmt.Sscanf(blob, `"confidence":%d`, &out.Confidence)
		}
		return normalizeReport(&out), nil
	}
	// 6. 被 max_tokens 截断的 reasoning 文本（如 qwen3.5 reasoning_content）：从中推断 bias/confidence
	if inferred := inferFromReasoningText(resp); inferred != nil {
		return inferred, nil
	}
	// 错误信息带响应片段便于排查
	snippet := rawResp
	if len(snippet) > 400 {
		snippet = snippet[:400] + "..."
	}
	snippet = strings.ReplaceAll(snippet, "\n", " ")
	return nil, fmt.Errorf("could not parse analyst response (snippet: %s)", snippet)
}

func normalizeReport(out *analystParseOut) *AnalystReport {
	bias := store.AnalystBias(strings.ToLower(strings.TrimSpace(out.Bias)))
	switch bias {
	case store.AnalystBiasBullish, store.AnalystBiasBearish, store.AnalystBiasNeutral,
		store.AnalystBiasStrongBull, store.AnalystBiasStrongBear:
	default:
		bias = store.AnalystBiasNeutral
	}
	if out.Confidence < 0 {
		out.Confidence = 0
	}
	if out.Confidence > 100 {
		out.Confidence = 100
	}
	return &AnalystReport{
		Bias:       bias,
		Confidence: out.Confidence,
		ReportText: strings.TrimSpace(out.ReportText),
		KeyRisks:   out.KeyRisks,
	}
}

