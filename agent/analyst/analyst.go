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

// AnalystReport 分析师输出：宏观偏向、信心、报告正文，可选 key_risks
type AnalystReport struct {
	Bias       store.AnalystBias `json:"bias"`
	Confidence int              `json:"confidence"`
	ReportText string           `json:"report_text"`
	KeyRisks   []string         `json:"key_risks,omitempty"`
	Raw        string           `json:"-"`
}

// analystRoleAndOutputPrompt 分析师角色与输出格式（不含数据字典，字典由 kernel.GetSchemaPromptForAnalyst 提供）
const analystRoleAndOutputZH = `---
# 角色
你是宏观市场观察员。你将看到：账户与持仓摘要、BTC/ETH 快照（含可选资金费率）、近期与历史表现、OI movers（Top 10）、全市场排名（OI/资金流/涨跌榜 Top 10）、候选币种列表。只根据上述**汇总数据**做宏观结论，不做逐币分析，不做交易决策。

# 输出格式（严格）
你必须**只输出一个 JSON 对象**，禁止 markdown 代码块、禁止前后说明文字。字段与类型如下：
- bias：字符串，且必须为 "bullish" | "bearish" | "neutral" | "strong_bullish" | "strong_bearish" 之一
- confidence：整数，0-100
- report_text：字符串，2-5 句（趋势、主要风险、建议立场）
- key_risks：（可选）字符串数组，1-3 条短语

示例（仅作格式参考）：{"bias":"neutral","confidence":60,"report_text":"BTC 横盘，全市场资金流分歧。建议观望。"}
可选：增加 "key_risks": ["风险1", "风险2"]。`

const analystRoleAndOutputEN = `---
# Role
You are a Macro Market Observer. You will see: account and positions summary, BTC/ETH snapshot (optional funding), recent and historical performance, OI movers (Top 10), market-wide rankings (OI / flow / gainers-losers, Top 10), candidate symbol list. Output a high-level view based only on these **summary data**; no per-coin analysis, no trading decisions.

# Output format (strict)
You must output **exactly one JSON object**; no markdown code fences, no text before or after. Fields and types:
- bias: string, one of "bullish" | "bearish" | "neutral" | "strong_bullish" | "strong_bearish"
- confidence: integer, 0-100
- report_text: string, 2-5 sentences (trend, main risk, recommended stance)
- key_risks: (optional) array of strings, 1-3 short items

Example (format only): {"bias":"neutral","confidence":60,"report_text":"BTC flat, fund flow mixed. Prefer wait."}
Optional: add "key_risks": ["risk1", "risk2"].`

// buildAnalystSystemPrompt 拼装分析师系统提示：数据字典（按职责拆分）+ 角色与输出格式
func buildAnalystSystemPrompt(engine *kernel.StrategyEngine) string {
	lang := engine.GetLanguage()
	schema := kernel.GetSchemaPromptForAnalyst(lang)
	if lang == kernel.LangChinese {
		return schema + "\n" + analystRoleAndOutputZH
	}
	return schema + "\n" + analystRoleAndOutputEN
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
	return report, nil
}

// buildAnalystUserPrompt 汇总版：仅给宏观结论所需信息，不喂逐币完整数据（降低分析师职责过重与幻觉）
func buildAnalystUserPrompt(ctx *kernel.Context, lang kernel.Language) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Time: %s | Period #%d | Runtime %d min\n\n", ctx.CurrentTime, ctx.CallCount, ctx.RuntimeMinutes))
	b.WriteString("## Account\n")
	b.WriteString(fmt.Sprintf("Equity: %.2f USDT | Available: %.2f | Positions: %d | Margin: %.1f%%\n\n",
		ctx.Account.TotalEquity, ctx.Account.AvailableBalance, ctx.Account.PositionCount, ctx.Account.MarginUsedPct))
	b.WriteString("## Positions summary\n")
	if len(ctx.Positions) == 0 {
		b.WriteString("(none)\n")
	} else {
		totalPnl := 0.0
		for _, p := range ctx.Positions {
			totalPnl += p.UnrealizedPnLPct
			b.WriteString(fmt.Sprintf("- %s %s: PnL %.2f%%, Liq %.0f\n", p.Symbol, p.Side, p.UnrealizedPnLPct, p.LiquidationPrice))
		}
		avgPnl := totalPnl / float64(len(ctx.Positions))
		b.WriteString(fmt.Sprintf("Total %d positions, avg PnL %.2f%%\n", len(ctx.Positions), avgPnl))
	}
	b.WriteString("\n## BTC / market snapshot\n")
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
	b.WriteString("\n## Recent performance\n")
	if len(ctx.RecentOrders) > 0 {
		wins := 0
		for _, o := range ctx.RecentOrders {
			if o.RealizedPnL > 0 {
				wins++
			}
		}
		b.WriteString(fmt.Sprintf("Recent %d trades: %d wins (%.1f%% win rate)\n", len(ctx.RecentOrders), wins, float64(wins)/float64(len(ctx.RecentOrders))*100))
	} else {
		b.WriteString("No recent trades\n")
	}
	if ctx.TradingStats != nil && ctx.TradingStats.TotalTrades > 0 {
		b.WriteString(fmt.Sprintf("Historical: %d trades, win rate %.1f%%, profit factor %.2f, max drawdown %.1f%%\n",
			ctx.TradingStats.TotalTrades, ctx.TradingStats.WinRate, ctx.TradingStats.ProfitFactor, ctx.TradingStats.MaxDrawdownPct))
	}
	b.WriteString("\n## OI movers (top 10)\n")
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
		b.WriteString("(no data)\n")
	}
	// 全市场排名（OI / 资金流 / 涨跌榜）Top 10，与交易员同源
	if s := kernel.FormatMarketRankingsForAnalyst(ctx, lang); s != "" {
		b.WriteString("\n## Market-wide rankings (top 10)\n")
		b.WriteString(s)
	}
	b.WriteString("\n## Candidate symbols (no per-coin data here)\n")
	if len(ctx.CandidateCoins) == 0 {
		b.WriteString("(none)\n")
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
	b.WriteString("\n---\nOutput only one JSON object (bias, confidence, report_text [, key_risks]). No other text.\n")
	return b.String()
}

var reAnalystJSON = regexp.MustCompile(`(?s)\{\s*"bias"\s*:\s*"[^"]*"\s*,\s*"confidence"\s*:\s*\d+\s*,\s*"report_text"\s*:\s*"([^"]*)"\s*\}`)

type analystParseOut struct {
	Bias       string   `json:"bias"`
	Confidence int      `json:"confidence"`
	ReportText string   `json:"report_text"`
	KeyRisks   []string `json:"key_risks"`
}

func parseAnalystResponse(resp string) (*AnalystReport, error) {
	resp = strings.TrimSpace(resp)
	var out analystParseOut
	// 1. Try raw JSON first
	if err := json.Unmarshal([]byte(resp), &out); err == nil {
		return normalizeReport(&out), nil
	}
	// 2. Try extract from code block (```json ... ``` or ``` ... ```)
	if idx := strings.Index(resp, "```"); idx >= 0 {
		rest := resp[idx+3:]
		if strings.HasPrefix(strings.ToLower(rest), "json") {
			rest = rest[4:]
		}
		end := strings.Index(rest, "```")
		if end > 0 {
			rest = strings.TrimSpace(rest[:end])
			if err := json.Unmarshal([]byte(rest), &out); err == nil {
				return normalizeReport(&out), nil
			}
		}
	}
	// 3. Try extract first { ... last } (model 可能输出 "分析如下：\n{...}\n")
	first := strings.Index(resp, "{")
	last := strings.LastIndex(resp, "}")
	if first >= 0 && last > first {
		sub := resp[first : last+1]
		if err := json.Unmarshal([]byte(sub), &out); err == nil {
			return normalizeReport(&out), nil
		}
	}
	// 4. Fallback regex
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
	return nil, fmt.Errorf("could not parse analyst response")
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

