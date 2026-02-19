package kernel

import (
	"nofx/market"
	"strings"
	"testing"
)

func TestComputeIntradaySummary_TooFewKlines(t *testing.T) {
	klines := []market.KlineBar{{Close: 100}}
	summary, ok := ComputeIntradaySummary(klines, 10, "5m", 0)
	if ok || summary != nil {
		t.Fatalf("expected no summary with 1 kline; got ok=%v", ok)
	}
}

func TestComputeIntradaySummary_PerBarSlope(t *testing.T) {
	// 10 根 K，价格从 100 涨到 101.5 => 相对涨幅 1.5%，每根 0.15% => SlopePerBar = 0.0015 > 0.001 => steep_up
	klines := make([]market.KlineBar, 10)
	for i := range klines {
		klines[i] = market.KlineBar{Close: 100 + float64(i)*1.5/9, Volume: 1000} // 最后一根 i=9 => 101.5
	}
	summary, ok := ComputeIntradaySummary(klines, 10, "", 0)
	if !ok || summary == nil {
		t.Fatalf("expected summary; got ok=%v", ok)
	}
	// SlopePerBar = (101.5 - 100) / 100 / 10 = 0.0015
	if summary.SlopePerBar < 0.0014 || summary.SlopePerBar > 0.0016 {
		t.Errorf("SlopePerBar = %v, want ~0.0015", summary.SlopePerBar)
	}
	if summary.SlopeSignal != "steep_up" {
		t.Errorf("SlopeSignal = %s, want steep_up", summary.SlopeSignal)
	}
	// 无时间戳、无 ATR 时 CanonicalSlopeSignal 应为按根数
	if summary.CanonicalSlopeSignal != "steep_up" {
		t.Errorf("CanonicalSlopeSignal = %s, want steep_up", summary.CanonicalSlopeSignal)
	}
}

func TestComputeIntradaySummary_PerMinSlope(t *testing.T) {
	// 10 根 K，时间跨度 9 分钟（0..9），价格 100 -> 100.9 => SlopePerMin = (100.9-100)/100/9 ≈ 0.001
	baseTime := int64(1700000000000) // 毫秒
	klines := make([]market.KlineBar, 10)
	for i := range klines {
		klines[i] = market.KlineBar{
			Time:   baseTime + int64(i)*60000,
			Close:  100 + float64(i)*0.1,
			Volume: 1000,
		}
	}
	summary, ok := ComputeIntradaySummary(klines, 10, "1m", 0)
	if !ok || summary == nil {
		t.Fatalf("expected summary; got ok=%v", ok)
	}
	elapsedMin := (klines[9].Time - klines[0].Time) / 60000
	if elapsedMin < 1 {
		elapsedMin = 1
	}
	priceAgo, priceNow := 100.0, 100.0+9*0.1
	expectPerMin := (priceNow - priceAgo) / priceAgo / float64(elapsedMin)
	if summary.SlopePerMin < expectPerMin*0.99 || summary.SlopePerMin > expectPerMin*1.01 {
		t.Errorf("SlopePerMin = %v, want ~%v", summary.SlopePerMin, expectPerMin)
	}
	if summary.SlopeSignalPerMin == "" {
		t.Error("SlopeSignalPerMin should be set when timestamps available")
	}
}

func TestComputeIntradaySummary_SlopeNorm(t *testing.T) {
	// 同上 10 根 100->101，SlopePerBar=0.001；ATR=2，Price=101 => SlopeNorm = 0.001*101/2 = 0.0505
	klines := make([]market.KlineBar, 10)
	for i := range klines {
		klines[i] = market.KlineBar{Close: 100 + float64(i)*0.1, Volume: 1000}
	}
	atr := 2.0
	summary, ok := ComputeIntradaySummary(klines, 10, "", atr)
	if !ok || summary == nil {
		t.Fatalf("expected summary; got ok=%v", ok)
	}
	expectNorm := summary.SlopePerBar * 101.0 / atr
	if summary.SlopeNorm < expectNorm*0.99 || summary.SlopeNorm > expectNorm*1.01 {
		t.Errorf("SlopeNorm = %v, want ~%v", summary.SlopeNorm, expectNorm)
	}
	if summary.SlopeSignalNorm == "" {
		t.Error("SlopeSignalNorm should be set when ATR > 0")
	}
	// 0.05 < 0.5 所以应为 flat
	if summary.SlopeSignalNorm != "flat" {
		t.Errorf("SlopeSignalNorm = %s, want flat (norm under threshold)", summary.SlopeSignalNorm)
	}
}

func TestComputeIntradaySummary_StrongBuy(t *testing.T) {
	// 陡峭上升 + 当前量 > 均量 => StrongBuy
	klines := make([]market.KlineBar, 10)
	for i := range klines {
		vol := 1000.0
		if i == 9 {
			vol = 2000 // 最后一根放量
		}
		klines[i] = market.KlineBar{Close: 100 + float64(i)*0.2, Volume: vol}
	}
	summary, ok := ComputeIntradaySummary(klines, 10, "", 0)
	if !ok || summary == nil {
		t.Fatalf("expected summary; got ok=%v", ok)
	}
	if summary.SlopeSignal != "steep_up" {
		t.Errorf("SlopeSignal = %s, want steep_up", summary.SlopeSignal)
	}
	if !summary.StrongBuy {
		t.Error("expected StrongBuy (steep_up + volume > avg)")
	}
}

func TestIsShortTimeframe(t *testing.T) {
	tests := []struct {
		tf   string
		want bool
	}{
		{"1m", true},
		{"5m", true},
		{"15m", true},
		{"1h", false},
		{"4h", false},
		{" 5M ", true},
	}
	for _, tt := range tests {
		if got := IsShortTimeframe(tt.tf); got != tt.want {
			t.Errorf("IsShortTimeframe(%q) = %v, want %v", tt.tf, got, tt.want)
		}
	}
}

func TestFormatIntradaySummaryForPrompt(t *testing.T) {
	s := &IntradaySummary{
		SlopePerBar:          0.001,
		SlopeSignal:          "steep_up",
		CanonicalSlopeSignal: "steep_up",
		PriceMomentum:        0.5,
		VolumeMomentum:       100,
		MatchType:            MatchCodeAligned,
		Conclusion:           "", // 展示时按 lang 由 buildIntradayConclusion 生成
	}
	zh := FormatIntradaySummaryForPrompt(s, true)
	if zh == "" || len(zh) < 10 {
		t.Errorf("FormatIntradaySummaryForPrompt(zh) empty or too short: %q", zh)
	}
	if !strings.Contains(zh, "一致") {
		t.Errorf("ZH output should contain 一致: %q", zh)
	}
	en := FormatIntradaySummaryForPrompt(s, false)
	if en == "" || len(en) < 10 {
		t.Errorf("FormatIntradaySummaryForPrompt(en) empty or too short: %q", en)
	}
	if !strings.Contains(en, "aligned") {
		t.Errorf("EN output should contain aligned: %q", en)
	}
}
