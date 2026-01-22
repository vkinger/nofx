package nofxos

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// PriceRankingItem represents single coin price ranking data
type PriceRankingItem struct {
	Pair         string  `json:"pair"`
	Symbol       string  `json:"symbol"`
	PriceDelta   float64 `json:"price_delta"` // Decimal format: 0.0723 = 7.23%
	Price        float64 `json:"price"`
	FutureFlow   float64 `json:"future_flow"`
	SpotFlow     float64 `json:"spot_flow"`
	OI           float64 `json:"oi"`
	OIDelta      float64 `json:"oi_delta"`
	OIDeltaValue float64 `json:"oi_delta_value"`
}

// PriceRankingDuration contains top gainers and losers for a single duration
type PriceRankingDuration struct {
	Top []PriceRankingItem `json:"top"`
	Low []PriceRankingItem `json:"low"`
}

// PriceRankingResponse is the API response structure
type PriceRankingResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Durations []string                        `json:"durations"`
		Limit     int                             `json:"limit"`
		Data      map[string]PriceRankingDuration `json:"data"`
	} `json:"data"`
}

// PriceRankingData contains price ranking data for multiple durations
type PriceRankingData struct {
	Durations map[string]*PriceRankingDuration `json:"durations"`
	FetchedAt time.Time                        `json:"fetched_at"`
}

// GetPriceRanking retrieves price ranking data (gainers/losers)
func (c *Client) GetPriceRanking(durations string, limit int) (*PriceRankingData, error) {
	if durations == "" {
		durations = "1h"
	}
	if limit <= 0 {
		limit = 10
	}

	endpoint := fmt.Sprintf("/api/price/ranking?duration=%s&limit=%d", durations, limit)

	body, err := c.doRequest(endpoint)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	var response PriceRankingResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("JSON parsing failed: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("API returned failure status")
	}

	result := &PriceRankingData{
		Durations: make(map[string]*PriceRankingDuration),
		FetchedAt: time.Now(),
	}

	for duration, data := range response.Data.Data {
		d := data // Create a copy to avoid pointer issues
		result.Durations[duration] = &d
	}

	log.Printf("✓ Fetched Price ranking data for %d durations", len(result.Durations))

	return result, nil
}

// FormatPriceRankingForAI formats Price ranking data for AI consumption
func FormatPriceRankingForAI(data *PriceRankingData, lang Language) string {
	if data == nil || len(data.Durations) == 0 {
		return ""
	}

	if lang == LangChinese {
		return formatPriceRankingZH(data)
	}
	return formatPriceRankingEN(data)
}

func formatPriceRankingZH(data *PriceRankingData) string {
	var sb strings.Builder

	sb.WriteString("## 涨跌幅排行\n")

	// 只显示1h和24h，4h省略以节省token
	durationOrder := []string{"1h", "24h"}
	for _, duration := range durationOrder {
		durationData, exists := data.Durations[duration]
		if !exists || durationData == nil {
			continue
		}

		// 涨幅榜 - 紧凑格式
		if len(durationData.Top) > 0 {
			sb.WriteString(fmt.Sprintf("%s▲: ", duration))
			for i, item := range durationData.Top {
				if i > 0 {
					sb.WriteString(", ")
				}
				signal := getPriceSignalZH(item.PriceDelta, item.OIDeltaValue, item.FutureFlow)
				sb.WriteString(fmt.Sprintf("%s(%+.1f%%,OI%s,F%s)[%s]",
					strings.TrimSuffix(item.Symbol, "USDT"),
					item.PriceDelta*100,
					formatValue(item.OIDeltaValue),
					formatValue(item.FutureFlow),
					signal))
			}
			sb.WriteString("\n")
		}

		// 跌幅榜 - 紧凑格式
		if len(durationData.Low) > 0 {
			sb.WriteString(fmt.Sprintf("%s▼: ", duration))
			for i, item := range durationData.Low {
				if i > 0 {
					sb.WriteString(", ")
				}
				signal := getPriceSignalZH(item.PriceDelta, item.OIDeltaValue, item.FutureFlow)
				sb.WriteString(fmt.Sprintf("%s(%.1f%%,OI%s,F%s)[%s]",
					strings.TrimSuffix(item.Symbol, "USDT"),
					item.PriceDelta*100,
					formatValue(item.OIDeltaValue),
					formatValue(item.FutureFlow),
					signal))
			}
			sb.WriteString("\n")
		}
	}

	// 动量总结
	momentum := getPriceMomentumZH(data)
	sb.WriteString(fmt.Sprintf("动量: %s\n\n", momentum))

	return sb.String()
}

// getPriceSignalZH 根据涨跌幅、OI变化和资金流判断信号（中文）
func getPriceSignalZH(priceDelta, oiDelta, flowDelta float64) string {
	isUp := priceDelta > 0
	oiPositive := oiDelta > 0
	flowPositive := flowDelta > 0

	if isUp {
		if oiPositive && flowPositive {
			return "强势"
		} else if oiPositive || flowPositive {
			return "健康"
		} else if !oiPositive && !flowPositive {
			return "轧空"
		}
		return "上涨"
	} else {
		if !oiPositive && !flowPositive {
			return "弱势"
		} else if oiPositive && flowPositive {
			return "空头建仓"
		} else if !oiPositive {
			return "多头止损"
		}
		return "下跌"
	}
}

// getPriceMomentumZH 获取整体动量摘要（中文）
func getPriceMomentumZH(data *PriceRankingData) string {
	var strongUp, strongDown []string

	// 检查24h数据
	if d24h, ok := data.Durations["24h"]; ok && d24h != nil {
		for _, item := range d24h.Top {
			if item.PriceDelta > 0.08 && item.OIDeltaValue > 0 && item.FutureFlow > 0 {
				strongUp = append(strongUp, strings.TrimSuffix(item.Symbol, "USDT"))
			}
		}
		for _, item := range d24h.Low {
			if item.PriceDelta < -0.08 && item.OIDeltaValue < 0 && item.FutureFlow < 0 {
				strongDown = append(strongDown, strings.TrimSuffix(item.Symbol, "USDT"))
			}
		}
	}

	var parts []string
	if len(strongUp) > 0 {
		parts = append(parts, fmt.Sprintf("%s[强势上涨]", strings.Join(strongUp, "/")))
	}
	if len(strongDown) > 0 {
		parts = append(parts, fmt.Sprintf("%s[弱势下跌]", strings.Join(strongDown, "/")))
	}

	if len(parts) == 0 {
		return "无明显趋势"
	}
	return strings.Join(parts, " | ")
}

func formatPriceRankingEN(data *PriceRankingData) string {
	var sb strings.Builder

	sb.WriteString("## Price Ranking\n")

	// Only show 1h and 24h, skip 4h to save tokens
	durationOrder := []string{"1h", "24h"}
	for _, duration := range durationOrder {
		durationData, exists := data.Durations[duration]
		if !exists || durationData == nil {
			continue
		}

		// Top gainers - compact format
		if len(durationData.Top) > 0 {
			sb.WriteString(fmt.Sprintf("%s▲: ", duration))
			for i, item := range durationData.Top {
				if i > 0 {
					sb.WriteString(", ")
				}
				signal := getPriceSignalEN(item.PriceDelta, item.OIDeltaValue, item.FutureFlow)
				sb.WriteString(fmt.Sprintf("%s(%+.1f%%,OI%s,F%s)[%s]",
					strings.TrimSuffix(item.Symbol, "USDT"),
					item.PriceDelta*100,
					formatValue(item.OIDeltaValue),
					formatValue(item.FutureFlow),
					signal))
			}
			sb.WriteString("\n")
		}

		// Top losers - compact format
		if len(durationData.Low) > 0 {
			sb.WriteString(fmt.Sprintf("%s▼: ", duration))
			for i, item := range durationData.Low {
				if i > 0 {
					sb.WriteString(", ")
				}
				signal := getPriceSignalEN(item.PriceDelta, item.OIDeltaValue, item.FutureFlow)
				sb.WriteString(fmt.Sprintf("%s(%.1f%%,OI%s,F%s)[%s]",
					strings.TrimSuffix(item.Symbol, "USDT"),
					item.PriceDelta*100,
					formatValue(item.OIDeltaValue),
					formatValue(item.FutureFlow),
					signal))
			}
			sb.WriteString("\n")
		}
	}

	// Momentum summary
	momentum := getPriceMomentumEN(data)
	sb.WriteString(fmt.Sprintf("Momentum: %s\n\n", momentum))

	return sb.String()
}

// getPriceSignalEN returns price signal based on price/OI/flow changes (English)
func getPriceSignalEN(priceDelta, oiDelta, flowDelta float64) string {
	isUp := priceDelta > 0
	oiPositive := oiDelta > 0
	flowPositive := flowDelta > 0

	if isUp {
		if oiPositive && flowPositive {
			return "STRONG"
		} else if oiPositive || flowPositive {
			return "HEALTHY"
		} else if !oiPositive && !flowPositive {
			return "SQUEEZE"
		}
		return "UP"
	} else {
		if !oiPositive && !flowPositive {
			return "WEAK"
		} else if oiPositive && flowPositive {
			return "SHORT_BUILD"
		} else if !oiPositive {
			return "LONG_LIQ"
		}
		return "DOWN"
	}
}

// getPriceMomentumEN returns overall momentum summary (English)
func getPriceMomentumEN(data *PriceRankingData) string {
	var strongUp, strongDown []string

	// Check 24h data
	if d24h, ok := data.Durations["24h"]; ok && d24h != nil {
		for _, item := range d24h.Top {
			if item.PriceDelta > 0.08 && item.OIDeltaValue > 0 && item.FutureFlow > 0 {
				strongUp = append(strongUp, strings.TrimSuffix(item.Symbol, "USDT"))
			}
		}
		for _, item := range d24h.Low {
			if item.PriceDelta < -0.08 && item.OIDeltaValue < 0 && item.FutureFlow < 0 {
				strongDown = append(strongDown, strings.TrimSuffix(item.Symbol, "USDT"))
			}
		}
	}

	var parts []string
	if len(strongUp) > 0 {
		parts = append(parts, fmt.Sprintf("%s[STRONG_TREND]", strings.Join(strongUp, "/")))
	}
	if len(strongDown) > 0 {
		parts = append(parts, fmt.Sprintf("%s[WEAK_TREND]", strings.Join(strongDown, "/")))
	}

	if len(parts) == 0 {
		return "NO_CLEAR_TREND"
	}
	return strings.Join(parts, " | ")
}
