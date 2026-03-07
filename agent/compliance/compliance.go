// Package compliance: 风控官 Agent（P2-2），读待执行 decisions + 规则，输出 approved/reason/violations
package compliance

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"nofx/logger"
	"nofx/mcp"
)

const complianceSystemPrompt = `You are a Compliance Officer. Your job is to audit trading decisions before execution.
Input: trader's thinking (chain-of-thought), list of decisions (symbol, action, leverage, position_size_usd, stop_loss, take_profit, confidence, reasoning), account snapshot (equity, available, positions count), current positions, and compliance rules (max leverage, position limits, excluded coins, etc.).

Output MUST be valid JSON only (no other text):
{"approved": true or false, "reason": "brief explanation", "violations": ["optional list of rule violations"], "force_actions": [{"action": "close_all"|"close_position"|"pause_trading", "symbol": "optional for close_position", "param": optional number e.g. minutes for pause_trading}]}

Optional force_actions (executed before normal decisions when approved): use when you want to force risk reduction: "close_all" = close all positions; "close_position" with "symbol" = close that symbol; "pause_trading" with param = pause minutes.

Rules:
- approved=false if any decision uses excluded coins, or exceeds max leverage / position ratio / max positions, or confidence below min_confidence for opens.
- approved=false if reasoning is empty or decisions contradict risk limits.
- approved=true only when all decisions pass the rules; set reason to "OK" or brief summary.
- violations: list specific rule names or items that were violated (e.g. "BTC leverage 10x exceeds max 5x").`

// RunCompliance 运行风控官：输入 thinking + decisions + 账户/持仓/规则，返回 approved/reason/violations
func RunCompliance(input *ComplianceInput, client mcp.AIClient) (*ComplianceOutput, error) {
	if input == nil || client == nil {
		return nil, fmt.Errorf("compliance: input and client are required")
	}
	userPrompt := buildComplianceUserPrompt(input)
	resp, err := client.CallWithMessages(complianceSystemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("compliance AI call failed: %w", err)
	}
	out, err := parseComplianceResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("compliance parse response: %w", err)
	}
	return out, nil
}

func buildComplianceUserPrompt(in *ComplianceInput) string {
	var b strings.Builder
	b.WriteString("## Trader thinking (chain-of-thought)\n")
	b.WriteString(in.Thinking)
	b.WriteString("\n\n## Pending decisions\n")
	for i, d := range in.Decisions {
		b.WriteString(fmt.Sprintf("- [%d] %s %s", i+1, d.Symbol, d.Action))
		if d.Leverage > 0 {
			b.WriteString(fmt.Sprintf(" leverage=%d", d.Leverage))
		}
		if d.PositionSizeUSD > 0 {
			b.WriteString(fmt.Sprintf(" position_size_usd=%.0f", d.PositionSizeUSD))
		}
		if d.Confidence > 0 {
			b.WriteString(fmt.Sprintf(" confidence=%d", d.Confidence))
		}
		b.WriteString(fmt.Sprintf(" reasoning=%q\n", d.Reasoning))
	}
	b.WriteString("\n## Account\n")
	b.WriteString(fmt.Sprintf("Total equity: %.2f USDT, Available: %.2f, Positions: %d\n",
		in.Account.TotalEquity, in.Account.AvailableBalance, in.Account.PositionCount))
	b.WriteString("\n## Current positions\n")
	for _, p := range in.Positions {
		b.WriteString(fmt.Sprintf("- %s %s: entry=%.4f mark=%.4f liq=%.4f leverage=%.0f\n",
			p.Symbol, p.Side, p.EntryPrice, p.MarkPrice, p.LiquidationPrice, p.Leverage))
	}
	if len(in.Positions) == 0 {
		b.WriteString("(none)\n")
	}
	b.WriteString("\n## Compliance rules\n")
	r := in.Rules
	b.WriteString(fmt.Sprintf("max_positions=%d btc_eth_max_leverage=%d altcoin_max_leverage=%d ",
		r.MaxPositions, r.BTCETHMaxLeverage, r.AltcoinMaxLeverage))
	b.WriteString(fmt.Sprintf("btc_eth_max_position_ratio=%.2f altcoin_max_position_ratio=%.2f ",
		r.BTCETHMaxPositionValueRatio, r.AltcoinMaxPositionValueRatio))
	b.WriteString(fmt.Sprintf("max_margin_usage=%.2f min_position_size=%.0f min_confidence=%d\n",
		r.MaxMarginUsage, r.MinPositionSize, r.MinConfidence))
	if len(r.ExcludedCoins) > 0 {
		b.WriteString("excluded_coins: " + strings.Join(r.ExcludedCoins, ", ") + "\n")
	}
	if in.MarketType != "" {
		b.WriteString("market_type: " + in.MarketType + " (perpetual=合约/清算与资金费率; spot=现货)\n")
	}
	return b.String()
}

var reComplianceJSON = regexp.MustCompile(`(?s)\{\s*"approved"\s*:\s*(true|false)\s*,\s*"reason"\s*:\s*"([^"]*)"\s*(?:,\s*"violations"\s*:\s*(\[[^\]]*\]))?\s*\}`)

func parseComplianceResponse(resp string) (*ComplianceOutput, error) {
	resp = strings.TrimSpace(resp)
	var out struct {
		Approved   bool     `json:"approved"`
		Reason     string   `json:"reason"`
		Violations []string `json:"violations"`
		ForceActions []struct {
			Action string  `json:"action"`
			Symbol string  `json:"symbol"`
			Param  float64 `json:"param"`
		} `json:"force_actions"`
	}
	if err := json.Unmarshal([]byte(resp), &out); err == nil {
		return toOutput(&out), nil
	}
	if idx := strings.Index(resp, "```"); idx >= 0 {
		rest := resp[idx+3:]
		if strings.HasPrefix(strings.ToLower(rest), "json") {
			rest = rest[4:]
		}
		end := strings.Index(rest, "```")
		if end > 0 {
			rest = strings.TrimSpace(rest[:end])
			if err := json.Unmarshal([]byte(rest), &out); err == nil {
				return toOutput(&out), nil
			}
		}
	}
	if m := reComplianceJSON.FindStringSubmatch(resp); len(m) >= 3 {
		out.Approved = strings.EqualFold(m[1], "true")
		out.Reason = m[2]
		if len(m) > 3 && m[3] != "" {
			_ = json.Unmarshal([]byte(m[3]), &out.Violations)
		}
		return toOutput(&out), nil
	}
	logger.Warnf("[Compliance] Raw response (parse failed): %s", resp)
	return nil, fmt.Errorf("could not parse compliance response")
}

func toOutput(v *struct {
	Approved   bool     `json:"approved"`
	Reason     string   `json:"reason"`
	Violations []string `json:"violations"`
	ForceActions []struct {
		Action string  `json:"action"`
		Symbol string  `json:"symbol"`
		Param  float64 `json:"param"`
	} `json:"force_actions"`
}) *ComplianceOutput {
	o := &ComplianceOutput{
		Approved:   v.Approved,
		Reason:     v.Reason,
		Violations: v.Violations,
	}
	for _, fa := range v.ForceActions {
		o.ForceActions = append(o.ForceActions, ForceAction{Action: fa.Action, Symbol: fa.Symbol, Param: fa.Param})
	}
	return o
}
