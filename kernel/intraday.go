package kernel

import (
	"fmt"
	"math"
	"nofx/market"
	"strings"
)

// 分时斜率阈值（按根数）：大于此值视为陡峭（45度角拉升），可依品种调整
const IntradaySlopeThresholdUp = 0.001
const IntradaySlopeThresholdDown = -0.001

// 分时斜率阈值（按分钟）：%/分钟，用于跨周期可比
const IntradaySlopeThresholdUpPerMin = 0.0002
const IntradaySlopeThresholdDownPerMin = -0.0002

// ATR 归一化斜率阈值：SlopeNorm = SlopePerBar * Price / ATR，> 1 表示斜率超过 1 倍波动率/根
const IntradaySlopeNormThresholdUp = 0.5
const IntradaySlopeNormThresholdDown = -0.5

// 量能“近似不变”的容差（VM 相对前一根的比例）
const IntradayVolumeFlatTolerance = 0.1

// 量价匹配度 code（与字典一致，用于双语输出）
const (
	MatchCodeAligned    = "aligned"
	MatchCodeDivergence = "divergence"
	MatchCodeVolumeDrop = "volume_drop"
	MatchCodePanic      = "panic"
	MatchCodeShrinkDown = "shrink_down"
)

// IntradaySummary 分时摘要，供观察员/AI 使用（斜率、动量、量价匹配度）
//
// 斜率采用「多套共存、单一决策」：
// - 展示：按根数 / 按分钟 / ATR 归一化 三者都输出（当前链路中三者均有），便于跨周期、多币种观察。
// - 决策（StrongBuy 等）：只用 CanonicalSlopeSignal。实际计算中按根数、时间、ATR 均存在，故最终优先总是按 ATR（有 ATR 时用 SlopeSignalNorm）；无 ATR 时再按 按分钟 > 按根数 回退。
type IntradaySummary struct {
	Slope             float64 // 按根数：每根 K 的平均相对变化率（保留兼容）
	SlopePerBar       float64 // 同 Slope，显式命名
	SlopePerMin       float64 // 按分钟：每分钟相对变化率（有时间戳时有效，否则 0）
	SlopeNorm         float64 // ATR 归一化斜率：SlopePerBar*Price/ATR（ATR>0 时有效，否则 0）
	SlopeSignal       string  // 按根数判定的信号 "steep_up" | "flat" | "steep_down"
	SlopeSignalPerMin string  // 按分钟判定的信号（有 SlopePerMin 时有效）
	SlopeSignalNorm   string  // 按 ATR 归一化判定的信号（有 SlopeNorm 时有效）
	CanonicalSlopeSignal string // 决策用单一信号：有 ATR 时始终用 SlopeSignalNorm；否则 PerMin > PerBar
	PriceMomentum      float64 // PM: 当前收 - 前收
	VolumeMomentum     float64 // VM: 当前量 - 前量
	MatchType          string  // 量价匹配度 code: aligned|divergence|volume_drop|panic|shrink_down|""
	Conclusion         string  // 一句结论（中文，兼容；展示时按 lang 由 buildIntradayConclusion 生成）
	StrongBuy          bool    // 是否触发强力买入（用 CanonicalSlopeSignal 判陡峭 + 放量）
	StrongSell         bool    // 是否触发强力卖出（陡峭下行 + 放量或主动卖盘主导）
	AvgTakerBuyRatio   float64 // 窗口内 Taker Buy Ratio 均值，>0.6 偏多 <0.4 偏空
}

// ComputeIntradaySummary 基于短周期 K 线计算分时斜率、动量与量价匹配度（参考量价交易宝典）
// klines 时间正序，lookback 使用最近 N 根；若 lookback<=0 则用全部（最多 30 根）
// timeframe 可选，非空且 K 线含 Time 时计算 SlopePerMin；atr 可选，>0 时计算 SlopeNorm（多币种可比）
func ComputeIntradaySummary(klines []market.KlineBar, lookback int, timeframe string, atr float64) (*IntradaySummary, bool) {
	if len(klines) < 2 {
		return nil, false
	}
	n := len(klines)
	if lookback <= 0 || lookback > n {
		lookback = n
	}
	if lookback > 30 {
		lookback = 30
	}
	start := n - lookback
	if start < 0 {
		start = 0
	}
	window := klines[start:n]
	m := len(window)
	if m < 2 {
		return nil, false
	}

	priceNow := window[m-1].Close
	priceAgo := window[0].Close
	if priceAgo <= 0 {
		priceAgo = priceNow
		if priceAgo <= 0 {
			return nil, false
		}
	}
	// 方案一：按根数 SlopePerBar = (P_now - P_ago) / P_ago / m
	slopePerBar := (priceNow - priceAgo) / priceAgo / float64(m)

	var slopeSignal string
	if slopePerBar > IntradaySlopeThresholdUp {
		slopeSignal = "steep_up"
	} else if slopePerBar < IntradaySlopeThresholdDown {
		slopeSignal = "steep_down"
	} else {
		slopeSignal = "flat"
	}

	// 方案二：按分钟 SlopePerMin（有时间戳时）
	var slopePerMin float64
	var slopeSignalPerMin string
	t0, t1 := window[0].Time, window[m-1].Time
	if t1 > t0 {
		elapsedMin := (t1 - t0) / 60000 // 毫秒 -> 分钟
		if elapsedMin < 1 {
			elapsedMin = 1
		}
		slopePerMin = (priceNow - priceAgo) / priceAgo / float64(elapsedMin)
		if slopePerMin > IntradaySlopeThresholdUpPerMin {
			slopeSignalPerMin = "steep_up"
		} else if slopePerMin < IntradaySlopeThresholdDownPerMin {
			slopeSignalPerMin = "steep_down"
		} else {
			slopeSignalPerMin = "flat"
		}
	}

	// ATR 归一化：SlopeNorm = SlopePerBar * Price / ATR（相对波动率倍数）
	var slopeNorm float64
	var slopeSignalNorm string
	if atr > 0 && priceNow > 0 {
		slopeNorm = slopePerBar * priceNow / atr
		if slopeNorm > IntradaySlopeNormThresholdUp {
			slopeSignalNorm = "steep_up"
		} else if slopeNorm < IntradaySlopeNormThresholdDown {
			slopeSignalNorm = "steep_down"
		} else {
			slopeSignalNorm = "flat"
		}
	}

	// 最近两根的动量
	prev := window[m-2]
	cur := window[m-1]
	pm := cur.Close - prev.Close
	vm := cur.Volume - prev.Volume
	if prev.Volume <= 0 {
		prev.Volume = 1
	}
	vmRatio := vm / prev.Volume // VM 相对前一根的比例

	// 量价匹配度（存 code，便于双语展示）
	matchType := ""
	if pm > 0 && vm > 0 {
		matchType = MatchCodeAligned
	} else if pm > 0 && vm < 0 {
		matchType = MatchCodeDivergence
	} else if pm < 0 && vm > 0 {
		matchType = MatchCodeVolumeDrop
	} else if pm < 0 && math.Abs(vmRatio) <= IntradayVolumeFlatTolerance {
		matchType = MatchCodePanic
	} else if pm < 0 && vm < 0 {
		matchType = MatchCodeShrinkDown
	}

	// 决策优先 ATR（当前链路恒有）；无 ATR 时回退：按分钟 > 按根数
	var canonicalSlopeSignal string
	if atr > 0 && priceNow > 0 {
		canonicalSlopeSignal = slopeSignalNorm
	} else if slopeSignalPerMin != "" {
		canonicalSlopeSignal = slopeSignalPerMin
	} else {
		canonicalSlopeSignal = slopeSignal
	}

	// 过去 lookback 根的平均量
	var volSum float64
	for _, k := range window {
		volSum += k.Volume
	}
	if volSum <= 0 {
		volSum = 1
	}
	avgVol := volSum / float64(m)
	strongBuy := canonicalSlopeSignal == "steep_up" && cur.Volume > avgVol

	// Taker Buy Ratio 窗口均值（多空区分）
	var tbSum float64
	var tbCount int
	for _, k := range window {
		if k.TakerBuyRatio > 0 {
			tbSum += k.TakerBuyRatio
			tbCount++
		}
	}
	avgTB := 0.0
	if tbCount > 0 {
		avgTB = tbSum / float64(tbCount)
	}
	strongSell := canonicalSlopeSignal == "steep_down" && (cur.Volume > avgVol || (avgTB > 0 && avgTB < 0.4))

	// 结论句（中文兼容；展示时由 FormatIntradaySummaryForPrompt 按 lang 再生成）
	conclusion := buildIntradayConclusion(canonicalSlopeSignal, matchType, strongBuy, strongSell, slopePerBar, pm, vm, true)

	return &IntradaySummary{
		Slope:                slopePerBar,
		SlopePerBar:          slopePerBar,
		SlopePerMin:          slopePerMin,
		SlopeNorm:            slopeNorm,
		SlopeSignal:          slopeSignal,
		SlopeSignalPerMin:    slopeSignalPerMin,
		SlopeSignalNorm:      slopeSignalNorm,
		CanonicalSlopeSignal: canonicalSlopeSignal,
		PriceMomentum:        pm,
		VolumeMomentum:       vm,
		MatchType:            matchType,
		Conclusion:           conclusion,
		StrongBuy:            strongBuy,
		StrongSell:           strongSell,
		AvgTakerBuyRatio:     avgTB,
	}, true
}

// getIntradayMatchTypeDisplay 返回量价匹配度的双语展示（code -> 中文/英文）
func getIntradayMatchTypeDisplay(code string, langZH bool) string {
	switch code {
	case MatchCodeAligned:
		if langZH {
			return "一致"
		}
		return "aligned"
	case MatchCodeDivergence:
		if langZH {
			return "背离"
		}
		return "divergence"
	case MatchCodeVolumeDrop:
		if langZH {
			return "放量跌"
		}
		return "volume_drop"
	case MatchCodePanic:
		if langZH {
			return "恐慌"
		}
		return "panic"
	case MatchCodeShrinkDown:
		if langZH {
			return "缩量跌"
		}
		return "shrink_down"
	default:
		return code
	}
}

func buildIntradayConclusion(slopeSignal, matchTypeCode string, strongBuy, strongSell bool, slope, pm, vm float64, langZH bool) string {
	var parts []string
	if langZH {
		if slopeSignal == "steep_up" {
			parts = append(parts, "斜率陡峭(类45度角拉升)")
		} else if slopeSignal == "steep_down" {
			parts = append(parts, "斜率陡峭下行")
		} else {
			parts = append(parts, "斜率平缓")
		}
		if matchTypeCode != "" {
			parts = append(parts, "量价"+getIntradayMatchTypeDisplay(matchTypeCode, true))
		}
		if strongBuy {
			parts = append(parts, "强力买入信号")
		}
		if strongSell {
			parts = append(parts, "强力卖出信号")
		}
		if len(parts) == 0 {
			return "分时无显著信号"
		}
		return strings.Join(parts, "，")
	}
	// EN
	if slopeSignal == "steep_up" {
		parts = append(parts, "steep slope (45°-like rally)")
	} else if slopeSignal == "steep_down" {
		parts = append(parts, "steep slope down")
	} else {
		parts = append(parts, "flat slope")
	}
	if matchTypeCode != "" {
		parts = append(parts, "vol-price "+getIntradayMatchTypeDisplay(matchTypeCode, false))
	}
	if strongBuy {
		parts = append(parts, "strong buy signal")
	}
	if strongSell {
		parts = append(parts, "strong sell signal")
	}
	if len(parts) == 0 {
		return "no significant intraday signal"
	}
	return strings.Join(parts, ", ")
}

// FormatIntradaySummaryForPrompt 生成给观察员的分时摘要文案（中英可选，含两套斜率及 ATR 归一化）
func FormatIntradaySummaryForPrompt(s *IntradaySummary, langZH bool) string {
	if s == nil {
		return ""
	}
	// 主句：按根数斜率
	var main string
	if langZH {
		main = fmt.Sprintf("分时摘要: Slope(根)=%.6f(%s)", s.SlopePerBar, s.SlopeSignal)
	} else {
		main = fmt.Sprintf("Intraday: Slope(bar)=%.6f(%s)", s.SlopePerBar, s.SlopeSignal)
	}
	// 按分钟斜率（有时间戳时）
	if s.SlopeSignalPerMin != "" || s.SlopePerMin != 0 {
		if langZH {
			main += fmt.Sprintf(" Slope(分)=%.6f(%s)", s.SlopePerMin, s.SlopeSignalPerMin)
		} else {
			main += fmt.Sprintf(" Slope(min)=%.6f(%s)", s.SlopePerMin, s.SlopeSignalPerMin)
		}
	}
	// ATR 归一化（有 ATR 时）
	if s.SlopeSignalNorm != "" || s.SlopeNorm != 0 {
		if langZH {
			main += fmt.Sprintf(" SlopeNorm=%.4f(%s)", s.SlopeNorm, s.SlopeSignalNorm)
		} else {
			main += fmt.Sprintf(" SlopeNorm=%.4f(%s)", s.SlopeNorm, s.SlopeSignalNorm)
		}
	}
	matchDisplay := getIntradayMatchTypeDisplay(s.MatchType, langZH)
	conclusionStr := buildIntradayConclusion(s.CanonicalSlopeSignal, s.MatchType, s.StrongBuy, s.StrongSell, s.SlopePerBar, s.PriceMomentum, s.VolumeMomentum, langZH)
	// 多空区分：Taker Buy Ratio 均值 >0.6 偏多 <0.4 偏空
	biasStr := ""
	if s.AvgTakerBuyRatio > 0 {
		if s.AvgTakerBuyRatio >= 0.6 {
			biasStr = " [偏多/TB↑]"
		} else if s.AvgTakerBuyRatio <= 0.4 {
			biasStr = " [偏空/TB↓]"
		}
	}
	if langZH {
		main += fmt.Sprintf(" | PM=%.4f VM=%.0f 量价%s | 决策=%s%s | %s", s.PriceMomentum, s.VolumeMomentum, matchDisplay, s.CanonicalSlopeSignal, biasStr, conclusionStr)
	} else {
		main += fmt.Sprintf(" | PM=%.4f VM=%.0f match=%s | canonical=%s%s | %s", s.PriceMomentum, s.VolumeMomentum, matchDisplay, s.CanonicalSlopeSignal, biasStr, conclusionStr)
	}
	if s.StrongSell {
		if langZH {
			main += " [STRONG_SELL]"
		} else {
			main += " [STRONG_SELL]"
		}
	}
	return main
}

// IsShortTimeframe 是否为短周期（用于决定是否输出分时摘要）
func IsShortTimeframe(tf string) bool {
	switch strings.ToLower(strings.TrimSpace(tf)) {
	case "1m", "3m", "5m", "15m":
		return true
	}
	return false
}

// TimeframePatternWeightLabel 返回周期形态权重提示（4h>1h>15m，长周期噪音小、更可靠）
func TimeframePatternWeightLabel(tf string, langZH bool) string {
	switch strings.ToLower(strings.TrimSpace(tf)) {
	case "4h":
		if langZH {
			return " [形态权重:高]"
		}
		return " [pattern weight: high]"
	case "1h", "2h":
		if langZH {
			return " [形态权重:中]"
		}
		return " [pattern weight: mid]"
	case "15m", "30m":
		if langZH {
			return " [形态权重:低/短周期]"
		}
		return " [pattern weight: low/short-TF]"
	default:
		return ""
	}
}

// TimeframeResonanceWeight 多周期共振权重（4h>1h>15m），用于加权共振判断
func TimeframeResonanceWeight(tf string) float64 {
	switch strings.ToLower(strings.TrimSpace(tf)) {
	case "4h":
		return 3.0
	case "1h", "2h":
		return 2.0
	case "15m", "30m":
		return 1.0
	default:
		return 1.0
	}
}
