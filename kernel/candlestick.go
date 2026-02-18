package kernel

import (
	"math"
	"nofx/market"
)

// SingleCandleType 单根K线形态（参考《实战进阶宝典》及常见单根形态清单）
// 返回与 SingleCandleTypes 字典 key 一致的大写码：DOJI, LONG_LEGGED_DOJI, T_LINE, INVERTED_T_LINE,
// HAMMER, INVERTED_HAMMER, SPINNING_TOP, STRONG_BULL, STRONG_BEAR, MARUBOZU_BULL, MARUBOZU_BEAR,
// BALD_BULL, BALD_BEAR, FOOTLESS_BULL, FOOTLESS_BEAR, SMALL_BULL, SMALL_BEAR, LIMIT_UP, LIMIT_DOWN
func SingleCandleType(k market.KlineBar) string {
	body := k.Close - k.Open
	totalRange := k.High - k.Low
	if totalRange <= 0 {
		if body > 0 {
			return "LIMIT_UP"
		}
		if body < 0 {
			return "LIMIT_DOWN"
		}
		return "DOJI"
	}
	bodySize := math.Abs(body)
	bodyRatio := bodySize / totalRange
	upperShadow := k.High - math.Max(k.Open, k.Close)
	lowerShadow := math.Min(k.Open, k.Close) - k.Low

	// 一字线（涨跌停）：全幅极窄
	if totalRange < (k.Open+1e-8)*1e-6 {
		if body > 0 {
			return "LIMIT_UP"
		}
		if body < 0 {
			return "LIMIT_DOWN"
		}
		return "DOJI"
	}

	// 十字星类（无实体或极小实体）
	if bodyRatio < 0.1 {
		if lowerShadow > totalRange*0.3 && upperShadow > totalRange*0.3 {
			return "LONG_LEGGED_DOJI"
		}
		if lowerShadow > totalRange*0.3 && upperShadow < totalRange*0.05 {
			return "T_LINE" // 上影极短，与后文 0.4 分支一致
		}
		if upperShadow > totalRange*0.3 && lowerShadow < totalRange*0.05 {
			return "INVERTED_T_LINE"
		}
		return "DOJI"
	}

	// 锤头/倒锤头/螺旋桨/T/倒T（影线主导，0.1 <= bodyRatio < 0.4）
	if bodyRatio < 0.4 {
		if upperShadow > totalRange*0.25 && lowerShadow > totalRange*0.25 {
			return "SPINNING_TOP"
		}
		if lowerShadow > bodySize*2 && upperShadow < bodySize*0.5 {
			return "HAMMER"
		}
		if upperShadow > bodySize*2 && lowerShadow < bodySize*0.5 {
			return "INVERTED_HAMMER"
		}
		if lowerShadow > bodySize*2 && upperShadow < totalRange*0.05 {
			return "T_LINE"
		}
		if upperShadow > bodySize*2 && lowerShadow < totalRange*0.05 {
			return "INVERTED_T_LINE"
		}
		if body > 0 {
			return "SMALL_BULL"
		}
		return "SMALL_BEAR"
	}

	// 大阳/大阴（实体主导，含光头光脚）
	if bodyRatio > 0.6 {
		noUpper := upperShadow < totalRange*0.02
		noLower := lowerShadow < totalRange*0.02
		if body > 0 {
			if noUpper && noLower {
				return "MARUBOZU_BULL"
			}
			if noUpper {
				return "BALD_BULL"
			}
			if noLower {
				return "FOOTLESS_BULL"
			}
			return "STRONG_BULL"
		}
		if noUpper && noLower {
			return "MARUBOZU_BEAR"
		}
		if noUpper {
			return "FOOTLESS_BEAR"
		}
		if noLower {
			return "BALD_BEAR"
		}
		return "STRONG_BEAR"
	}
	if body > 0 {
		return "SMALL_BULL"
	}
	return "SMALL_BEAR"
}

// isDoji 是否为十字星（实体占比很小）
func isDoji(k market.KlineBar) bool {
	totalRange := k.High - k.Low
	if totalRange <= 0 {
		return true
	}
	bodySize := math.Abs(k.Close - k.Open)
	return bodySize/totalRange < 0.1
}

// DetectCandlestickPatterns 识别组合K线形态（参考《实战进阶宝典》完整清单）
// klines 为时间正序（旧→新），trend 为 uptrend/downtrend/sideways
func DetectCandlestickPatterns(klines []market.KlineBar, trend string) []string {
	var out []string
	seen := make(map[string]bool)
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}

	n := len(klines)
	if n < 2 {
		return out
	}

	k1 := klines[n-2]
	k2 := klines[n-1]
	body1 := k1.Close - k1.Open
	body2 := k2.Close - k2.Open
	range1 := k1.High - k1.Low
	range2 := k2.High - k2.Low

	// ---------- 两根K线 ----------
	// 看涨吞没 / 巨阳包阴
	if body1 < 0 && body2 > 0 && k2.Open < k1.Close && k2.Close > k1.Open &&
		math.Abs(body2) > math.Abs(body1)*1.1 {
		if k2.High > k1.High && k2.Low < k1.Low {
			add("BULLISH_ENGULFING")
		}
	}
	// 看跌吞没 / 巨阴包阳
	if body1 > 0 && body2 < 0 && k2.Open > k1.Close && k2.Close < k1.Open &&
		math.Abs(body2) > math.Abs(body1)*1.1 {
		if k2.High > k1.High && k2.Low < k1.Low {
			add("BEARISH_ENGULFING")
		}
	}
	// 淡友反攻：阳+阴，阴高开低走，收盘与前一收相近
	if body1 > 0 && body2 < 0 && k2.Open > k1.Close {
		mid := (k1.Close + k2.Close) / 2
		if math.Abs(k2.Close-k1.Close) <= mid*0.01 || math.Abs(k2.Close-k1.Close) <= range1*0.2 {
			add("BEARISH_MEETING")
		}
	}
	// 乌云压顶：阳+阴，阴高开低走，收盘深入阳线实体
	if body1 > 0 && body2 < 0 && k2.Open > k1.Close && k2.Close > k1.Open && k2.Close < (k1.Open+k1.Close)/2 {
		add("DARK_CLOUD_COVER")
	}
	// 倾盆大雨：阳+阴，阴低开低走，收盘低于阳线开盘
	if body1 > 0 && body2 < 0 && k2.Open < k1.Open && k2.Close < k1.Open {
		add("DOWNPOUR")
	}
	// 好友反攻：大阴+阳，阳收盘与阴收盘相近
	if body1 < 0 && body2 > 0 && range1 > 0 && math.Abs(body1)/range1 > 0.5 {
		if math.Abs(k2.Close-k1.Close) <= (k1.Close+k2.Close)/2*0.01 || math.Abs(k2.Close-k1.Close) <= range1*0.2 {
			add("BULLISH_MEETING")
		}
	}
	// 曙光初现：大阴+阳，阳收盘深入阴线实体
	if body1 < 0 && body2 > 0 && range1 > 0 && math.Abs(body1)/range1 > 0.5 &&
		k2.Close > k1.Open && k2.Close < (k1.Open+k1.Close)/2 {
		add("PIERCING_LINE")
	}
	// 旭日东升：大阴+阳，阳开盘在阴实体内部，收盘高于阴开盘
	if body1 < 0 && body2 > 0 && range1 > 0 && math.Abs(body1)/range1 > 0.5 &&
		k2.Open > k1.Close && k2.Open < k1.Open && k2.Close > k1.Open {
		add("RISING_SUN")
	}
	// 高位平顶：两根最高价相同或非常接近
	if n >= 2 && math.Abs(k1.High-k2.High) <= (k1.High+k2.High)/2*0.002 && trend == "uptrend" {
		add("TOPPING_FLAT_HIGHS")
	}
	// 低位平底：两根最低价相同或非常接近
	if n >= 2 && math.Abs(k1.Low-k2.Low) <= (k1.Low+k2.Low)/2*0.002 && trend == "downtrend" {
		add("BOTTOM_FLAT_LOWS")
	}
	// 锤子/射击之星/十字星：单根形态由 SingleCandleType 在 Recent candles 中输出，此处不再重复 add，避免与单K字典重复
	// 顶部双桨：两根螺旋桨（上下影都较长、小实体），且最高价相近
	if n >= 2 && range1 > 0 && range2 > 0 {
		b1 := math.Abs(body1) / range1
		b2 := math.Abs(body2) / range2
		u1, l1 := k1.High-math.Max(k1.Open, k1.Close), math.Min(k1.Open, k1.Close)-k1.Low
		u2, l2 := k2.High-math.Max(k2.Open, k2.Close), math.Min(k2.Open, k2.Close)-k2.Low
		if b1 < 0.3 && b2 < 0.3 && u1 > range1*0.3 && l1 > range1*0.3 && u2 > range2*0.3 && l2 > range2*0.3 {
			if math.Abs(k1.High-k2.High) <= (k1.High+k2.High)/2*0.005 && trend == "uptrend" {
				add("TOPPING_TWIN_SPINNERS")
			}
		}
	}

	// ---------- 第六章：双星验证逻辑与变盘协议 ----------
	// 6.1 连续双星/多星：高位双星-动能衰竭
	if n >= 2 && trend == "uptrend" && isDoji(k1) && isDoji(k2) {
		// 第二根星线无法突破第一根星线高点时，先减仓信号
		if k2.High <= k1.High*(1+1e-6) {
			add("HIGH_DOUBLE_STAR_EXHAUSTION")
		}
	}
	// 6.1 连续双星：低位双星-卖盘枯竭（双十字星）
	if n >= 2 && trend == "downtrend" && isDoji(k1) && isDoji(k2) {
		add("LOW_DOUBLE_STAR_BOTTOM")
	}
	// 双针探底：两根带长下影的「针线」在相近低点探底（锤子/长下影），跌势末端卖压衰竭
	if n >= 2 && trend == "downtrend" && range1 > 0 && range2 > 0 {
		l1 := math.Min(k1.Open, k1.Close) - k1.Low
		l2 := math.Min(k2.Open, k2.Close) - k2.Low
		b1 := math.Abs(body1)
		b2 := math.Abs(body2)
		if l1 > b1*1.8 && l2 > b2*1.8 && math.Abs(k1.Low-k2.Low) <= (k1.Low+k2.Low)/2*0.008 {
			add("DOUBLE_NEEDLE_BOTTOM")
		}
	}
	// 6.2 非连续双星：当前为星线时，与历史星线对比
	if n >= 3 && isDoji(k2) {
		// 从倒数第二根往前找最近一根十字星
		var prevDojiIdx int = -1
		for i := n - 2; i >= 0; i-- {
			if isDoji(klines[i]) {
				prevDojiIdx = i
				break
			}
		}
		if prevDojiIdx >= 0 {
			kPrev := klines[prevDojiIdx]
			P1, V1 := kPrev.High, kPrev.Volume
			P2, V2 := k2.High, k2.Volume
			if V1 <= 0 {
				V1 = 1
			}
			// 放量滞涨诱多：P2 无法突破 P1 且 V2 >= V1（同一位置抛压沉重，假突破）
			if trend == "uptrend" && P2 <= P1*(1+1e-6) && V2 >= V1 {
				add("VOLUME_STAGNATION_FALSE_BREAKOUT")
			}
			// 缩量止跌真底：P2 >= P1 且 V2 < V1（二次回踩量能显著缩减，真底确立）
			P1Low := kPrev.Low
			P2Low := k2.Low
			if trend == "downtrend" && P2Low >= P1Low*(1-1e-6) && V2 < V1*0.9 {
				add("VOLUME_SHRINK_TRUE_BOTTOM")
			}
		}
	}

	// ---------- 三根K线 ----------
	if n < 3 {
		return out
	}
	k0 := klines[n-3] // 三根中最旧
	body0 := k0.Close - k0.Open
	range0 := k0.High - k0.Low

	// 早晨十字星：阴+十字星+阳，阳收盘深入阴实体（body2/range2<0.2）
	if body0 < 0 && range2 > 0 && math.Abs(body2)/range2 < 0.2 && body1 > 0 &&
		math.Abs(body0) > range0*0.5 && math.Abs(body1) > range1*0.5 &&
		k1.Close > (k0.Open+k0.Close)/2 {
		add("MORNING_DOJI_STAR")
	} else if body0 < 0 && range2 > 0 && math.Abs(body2)/range2 < 0.3 && body1 > 0 &&
		math.Abs(body0) > range0*0.5 && math.Abs(body1) > range1*0.5 &&
		k1.Close > (k0.Open+k0.Close)/2 {
		// 早晨之星：阴+小实体+阳（0.2<=body2/range2<0.3，与十字星二选一）
		add("MORNING_STAR")
	}
	// 黄昏十字星：阳+十字星+阴
	if body0 > 0 && range2 > 0 && math.Abs(body2)/range2 < 0.2 && body1 < 0 &&
		math.Abs(body0) > range0*0.5 && math.Abs(body1) > range1*0.5 &&
		k1.Close < (k0.Open+k0.Close)/2 {
		add("EVENING_DOJI_STAR")
	} else if body0 > 0 && range2 > 0 && math.Abs(body2)/range2 < 0.3 && body1 < 0 &&
		math.Abs(body0) > range0*0.5 && math.Abs(body1) > range1*0.5 &&
		k1.Close < (k0.Open+k0.Close)/2 {
		// 黄昏之星：阳+小实体+阴（与黄昏十字星二选一）
		add("EVENING_STAR")
	}
	// 三白兵/红三兵：三根阳线收盘节节升高 k0.Close < k1.Close < k2.Close
	if body0 > 0 && body2 > 0 && body1 > 0 &&
		k1.Close > k0.Close && k2.Close > k1.Close &&
		k1.Open > k0.Open && k2.Open > k1.Open {
		add("THREE_WHITE_SOLDIERS")
	}
	// 三黑鸦/黑三兵：三根阴线收盘节节下降 k0.Close > k1.Close > k2.Close
	if body0 < 0 && body2 < 0 && body1 < 0 &&
		k1.Close < k0.Close && k2.Close < k1.Close &&
		k1.Open < k0.Open && k2.Open < k1.Open {
		add("THREE_BLACK_CROWS")
	}
	// 三级跳水：三阴，第一根跳空高开，后两根跳空低开
	if body0 < 0 && body2 < 0 && body1 < 0 && n >= 4 &&
		k0.Open > klines[n-4].Close && k2.Open < k0.Close {
		add("THREE_STAGE_DIVE")
	}

	// ---------- 多根K线（5根窗口）----------
	if n >= 5 {
		last5 := klines[n-5:]
		// 五阴连天：5根阴线收盘节节下降
		allBear := len(last5) == 5
		for i := 0; i < 4 && allBear; i++ {
			if last5[i].Close <= last5[i+1].Close {
				allBear = false
			}
			if last5[i].Close >= last5[i].Open {
				allBear = false
			}
		}
		if allBear && last5[4].Close < last5[4].Open {
			add("FIVE_YIN_ROW")
		}
		// 五阳上阵/连续跳高：多根阳线且可带跳空
		allBull := true
		for i := 0; i < len(last5) && allBull; i++ {
			if last5[i].Close <= last5[i].Open {
				allBull = false
			}
		}
		if allBull && len(last5) >= 3 {
			add("FIVE_YANG_LINEUP")
		}
		// 连续跳高：多根阳线且每根跳空高开
		gapUp := true
		for i := 1; i < len(last5) && gapUp; i++ {
			if last5[i].Open <= last5[i-1].Close {
				gapUp = false
			}
		}
		if gapUp && allBull && len(last5) >= 3 {
			add("CONSECUTIVE_GAP_UP")
		}
		// 高位塔顶：大阳+多根小阴小阳+大阴
		if n >= 4 {
			first := last5[0]
			last := last5[len(last5)-1]
			fb := first.Close - first.Open
			lb := last.Close - last.Open
			if fb > 0 && lb < 0 && math.Abs(fb) > (first.High-first.Low)*0.5 && math.Abs(lb) > (last.High-last.Low)*0.5 {
				add("TOPPING_TOWER")
			}
		}
		// 低位塔底：大阴+多根小阴小阳+大阳
		if n >= 4 {
			first := last5[0]
			last := last5[len(last5)-1]
			fb := first.Close - first.Open
			lb := last.Close - last.Open
			if fb < 0 && lb > 0 && math.Abs(fb) > (first.High-first.Low)*0.5 && math.Abs(lb) > (last.High-last.Low)*0.5 {
				add("BOTTOM_TOWER")
			}
		}
		// 高位盘旋：大阳后多根小阳小阴，最低价高于大阳收盘
		if n >= 4 {
			first := last5[0]
			minLow := first.Low
			for i := 1; i < len(last5); i++ {
				if last5[i].Low < minLow {
					minLow = last5[i].Low
				}
			}
			if first.Close-first.Open > (first.High-first.Low)*0.5 && minLow >= first.Close*0.998 {
				add("HIGH_SIDE_COIL")
			}
		}
		// 低档排列：大阴后多根小阳小阴，最高价低于大阴最低价
		if n >= 4 {
			first := last5[0]
			maxHigh := first.High
			for i := 1; i < len(last5); i++ {
				if last5[i].High > maxHigh {
					maxHigh = last5[i].High
				}
			}
			if first.Open-first.Close > (first.High-first.Low)*0.5 && maxHigh <= first.Low*1.002 {
				add("LOW_TIER_ARRANGEMENT")
			}
		}

		// 高位圆顶：涨势末端高点逐渐降低、K线实体变小，呈圆弧状回落（简化：近5根内最高点在前半段，后半段高点逐降）
		if n >= 5 && trend == "uptrend" {
			maxHigh := last5[0].High
			maxIdx := 0
			for i := 1; i < len(last5); i++ {
				if last5[i].High > maxHigh {
					maxHigh = last5[i].High
					maxIdx = i
				}
			}
			// 高点出现在前 2 根或中间，且最后 2 根高点低于或接近前高
			if maxIdx <= 2 && last5[4].High < maxHigh*0.998 && last5[3].High <= maxHigh*1.002 {
				add("TOPPING_ROUNDING_TOP")
			}
		}
		// 低位圆底：跌势末端低点逐渐抬高、实体变小，呈圆弧状企稳（简化：近5根内最低点在前半段，后半段低点逐升）
		if n >= 5 && trend == "downtrend" {
			minLow := last5[0].Low
			minIdx := 0
			for i := 1; i < len(last5); i++ {
				if last5[i].Low < minLow {
					minLow = last5[i].Low
					minIdx = i
				}
			}
			if minIdx <= 2 && last5[4].Low > minLow*1.002 && last5[3].Low >= minLow*0.998 {
				add("BOTTOM_ROUNDING_BOTTOM")
			}
		}
		// 低位五档线：底部区域多根小阴小阳呈阶梯状，低点持平或略抬（简化：5根小实体且低点不创新低或阶梯抬升）
		if n >= 5 && trend == "downtrend" {
			smallBodies := true
			for i := 0; i < len(last5) && smallBodies; i++ {
				r := last5[i].High - last5[i].Low
				if r <= 0 {
					smallBodies = false
					break
				}
				bodyRatio := math.Abs(last5[i].Close-last5[i].Open) / r
				if bodyRatio > 0.5 {
					smallBodies = false
				}
			}
			lowsRising := last5[4].Low >= last5[0].Low*0.998 && last5[3].Low >= last5[0].Low*0.998
			if smallBodies && lowsRising {
				add("BOTTOM_FIVE_TIER_LINE")
			}
		}
	}

	return out
}
