package kernel

import (
	"nofx/market"
	"testing"
)

func TestSingleCandleType(t *testing.T) {
	tests := []struct {
		name     string
		k        market.KlineBar
		wantType string
	}{
		{"long_legged_doji", market.KlineBar{Open: 100, High: 102, Low: 98, Close: 100}, "LONG_LEGGED_DOJI"},
		{"strong_bull", market.KlineBar{Open: 100, High: 108, Low: 99, Close: 107}, "STRONG_BULL"},
		{"strong_bear", market.KlineBar{Open: 100, High: 101, Low: 93, Close: 94}, "STRONG_BEAR"},
		{"hammer", market.KlineBar{Open: 99, High: 101, Low: 90, Close: 101}, "HAMMER"}, // 下影长、上影极短、小实体，满足 HAMMER 条件
		{"spinning_top", market.KlineBar{Open: 100, High: 104, Low: 96, Close: 101}, "SPINNING_TOP"},
		{"limit_up", market.KlineBar{Open: 100, High: 100.00001, Low: 100, Close: 100.00001}, "LIMIT_UP"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SingleCandleType(tt.k)
			if got != tt.wantType {
				t.Errorf("SingleCandleType() = %v, want %v", got, tt.wantType)
			}
		})
	}
}

func TestDetectCandlestickPatterns(t *testing.T) {
	// 三白兵：三根阳线收盘节节升高
	klines3White := []market.KlineBar{
		{Open: 98, High: 100, Low: 97, Close: 99},
		{Open: 99, High: 102, Low: 98, Close: 101},
		{Open: 101, High: 104, Low: 100, Close: 103},
	}
	got := DetectCandlestickPatterns(klines3White, "uptrend")
	found := false
	for _, s := range got {
		if s == "THREE_WHITE_SOLDIERS" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("DetectCandlestickPatterns() expected THREE_WHITE_SOLDIERS in %v", got)
	}

	// 看涨吞没：阴+阳且阳完全包裹阴
	klinesEngulf := []market.KlineBar{
		{Open: 102, High: 103, Low: 100, Close: 101},
		{Open: 99, High: 104, Low: 98, Close: 103},
	}
	got2 := DetectCandlestickPatterns(klinesEngulf, "downtrend")
	found2 := false
	for _, s := range got2 {
		if s == "BULLISH_ENGULFING" {
			found2 = true
			break
		}
	}
	if !found2 {
		t.Errorf("DetectCandlestickPatterns() expected BULLISH_ENGULFING in %v", got2)
	}
}
