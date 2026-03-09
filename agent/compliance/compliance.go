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

const complianceSystemPromptEN = `You are a Compliance Officer. Your job is to audit trading decisions before execution. You MUST output per-decision audit (decisions_audit) so that open/close actions can be approved or rejected independently.

Input: trader's thinking (chain-of-thought), list of decisions (symbol, action, leverage, position_size_usd, stop_loss, take_profit, confidence, reasoning), account snapshot, current positions, and compliance rules.

Output MUST be valid JSON only (no other text):
{"approved": true or false, "reason": "brief summary", "violations": ["optional"], "decisions_audit": [{"index": 0, "approved": true or false, "reason": ""}, ...], "force_actions": [{"action": "close_all"|"close_position"|"pause_trading", "symbol": "optional", "param": optional}]}

Required: decisions_audit must have exactly one entry per input decision, same order (index 0,1,2,...). For each decision:
- approved=true: passes (e.g. close positions often pass; opens that respect limits pass).
- approved=false: reject with reason (e.g. "excluded coin", "leverage exceeds max", "confidence below min").
Batch "approved" = true if at least one decision approved (partial execution); "reason" = brief summary.

Rules:
- Reject a decision if excluded coins, exceeds max leverage/position ratio/max positions, or confidence below min_confidence for opens.
- Close/hedge actions (close_long, close_short, reduce) can often be approved; reject only if they violate rules.
- violations: list rule names or items violated. force_actions: optional (close_all, close_position, pause_trading).`

const complianceSystemPromptZH = `你是风控官。你的职责是在执行前审计交易决策。你必须按条输出审计结果（decisions_audit），使开仓/平仓可分别通过或驳回。

输入：交易员的思考链、决策列表（symbol, action, leverage, position_size_usd, stop_loss, take_profit, confidence, reasoning）、账户快照、当前持仓、风控规则。

输出必须是纯 JSON（无其他文字）：
{"approved": true 或 false, "reason": "简要说明", "violations": ["可选"], "decisions_audit": [{"index": 0, "approved": true 或 false, "reason": ""}, ...], "force_actions": [{"action": "close_all"|"close_position"|"pause_trading", "symbol": "可选", "param": 可选}]}

要求：decisions_audit 与输入决策一一对应、顺序一致（index 0,1,2,...）。每条决策：
- approved=true：通过（例如平仓多放行；符合限制的开仓通过）。
- approved=false：驳回并填写 reason（如 "排除币种"、"杠杆超限"、"置信度不足"）。
批级 "approved" = 至少有一条通过时为 true（部分执行）；"reason" 为简要总结。

规则：
- 若决策涉及排除币种、超过最大杠杆/仓位占比/最大持仓数、或开仓置信度低于 min_confidence，则驳回该条。
- 平仓/对冲（close_long, close_short, reduce）通常可放行；仅在违反规则时驳回。
- violations：列出违规项。force_actions：可选（close_all, close_position, pause_trading）。JSON 字段名保持英文。`

func getComplianceSystemPrompt(lang string) string {
	if lang == "zh" || lang == "zh-CN" {
		return complianceSystemPromptZH
	}
	return complianceSystemPromptEN
}

// RunCompliance 运行风控官：输入 thinking + decisions + 账户/持仓/规则，返回 approved/reason/violations
func RunCompliance(input *ComplianceInput, client mcp.AIClient) (*ComplianceOutput, error) {
	if input == nil || client == nil {
		return nil, fmt.Errorf("compliance: input and client are required")
	}
	systemPrompt := getComplianceSystemPrompt(input.Language)
	userPrompt := buildComplianceUserPrompt(input)
	resp, err := client.CallWithMessages(systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("compliance AI call failed: %w", err)
	}
	out, err := parseComplianceResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("compliance parse response: %w", err)
	}
	out.SystemPrompt = systemPrompt
	out.UserPrompt = userPrompt
	// 单条审批：若 decisions_audit 与输入条数一致，批级 approved = 至少有一条通过（便于执行层部分执行）
	if n := len(input.Decisions); n > 0 && len(out.DecisionsAudit) == n {
		anyApproved := false
		for i := range out.DecisionsAudit {
			if out.DecisionsAudit[i].Index != i {
				out.DecisionsAudit[i].Index = i
			}
			if out.DecisionsAudit[i].Approved {
				anyApproved = true
			}
		}
		if anyApproved {
			out.Approved = true
		}
	}
	return out, nil
}

func buildComplianceUserPrompt(in *ComplianceInput) string {
	zh := in.Language == "zh" || in.Language == "zh-CN"
	var (
		labelAnalyst, labelThinking, labelPending, labelAccount, labelPositions, labelRules, labelNone string
	)
	if zh {
		labelAnalyst = "## 分析师报告（本轮）\n"
		labelThinking = "## 交易员思考链\n"
		labelPending = "## 待执行决策\n"
		labelAccount = "\n## 账户\n"
		labelPositions = "\n## 当前持仓\n"
		labelRules = "\n## 风控规则\n"
		labelNone = "(无)\n"
	} else {
		labelAnalyst = "## Analyst report (this round)\n"
		labelThinking = "## Trader thinking (chain-of-thought)\n"
		labelPending = "## Pending decisions\n"
		labelAccount = "\n## Account\n"
		labelPositions = "\n## Current positions\n"
		labelRules = "\n## Compliance rules\n"
		labelNone = "(none)\n"
	}
	var b strings.Builder
	if in.AnalystBias != "" {
		b.WriteString(labelAnalyst)
		b.WriteString(fmt.Sprintf("Bias: %s | Confidence: %d\n\n", in.AnalystBias, in.AnalystConfidence))
	}
	b.WriteString(labelThinking)
	b.WriteString(in.Thinking)
	b.WriteString("\n\n")
	b.WriteString(labelPending)
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
	b.WriteString(labelAccount)
	b.WriteString(fmt.Sprintf("Total equity: %.2f USDT, Available: %.2f, Positions: %d\n",
		in.Account.TotalEquity, in.Account.AvailableBalance, in.Account.PositionCount))
	b.WriteString(labelPositions)
	for _, p := range in.Positions {
		b.WriteString(fmt.Sprintf("- %s %s: entry=%.4f mark=%.4f liq=%.4f leverage=%.0f\n",
			p.Symbol, p.Side, p.EntryPrice, p.MarkPrice, p.LiquidationPrice, p.Leverage))
	}
	if len(in.Positions) == 0 {
		b.WriteString(labelNone)
	}
	b.WriteString(labelRules)
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
		if zh {
			b.WriteString("market_type: " + in.MarketType + " (perpetual=合约/清算与资金费率; spot=现货)\n")
		} else {
			b.WriteString("market_type: " + in.MarketType + " (perpetual=leverage/funding; spot=spot)\n")
		}
	}
	return b.String()
}

var reComplianceJSON = regexp.MustCompile(`(?s)\{\s*"approved"\s*:\s*(true|false)\s*,\s*"reason"\s*:\s*"([^"]*)"\s*(?:,\s*"violations"\s*:\s*(\[[^\]]*\]))?\s*\}`)

func parseComplianceResponse(resp string) (*ComplianceOutput, error) {
	resp = strings.TrimSpace(resp)
	var out struct {
		Approved      bool     `json:"approved"`
		Reason        string   `json:"reason"`
		Violations    []string `json:"violations"`
		DecisionsAudit []struct {
			Index   int    `json:"index"`
			Approved bool  `json:"approved"`
			Reason  string `json:"reason"`
		} `json:"decisions_audit"`
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
	Approved      bool     `json:"approved"`
	Reason        string   `json:"reason"`
	Violations    []string `json:"violations"`
	DecisionsAudit []struct {
		Index   int    `json:"index"`
		Approved bool  `json:"approved"`
		Reason  string `json:"reason"`
	} `json:"decisions_audit"`
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
	for _, da := range v.DecisionsAudit {
		o.DecisionsAudit = append(o.DecisionsAudit, DecisionAuditItem{Index: da.Index, Approved: da.Approved, Reason: da.Reason})
	}
	for _, fa := range v.ForceActions {
		o.ForceActions = append(o.ForceActions, ForceAction{Action: fa.Action, Symbol: fa.Symbol, Param: fa.Param})
	}
	return o
}
