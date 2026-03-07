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

// AnalystReport 分析师输出：宏观偏向、信心、报告正文
type AnalystReport struct {
	Bias       store.AnalystBias `json:"bias"`
	Confidence int              `json:"confidence"`
	ReportText string           `json:"report_text"`
	Raw        string           `json:"-"`
}

const analystSystemPrompt = `You are a Market Insight Analyst. Your job is to read the given market data and output a brief analysis with:
1. **bias**: One of "bullish", "bearish", "neutral", "strong_bullish", "strong_bearish"
2. **confidence**: Integer 0-100 (how confident you are in this bias)
3. **report_text**: A short paragraph (2-5 sentences) summarizing: key price/volume/OI signals, main risk, and recommended stance.

Output MUST be valid JSON in this exact format (no other text):
{"bias":"...","confidence":NN,"report_text":"..."}`

// RunAnalyst 运行分析师 Agent：根据 ctx 生成报告（不写黑板，由调用方写入）
func RunAnalyst(ctx *kernel.Context, engine *kernel.StrategyEngine, client mcp.AIClient) (*AnalystReport, error) {
	if ctx == nil || engine == nil || client == nil {
		return nil, fmt.Errorf("analyst: ctx, engine and client are required")
	}
	userPrompt := buildAnalystUserPrompt(ctx)
	resp, err := client.CallWithMessages(analystSystemPrompt, userPrompt)
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

func buildAnalystUserPrompt(ctx *kernel.Context) string {
	var b strings.Builder
	b.WriteString("## Account\n")
	b.WriteString(fmt.Sprintf("Equity: %.2f USDT, Available: %.2f, Positions: %d\n\n", ctx.Account.TotalEquity, ctx.Account.AvailableBalance, ctx.Account.PositionCount))
	b.WriteString("## Positions\n")
	for _, p := range ctx.Positions {
		b.WriteString(fmt.Sprintf("- %s %s: PnL %.2f%%, Liq %.0f\n", p.Symbol, p.Side, p.UnrealizedPnLPct, p.LiquidationPrice))
	}
	if len(ctx.Positions) == 0 {
		b.WriteString("(none)\n")
	}
	b.WriteString("\n## Candidate coins & market snapshot\n")
	for _, c := range ctx.CandidateCoins {
		data, ok := ctx.MarketDataMap[c.Symbol]
		if !ok {
			continue
		}
		b.WriteString(fmt.Sprintf("- %s: price %.4f, 1h chg %.2f%%, 4h chg %.2f%%",
			c.Symbol, data.CurrentPrice, data.PriceChange1h, data.PriceChange4h))
		if data.CurrentRSI7 > 0 {
			b.WriteString(fmt.Sprintf(", RSI7 %.0f", data.CurrentRSI7))
		}
		if data.FundingRate != 0 {
			b.WriteString(fmt.Sprintf(", funding %.2e", data.FundingRate))
		}
		if data.OpenInterest != nil && data.OpenInterest.Latest > 0 {
			b.WriteString(fmt.Sprintf(", OI %.0f", data.OpenInterest.Latest))
		}
		b.WriteString("\n")
	}
	if ctx.OITopDataMap != nil && len(ctx.OITopDataMap) > 0 {
		b.WriteString("\n## OI ranking (top movers)\n")
		for sym, oi := range ctx.OITopDataMap {
			b.WriteString(fmt.Sprintf("- %s: OI delta %.2f%%, price chg %.2f%%\n", sym, oi.OIDeltaPercent, oi.PriceDeltaPercent))
		}
	}
	return b.String()
}

var reAnalystJSON = regexp.MustCompile(`(?s)\{\s*"bias"\s*:\s*"[^"]*"\s*,\s*"confidence"\s*:\s*\d+\s*,\s*"report_text"\s*:\s*"([^"]*)"\s*\}`)

func parseAnalystResponse(resp string) (*AnalystReport, error) {
	resp = strings.TrimSpace(resp)
	// Try raw JSON first
	var out struct {
		Bias       string `json:"bias"`
		Confidence int    `json:"confidence"`
		ReportText string `json:"report_text"`
	}
	if err := json.Unmarshal([]byte(resp), &out); err == nil {
		return normalizeReport(&out), nil
	}
	// Try extract from code block
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
	// Fallback regex
	if m := reAnalystJSON.FindStringSubmatch(resp); len(m) >= 2 {
		out.ReportText = m[1]
		// try to get bias and confidence from same blob
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

func normalizeReport(out *struct {
	Bias       string `json:"bias"`
	Confidence int    `json:"confidence"`
	ReportText string `json:"report_text"`
}) *AnalystReport {
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
	}
}

