package market

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"nofx/logger"
	"nofx/provider/coinank/coinank_api"
	"nofx/provider/coinank/coinank_enum"
	"nofx/provider/hyperliquid"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FundingRateCache is the funding rate cache structure
// Binance Funding Rate only updates every 8 hours, using 1-hour cache can significantly reduce API calls
type FundingRateCache struct {
	Rate              float64
	NextFundingTimeMs int64 // 下次资金费结算时间（毫秒），0 表示未知
	UpdatedAt         time.Time
}

type TradingFeeRateCache struct {
	MakerRate float64
	TakerRate float64
	Source    string
	UpdatedAt time.Time
}

var (
	fundingRateMap sync.Map // map[string]*FundingRateCache
	frCacheTTL     = 1 * time.Hour
	tradingFeeMap  sync.Map // map[string]*TradingFeeRateCache
	feeCacheTTL    = 1 * time.Hour
)

// Note: Kline data now uses free/open API (coinank_api.Kline) which doesn't require authentication

// getKlinesFromCoinAnk fetches kline data from CoinAnk API (replacement for WSMonitorCli)
func getKlinesFromCoinAnk(symbol, interval, exchange string, limit int) ([]Kline, error) {
	// Map interval string to coinank enum
	var coinankInterval coinank_enum.Interval
	switch interval {
	case "1m":
		coinankInterval = coinank_enum.Minute1
	case "3m":
		coinankInterval = coinank_enum.Minute3
	case "5m":
		coinankInterval = coinank_enum.Minute5
	case "15m":
		coinankInterval = coinank_enum.Minute15
	case "30m":
		coinankInterval = coinank_enum.Minute30
	case "1h":
		coinankInterval = coinank_enum.Hour1
	case "2h":
		coinankInterval = coinank_enum.Hour2
	case "4h":
		coinankInterval = coinank_enum.Hour4
	case "6h":
		coinankInterval = coinank_enum.Hour6
	case "8h":
		coinankInterval = coinank_enum.Hour8
	case "12h":
		coinankInterval = coinank_enum.Hour12
	case "1d":
		coinankInterval = coinank_enum.Day1
	case "3d":
		coinankInterval = coinank_enum.Day3
	case "1w":
		coinankInterval = coinank_enum.Week1
	default:
		return nil, fmt.Errorf("unsupported interval: %s", interval)
	}

	// Map exchange string to coinank enum
	var coinankExchange coinank_enum.Exchange
	switch strings.ToLower(exchange) {
	case "binance":
		coinankExchange = coinank_enum.Binance
	case "bybit":
		coinankExchange = coinank_enum.Bybit
	case "okx":
		coinankExchange = coinank_enum.Okex
	case "bitget":
		coinankExchange = coinank_enum.Bitget
	case "gate":
		coinankExchange = coinank_enum.Gate
	case "hyperliquid":
		coinankExchange = coinank_enum.Hyperliquid
	case "aster":
		coinankExchange = coinank_enum.Aster
	default:
		// Default to Binance for unknown exchanges
		coinankExchange = coinank_enum.Binance
	}

	// Call CoinAnk free/open API (no authentication required)
	ctx := context.Background()
	ts := time.Now().UnixMilli()
	// Use "To" side to search backward from current time (get historical klines)
	coinankKlines, err := coinank_api.Kline(ctx, symbol, coinankExchange, ts, coinank_enum.To, limit, coinankInterval)
	if err != nil {
		// If exchange-specific data fails, fallback to Binance
		if coinankExchange != coinank_enum.Binance {
			logger.Warnf("⚠️ CoinAnk %s data failed, falling back to Binance: %v", exchange, err)
			coinankKlines, err = coinank_api.Kline(ctx, symbol, coinank_enum.Binance, ts, coinank_enum.To, limit, coinankInterval)
			if err != nil {
				return nil, fmt.Errorf("CoinAnk API error (fallback): %w", err)
			}
		} else {
			return nil, fmt.Errorf("CoinAnk API error: %w", err)
		}
	}

	// Convert coinank kline format to market.Kline format
	klines := make([]Kline, len(coinankKlines))
	for i, ck := range coinankKlines {
		klines[i] = Kline{
			OpenTime:  ck.StartTime,
			Open:      ck.Open,
			High:      ck.High,
			Low:       ck.Low,
			Close:     ck.Close,
			Volume:    ck.Volume,
			CloseTime: ck.EndTime,
		}
	}

	return klines, nil
}

// getKlinesFromHyperliquid fetches kline data from Hyperliquid API for xyz dex assets
func getKlinesFromHyperliquid(symbol, interval string, limit int) ([]Kline, error) {
	// Remove xyz: prefix if present for the API call
	baseCoin := strings.TrimPrefix(symbol, "xyz:")

	// Map interval to Hyperliquid format
	hlInterval := hyperliquid.MapTimeframe(interval)

	// Create Hyperliquid client
	client := hyperliquid.NewClient()

	// Fetch candles
	ctx := context.Background()
	candles, err := client.GetCandles(ctx, baseCoin, hlInterval, limit)
	if err != nil {
		return nil, fmt.Errorf("Hyperliquid API error: %w", err)
	}

	// Convert to market.Kline format
	klines := make([]Kline, len(candles))
	for i, c := range candles {
		open, _ := strconv.ParseFloat(c.Open, 64)
		high, _ := strconv.ParseFloat(c.High, 64)
		low, _ := strconv.ParseFloat(c.Low, 64)
		closePrice, _ := strconv.ParseFloat(c.Close, 64)
		volume, _ := strconv.ParseFloat(c.Volume, 64)

		klines[i] = Kline{
			OpenTime:  c.OpenTime,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closePrice,
			Volume:    volume,
			CloseTime: c.CloseTime,
		}
	}

	return klines, nil
}

// Get retrieves market data for the specified token (uses Binance data by default)
func Get(symbol string) (*Data, error) {
	return GetWithExchange(symbol, "binance")
}

// GetWithExchange retrieves market data for the specified token using exchange-specific data
func GetWithExchange(symbol, exchange string) (*Data, error) {
	var klines3m, klines4h []Kline
	var err error
	// Normalize symbol
	symbol = Normalize(symbol)

	// Check if this is an xyz dex asset (use Hyperliquid API)
	isXyzAsset := IsXyzDexAsset(symbol)

	// For hyperliquid exchange, also use Hyperliquid API
	useHyperliquidAPI := isXyzAsset || strings.ToLower(exchange) == "hyperliquid"

	// Get 3-minute K-line data (or 5-minute for xyz assets as 3m may not be available)
	if useHyperliquidAPI {
		// Use Hyperliquid API for xyz dex assets (use 5m since 3m may not be available)
		klines3m, err = getKlinesFromHyperliquid(symbol, "5m", 100)
		if err != nil {
			return nil, fmt.Errorf("Failed to get 5-minute K-line from Hyperliquid: %v", err)
		}
	} else {
		// Use CoinAnk for regular crypto assets with exchange-specific data
		klines3m, err = getKlinesFromCoinAnk(symbol, "3m", exchange, 100)
		if err != nil {
			return nil, fmt.Errorf("Failed to get 3-minute K-line from CoinAnk (%s): %v", exchange, err)
		}
	}

	// Data staleness detection: Prevent DOGEUSDT-style price freeze issues
	if isStaleData(klines3m, symbol) {
		logger.Infof("⚠️  WARNING: %s detected stale data (consecutive price freeze), skipping symbol", symbol)
		return nil, fmt.Errorf("%s data is stale, possible cache failure", symbol)
	}

	// Get 4-hour K-line data
	if useHyperliquidAPI {
		klines4h, err = getKlinesFromHyperliquid(symbol, "4h", 100)
		if err != nil {
			return nil, fmt.Errorf("Failed to get 4-hour K-line from Hyperliquid: %v", err)
		}
	} else {
		klines4h, err = getKlinesFromCoinAnk(symbol, "4h", exchange, 100)
		if err != nil {
			return nil, fmt.Errorf("Failed to get 4-hour K-line from CoinAnk (%s): %v", exchange, err)
		}
	}

	// Check if data is empty
	if len(klines3m) == 0 {
		return nil, fmt.Errorf("3-minute K-line data is empty")
	}
	if len(klines4h) == 0 {
		return nil, fmt.Errorf("4-hour K-line data is empty")
	}

	// Calculate current indicators (based on 3-minute latest data)
	currentPrice := klines3m[len(klines3m)-1].Close
	currentEMA20 := calculateEMA(klines3m, 20)
	currentMACD := calculateMACD(klines3m)
	currentRSI7 := calculateRSI(klines3m, 7)

	// Calculate price change percentage
	// 1-hour price change = price from 20 3-minute K-lines ago
	priceChange1h := 0.0
	if len(klines3m) >= 21 { // Need at least 21 K-lines (current + 20 previous)
		price1hAgo := klines3m[len(klines3m)-21].Close
		if price1hAgo > 0 {
			priceChange1h = ((currentPrice - price1hAgo) / price1hAgo) * 100
		}
	}

	// 4-hour price change = price from 1 4-hour K-line ago
	priceChange4h := 0.0
	if len(klines4h) >= 2 {
		price4hAgo := klines4h[len(klines4h)-2].Close
		if price4hAgo > 0 {
			priceChange4h = ((currentPrice - price4hAgo) / price4hAgo) * 100
		}
	}

	// Get OI data (use same exchange as K-line source)
	oiData, err := getOpenInterestData(symbol, exchange)
	if err != nil {
		oiData = &OIData{Latest: 0, Average: 0}
	}

	// Get Funding Rate and next settlement time (same exchange)
	fundingRate, nextFundingMs, _ := getFundingRate(symbol, exchange)
	makerFeeRate, takerFeeRate, feeSource := getTradingFeeRates(symbol)

	// Calculate intraday series data
	intradayData := calculateIntradaySeries(klines3m)

	// Calculate longer-term data
	longerTermData := calculateLongerTermData(klines4h)

	return &Data{
		Symbol:             symbol,
		CurrentPrice:       currentPrice,
		PriceChange1h:     priceChange1h,
		PriceChange4h:     priceChange4h,
		CurrentEMA20:      currentEMA20,
		CurrentMACD:        currentMACD,
		CurrentRSI7:        currentRSI7,
		OpenInterest:       oiData,
		FundingRate:        fundingRate,
		NextFundingTimeMs:  nextFundingMs,
		MakerFeeRate:       makerFeeRate,
		TakerFeeRate:       takerFeeRate,
		FeeSource:          feeSource,
		IntradaySeries:     intradayData,
		LongerTermContext:  longerTermData,
	}, nil
}

// timeframeOrder defines order from shortest to longest (used to pick "shortest" TF for current price to reduce lag)
var timeframeOrder = []string{"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d", "3d", "1w"}

func shortestTimeframeInList(timeframes []string) string {
	for _, ordered := range timeframeOrder {
		for _, tf := range timeframes {
			if tf == ordered {
				return tf
			}
		}
	}
	return "" // fallback: use first in list
}

// GetWithTimeframes retrieves market data for specified multiple timeframes using the given exchange.
// exchange: "binance", "bybit", "okx", "hyperliquid", etc.; empty defaults to "binance". For xyz/hyperliquid assets, exchange is ignored and Hyperliquid API is used.
// timeframes: list of timeframes, e.g. ["5m", "15m", "1h", "4h"]
// primaryTimeframe: primary timeframe (used for calculating current indicators), defaults to timeframes[0]
// count: number of K-lines for each timeframe
// CurrentPrice is set from the shortest configured timeframe's last closed bar to minimize lag (e.g. 3m close when 3m and 5m are both configured).
func GetWithTimeframes(symbol string, exchange string, timeframes []string, primaryTimeframe string, count int) (*Data, error) {
	symbol = Normalize(symbol)
	ex := strings.ToLower(strings.TrimSpace(exchange))
	if ex == "" {
		ex = "binance"
	}

	if len(timeframes) == 0 {
		return nil, fmt.Errorf("at least one timeframe is required")
	}

	// If primary timeframe is not specified, use the first one
	if primaryTimeframe == "" {
		primaryTimeframe = timeframes[0]
	}

	// Ensure primary timeframe is in the list
	hasPrimary := false
	for _, tf := range timeframes {
		if tf == primaryTimeframe {
			hasPrimary = true
			break
		}
	}
	if !hasPrimary {
		timeframes = append([]string{primaryTimeframe}, timeframes...)
	}

	// Store data for all timeframes
	timeframeData := make(map[string]*TimeframeSeriesData)
	var primaryKlines []Kline

	isXyzAsset := IsXyzDexAsset(symbol)
	useHyperliquidAPI := isXyzAsset || ex == "hyperliquid"

	// Get K-line data for each timeframe (use exchange-specific source)
	for _, tf := range timeframes {
		var klines []Kline
		var err error

		if useHyperliquidAPI {
			klines, err = getKlinesFromHyperliquid(symbol, tf, 200)
			if err != nil {
				logger.Infof("⚠️ Failed to get %s %s K-line from Hyperliquid: %v", symbol, tf, err)
				continue
			}
		} else {
			klines, err = getKlinesFromCoinAnk(symbol, tf, ex, 200)
			if err != nil {
				logger.Infof("⚠️ Failed to get %s %s K-line from CoinAnk (%s): %v", symbol, tf, ex, err)
				continue
			}
		}

		if len(klines) == 0 {
			logger.Infof("⚠️ %s %s K-line data is empty", symbol, tf)
			continue
		}

		// Save primary timeframe K-lines for calculating base indicators
		if tf == primaryTimeframe {
			primaryKlines = klines
		}

		// Calculate series data for this timeframe (use count from config)
		seriesData := calculateTimeframeSeries(klines, tf, count)
		timeframeData[tf] = seriesData
	}

	// If primary timeframe data is empty, return error
	if len(primaryKlines) == 0 {
		return nil, fmt.Errorf("Primary timeframe %s K-line data is empty", primaryTimeframe)
	}

	// Data staleness detection
	if isStaleData(primaryKlines, symbol) {
		logger.Infof("⚠️  WARNING: %s detected stale data (consecutive price freeze), skipping symbol", symbol)
		return nil, fmt.Errorf("%s data is stale, possible cache failure", symbol)
	}

	// Current price: use shortest available timeframe's last closed bar to minimize lag (K-line APIs only return closed bars)
	availableTFs := make([]string, 0, len(timeframeData))
	for tf := range timeframeData {
		availableTFs = append(availableTFs, tf)
	}
	shortestTF := shortestTimeframeInList(availableTFs)
	currentPrice := primaryKlines[len(primaryKlines)-1].Close
	if shortestTF != "" {
		if sd, ok := timeframeData[shortestTF]; ok && len(sd.Klines) > 0 {
			currentPrice = sd.Klines[len(sd.Klines)-1].Close
		}
	}

	// Calculate current indicators (based on primary timeframe latest data)
	currentEMA20 := calculateEMA(primaryKlines, 20)
	currentMACD := calculateMACD(primaryKlines)
	currentRSI7 := calculateRSI(primaryKlines, 7)

	// Calculate price changes
	priceChange1h := calculatePriceChangeByBars(primaryKlines, primaryTimeframe, 60)  // 1 hour
	priceChange4h := calculatePriceChangeByBars(primaryKlines, primaryTimeframe, 240) // 4 hours

	// Get OI and Funding from same exchange (hyperliquid has no public OI/funding in this form; use binance as fallback for crypto)
	oiExchange := ex
	if useHyperliquidAPI {
		oiExchange = "binance"
	}
	oiData, err := getOpenInterestData(symbol, oiExchange)
	if err != nil {
		oiData = &OIData{Latest: 0, Average: 0}
	}
	var fundingRate float64
	var nextFundingMs int64
	if useHyperliquidAPI {
		fundingRate, nextFundingMs, _ = getFundingRate(symbol, "binance")
	} else {
		fundingRate, nextFundingMs, _ = getFundingRate(symbol, ex)
	}
	makerFeeRate, takerFeeRate, feeSource := getTradingFeeRates(symbol)

	return &Data{
		Symbol:             symbol,
		CurrentPrice:       currentPrice,
		PriceChange1h:      priceChange1h,
		PriceChange4h:      priceChange4h,
		CurrentEMA20:       currentEMA20,
		CurrentMACD:        currentMACD,
		CurrentRSI7:        currentRSI7,
		OpenInterest:       oiData,
		FundingRate:        fundingRate,
		NextFundingTimeMs:  nextFundingMs,
		MakerFeeRate:       makerFeeRate,
		TakerFeeRate:       takerFeeRate,
		FeeSource:          feeSource,
		TimeframeData:      timeframeData,
	}, nil
}

// calculateTimeframeSeries calculates series data for a single timeframe
func calculateTimeframeSeries(klines []Kline, timeframe string, count int) *TimeframeSeriesData {
	if count <= 0 {
		count = 10 // default
	}

	data := &TimeframeSeriesData{
		Timeframe:   timeframe,
		Klines:      make([]KlineBar, 0, count),
		MidPrices:   make([]float64, 0, count),
		EMA20Values: make([]float64, 0, count),
		EMA50Values: make([]float64, 0, count),
		MACDValues:  make([]float64, 0, count),
		RSI7Values:  make([]float64, 0, count),
		RSI14Values: make([]float64, 0, count),
		Volume:      make([]float64, 0, count),
		BOLLUpper:   make([]float64, 0, count),
		BOLLMiddle:  make([]float64, 0, count),
		BOLLLower:   make([]float64, 0, count),
	}

	// Get latest N data points based on count from config
	start := len(klines) - count
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		// Store full OHLCV kline data with taker buy ratio
		takerBuyRatio := 0.0
		if klines[i].Volume > 0 && klines[i].TakerBuyBaseVolume > 0 {
			takerBuyRatio = klines[i].TakerBuyBaseVolume / klines[i].Volume
		}
		data.Klines = append(data.Klines, KlineBar{
			Time:          klines[i].OpenTime,
			Open:          klines[i].Open,
			High:          klines[i].High,
			Low:           klines[i].Low,
			Close:         klines[i].Close,
			Volume:        klines[i].Volume,
			TakerBuyRatio: takerBuyRatio,
		})

		// Keep MidPrices and Volume for backward compatibility
		data.MidPrices = append(data.MidPrices, klines[i].Close)
		data.Volume = append(data.Volume, klines[i].Volume)

		// Calculate EMA20 for each point
		if i >= 19 {
			ema20 := calculateEMA(klines[:i+1], 20)
			data.EMA20Values = append(data.EMA20Values, ema20)
		}

		// Calculate EMA50 for each point
		if i >= 49 {
			ema50 := calculateEMA(klines[:i+1], 50)
			data.EMA50Values = append(data.EMA50Values, ema50)
		}

		// Calculate MACD for each point
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}

		// Calculate RSI for each point
		if i >= 7 {
			rsi7 := calculateRSI(klines[:i+1], 7)
			data.RSI7Values = append(data.RSI7Values, rsi7)
		}
		if i >= 14 {
			rsi14 := calculateRSI(klines[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}

		// Calculate Bollinger Bands (period 20, std dev multiplier 2)
		if i >= 19 {
			upper, middle, lower := calculateBOLL(klines[:i+1], 20, 2.0)
			data.BOLLUpper = append(data.BOLLUpper, upper)
			data.BOLLMiddle = append(data.BOLLMiddle, middle)
			data.BOLLLower = append(data.BOLLLower, lower)
		}
	}

	// Calculate ATR14
	data.ATR14 = calculateATR(klines, 14)

	// Calculate ATR14 percentile over recent history (rolling ATR values)
	data.ATR14Percentile = calculateATRPercentile(klines, 14, 100)

	return data
}

// calculatePriceChangeByBars calculates how many K-lines to look back for price change based on timeframe
func calculatePriceChangeByBars(klines []Kline, timeframe string, targetMinutes int) float64 {
	if len(klines) < 2 {
		return 0
	}

	// Parse timeframe to minutes
	tfMinutes := parseTimeframeToMinutes(timeframe)
	if tfMinutes <= 0 {
		return 0
	}

	// Calculate how many K-lines to look back
	barsBack := targetMinutes / tfMinutes
	if barsBack < 1 {
		barsBack = 1
	}

	currentPrice := klines[len(klines)-1].Close
	idx := len(klines) - 1 - barsBack
	if idx < 0 {
		idx = 0
	}

	oldPrice := klines[idx].Close
	if oldPrice > 0 {
		return ((currentPrice - oldPrice) / oldPrice) * 100
	}
	return 0
}

// parseTimeframeToMinutes parses timeframe string to minutes
func parseTimeframeToMinutes(tf string) int {
	switch tf {
	case "1m":
		return 1
	case "3m":
		return 3
	case "5m":
		return 5
	case "15m":
		return 15
	case "30m":
		return 30
	case "1h":
		return 60
	case "2h":
		return 120
	case "4h":
		return 240
	case "6h":
		return 360
	case "8h":
		return 480
	case "12h":
		return 720
	case "1d":
		return 1440
	case "3d":
		return 4320
	case "1w":
		return 10080
	default:
		return 0
	}
}

// calculateEMA calculates EMA
func calculateEMA(klines []Kline, period int) float64 {
	if len(klines) < period {
		return 0
	}

	// Calculate SMA as initial EMA
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += klines[i].Close
	}
	ema := sum / float64(period)

	// Calculate EMA
	multiplier := 2.0 / float64(period+1)
	for i := period; i < len(klines); i++ {
		ema = (klines[i].Close-ema)*multiplier + ema
	}

	return ema
}

// calculateMACD calculates MACD
func calculateMACD(klines []Kline) float64 {
	if len(klines) < 26 {
		return 0
	}

	// Calculate 12-period and 26-period EMA
	ema12 := calculateEMA(klines, 12)
	ema26 := calculateEMA(klines, 26)

	// MACD = EMA12 - EMA26
	return ema12 - ema26
}

// calculateRSI calculates RSI
func calculateRSI(klines []Kline, period int) float64 {
	if len(klines) <= period {
		return 0
	}

	gains := 0.0
	losses := 0.0

	// Calculate initial average gain/loss
	for i := 1; i <= period; i++ {
		change := klines[i].Close - klines[i-1].Close
		if change > 0 {
			gains += change
		} else {
			losses += -change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	// Use Wilder smoothing method to calculate subsequent RSI
	for i := period + 1; i < len(klines); i++ {
		change := klines[i].Close - klines[i-1].Close
		if change > 0 {
			avgGain = (avgGain*float64(period-1) + change) / float64(period)
			avgLoss = (avgLoss * float64(period-1)) / float64(period)
		} else {
			avgGain = (avgGain * float64(period-1)) / float64(period)
			avgLoss = (avgLoss*float64(period-1) + (-change)) / float64(period)
		}
	}

	if avgLoss == 0 {
		return 100
	}

	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))

	return rsi
}

// calculateATR calculates ATR
func calculateATR(klines []Kline, period int) float64 {
	if len(klines) <= period {
		return 0
	}

	trs := make([]float64, len(klines))
	for i := 1; i < len(klines); i++ {
		high := klines[i].High
		low := klines[i].Low
		prevClose := klines[i-1].Close

		tr1 := high - low
		tr2 := math.Abs(high - prevClose)
		tr3 := math.Abs(low - prevClose)

		trs[i] = math.Max(tr1, math.Max(tr2, tr3))
	}

	// Calculate initial ATR
	sum := 0.0
	for i := 1; i <= period; i++ {
		sum += trs[i]
	}
	atr := sum / float64(period)

	// Wilder smoothing
	for i := period + 1; i < len(klines); i++ {
		atr = (atr*float64(period-1) + trs[i]) / float64(period)
	}

	return atr
}

// calculateATRPercentile calculates the percentile rank of current ATR14 over the last `window` bars.
// Returns 0-100: 90 means current ATR is higher than 90% of recent ATR values (high volatility).
func calculateATRPercentile(klines []Kline, period int, window int) float64 {
	n := len(klines)
	if n <= period+window {
		return 50 // not enough data, return neutral
	}

	currentATR := calculateATR(klines, period)
	if currentATR <= 0 {
		return 50
	}

	count := 0
	below := 0
	for end := n - 1; end >= period+1 && count < window; end-- {
		atr := calculateATR(klines[:end+1], period)
		if atr > 0 {
			count++
			if atr < currentATR {
				below++
			}
		}
	}
	if count == 0 {
		return 50
	}
	return float64(below) / float64(count) * 100
}

// calculateBOLL calculates Bollinger Bands (upper, middle, lower)
// period: typically 20, multiplier: typically 2
func calculateBOLL(klines []Kline, period int, multiplier float64) (upper, middle, lower float64) {
	if len(klines) < period {
		return 0, 0, 0
	}

	// Calculate SMA (middle band)
	sum := 0.0
	for i := len(klines) - period; i < len(klines); i++ {
		sum += klines[i].Close
	}
	sma := sum / float64(period)

	// Calculate standard deviation
	variance := 0.0
	for i := len(klines) - period; i < len(klines); i++ {
		diff := klines[i].Close - sma
		variance += diff * diff
	}
	stdDev := math.Sqrt(variance / float64(period))

	// Calculate bands
	middle = sma
	upper = sma + multiplier*stdDev
	lower = sma - multiplier*stdDev

	return upper, middle, lower
}

// calculateIntradaySeries calculates intraday series data
func calculateIntradaySeries(klines []Kline) *IntradayData {
	data := &IntradayData{
		MidPrices:   make([]float64, 0, 10),
		EMA20Values: make([]float64, 0, 10),
		MACDValues:  make([]float64, 0, 10),
		RSI7Values:  make([]float64, 0, 10),
		RSI14Values: make([]float64, 0, 10),
		Volume:      make([]float64, 0, 10),
	}

	// Get latest 10 data points
	start := len(klines) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		data.MidPrices = append(data.MidPrices, klines[i].Close)
		data.Volume = append(data.Volume, klines[i].Volume)

		// Calculate EMA20 for each point
		if i >= 19 {
			ema20 := calculateEMA(klines[:i+1], 20)
			data.EMA20Values = append(data.EMA20Values, ema20)
		}

		// Calculate MACD for each point
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}

		// Calculate RSI for each point
		if i >= 7 {
			rsi7 := calculateRSI(klines[:i+1], 7)
			data.RSI7Values = append(data.RSI7Values, rsi7)
		}
		if i >= 14 {
			rsi14 := calculateRSI(klines[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}
	}

	// Calculate 3m ATR14
	data.ATR14 = calculateATR(klines, 14)

	return data
}

// calculateLongerTermData calculates longer-term data
func calculateLongerTermData(klines []Kline) *LongerTermData {
	data := &LongerTermData{
		MACDValues:  make([]float64, 0, 10),
		RSI14Values: make([]float64, 0, 10),
	}

	// Calculate EMA
	data.EMA20 = calculateEMA(klines, 20)
	data.EMA50 = calculateEMA(klines, 50)

	// Calculate ATR
	data.ATR3 = calculateATR(klines, 3)
	data.ATR14 = calculateATR(klines, 14)

	// Calculate volume
	if len(klines) > 0 {
		data.CurrentVolume = klines[len(klines)-1].Volume
		// Calculate average volume
		sum := 0.0
		for _, k := range klines {
			sum += k.Volume
		}
		data.AverageVolume = sum / float64(len(klines))
	}

	// Calculate MACD and RSI series
	start := len(klines) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}
		if i >= 14 {
			rsi14 := calculateRSI(klines[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}
	}

	return data
}

// getOpenInterestData retrieves OI data from the specified exchange (binance, bybit, okx). Empty exchange defaults to binance.
func getOpenInterestData(symbol string, exchange string) (*OIData, error) {
	ex := strings.ToLower(strings.TrimSpace(exchange))
	if ex == "" {
		ex = "binance"
	}
	switch ex {
	case "bybit":
		return getOpenInterestDataBybit(symbol)
	case "okx":
		return getOpenInterestDataOKX(symbol)
	case "binance":
		fallthrough
	default:
		return getOpenInterestDataBinance(symbol)
	}
}

func getOpenInterestDataBinance(symbol string) (*OIData, error) {
	url := fmt.Sprintf("https://fapi.binance.com/fapi/v1/openInterest?symbol=%s", symbol)
	apiClient := NewAPIClient()
	resp, err := apiClient.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var result struct {
		OpenInterest string `json:"openInterest"`
		Symbol       string `json:"symbol"`
		Time         int64  `json:"time"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	oi, _ := strconv.ParseFloat(result.OpenInterest, 64)
	return &OIData{Latest: oi, Average: oi * 0.999}, nil
}

func getOpenInterestDataBybit(symbol string) (*OIData, error) {
	url := fmt.Sprintf("https://api.bybit.com/v5/market/tickers?category=linear&symbol=%s", symbol)
	apiClient := NewAPIClient()
	resp, err := apiClient.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var result struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List []struct {
				OpenInterest string `json:"openInterest"`
			} `json:"list"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.RetCode != 0 || len(result.Result.List) == 0 {
		return nil, fmt.Errorf("bybit OI: no data")
	}
	oi, _ := strconv.ParseFloat(result.Result.List[0].OpenInterest, 64)
	return &OIData{Latest: oi, Average: oi * 0.999}, nil
}

func getOpenInterestDataOKX(symbol string) (*OIData, error) {
	// OKX instId format: BTC-USDT-SWAP
	base := strings.ReplaceAll(symbol, "USDT", "")
	instId := base + "-USDT-SWAP"
	url := fmt.Sprintf("https://www.okx.com/api/v5/public/open-interest?instId=%s", instId)
	apiClient := NewAPIClient()
	resp, err := apiClient.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var result struct {
		Code string `json:"code"`
		Data []struct {
			Oi   string `json:"oi"`
			Inst string `json:"instId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Code != "0" || len(result.Data) == 0 {
		return nil, fmt.Errorf("okx OI: no data")
	}
	oi, _ := strconv.ParseFloat(result.Data[0].Oi, 64)
	return &OIData{Latest: oi, Average: oi * 0.999}, nil
}

// GetFundingRate returns the current funding rate and next funding time for the symbol on the given exchange (binance, bybit, okx). Used e.g. to estimate funding fee when closing a position. Empty exchange defaults to binance. Cached per exchange+symbol.
func GetFundingRate(symbol string, exchange string) (rate float64, nextFundingTimeMs int64, err error) {
	return getFundingRate(Normalize(symbol), exchange)
}

// getFundingRate retrieves funding rate and next funding time from the specified exchange (binance, bybit, okx). Empty exchange defaults to binance. Uses 1-hour cache per exchange+symbol.
func getFundingRate(symbol string, exchange string) (rate float64, nextFundingTimeMs int64, err error) {
	ex := strings.ToLower(strings.TrimSpace(exchange))
	if ex == "" {
		ex = "binance"
	}
	cacheKey := ex + ":" + symbol
	if cached, ok := fundingRateMap.Load(cacheKey); ok {
		cache := cached.(*FundingRateCache)
		if time.Since(cache.UpdatedAt) < frCacheTTL {
			return cache.Rate, cache.NextFundingTimeMs, nil
		}
	}

	switch ex {
	case "bybit":
		rate, nextFundingTimeMs, err = getFundingRateBybit(symbol)
	case "okx":
		rate, nextFundingTimeMs, err = getFundingRateOKX(symbol)
	case "binance":
		fallthrough
	default:
		rate, nextFundingTimeMs, err = getFundingRateBinance(symbol)
	}
	if err != nil {
		return 0, 0, err
	}
	fundingRateMap.Store(cacheKey, &FundingRateCache{Rate: rate, NextFundingTimeMs: nextFundingTimeMs, UpdatedAt: time.Now()})
	return rate, nextFundingTimeMs, nil
}

func getFundingRateBinance(symbol string) (float64, int64, error) {
	url := fmt.Sprintf("https://fapi.binance.com/fapi/v1/premiumIndex?symbol=%s", symbol)
	apiClient := NewAPIClient()
	resp, err := apiClient.client.Get(url)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, err
	}
	var result struct {
		LastFundingRate   string `json:"lastFundingRate"`
		NextFundingTime   int64  `json:"nextFundingTime"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, 0, err
	}
	rate, _ := strconv.ParseFloat(result.LastFundingRate, 64)
	return rate, result.NextFundingTime, nil
}

func getFundingRateBybit(symbol string) (float64, int64, error) {
	url := fmt.Sprintf("https://api.bybit.com/v5/market/tickers?category=linear&symbol=%s", symbol)
	apiClient := NewAPIClient()
	resp, err := apiClient.client.Get(url)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, err
	}
	var result struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List []struct {
				FundingRate     string `json:"fundingRate"`
				NextFundingTime string `json:"nextFundingTime"`
			} `json:"list"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, 0, err
	}
	if result.RetCode != 0 || len(result.Result.List) == 0 {
		return 0, 0, fmt.Errorf("bybit funding: no data")
	}
	item := result.Result.List[0]
	rate, _ := strconv.ParseFloat(item.FundingRate, 64)
	nextMs := int64(0)
	if item.NextFundingTime != "" {
		nextMs, _ = strconv.ParseInt(item.NextFundingTime, 10, 64)
	}
	return rate, nextMs, nil
}

func getFundingRateOKX(symbol string) (float64, int64, error) {
	base := strings.ReplaceAll(symbol, "USDT", "")
	instId := base + "-USDT-SWAP"
	url := fmt.Sprintf("https://www.okx.com/api/v5/public/funding-rate?instId=%s", instId)
	apiClient := NewAPIClient()
	resp, err := apiClient.client.Get(url)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, err
	}
	var result struct {
		Code string `json:"code"`
		Data []struct {
			FundingRate   string `json:"fundingRate"`
			NextFundingTime string `json:"nextFundingTime"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, 0, err
	}
	if result.Code != "0" || len(result.Data) == 0 {
		return 0, 0, fmt.Errorf("okx funding: no data")
	}
	item := result.Data[0]
	rate, _ := strconv.ParseFloat(item.FundingRate, 64)
	nextMs := int64(0)
	if item.NextFundingTime != "" {
		nextMs, _ = strconv.ParseInt(item.NextFundingTime, 10, 64)
	}
	return rate, nextMs, nil
}

// ExchangeCredentials holds API credentials for fetching trading fees from exchange
type ExchangeCredentials struct {
	ExchangeType string // "binance", "bybit", "okx", etc.
	APIKey       string
	SecretKey    string
	Passphrase   string // OKX 必填；其他交易所忽略
}

// getTradingFeeRates is the internal function that uses default credentials (env vars)
func getTradingFeeRates(symbol string) (float64, float64, string) {
	return getTradingFeeRatesWithCredentials(symbol, nil)
}

// getTradingFeeRatesWithCredentials fetches trading fee rates with optional exchange credentials
// If credentials is nil or empty, falls back to environment variables or default values
func getTradingFeeRatesWithCredentials(symbol string, credentials *ExchangeCredentials) (float64, float64, string) {
	// Generate cache key based on symbol and exchange type
	cacheKey := symbol
	if credentials != nil && credentials.ExchangeType != "" {
		cacheKey = fmt.Sprintf("%s:%s", credentials.ExchangeType, symbol)
	}

	// Check cache
	if cached, ok := tradingFeeMap.Load(cacheKey); ok {
		cache := cached.(*TradingFeeRateCache)
		if time.Since(cache.UpdatedAt) < feeCacheTTL {
			return cache.MakerRate, cache.TakerRate, cache.Source
		}
	}

	// Determine API credentials to use
	var apiKey, apiSecret, exchangeType, passphrase string
	if credentials != nil && credentials.APIKey != "" && credentials.SecretKey != "" {
		apiKey = credentials.APIKey
		apiSecret = credentials.SecretKey
		exchangeType = credentials.ExchangeType
		passphrase = credentials.Passphrase
	} else {
		// Fallback to environment variables
		apiKey = strings.TrimSpace(os.Getenv("BINANCE_API_KEY"))
		apiSecret = strings.TrimSpace(os.Getenv("BINANCE_API_SECRET"))
		exchangeType = "binance"
	}

	// If no credentials available, use defaults
	if apiKey == "" || apiSecret == "" {
		makerRate, takerRate, source := defaultFeeRates(symbol)
		tradingFeeMap.Store(cacheKey, &TradingFeeRateCache{
			MakerRate: makerRate,
			TakerRate: takerRate,
			Source:    source,
			UpdatedAt: time.Now(),
		})
		return makerRate, takerRate, source
	}

	// Fetch from exchange based on type
	var makerRate, takerRate float64
	var err error

	switch exchangeType {
	case "binance":
		makerRate, takerRate, err = fetchBinanceCommissionRate(symbol, apiKey, apiSecret)
	case "bybit":
		makerRate, takerRate, err = fetchBybitCommissionRate(symbol, apiKey, apiSecret)
	case "okx":
		makerRate, takerRate, err = fetchOKXCommissionRate(symbol, apiKey, apiSecret, passphrase)
	default:
		// For unsupported exchanges, use defaults
		makerRate, takerRate, source := defaultFeeRates(symbol)
		tradingFeeMap.Store(cacheKey, &TradingFeeRateCache{
			MakerRate: makerRate,
			TakerRate: takerRate,
			Source:    source,
			UpdatedAt: time.Now(),
		})
		return makerRate, takerRate, source
	}

	if err != nil {
		logger.Warnf("Failed to fetch commission rate for %s from %s, using defaults: %v", symbol, exchangeType, err)
		makerRate, takerRate, source := defaultFeeRates(symbol)
		tradingFeeMap.Store(cacheKey, &TradingFeeRateCache{
			MakerRate: makerRate,
			TakerRate: takerRate,
			Source:    source,
			UpdatedAt: time.Now(),
		})
		return makerRate, takerRate, source
	}

	tradingFeeMap.Store(cacheKey, &TradingFeeRateCache{
		MakerRate: makerRate,
		TakerRate: takerRate,
		Source:    exchangeType,
		UpdatedAt: time.Now(),
	})

	return makerRate, takerRate, exchangeType
}

// FetchTradingFeeRates is the public function to fetch trading fees with exchange credentials
// This allows external packages (like AutoTrader) to pass exchange credentials from database
func FetchTradingFeeRates(symbol string, credentials *ExchangeCredentials) (makerRate, takerRate float64, source string) {
	return getTradingFeeRatesWithCredentials(Normalize(symbol), credentials)
}

func defaultFeeRates(symbol string) (float64, float64, string) {
	base := strings.ToUpper(symbol)
	if strings.HasPrefix(base, "BTC") || strings.HasPrefix(base, "ETH") || strings.HasPrefix(base, "BNB") {
		return 0.0002, 0.0004, "default"
	}
	return 0.0004, 0.0005, "default"
}

func fetchBinanceCommissionRate(symbol, apiKey, apiSecret string) (float64, float64, error) {
	timestamp := time.Now().UnixMilli()
	query := url.Values{}
	query.Set("symbol", symbol)
	query.Set("timestamp", strconv.FormatInt(timestamp, 10))
	// Add recvWindow to handle time drift (5000ms = 5 seconds tolerance)
	query.Set("recvWindow", "5000")

	// Sign the query string BEFORE adding signature
	queryString := query.Encode()
	signature := signQuery(queryString, apiSecret)
	query.Set("signature", signature)

	endpoint := fmt.Sprintf("https://fapi.binance.com/fapi/v1/commissionRate?%s", query.Encode())
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("X-MBX-APIKEY", apiKey)

	apiClient := NewAPIClient()
	resp, err := apiClient.client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		// Log detailed error for debugging signature issues
		var binanceError struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if json.Unmarshal(body, &binanceError) == nil {
			logger.Warnf("Binance commission rate API error: code=%d, msg=%s, symbol=%s, timestamp=%d",
				binanceError.Code, binanceError.Msg, symbol, timestamp)
		}
		return 0, 0, fmt.Errorf("commission rate api status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		MakerCommissionRate string `json:"makerCommissionRate"`
		TakerCommissionRate string `json:"takerCommissionRate"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, 0, err
	}

	makerRate, _ := strconv.ParseFloat(result.MakerCommissionRate, 64)
	takerRate, _ := strconv.ParseFloat(result.TakerCommissionRate, 64)
	return makerRate, takerRate, nil
}

func fetchBybitCommissionRate(symbol, apiKey, apiSecret string) (float64, float64, error) {
	queryParams := url.Values{}
	queryParams.Set("category", "linear")
	queryParams.Set("symbol", symbol)
	queryString := queryParams.Encode()
	urlStr := "https://api.bybit.com/v5/account/fee-rate?" + queryString

	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	recvWindow := "5000"
	signPayload := timestamp + apiKey + recvWindow + queryString
	h := hmac.New(sha256.New, []byte(apiSecret))
	h.Write([]byte(signPayload))
	signature := hex.EncodeToString(h.Sum(nil))

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("X-BAPI-API-KEY", apiKey)
	req.Header.Set("X-BAPI-SIGN", signature)
	req.Header.Set("X-BAPI-SIGN-TYPE", "2")
	req.Header.Set("X-BAPI-TIMESTAMP", timestamp)
	req.Header.Set("X-BAPI-RECV-WINDOW", recvWindow)

	apiClient := NewAPIClient()
	resp, err := apiClient.client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("bybit fee-rate api status %d: %s", resp.StatusCode, string(body))
	}
	var result struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List []struct {
				MakerFeeRate string `json:"makerFeeRate"`
				TakerFeeRate string `json:"takerFeeRate"`
			} `json:"list"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, 0, err
	}
	if result.RetCode != 0 || len(result.Result.List) == 0 {
		return 0, 0, fmt.Errorf("bybit fee-rate: no data")
	}
	makerRate, _ := strconv.ParseFloat(result.Result.List[0].MakerFeeRate, 64)
	takerRate, _ := strconv.ParseFloat(result.Result.List[0].TakerFeeRate, 64)
	return makerRate, takerRate, nil
}

func fetchOKXCommissionRate(symbol, apiKey, apiSecret, passphrase string) (float64, float64, error) {
	// GET /api/v5/account/trade-fee?instType=SWAP
	path := "/api/v5/account/trade-fee?instType=SWAP"
	urlStr := "https://www.okx.com" + path

	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	preHash := timestamp + "GET" + path + ""
	h := hmac.New(sha256.New, []byte(apiSecret))
	h.Write([]byte(preHash))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("OK-ACCESS-KEY", apiKey)
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", passphrase)
	req.Header.Set("Content-Type", "application/json")

	apiClient := NewAPIClient()
	resp, err := apiClient.client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("okx trade-fee api status %d: %s", resp.StatusCode, string(body))
	}
	var result struct {
		Code string `json:"code"`
		Data []struct {
			MakerU string `json:"makerU"`
			TakerU string `json:"takerU"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, 0, err
	}
	if result.Code != "0" || len(result.Data) == 0 {
		return 0, 0, fmt.Errorf("okx trade-fee: no data")
	}
	makerRate, _ := strconv.ParseFloat(result.Data[0].MakerU, 64)
	takerRate, _ := strconv.ParseFloat(result.Data[0].TakerU, 64)
	return makerRate, takerRate, nil
}

func signQuery(query, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(query))
	return hex.EncodeToString(mac.Sum(nil))
}

// Format formats and outputs market data
func Format(data *Data) string {
	var sb strings.Builder

	// Format price with dynamic precision
	priceStr := FormatPriceWithDynamicPrecision(data.CurrentPrice)
	sb.WriteString(fmt.Sprintf("current_price = %s, current_ema20 = %.3f, current_macd = %.3f, current_rsi (7 period) = %.3f\n\n",
		priceStr, data.CurrentEMA20, data.CurrentMACD, data.CurrentRSI7))

	sb.WriteString(fmt.Sprintf("In addition, here is the latest %s open interest and funding rate for perps:\n\n",
		data.Symbol))

	if data.OpenInterest != nil {
		// Format OI data with dynamic precision
		oiLatestStr := FormatPriceWithDynamicPrecision(data.OpenInterest.Latest)
		oiAverageStr := FormatPriceWithDynamicPrecision(data.OpenInterest.Average)
		sb.WriteString(fmt.Sprintf("Open Interest: Latest: %s Average: %s\n\n",
			oiLatestStr, oiAverageStr))
	}

	sb.WriteString(fmt.Sprintf("Funding Rate: %.2e\n\n", data.FundingRate))

	if data.IntradaySeries != nil {
		sb.WriteString("Intraday series (3‑minute intervals, oldest → latest):\n\n")

		if len(data.IntradaySeries.MidPrices) > 0 {
			sb.WriteString(fmt.Sprintf("Mid prices: %s\n\n", formatFloatSlice(data.IntradaySeries.MidPrices)))
		}

		if len(data.IntradaySeries.EMA20Values) > 0 {
			sb.WriteString(fmt.Sprintf("EMA indicators (20‑period): %s\n\n", formatFloatSlice(data.IntradaySeries.EMA20Values)))
		}

		if len(data.IntradaySeries.MACDValues) > 0 {
			sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.IntradaySeries.MACDValues)))
		}

		if len(data.IntradaySeries.RSI7Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (7‑Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI7Values)))
		}

		if len(data.IntradaySeries.RSI14Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (14‑Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI14Values)))
		}

		if len(data.IntradaySeries.Volume) > 0 {
			sb.WriteString(fmt.Sprintf("Volume: %s\n\n", formatFloatSlice(data.IntradaySeries.Volume)))
		}

		sb.WriteString(fmt.Sprintf("3m ATR (14‑period): %.3f\n\n", data.IntradaySeries.ATR14))
	}

	if data.LongerTermContext != nil {
		sb.WriteString("Longer‑term context (4‑hour timeframe):\n\n")

		sb.WriteString(fmt.Sprintf("20‑Period EMA: %.3f vs. 50‑Period EMA: %.3f\n\n",
			data.LongerTermContext.EMA20, data.LongerTermContext.EMA50))

		sb.WriteString(fmt.Sprintf("3‑Period ATR: %.3f vs. 14‑Period ATR: %.3f\n\n",
			data.LongerTermContext.ATR3, data.LongerTermContext.ATR14))

		sb.WriteString(fmt.Sprintf("Current Volume: %.3f vs. Average Volume: %.3f\n\n",
			data.LongerTermContext.CurrentVolume, data.LongerTermContext.AverageVolume))

		if len(data.LongerTermContext.MACDValues) > 0 {
			sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.LongerTermContext.MACDValues)))
		}

		if len(data.LongerTermContext.RSI14Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (14‑Period): %s\n\n", formatFloatSlice(data.LongerTermContext.RSI14Values)))
		}
	}

	// Multi-timeframe data (new)
	if len(data.TimeframeData) > 0 {
		// Output sorted by timeframe
		timeframeOrder := []string{"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d", "3d", "1w"}
		for _, tf := range timeframeOrder {
			if tfData, ok := data.TimeframeData[tf]; ok {
				sb.WriteString(fmt.Sprintf("=== %s Timeframe ===\n\n", strings.ToUpper(tf)))
				formatTimeframeData(&sb, tfData)
			}
		}
	}

	return sb.String()
}

// formatTimeframeData formats data for a single timeframe
func formatTimeframeData(sb *strings.Builder, data *TimeframeSeriesData) {
	// Use OHLCV table format if kline data is available
	if len(data.Klines) > 0 {
		sb.WriteString("Time(UTC)      Open      High      Low       Close     Volume\n")
		for i, k := range data.Klines {
			t := time.Unix(k.Time/1000, 0).UTC()
			timeStr := t.Format("01-02 15:04")
			marker := ""
			if i == len(data.Klines)-1 {
				marker = "  <- current"
			}
			sb.WriteString(fmt.Sprintf("%-14s %-9.4f %-9.4f %-9.4f %-9.4f %-12.2f%s\n",
				timeStr, k.Open, k.High, k.Low, k.Close, k.Volume, marker))
		}
		sb.WriteString("\n")
	} else if len(data.MidPrices) > 0 {
		// Fallback to old format for backward compatibility
		sb.WriteString(fmt.Sprintf("Mid prices: %s\n\n", formatFloatSlice(data.MidPrices)))
		if len(data.Volume) > 0 {
			sb.WriteString(fmt.Sprintf("Volume: %s\n\n", formatFloatSlice(data.Volume)))
		}
	}

	// Technical indicators
	if len(data.EMA20Values) > 0 {
		sb.WriteString(fmt.Sprintf("EMA20: %s\n", formatFloatSlice(data.EMA20Values)))
	}

	if len(data.EMA50Values) > 0 {
		sb.WriteString(fmt.Sprintf("EMA50: %s\n", formatFloatSlice(data.EMA50Values)))
	}

	if len(data.MACDValues) > 0 {
		sb.WriteString(fmt.Sprintf("MACD: %s\n", formatFloatSlice(data.MACDValues)))
	}

	if len(data.RSI7Values) > 0 {
		sb.WriteString(fmt.Sprintf("RSI7: %s\n", formatFloatSlice(data.RSI7Values)))
	}

	if len(data.RSI14Values) > 0 {
		sb.WriteString(fmt.Sprintf("RSI14: %s\n", formatFloatSlice(data.RSI14Values)))
	}

	if data.ATR14 > 0 {
		sb.WriteString(fmt.Sprintf("ATR14: %.4f\n", data.ATR14))
	}

	sb.WriteString("\n")
}

// FormatPriceWithDynamicPrecision dynamically selects precision based on price range
// This perfectly supports all coins from ultra-low price meme coins (< 0.0001) to BTC/ETH
// Exported for use by notification package
func FormatPriceWithDynamicPrecision(price float64) string {
	switch {
	case price < 0.0001:
		// Ultra-low price meme coins: 1000SATS, 1000WHY, DOGS
		// 0.00002070 → "0.00002070" (8 decimal places)
		return fmt.Sprintf("%.8f", price)
	case price < 0.001:
		// Low price meme coins: NEIRO, HMSTR, HOT, NOT
		// 0.00015060 → "0.000151" (6 decimal places)
		return fmt.Sprintf("%.6f", price)
	case price < 0.01:
		// Mid-low price coins: PEPE, SHIB, MEME
		// 0.00556800 → "0.005568" (6 decimal places)
		return fmt.Sprintf("%.6f", price)
	case price < 1.0:
		// Low price coins: ASTER, DOGE, ADA, TRX
		// 0.9954 → "0.9954" (4 decimal places)
		return fmt.Sprintf("%.4f", price)
	case price < 100:
		// Mid price coins: SOL, AVAX, LINK, MATIC
		// 23.4567 → "23.4567" (4 decimal places)
		return fmt.Sprintf("%.4f", price)
	default:
		// High price coins: BTC, ETH (save tokens)
		// 45678.9123 → "45678.91" (2 decimal places)
		return fmt.Sprintf("%.2f", price)
	}
}

// formatFloatSlice formats float64 slice to string (using dynamic precision)
func formatFloatSlice(values []float64) string {
	strValues := make([]string, len(values))
	for i, v := range values {
		strValues[i] = FormatPriceWithDynamicPrecision(v)
	}
	return "[" + strings.Join(strValues, ", ") + "]"
}

// xyz dex assets that should NOT get USDT suffix
var xyzDexAssets = map[string]bool{
	// Stocks
	"TSLA": true, "NVDA": true, "AAPL": true, "MSFT": true, "META": true,
	"AMZN": true, "GOOGL": true, "AMD": true, "COIN": true, "NFLX": true,
	"PLTR": true, "HOOD": true, "INTC": true, "MSTR": true, "TSM": true,
	"ORCL": true, "MU": true, "RIVN": true, "COST": true, "LLY": true,
	"CRCL": true, "SKHX": true, "SNDK": true,
	// Forex
	"EUR": true, "JPY": true,
	// Commodities
	"GOLD": true, "SILVER": true,
	// Index
	"XYZ100": true,
}

// IsXyzDexAsset checks if a symbol is an xyz dex asset
func IsXyzDexAsset(symbol string) bool {
	base := strings.ToUpper(symbol)
	// Remove any prefix/suffix
	base = strings.TrimPrefix(base, "XYZ:")
	for _, suffix := range []string{"USDT", "USD", "-USDC"} {
		if strings.HasSuffix(base, suffix) {
			base = strings.TrimSuffix(base, suffix)
			break
		}
	}
	return xyzDexAssets[base]
}

// Normalize normalizes symbol
// For crypto: ensures it's a USDT trading pair
// For xyz dex assets (stocks, forex, commodities): uses xyz: prefix without USDT suffix
func Normalize(symbol string) string {
	symbol = strings.ToUpper(symbol)

	// Check if this is an xyz dex asset
	if IsXyzDexAsset(symbol) {
		// Remove any xyz: prefix (case-insensitive) and USDT suffix, then add xyz: prefix
		base := symbol
		// Handle both lowercase and uppercase xyz: prefix
		if strings.HasPrefix(strings.ToLower(base), "xyz:") {
			base = base[4:] // Remove first 4 characters ("xyz:")
		}
		for _, suffix := range []string{"USDT", "USD", "-USDC"} {
			if strings.HasSuffix(base, suffix) {
				base = strings.TrimSuffix(base, suffix)
				break
			}
		}
		return "xyz:" + base
	}

	// Remove exchange-specific separators (Gate uses BTC_USDT, OKX uses BTC-USDT-SWAP)
	symbol = strings.ReplaceAll(symbol, "_", "")
	symbol = strings.ReplaceAll(symbol, "-SWAP", "")
	symbol = strings.ReplaceAll(symbol, "-", "")

	// For regular crypto assets
	if strings.HasSuffix(symbol, "USDT") {
		return symbol
	}
	return symbol + "USDT"
}

// parseFloat parses float value
func parseFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case string:
		return strconv.ParseFloat(val, 64)
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	default:
		return 0, fmt.Errorf("unsupported type: %T", v)
	}
}

// BuildDataFromKlines constructs market data snapshot from preloaded K-line series (for backtesting/simulation).
func BuildDataFromKlines(symbol string, primary []Kline, longer []Kline) (*Data, error) {
	if len(primary) == 0 {
		return nil, fmt.Errorf("primary series is empty")
	}

	symbol = Normalize(symbol)
	current := primary[len(primary)-1]
	currentPrice := current.Close

	data := &Data{
		Symbol:            symbol,
		CurrentPrice:      currentPrice,
		CurrentEMA20:      calculateEMA(primary, 20),
		CurrentMACD:       calculateMACD(primary),
		CurrentRSI7:       calculateRSI(primary, 7),
		PriceChange1h:     priceChangeFromSeries(primary, time.Hour),
		PriceChange4h:     priceChangeFromSeries(primary, 4*time.Hour),
		OpenInterest:      &OIData{Latest: 0, Average: 0},
		FundingRate:       0,
		IntradaySeries:    calculateIntradaySeries(primary),
		LongerTermContext: nil,
	}

	if len(longer) > 0 {
		data.LongerTermContext = calculateLongerTermData(longer)
	}

	return data, nil
}

func priceChangeFromSeries(series []Kline, duration time.Duration) float64 {
	if len(series) == 0 || duration <= 0 {
		return 0
	}
	last := series[len(series)-1]
	target := last.CloseTime - duration.Milliseconds()
	for i := len(series) - 1; i >= 0; i-- {
		if series[i].CloseTime <= target {
			price := series[i].Close
			if price > 0 {
				return ((last.Close - price) / price) * 100
			}
			break
		}
	}
	return 0
}

// isStaleData detects stale data (consecutive price freeze)
// Fix DOGEUSDT-style issue: consecutive N periods with completely unchanged prices indicate data source anomaly
func isStaleData(klines []Kline, symbol string) bool {
	if len(klines) < 5 {
		return false // Insufficient data to determine
	}

	// Detection threshold: 5 consecutive 3-minute periods with unchanged price (15 minutes without fluctuation)
	const stalePriceThreshold = 5
	const priceTolerancePct = 0.0001 // 0.01% fluctuation tolerance (avoid false positives)

	// Take the last stalePriceThreshold K-lines
	recentKlines := klines[len(klines)-stalePriceThreshold:]
	firstPrice := recentKlines[0].Close

	// Check if all prices are within tolerance
	for i := 1; i < len(recentKlines); i++ {
		priceDiff := math.Abs(recentKlines[i].Close-firstPrice) / firstPrice
		if priceDiff > priceTolerancePct {
			return false // Price fluctuation exists, data is normal
		}
	}

	// Additional check: MACD and volume
	// If price is unchanged but MACD/volume shows normal fluctuation, it might be a real market situation (extremely low volatility)
	// Check if volume is also 0 (data completely frozen)
	allVolumeZero := true
	for _, k := range recentKlines {
		if k.Volume > 0 {
			allVolumeZero = false
			break
		}
	}

	if allVolumeZero {
		logger.Infof("⚠️  %s stale data confirmed: price freeze + zero volume", symbol)
		return true
	}

	// Price frozen but has volume: might be extremely low volatility market, allow but log warning
	logger.Infof("⚠️  %s detected extreme price stability (no fluctuation for %d consecutive periods), but volume is normal", symbol, stalePriceThreshold)
	return false
}

// ========== 导出的指标计算函数（供测试使用） ==========

// ExportCalculateEMA exports calculateEMA for testing
func ExportCalculateEMA(klines []Kline, period int) float64 {
	return calculateEMA(klines, period)
}

// ExportCalculateMACD exports calculateMACD for testing
func ExportCalculateMACD(klines []Kline) float64 {
	return calculateMACD(klines)
}

// ExportCalculateRSI exports calculateRSI for testing
func ExportCalculateRSI(klines []Kline, period int) float64 {
	return calculateRSI(klines, period)
}

// ExportCalculateATR exports calculateATR for testing
func ExportCalculateATR(klines []Kline, period int) float64 {
	return calculateATR(klines, period)
}

// ExportCalculateBOLL exports calculateBOLL for testing
func ExportCalculateBOLL(klines []Kline, period int, multiplier float64) (upper, middle, lower float64) {
	return calculateBOLL(klines, period, multiplier)
}

// calculateDonchian calculates Donchian channel (highest high, lowest low) for given period
func calculateDonchian(klines []Kline, period int) (upper, lower float64) {
	if len(klines) == 0 || period <= 0 {
		return 0, 0
	}

	// Use all available klines if period > len(klines)
	start := len(klines) - period
	if start < 0 {
		start = 0
	}

	upper = klines[start].High
	lower = klines[start].Low

	for i := start + 1; i < len(klines); i++ {
		if klines[i].High > upper {
			upper = klines[i].High
		}
		if klines[i].Low < lower {
			lower = klines[i].Low
		}
	}

	return upper, lower
}

// ExportCalculateDonchian exports calculateDonchian for testing
func ExportCalculateDonchian(klines []Kline, period int) (float64, float64) {
	return calculateDonchian(klines, period)
}

// Box period constants (in 1h candles)
const (
	ShortBoxPeriod = 72  // 3 days of 1h candles
	MidBoxPeriod   = 240 // 10 days of 1h candles
	LongBoxPeriod  = 500 // ~21 days of 1h candles
)

// calculateBoxData calculates multi-period box data from klines
func calculateBoxData(klines []Kline, currentPrice float64) *BoxData {
	box := &BoxData{
		CurrentPrice: currentPrice,
	}

	if len(klines) == 0 {
		return box
	}

	box.ShortUpper, box.ShortLower = calculateDonchian(klines, ShortBoxPeriod)
	box.MidUpper, box.MidLower = calculateDonchian(klines, MidBoxPeriod)
	box.LongUpper, box.LongLower = calculateDonchian(klines, LongBoxPeriod)

	return box
}

// ExportCalculateBoxData exports calculateBoxData for testing
func ExportCalculateBoxData(klines []Kline, currentPrice float64) *BoxData {
	return calculateBoxData(klines, currentPrice)
}

// GetBoxData fetches 1h klines and calculates box data for a symbol
func GetBoxData(symbol string) (*BoxData, error) {
	symbol = Normalize(symbol)

	// Fetch 500 1h klines
	var klines []Kline
	var err error

	if IsXyzDexAsset(symbol) {
		klines, err = getKlinesFromHyperliquid(symbol, "1h", LongBoxPeriod)
	} else {
		klines, err = getKlinesFromCoinAnk(symbol, "1h", "binance", LongBoxPeriod)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get 1h klines: %w", err)
	}

	if len(klines) == 0 {
		return nil, fmt.Errorf("no kline data available")
	}

	currentPrice := klines[len(klines)-1].Close

	return calculateBoxData(klines, currentPrice), nil
}
