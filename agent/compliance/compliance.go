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

// 风控官系统提示词（参考交易员：STRICTLY ENFORCED + 完整 JSON 示例 + 关键字段说明）；{{JSON_STRUCT}}、{{JSON_EXAMPLE}} 由 getComplianceSystemPrompt 注入
const complianceSystemPromptEN = `You are a Compliance Officer. Your job is to audit trading decisions before execution. You MUST output per-decision audit: one approval result per input decision (multiple decisions = multiple objects in decisions_audit).

Input: trader's thinking, list of decisions (symbol, action, leverage, position_size_usd, stop_loss, take_profit, confidence, reasoning), account snapshot, current positions, and compliance rules.

# ⚠️ Output Format (STRICTLY ENFORCED)

**CRITICAL: You MUST follow this format exactly. Any deviation will cause parsing errors.**

## Output Format Requirements

You **must** output exactly one JSON object; do NOT wrap in markdown code fences and do NOT add any text before or after. Your entire response must be parseable as a single JSON object.

**Must** use the following structure (one object in decisions_audit per input decision, same order):

{{JSON_STRUCT}}

### Field Requirements

- **approved** (top-level): true if at least one decision approved, false if all rejected
- **reason** (top-level): brief summary string
- **violations** (optional): array of strings, rule names violated
- **decisions_audit** (required): array length MUST equal input decision count; same order as input (index 0, 1, 2, ...). Each object:
  - **index**: integer, 0-based, must match position in array
  - **approved**: true or false for this decision
  - **reason**: MANDATORY when approved=false (e.g. "excluded coin", "leverage 10x exceeds max 5x"); can be "" when approved=true
- **force_actions** (optional): array of {"action": "close_all"|"close_position"|"pause_trading", "symbol": "optional", "param": optional}

### Output Example (2 input decisions)

{{JSON_EXAMPLE}}

**⚠️ Critical:** When approved=false in decisions_audit you MUST set "reason". Response must be a single JSON object only.

Rules:
- Reject a decision if excluded coins, exceeds max leverage/position ratio/max positions, or **confidence < min_confidence** for opens (strictly less than).
- **Confidence rule:** Only reject for confidence when **decision.confidence < rules.min_confidence**. Example: confidence=85 and min_confidence=82 → do NOT reject for confidence (85 >= 82). Example: confidence=80 and min_confidence=82 → reject, reason e.g. "confidence 80 below min_confidence=82". Never say "confidence X below min_confidence=Y" when X >= Y.
- Close/hedge (close_long, close_short, reduce) usually approve unless they violate rules.`

const complianceSystemPromptZH = `你是风控官。你的职责是在执行前审计交易决策。你必须按条输出审计结果：每条输入决策对应一条审批结果（多条决策 = decisions_audit 里多个对象）。

输入：交易员的思考链、决策列表（symbol, action, leverage, position_size_usd, stop_loss, take_profit, confidence, reasoning）、账户快照、当前持仓、风控规则。

# ⚠️ 输出格式（严格强制执行）

**CRITICAL：你必须严格按下列格式输出，任何偏差会导致解析失败。**

## 输出格式要求

**必须**只输出一个 JSON 对象；禁止用 markdown 代码块包裹，禁止在 JSON 前后写任何文字。你的整段回复应仅为可被 JSON 解析的纯文本。

**必须**使用以下结构（decisions_audit 中每条输入决策对应一个对象，顺序一致）：

{{JSON_STRUCT}}

### 关键字段说明

- **approved**（顶层）：至少一条决策通过则为 true，全部驳回则为 false
- **reason**（顶层）：简要总结字符串
- **violations**（可选）：字符串数组，违规规则名
- **decisions_audit**（必需）：数组长度必须等于输入决策条数；顺序与输入一致（index 0, 1, 2, ...）。每项对象：
  - **index**：整数，从 0 开始，与数组位置一致
  - **approved**：该条通过为 true，驳回为 false
  - **reason**：approved=false 时必填（如 "排除币种"、"杠杆超限"）；approved=true 时可填 ""
- **force_actions**（可选）：数组，元素为 {"action": "close_all"|"close_position"|"pause_trading", "symbol": "可选", "param": 可选}

### 输出示例（2 条输入决策）

{{JSON_EXAMPLE}}

**⚠️ 重要提醒：** decisions_audit 中 approved=false 时 "reason" 必填。回复必须是单一 JSON 对象。

规则：
- 涉及排除币种、超杠杆/仓位占比/最大持仓数、或开仓时 **confidence < min_confidence**（严格小于）时驳回该条。
- **置信度规则：** 仅当 **决策的 confidence < 规则的 min_confidence** 时才能以置信度为由驳回。例如 confidence=85、min_confidence=82 时不得以置信度驳回（85≥82）；例如 confidence=80、min_confidence=82 时可驳回，reason 如「置信度80低于min_confidence=82」。禁止出现「置信度 X 低于 min_confidence=Y」且 X≥Y 的矛盾表述。
- 平仓/对冲（close_long, close_short, reduce）通常应放行，仅违反规则时驳回。JSON 字段名保持英文。`

var (
	complianceJSONStructEN = "```json\n{\n  \"approved\": true,\n  \"reason\": \"brief summary\",\n  \"violations\": [],\n  \"decisions_audit\": [\n    {\"index\": 0, \"approved\": true, \"reason\": \"\"},\n    {\"index\": 1, \"approved\": false, \"reason\": \"leverage 10x exceeds max 5x\"}\n  ],\n  \"force_actions\": []\n}\n```"
	complianceJSONStructZH = "```json\n{\n  \"approved\": true,\n  \"reason\": \"简要总结\",\n  \"violations\": [],\n  \"decisions_audit\": [\n    {\"index\": 0, \"approved\": true, \"reason\": \"\"},\n    {\"index\": 1, \"approved\": false, \"reason\": \"杠杆10x超过最大5x\"}\n  ],\n  \"force_actions\": []\n}\n```"
	complianceJSONExampleEN = "```json\n{\"approved\":true,\"reason\":\"One approved, one rejected.\",\"violations\":[],\"decisions_audit\":[{\"index\":0,\"approved\":true,\"reason\":\"\"},{\"index\":1,\"approved\":false,\"reason\":\"excluded coin\"}],\"force_actions\":[]}\n```"
	complianceJSONExampleZH = "```json\n{\"approved\":true,\"reason\":\"一条通过一条驳回。\",\"violations\":[],\"decisions_audit\":[{\"index\":0,\"approved\":true,\"reason\":\"\"},{\"index\":1,\"approved\":false,\"reason\":\"排除币种\"}],\"force_actions\":[]}\n```"
)

func getComplianceSystemPrompt(lang string) string {
	base := complianceSystemPromptEN
	structBlock := complianceJSONStructEN
	exampleBlock := complianceJSONExampleEN
	if lang == "zh" || lang == "zh-CN" {
		base = complianceSystemPromptZH
		structBlock = complianceJSONStructZH
		exampleBlock = complianceJSONExampleZH
	}
	base = strings.ReplaceAll(base, "{{JSON_STRUCT}}", structBlock)
	base = strings.ReplaceAll(base, "{{JSON_EXAMPLE}}", exampleBlock)
	return base
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
		// 双重保障：若某条因「置信度」被驳回但实际 confidence >= min_confidence，按实际结果改为通过并执行
		fixConfidenceReasonContradiction(input, out)
		// 修正后可能由驳回变通过，需重算批级 approved
		anyApproved = false
		for i := range out.DecisionsAudit {
			if out.DecisionsAudit[i].Approved {
				anyApproved = true
				break
			}
		}
		if anyApproved {
			out.Approved = true
		}
	}
	return out, nil
}

// fixConfidenceReasonContradiction 当驳回原因提到置信度但实际 confidence >= min_confidence 时，按实际结果将该条改为通过（approved=true），以便执行
func fixConfidenceReasonContradiction(input *ComplianceInput, out *ComplianceOutput) {
	if input == nil || out == nil || len(out.DecisionsAudit) == 0 || input.Rules.MinConfidence <= 0 {
		return
	}
	for i := range out.DecisionsAudit {
		if out.DecisionsAudit[i].Approved {
			continue
		}
		if i >= len(input.Decisions) {
			continue
		}
		d := &input.Decisions[i]
		if d.Confidence < input.Rules.MinConfidence {
			continue
		}
		r := strings.ToLower(out.DecisionsAudit[i].Reason)
		if strings.Contains(r, "confidence") || strings.Contains(r, "min_confidence") || strings.Contains(r, "置信度") {
			out.DecisionsAudit[i].Approved = true
			out.DecisionsAudit[i].Reason = ""
			logger.Infof("[Compliance] Override to approved for decision %d: confidence=%d >= min_confidence=%d (was wrongly rejected)", i, d.Confidence, input.Rules.MinConfidence)
		}
	}
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
