package nofxos

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// NetFlowPosition represents fund flow data for a single coin
type NetFlowPosition struct {
	Rank   int     `json:"rank"`
	Symbol string  `json:"symbol"`
	Amount float64 `json:"amount"` // Fund flow amount in USDT (positive=inflow, negative=outflow)
	Price  float64 `json:"price"`
}

// NetFlowResponse is the API response structure
type NetFlowResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Netflows  []NetFlowPosition `json:"netflows"`
		Count     int               `json:"count"`
		Type      string            `json:"type"`      // institution or personal
		Trade     string            `json:"trade"`     // 合约 or 现货
		TimeRange string            `json:"time_range"`
		RankType  string            `json:"rank_type"` // top or low
		Limit     int               `json:"limit"`
	} `json:"data"`
}

// NetFlowRankingData contains institution and personal fund flow rankings
type NetFlowRankingData struct {
	Duration             string            `json:"duration"`
	TimeRange            string            `json:"time_range"`
	InstitutionFutureTop []NetFlowPosition `json:"institution_future_top"`
	InstitutionFutureLow []NetFlowPosition `json:"institution_future_low"`
	PersonalFutureTop    []NetFlowPosition `json:"personal_future_top"`
	PersonalFutureLow    []NetFlowPosition `json:"personal_future_low"`
	FetchedAt            time.Time         `json:"fetched_at"`
}

// GetNetFlowRanking retrieves NetFlow ranking data (institution/personal, top/low)
func (c *Client) GetNetFlowRanking(duration string, limit int) (*NetFlowRankingData, error) {
	if duration == "" {
		duration = "1h"
	}
	if limit <= 0 {
		limit = 10
	}

	result := &NetFlowRankingData{
		Duration:  duration,
		FetchedAt: time.Now(),
	}

	// Fetch institution futures top (inflow)
	positions, timeRange, err := c.fetchNetFlowRanking("top", duration, limit, "institution", "future")
	if err != nil {
		log.Printf("⚠️  Failed to fetch institution future inflow ranking: %v", err)
	} else {
		result.InstitutionFutureTop = positions
		result.TimeRange = timeRange
	}

	// Fetch institution futures low (outflow)
	positions, _, err = c.fetchNetFlowRanking("low", duration, limit, "institution", "future")
	if err != nil {
		log.Printf("⚠️  Failed to fetch institution future outflow ranking: %v", err)
	} else {
		result.InstitutionFutureLow = positions
	}

	// Fetch personal futures top (retail inflow)
	positions, _, err = c.fetchNetFlowRanking("top", duration, limit, "personal", "future")
	if err != nil {
		log.Printf("⚠️  Failed to fetch personal future inflow ranking: %v", err)
	} else {
		result.PersonalFutureTop = positions
	}

	// Fetch personal futures low (retail outflow)
	positions, _, err = c.fetchNetFlowRanking("low", duration, limit, "personal", "future")
	if err != nil {
		log.Printf("⚠️  Failed to fetch personal future outflow ranking: %v", err)
	} else {
		result.PersonalFutureLow = positions
	}

	log.Printf("✓ Fetched NetFlow ranking data: inst_in=%d, inst_out=%d, retail_in=%d, retail_out=%d (duration: %s)",
		len(result.InstitutionFutureTop), len(result.InstitutionFutureLow),
		len(result.PersonalFutureTop), len(result.PersonalFutureLow), duration)

	return result, nil
}

func (c *Client) fetchNetFlowRanking(rankType, duration string, limit int, flowType, trade string) ([]NetFlowPosition, string, error) {
	endpoint := fmt.Sprintf("/api/netflow/%s-ranking?limit=%d&duration=%s&type=%s&trade=%s",
		rankType, limit, duration, flowType, trade)

	body, err := c.doRequest(endpoint)
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}

	var response NetFlowResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, "", fmt.Errorf("JSON parsing failed: %w", err)
	}

	if !response.Success {
		return nil, "", fmt.Errorf("API returned failure status")
	}

	return response.Data.Netflows, response.Data.TimeRange, nil
}

// FormatNetFlowRankingForAI formats NetFlow ranking data for AI consumption
func FormatNetFlowRankingForAI(data *NetFlowRankingData, lang Language) string {
	if data == nil {
		return ""
	}

	if lang == LangChinese {
		return formatNetFlowRankingZH(data)
	}
	return formatNetFlowRankingEN(data)
}

func formatNetFlowRankingZH(data *NetFlowRankingData) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## 资金流排行 (%s)\n", data.Duration))

	// 机构流入 - 紧凑格式
	if len(data.InstitutionFutureTop) > 0 {
		sb.WriteString("机构▲: ")
		for i, pos := range data.InstitutionFutureTop {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%s(%s)",
				strings.TrimSuffix(pos.Symbol, "USDT"),
				formatValue(pos.Amount)))
		}
		sb.WriteString("\n")
	}

	// 机构流出 - 紧凑格式
	if len(data.InstitutionFutureLow) > 0 {
		sb.WriteString("机构▼: ")
		for i, pos := range data.InstitutionFutureLow {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%s(%s)",
				strings.TrimSuffix(pos.Symbol, "USDT"),
				formatValue(pos.Amount)))
		}
		sb.WriteString("\n")
	}

	// 散户流入 - 紧凑格式
	if len(data.PersonalFutureTop) > 0 {
		sb.WriteString("散户▲: ")
		for i, pos := range data.PersonalFutureTop {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%s(%s)",
				strings.TrimSuffix(pos.Symbol, "USDT"),
				formatValue(pos.Amount)))
		}
		sb.WriteString("\n")
	}

	// 散户流出 - 紧凑格式
	if len(data.PersonalFutureLow) > 0 {
		sb.WriteString("散户▼: ")
		for i, pos := range data.PersonalFutureLow {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%s(%s)",
				strings.TrimSuffix(pos.Symbol, "USDT"),
				formatValue(pos.Amount)))
		}
		sb.WriteString("\n")
	}

	// 综合信号判断
	signal := getFlowSignalZH(data)
	sb.WriteString(fmt.Sprintf("信号: %s\n\n", signal))

	return sb.String()
}

// getFlowSignalZH 综合判断资金流信号（中文）
func getFlowSignalZH(data *NetFlowRankingData) string {
	var signals []string

	// 检查机构和散户对同一币种的相反操作
	instInSymbols := make(map[string]bool)
	for _, pos := range data.InstitutionFutureTop {
		instInSymbols[pos.Symbol] = true
	}

	retailOutSymbols := make(map[string]bool)
	for _, pos := range data.PersonalFutureLow {
		retailOutSymbols[pos.Symbol] = true
	}

	// 机构买+散户卖 = 强看多
	var smartMoneyBuy []string
	for symbol := range instInSymbols {
		if retailOutSymbols[symbol] {
			smartMoneyBuy = append(smartMoneyBuy, strings.TrimSuffix(symbol, "USDT"))
		}
	}
	if len(smartMoneyBuy) > 0 {
		signals = append(signals, fmt.Sprintf("%s [机构买+散户卖=强看多]", strings.Join(smartMoneyBuy, "/")))
	}

	// 机构卖+散户买 = 强看空
	instOutSymbols := make(map[string]bool)
	for _, pos := range data.InstitutionFutureLow {
		instOutSymbols[pos.Symbol] = true
	}

	retailInSymbols := make(map[string]bool)
	for _, pos := range data.PersonalFutureTop {
		retailInSymbols[pos.Symbol] = true
	}

	var distributionWarning []string
	for symbol := range instOutSymbols {
		if retailInSymbols[symbol] {
			distributionWarning = append(distributionWarning, strings.TrimSuffix(symbol, "USDT"))
		}
	}
	if len(distributionWarning) > 0 {
		signals = append(signals, fmt.Sprintf("%s [机构卖+散户买=派发警告]", strings.Join(distributionWarning, "/")))
	}

	if len(signals) == 0 {
		return "无明显异常信号"
	}
	return strings.Join(signals, " | ")
}

func formatNetFlowRankingEN(data *NetFlowRankingData) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Flow Ranking (%s)\n", data.Duration))

	// Institution inflow - compact format
	if len(data.InstitutionFutureTop) > 0 {
		sb.WriteString("Inst▲: ")
		for i, pos := range data.InstitutionFutureTop {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%s(%s)",
				strings.TrimSuffix(pos.Symbol, "USDT"),
				formatValue(pos.Amount)))
		}
		sb.WriteString("\n")
	}

	// Institution outflow - compact format
	if len(data.InstitutionFutureLow) > 0 {
		sb.WriteString("Inst▼: ")
		for i, pos := range data.InstitutionFutureLow {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%s(%s)",
				strings.TrimSuffix(pos.Symbol, "USDT"),
				formatValue(pos.Amount)))
		}
		sb.WriteString("\n")
	}

	// Retail inflow - compact format
	if len(data.PersonalFutureTop) > 0 {
		sb.WriteString("Retail▲: ")
		for i, pos := range data.PersonalFutureTop {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%s(%s)",
				strings.TrimSuffix(pos.Symbol, "USDT"),
				formatValue(pos.Amount)))
		}
		sb.WriteString("\n")
	}

	// Retail outflow - compact format
	if len(data.PersonalFutureLow) > 0 {
		sb.WriteString("Retail▼: ")
		for i, pos := range data.PersonalFutureLow {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%s(%s)",
				strings.TrimSuffix(pos.Symbol, "USDT"),
				formatValue(pos.Amount)))
		}
		sb.WriteString("\n")
	}

	// Signal summary
	signal := getFlowSignalEN(data)
	sb.WriteString(fmt.Sprintf("Signal: %s\n\n", signal))

	return sb.String()
}

// getFlowSignalEN returns flow signal summary in English
func getFlowSignalEN(data *NetFlowRankingData) string {
	var signals []string

	// Check for smart money accumulation (inst buy + retail sell)
	instInSymbols := make(map[string]bool)
	for _, pos := range data.InstitutionFutureTop {
		instInSymbols[pos.Symbol] = true
	}

	retailOutSymbols := make(map[string]bool)
	for _, pos := range data.PersonalFutureLow {
		retailOutSymbols[pos.Symbol] = true
	}

	var smartMoneyBuy []string
	for symbol := range instInSymbols {
		if retailOutSymbols[symbol] {
			smartMoneyBuy = append(smartMoneyBuy, strings.TrimSuffix(symbol, "USDT"))
		}
	}
	if len(smartMoneyBuy) > 0 {
		signals = append(signals, fmt.Sprintf("%s [SMART_MONEY_ACCUMULATION]", strings.Join(smartMoneyBuy, "/")))
	}

	// Check for distribution (inst sell + retail buy)
	instOutSymbols := make(map[string]bool)
	for _, pos := range data.InstitutionFutureLow {
		instOutSymbols[pos.Symbol] = true
	}

	retailInSymbols := make(map[string]bool)
	for _, pos := range data.PersonalFutureTop {
		retailInSymbols[pos.Symbol] = true
	}

	var distributionWarning []string
	for symbol := range instOutSymbols {
		if retailInSymbols[symbol] {
			distributionWarning = append(distributionWarning, strings.TrimSuffix(symbol, "USDT"))
		}
	}
	if len(distributionWarning) > 0 {
		signals = append(signals, fmt.Sprintf("%s [DISTRIBUTION_WARNING]", strings.Join(distributionWarning, "/")))
	}

	if len(signals) == 0 {
		return "NO_SIGNIFICANT_DIVERGENCE"
	}
	return strings.Join(signals, " | ")
}
