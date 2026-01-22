package nofxos

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// OIPosition represents open interest data for a single coin
type OIPosition struct {
	Symbol            string  `json:"symbol"`
	Rank              int     `json:"rank"`
	Price             float64 `json:"price"`
	CurrentOI         float64 `json:"current_oi"`
	OIDelta           float64 `json:"oi_delta"`
	OIDeltaPercent    float64 `json:"oi_delta_percent"`    // Already x100 (5.0 = 5%)
	OIDeltaValue      float64 `json:"oi_delta_value"`      // USDT value
	PriceDeltaPercent float64 `json:"price_delta_percent"` // Already x100 (5.0 = 5%)
	NetLong           float64 `json:"net_long"`
	NetShort          float64 `json:"net_short"`
}

// OIRankingResponse is the API response structure for OI ranking
type OIRankingResponse struct {
	Success bool `json:"success"`
	Code    int  `json:"code"`
	Data    struct {
		Positions      []OIPosition `json:"positions"`
		Count          int          `json:"count"`
		Exchange       string       `json:"exchange"`
		TimeRange      string       `json:"time_range"`
		TimeRangeParam string       `json:"time_range_param"`
		RankType       string       `json:"rank_type"`
		Limit          int          `json:"limit"`
	} `json:"data"`
}

// OIRankingData contains both top and low OI rankings
type OIRankingData struct {
	TimeRange    string       `json:"time_range"`
	Duration     string       `json:"duration"`
	TopPositions []OIPosition `json:"top_positions"`
	LowPositions []OIPosition `json:"low_positions"`
	FetchedAt    time.Time    `json:"fetched_at"`
}

// GetOIRanking retrieves OI ranking data (both top increase and low decrease)
func (c *Client) GetOIRanking(duration string, limit int) (*OIRankingData, error) {
	if duration == "" {
		duration = "1h"
	}
	if limit <= 0 {
		limit = 20
	}

	result := &OIRankingData{
		Duration:  duration,
		FetchedAt: time.Now(),
	}

	// Fetch top ranking (OI increase)
	topPositions, timeRange, err := c.fetchOIRanking("top", duration, limit)
	if err != nil {
		log.Printf("⚠️  Failed to fetch OI top ranking: %v", err)
	} else {
		result.TopPositions = topPositions
		result.TimeRange = timeRange
	}

	// Fetch low ranking (OI decrease)
	lowPositions, _, err := c.fetchOIRanking("low", duration, limit)
	if err != nil {
		log.Printf("⚠️  Failed to fetch OI low ranking: %v", err)
	} else {
		result.LowPositions = lowPositions
	}

	log.Printf("✓ Fetched OI ranking data: %d top, %d low (duration: %s)",
		len(result.TopPositions), len(result.LowPositions), duration)

	return result, nil
}

func (c *Client) fetchOIRanking(rankType, duration string, limit int) ([]OIPosition, string, error) {
	endpoint := fmt.Sprintf("/api/oi/%s-ranking?limit=%d&duration=%s", rankType, limit, duration)

	body, err := c.doRequest(endpoint)
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}

	var response OIRankingResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, "", fmt.Errorf("JSON parsing failed: %w", err)
	}

	// Check for success (support both success field and code field)
	if !response.Success && response.Code != 0 {
		return nil, "", fmt.Errorf("API returned error code: %d", response.Code)
	}

	return response.Data.Positions, response.Data.TimeRange, nil
}

// GetOITopPositions retrieves top OI increase positions (legacy compatibility)
func (c *Client) GetOITopPositions() ([]OIPosition, error) {
	data, err := c.GetOIRanking("1h", 20)
	if err != nil {
		return nil, err
	}
	return data.TopPositions, nil
}

// GetOITopSymbols retrieves OI top coin symbol list
func (c *Client) GetOITopSymbols() ([]string, error) {
	positions, err := c.GetOITopPositions()
	if err != nil {
		return nil, err
	}

	var symbols []string
	for _, pos := range positions {
		symbol := NormalizeSymbol(pos.Symbol)
		symbols = append(symbols, symbol)
	}

	return symbols, nil
}

// FormatOIRankingForAI formats OI ranking data for AI consumption
func FormatOIRankingForAI(data *OIRankingData, lang Language) string {
	if data == nil {
		return ""
	}

	if lang == LangChinese {
		return formatOIRankingZH(data)
	}
	return formatOIRankingEN(data)
}

func formatOIRankingZH(data *OIRankingData) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## OI排行 (%s)\n", data.Duration))

	// OI增加榜 - 紧凑格式
	if len(data.TopPositions) > 0 {
		sb.WriteString("▲ ")
		for i, pos := range data.TopPositions {
			if i > 0 {
				sb.WriteString(", ")
			}
			signal := getOISignal(pos.OIDeltaPercent, pos.PriceDeltaPercent)
			sb.WriteString(fmt.Sprintf("%s(%s,%+.1f%%,P%+.1f%%)[%s]",
				strings.TrimSuffix(pos.Symbol, "USDT"),
				formatValue(pos.OIDeltaValue),
				pos.OIDeltaPercent,
				pos.PriceDeltaPercent,
				signal))
		}
		sb.WriteString("\n")
	}

	// OI减少榜 - 紧凑格式
	if len(data.LowPositions) > 0 {
		sb.WriteString("▼ ")
		for i, pos := range data.LowPositions {
			if i > 0 {
				sb.WriteString(", ")
			}
			signal := getOISignal(pos.OIDeltaPercent, pos.PriceDeltaPercent)
			sb.WriteString(fmt.Sprintf("%s(%s,%+.1f%%,P%+.1f%%)[%s]",
				strings.TrimSuffix(pos.Symbol, "USDT"),
				formatValue(pos.OIDeltaValue),
				pos.OIDeltaPercent,
				pos.PriceDeltaPercent,
				signal))
		}
		sb.WriteString("\n")
	}

	// 市场综合判断
	marketSignal := getMarketOISignal(data.TopPositions, data.LowPositions)
	sb.WriteString(fmt.Sprintf("信号: %s\n\n", marketSignal))

	return sb.String()
}

// getOISignal 根据OI变化和价格变化判断信号
func getOISignal(oiDelta, priceDelta float64) string {
	if oiDelta > 0 && priceDelta > 0 {
		return "多头建仓"
	} else if oiDelta > 0 && priceDelta < 0 {
		return "空头建仓"
	} else if oiDelta < 0 && priceDelta > 0 {
		return "空头回补"
	} else if oiDelta < 0 && priceDelta < 0 {
		return "多头平仓"
	}
	return "观望"
}

// getMarketOISignal 综合判断市场OI信号
func getMarketOISignal(topPositions, lowPositions []OIPosition) string {
	longBuild, shortBuild := 0, 0
	for _, pos := range topPositions {
		if pos.PriceDeltaPercent > 0 {
			longBuild++
		} else {
			shortBuild++
		}
	}

	if longBuild > shortBuild && longBuild >= 3 {
		return "多头主导 (多数OI↑伴随价格↑)"
	} else if shortBuild > longBuild && shortBuild >= 3 {
		return "空头主导 (多数OI↑伴随价格↓)"
	}
	return "多空均衡"
}

func formatOIRankingEN(data *OIRankingData) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## OI Ranking (%s)\n", data.Duration))

	// OI Increase - compact format
	if len(data.TopPositions) > 0 {
		sb.WriteString("▲ ")
		for i, pos := range data.TopPositions {
			if i > 0 {
				sb.WriteString(", ")
			}
			signal := getOISignalEN(pos.OIDeltaPercent, pos.PriceDeltaPercent)
			sb.WriteString(fmt.Sprintf("%s(%s,%+.1f%%,P%+.1f%%)[%s]",
				strings.TrimSuffix(pos.Symbol, "USDT"),
				formatValue(pos.OIDeltaValue),
				pos.OIDeltaPercent,
				pos.PriceDeltaPercent,
				signal))
		}
		sb.WriteString("\n")
	}

	// OI Decrease - compact format
	if len(data.LowPositions) > 0 {
		sb.WriteString("▼ ")
		for i, pos := range data.LowPositions {
			if i > 0 {
				sb.WriteString(", ")
			}
			signal := getOISignalEN(pos.OIDeltaPercent, pos.PriceDeltaPercent)
			sb.WriteString(fmt.Sprintf("%s(%s,%+.1f%%,P%+.1f%%)[%s]",
				strings.TrimSuffix(pos.Symbol, "USDT"),
				formatValue(pos.OIDeltaValue),
				pos.OIDeltaPercent,
				pos.PriceDeltaPercent,
				signal))
		}
		sb.WriteString("\n")
	}

	// Market signal summary
	marketSignal := getMarketOISignalEN(data.TopPositions, data.LowPositions)
	sb.WriteString(fmt.Sprintf("Signal: %s\n\n", marketSignal))

	return sb.String()
}

// getOISignalEN returns OI signal in English
func getOISignalEN(oiDelta, priceDelta float64) string {
	if oiDelta > 0 && priceDelta > 0 {
		return "LONG_BUILD"
	} else if oiDelta > 0 && priceDelta < 0 {
		return "SHORT_BUILD"
	} else if oiDelta < 0 && priceDelta > 0 {
		return "SHORT_COV"
	} else if oiDelta < 0 && priceDelta < 0 {
		return "LONG_LIQ"
	}
	return "NEUTRAL"
}

// getMarketOISignalEN returns market OI signal summary in English
func getMarketOISignalEN(topPositions, lowPositions []OIPosition) string {
	longBuild, shortBuild := 0, 0
	for _, pos := range topPositions {
		if pos.PriceDeltaPercent > 0 {
			longBuild++
		} else {
			shortBuild++
		}
	}

	if longBuild > shortBuild && longBuild >= 3 {
		return "LONG_DOMINANT (most OI↑ with price↑)"
	} else if shortBuild > longBuild && shortBuild >= 3 {
		return "SHORT_DOMINANT (most OI↑ with price↓)"
	}
	return "BALANCED"
}
