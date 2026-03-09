package kernel

import (
	"fmt"
	"math"
	"nofx/market"
	"sort"
)

// PriceLevel 关键价位：价格 + 来源标签（用于支撑/阻力）
type PriceLevel struct {
	Price float64
	Label string // e.g. "Swing High", "Fib 0.382", "Round 50k", "VP"
}

// SupportResistanceResult 预处理得到的支撑/阻力集合（前高前低、斐波那契、整数关、量能密集区等）
type SupportResistanceResult struct {
	Supports    []PriceLevel // 当前价下方的支撑
	Resistances []PriceLevel // 当前价上方的阻力
	RangeHigh   float64      // 近期区间最高（兼容原有）
	RangeLow    float64      // 近期区间最低
}

const (
	pivotBars   = 2   //  pivot 确认：左右各 2 根
	maxPivots   = 5   // 最多取前 N 个前高/前低
	maxFibLevel = 5   // 斐波那契档位数量
	vpBins      = 30  // 量能分布分档数
	vpTopPct    = 0.85 // 取体积占比前 15% 的档位视为密集区
	minKlinesSR = 10  // 至少 K 线数才计算 S/R
)

var fibRatios = []float64{0.236, 0.382, 0.5, 0.618, 0.786}

// ComputeSupportResistance 基于 K 线预处理支撑/阻力：前高前低（pivot/swing）、斐波那契、整数关、量能密集区
func ComputeSupportResistance(klines []market.KlineBar, currentPrice float64) SupportResistanceResult {
	var out SupportResistanceResult
	if len(klines) < minKlinesSR || currentPrice <= 0 {
		return out
	}
	// 近期区间最高/最低
	for _, k := range klines {
		if k.High > out.RangeHigh {
			out.RangeHigh = k.High
		}
		if out.RangeLow <= 0 || k.Low < out.RangeLow {
			out.RangeLow = k.Low
		}
	}

	// 1) Pivot / Swing 前高前低
	swingHighs, swingLows := computeSwingHighsLows(klines)
	for _, p := range swingHighs {
		if p > currentPrice {
			out.Resistances = append(out.Resistances, PriceLevel{Price: p, Label: "Swing High"})
		}
	}
	for _, p := range swingLows {
		if p < currentPrice && p > 0 {
			out.Supports = append(out.Supports, PriceLevel{Price: p, Label: "Swing Low"})
		}
	}

	// 2) 斐波那契：取近期摆动区间 (swing high / swing low) 计算回撤档位
	swingHigh, swingLow := lastSwingHighLow(klines, swingHighs, swingLows)
	if swingHigh > swingLow && swingHigh > currentPrice && swingLow < currentPrice {
		span := swingHigh - swingLow
		for _, r := range fibRatios {
			level := swingHigh - span*r
			if level < currentPrice && level > 0 {
				out.Supports = append(out.Supports, PriceLevel{Price: level, Label: "Fib " + formatFib(r)})
			} else if level > currentPrice {
				out.Resistances = append(out.Resistances, PriceLevel{Price: level, Label: "Fib " + formatFib(r)})
			}
		}
	}

	// 3) 整数关：按价格数量级取整
	rounds := computeRoundLevels(currentPrice, out.RangeLow, out.RangeHigh)
	for _, p := range rounds {
		if p < currentPrice && p > 0 {
			out.Supports = append(out.Supports, PriceLevel{Price: p, Label: "Round"})
		} else if p > currentPrice {
			out.Resistances = append(out.Resistances, PriceLevel{Price: p, Label: "Round"})
		}
	}

	// 4) 量能密集区（VP）：按价格分档汇总成交量，取高量档位中心价
	vpLevels := computeVolumeProfileLevels(klines, currentPrice, out.RangeLow, out.RangeHigh)
	for _, p := range vpLevels {
		if p < currentPrice && p > 0 {
			out.Supports = append(out.Supports, PriceLevel{Price: p, Label: "VP"})
		} else if p > currentPrice {
			out.Resistances = append(out.Resistances, PriceLevel{Price: p, Label: "VP"})
		}
	}

	// 去重：同侧相近价位只保留一个（按 0.15% 容差合并，保留第一个出现的标签）
	out.Supports = dedupeLevels(out.Supports, true)
	out.Resistances = dedupeLevels(out.Resistances, false)
	// 各来源限制数量，避免刷屏
	out.Supports = trimLevels(out.Supports, 8)
	out.Resistances = trimLevels(out.Resistances, 8)
	return out
}

func formatFib(r float64) string {
	if r == 0.5 {
		return "0.5"
	}
	return fmt.Sprintf("%.3g", r)
}

// computeSwingHighsLows 找出所有 pivot 高/低点（局部最高/最低，左右各 pivotBars 根确认）
func computeSwingHighsLows(klines []market.KlineBar) (highs, lows []float64) {
	n := len(klines)
	if n < 2*pivotBars+1 {
		return nil, nil
	}
	for i := pivotBars; i < n-pivotBars; i++ {
		h := klines[i].High
		isHigh := true
		for j := i - pivotBars; j <= i+pivotBars && j != i; j++ {
			if klines[j].High >= h {
				isHigh = false
				break
			}
		}
		if isHigh {
			highs = append(highs, h)
		}
		l := klines[i].Low
		isLow := true
		for j := i - pivotBars; j <= i+pivotBars && j != i; j++ {
			if klines[j].Low <= l {
				isLow = false
				break
			}
		}
		if isLow {
			lows = append(lows, l)
		}
	}
	// 从新到旧取前 maxPivots 个
	if len(highs) > maxPivots {
		highs = highs[len(highs)-maxPivots:]
	}
	if len(lows) > maxPivots {
		lows = lows[len(lows)-maxPivots:]
	}
	return highs, lows
}

// lastSwingHighLow 取近期 pivot 的极值构成一个摆动区间（用于斐波那契）
func lastSwingHighLow(klines []market.KlineBar, highs, lows []float64) (swingHigh, swingLow float64) {
	if len(highs) == 0 || len(lows) == 0 {
		return 0, 0
	}
	sh := highs[0]
	for _, h := range highs {
		if h > sh {
			sh = h
		}
	}
	sl := lows[0]
	for _, l := range lows {
		if l < sl {
			sl = l
		}
	}
	if sh > sl {
		return sh, sl
	}
	return 0, 0
}

// computeRoundLevels 在 [lo, hi] 内取当前价附近的整数关（按数量级：万/千/百/十/元/角/分）
func computeRoundLevels(currentPrice, lo, hi float64) []float64 {
	if hi <= lo || currentPrice <= 0 {
		return nil
	}
	mag := math.Pow(10, math.Floor(math.Log10(currentPrice)))
	if mag < 1e-10 {
		mag = 1
	}
	// 当前价上下各 2 个整数关
	step := mag
	if currentPrice < 1 {
		step = 0.1
		if currentPrice < 0.1 {
			step = 0.01
		}
	}
	base := math.Floor(currentPrice/step) * step
	var out []float64
	for _, d := range []float64{-2, -1, 1, 2} {
		p := base + d*step
		if p >= lo && p <= hi && p > 0 {
			out = append(out, p)
		}
	}
	return out
}

// computeVolumeProfileLevels 量能密集区：分 vpBins 档，取体积占比前 vpTopPct 的档位中心价
func computeVolumeProfileLevels(klines []market.KlineBar, currentPrice, lo, hi float64) []float64 {
	if hi <= lo || len(klines) == 0 {
		return nil
	}
	binSize := (hi - lo) / float64(vpBins)
	if binSize <= 0 {
		return nil
	}
	vols := make([]float64, vpBins)
	var totalVol float64
	for _, k := range klines {
		// 用 H/L 的中间或典型价落入档位
		mid := (k.High + k.Low) / 2
		idx := int((mid - lo) / binSize)
		if idx < 0 {
			idx = 0
		}
		if idx >= vpBins {
			idx = vpBins - 1
		}
		vols[idx] += k.Volume
		totalVol += k.Volume
	}
	if totalVol <= 0 {
		return nil
	}
	type bin struct {
		idx int
		vol float64
	}
	bins := make([]bin, vpBins)
	for i := range vols {
		bins[i] = bin{i, vols[i]}
	}
	sort.Slice(bins, func(i, j int) bool { return bins[i].vol > bins[j].vol })
	// 取前 15% 体积对应的档位（最多 3 个）
	var levels []float64
	var cum float64
	threshold := totalVol * vpTopPct
	for _, b := range bins {
		if cum >= threshold || len(levels) >= 3 {
			break
		}
		if b.vol <= 0 {
			continue
		}
		center := lo + (float64(b.idx)+0.5)*binSize
		levels = append(levels, center)
		cum += b.vol
	}
	return levels
}

// dedupeLevels 同侧相近价位合并（容差 0.15%），supports 按价格降序，resistances 按价格升序
func dedupeLevels(levels []PriceLevel, supports bool) []PriceLevel {
	if len(levels) <= 1 {
		return levels
	}
	if supports {
		sort.Slice(levels, func(i, j int) bool { return levels[i].Price > levels[j].Price })
	} else {
		sort.Slice(levels, func(i, j int) bool { return levels[i].Price < levels[j].Price })
	}
	out := levels[:1]
	for i := 1; i < len(levels); i++ {
		last := out[len(out)-1].Price
		if math.Abs(levels[i].Price-last)/last <= 0.0015 {
			continue
		}
		out = append(out, levels[i])
	}
	return out
}

func trimLevels(levels []PriceLevel, max int) []PriceLevel {
	if len(levels) <= max {
		return levels
	}
	return levels[:max]
}
