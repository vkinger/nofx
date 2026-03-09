package kernel

import (
	"math"
	"nofx/market"
)

// Taker Buy Ratio 阈值：>0.6 主动买盘主导，<0.4 主动卖盘主导（与 engine 展示一致）
const TakerBuyDominantThreshold = 0.60
const TakerSellDominantThreshold = 0.40

// DetectVolumePriceSignals 根据最近 K 线、成交量及 Taker Buy Ratio 判断量价信号（参考量价交易宝典）
// klines 时间正序（旧→新），至少需要 3 根，建议 5 根；TakerBuyRatio 已用则叠加买卖主导信号
func DetectVolumePriceSignals(klines []market.KlineBar) []string {
	var out []string
	seen := make(map[string]bool)
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}

	n := len(klines)
	if n < 3 {
		return out
	}
	// 使用最后 5 根（或全部）
	start := 0
	if n > 5 {
		start = n - 5
	}
	window := klines[start:n]
	m := len(window)
	if m < 3 {
		return out
	}

	// Taker Buy Ratio：窗口内有效数据的平均，高 ROI 量价增强
	var tbSum float64
	var tbCount int
	for _, k := range window {
		if k.TakerBuyRatio > 0 {
			tbSum += k.TakerBuyRatio
			tbCount++
		}
	}
	if tbCount > 0 {
		avgTB := tbSum / float64(tbCount)
		if avgTB >= TakerBuyDominantThreshold {
			add("TAKER_BUY_DOMINANT")
		} else if avgTB <= TakerSellDominantThreshold {
			add("TAKER_SELL_DOMINANT")
		}
	}

	// 每根 body、volume
	bodies := make([]float64, m)
	vols := make([]float64, m)
	var upSum, downSum float64
	var upCount, downCount int
	for i, k := range window {
		bodies[i] = k.Close - k.Open
		vols[i] = k.Volume
		if k.Volume <= 0 {
			vols[i] = 1
		}
		if bodies[i] > 0 {
			upSum += vols[i]
			upCount++
		} else if bodies[i] < 0 {
			downSum += vols[i]
			downCount++
		}
	}

	// 最近 3 根：价格方向、量能趋势、涨跌加速度
	body0, body1, body2 := bodies[m-3], bodies[m-2], bodies[m-1]
	vol0, vol2 := vols[m-3], vols[m-1]
	if vol0 <= 0 {
		vol0 = 1
	}
	netUp := body0+body1+body2 > 0
	netDown := body0+body1+body2 < 0

	// 量能趋势：首尾对比（最近 3 根内）
	volIncreasing := vol2 > vol0*1.15
	volDecreasing := vol2 < vol0*0.85
	volFlat := !volIncreasing && !volDecreasing

	// 涨跌加速度：比较三根 body 绝对值（越涨越慢 = 绝对值递减，越涨越快 = 递增）
	abs0 := math.Abs(body0)
	abs1 := math.Abs(body1)
	abs2 := math.Abs(body2)
	upAccelerating := netUp && body0 > 0 && body1 > 0 && body2 > 0 && body2 > body1 && body1 > body0
	upDecelerating := netUp && body0 > 0 && body1 > 0 && body2 > 0 && body2 < body1 && body1 < body0
	downAccelerating := netDown && body0 < 0 && body1 < 0 && body2 < 0 && abs2 > abs1 && abs1 > abs0
	downDecelerating := netDown && body0 < 0 && body1 < 0 && body2 < 0 && abs2 < abs1 && abs1 < abs0

	// ---------- 全阳 / 全阴 ----------
	if upCount == m {
		add("STRONG_BUY")
		if volFlat && upAccelerating {
			add("FLAT_VOLUME_RALLY")
		} else if volDecreasing && upAccelerating {
			add("SHRINK_UP_LOCK")
		} else if volDecreasing && upDecelerating {
			add("DISTRIBUTION")
		}
		return out
	}
	if downCount == m {
		add("STRONG_SELL")
		// 全阴时量价细化只取一个最贴切的，且平量才标 FLAT_VOLUME_DECLINE（缩量用 SHRINK/ACCUMULATION）
		if volFlat {
			add("FLAT_VOLUME_DECLINE")
		} else if volDecreasing && downAccelerating {
			add("SHRINK_DOWN_CONTINUE")
		} else if volDecreasing && downDecelerating {
			add("ACCUMULATION")
		} else if volIncreasing && downDecelerating {
			add("BOTTOM_WITH_VOLUME")
		} else if volIncreasing && downAccelerating {
			add("HEALTHY_DOWNTREND")
		}
		return out
	}

	// ---------- 阴阳混合：先看整体量能偏向 ----------
	avgUpVol := upSum / float64(upCount)
	if upCount == 0 {
		avgUpVol = 0
	}
	avgDownVol := downSum / float64(downCount)
	if downCount == 0 {
		avgDownVol = 0
	}

	// 再根据最近 3 根方向与量价配合细化
	if netUp {
		if volIncreasing && upDecelerating {
			add("STAGNATION_WITH_VOLUME")
		} else if volDecreasing && upDecelerating {
			add("DISTRIBUTION")
		} else if volDecreasing && upAccelerating {
			add("SHRINK_UP_LOCK")
		} else if volFlat && upDecelerating {
			add("FLAT_VOLUME_STAGNATION")
		} else if volFlat && upAccelerating {
			add("FLAT_VOLUME_RALLY")
		} else if avgUpVol > avgDownVol*1.2 {
			add("HEALTHY_UPTREND")
		} else if avgDownVol > avgUpVol*1.2 {
			add("DISTRIBUTION")
		} else {
			add("NEUTRAL")
		}
	} else if netDown {
		if volIncreasing && downDecelerating {
			add("BOTTOM_WITH_VOLUME")
		} else if volIncreasing && downAccelerating {
			add("HEALTHY_DOWNTREND")
		} else if volDecreasing && downDecelerating {
			add("ACCUMULATION")
		} else if volDecreasing && downAccelerating {
			add("SHRINK_DOWN_CONTINUE")
		} else if volFlat {
			add("FLAT_VOLUME_DECLINE")
		}
		if len(out) == 0 {
			if avgDownVol > avgUpVol*1.2 {
				add("HEALTHY_DOWNTREND")
			} else if avgUpVol > avgDownVol*1.2 {
				add("ACCUMULATION")
			} else {
				add("NEUTRAL")
			}
		}
	} else {
		// 横盘
		if avgUpVol > avgDownVol*1.5 {
			add("HEALTHY_UPTREND")
		} else if avgDownVol > avgUpVol*1.5 {
			add("DISTRIBUTION")
		} else {
			add("NEUTRAL")
		}
	}

	return out
}
