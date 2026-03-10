import { useEffect, useRef, useState } from 'react'
import {
  createChart,
  IChartApi,
  ISeriesApi,
  Time,
  UTCTimestamp,
  CandlestickSeries,
  LineSeries,
  HistogramSeries,
  createSeriesMarkers,
} from 'lightweight-charts'
import { useLanguage } from '../contexts/LanguageContext'
import { useTheme } from '../contexts/ThemeContext'
import { httpClient } from '../lib/httpClient'
import {
  calculateSMA,
  calculateEMA,
  calculateBollingerBands,
  type Kline,
} from '../utils/indicators'
import { Settings, BarChart2 } from 'lucide-react'

// 订单接口定义（含启发式成对序号与持仓开仓标记）
interface OrderMarker {
  time: number
  price: number
  side: 'long' | 'short'
  rawSide: string
  action: 'open' | 'close'
  pnl?: number
  symbol: string
  /** 启发式成对序号：同一对开/平相同 */
  pairIndex?: number
  /** 来自当前持仓的开仓标记（无订单时间，用最后一根 K 线时间） */
  fromPosition?: boolean
}

// 挂单接口定义 (交易所的止盈止损订单)
interface OpenOrder {
  order_id: string
  symbol: string
  side: string          // BUY/SELL
  position_side: string // LONG/SHORT
  type: string          // LIMIT/STOP_MARKET/TAKE_PROFIT_MARKET
  price: number         // 限价单价格
  stop_price: number    // 触发价格 (止损/止盈)
  quantity: number
  status: string
}

import { Position } from '../types'

interface AdvancedChartProps {
  symbol: string
  interval?: string
  traderID?: string
  height?: number
  exchange?: string // 交易所类型：binance, bybit, okx, bitget, hyperliquid, aster, lighter
  onSymbolChange?: (symbol: string) => void // 币种切换回调
  positions?: Position[] // 持仓数据，用于显示止盈止损价格线
}

// 指标配置
interface IndicatorConfig {
  id: string
  name: string
  enabled: boolean
  color: string
  params?: any
}

// 获取成交额货币单位
const getQuoteUnit = (exchange: string): string => {
  if (['alpaca'].includes(exchange)) {
    return 'USD'
  }
  if (['forex', 'metals'].includes(exchange)) {
    return '' // 外汇/贵金属没有真实成交量
  }
  return 'USDT' // 加密货币默认 USDT
}

// 获取成交量数量单位
const getBaseUnit = (exchange: string, symbol: string): string => {
  if (['alpaca'].includes(exchange)) {
    return '股'
  }
  if (['forex', 'metals'].includes(exchange)) {
    return ''
  }
  // 加密货币：从 symbol 提取基础资产
  const base = symbol.replace(/USDT$|USD$|BUSD$/, '')
  return base || '个'
}

// 格式化大数字
const formatVolume = (value: number): string => {
  if (value >= 1e9) return (value / 1e9).toFixed(2) + 'B'
  if (value >= 1e6) return (value / 1e6).toFixed(2) + 'M'
  if (value >= 1e3) return (value / 1e3).toFixed(2) + 'K'
  return value.toFixed(2)
}

// Format price with dynamic precision based on price range
// Matches backend FormatPriceWithDynamicPrecision logic
const formatPriceWithDynamicPrecision = (price: number): string => {
  if (!price || price === 0) return '0'
  
  if (price < 0.0001) {
    // Ultra-low price meme coins: 1000SATS, 1000WHY, DOGS
    // 0.00002070 → "0.00002070" (8 decimal places)
    return price.toFixed(8)
  } else if (price < 0.001) {
    // Low price meme coins: NEIRO, HMSTR, HOT, NOT
    // 0.00015060 → "0.000151" (6 decimal places)
    return price.toFixed(6)
  } else if (price < 0.01) {
    // Mid-low price coins: PEPE, SHIB, MEME
    // 0.00556800 → "0.005568" (6 decimal places)
    return price.toFixed(6)
  } else if (price < 1.0) {
    // Low price coins: ASTER, DOGE, ADA, TRX
    // 0.9954 → "0.9954" (4 decimal places)
    return price.toFixed(4)
  } else if (price < 100) {
    // Mid price coins: SOL, AVAX, LINK, MATIC
    // 23.4567 → "23.4567" (4 decimal places)
    return price.toFixed(4)
  } else {
    // High price coins: BTC, ETH (save tokens)
    // 45678.9123 → "45678.91" (2 decimal places)
    return price.toFixed(2)
  }
}

export function AdvancedChart({
  symbol = 'BTCUSDT',
  interval = '5m',
  traderID,
  height = 550,
  exchange = 'binance', // 默认使用 binance
  onSymbolChange: _onSymbolChange, // Available for future use
  positions, // 持仓数据
}: AdvancedChartProps) {
  void _onSymbolChange // Prevent unused warning
  const { language } = useLanguage()
  const { theme } = useTheme()
  const isDark = theme === 'dark'
  const quoteUnit = getQuoteUnit(exchange)
  
  // 根据主题获取图表颜色
  const getChartColors = () => {
    return {
      background: isDark ? '#0B0E11' : '#FFFFFF',
      textColor: isDark ? '#B7BDC6' : '#111827',
      gridColor: isDark ? 'rgba(43, 49, 57, 0.2)' : 'rgba(0, 0, 0, 0.1)',
      borderColor: isDark ? '#2B3139' : 'rgba(0, 0, 0, 0.15)',
      crosshairColor: isDark ? 'rgba(240, 185, 11, 0.5)' : 'rgba(240, 185, 11, 0.7)',
    }
  }
  
  const chartColors = getChartColors()
  const baseUnit = getBaseUnit(exchange, symbol)
  const chartContainerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)
  const candlestickSeriesRef = useRef<ISeriesApi<'Candlestick'> | null>(null)
  const volumeSeriesRef = useRef<ISeriesApi<'Histogram'> | null>(null)
  const indicatorSeriesRef = useRef<Map<string, ISeriesApi<any>>>(new Map())
  const seriesMarkersRef = useRef<any>(null) // Markers primitive for v5
  const currentMarkersDataRef = useRef<any[]>([]) // 存储当前的标记数据
  const currentOrderPointsRef = useRef<Array<{ time: number; price: number; pairIndex?: number; pnl?: number }>>([]) // 每个 marker 对应的时间/价格/配对序号/盈亏，用于悬停检测与连线
  const pairLineSeriesRef = useRef<ISeriesApi<'Line'> | null>(null) // 成对买卖点连线
  const lastHoveredPairRef = useRef<number | null | undefined>(undefined) // 上次悬停的 pairIndex，undefined 表示未初始化
  const [hoveredPairPnl, setHoveredPairPnl] = useState<number | null>(null) // 悬停成对连线时显示的盈亏
  const setHoveredPairPnlRef = useRef(setHoveredPairPnl)
  setHoveredPairPnlRef.current = setHoveredPairPnl
  const klineDataRef = useRef<Map<number, { volume: number; quoteVolume: number }>>(new Map()) // 存储 kline 额外数据
  const currentKlineDataRef = useRef<Kline[]>([]) // 存储当前的完整 K 线数据，用于指标更新
  const priceLinesRef = useRef<any[]>([]) // 存储挂单价格线

  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showIndicatorPanel, setShowIndicatorPanel] = useState(false)
  const [showOrderMarkers, setShowOrderMarkers] = useState(true) // 订单标记显示开关，默认显示
  const showOrderMarkersRef = useRef(showOrderMarkers)
  showOrderMarkersRef.current = showOrderMarkers
  const isInitialLoadRef = useRef(true) // 跟踪是否为初始加载
  const [tooltipData, setTooltipData] = useState<any>(null)
  const tooltipRef = useRef<HTMLDivElement>(null)

  // 行情统计数据（当前K线）
  const [marketStats, setMarketStats] = useState<{
    price: number
    priceChange: number
    priceChangePercent: number
    high: number
    low: number
    volume: number      // 数量（BTC/股数）
    quoteVolume: number // 成交额（USDT/USD）
  } | null>(null)

  // 指标配置
  const [indicators, setIndicators] = useState<IndicatorConfig[]>([
    { id: 'volume', name: 'Volume', enabled: true, color: '#3B82F6' },
    { id: 'ma5', name: 'MA5', enabled: false, color: '#FF6B6B', params: { period: 5 } },
    { id: 'ma10', name: 'MA10', enabled: false, color: '#4ECDC4', params: { period: 10 } },
    { id: 'ma20', name: 'MA20', enabled: false, color: '#FFD93D', params: { period: 20 } },
    { id: 'ma60', name: 'MA60', enabled: false, color: '#95E1D3', params: { period: 60 } },
    { id: 'ema12', name: 'EMA12', enabled: false, color: '#A8E6CF', params: { period: 12 } },
    { id: 'ema26', name: 'EMA26', enabled: false, color: '#FFD3B6', params: { period: 26 } },
    { id: 'bb', name: 'Bollinger Bands', enabled: false, color: '#9B59B6' },
  ])

  // 从服务获取K线数据
  const fetchKlineData = async (symbol: string, interval: string) => {
    try {
      const limit = 1500
      const klineUrl = `/api/klines?symbol=${symbol}&interval=${interval}&limit=${limit}&exchange=${exchange}`
      const result = await httpClient.get(klineUrl)

      if (!result.success || !result.data) {
        throw new Error('Failed to fetch kline data')
      }

      // 转换数据格式
      const rawData = result.data.map((candle: any) => ({
        time: Math.floor(candle.openTime / 1000) as UTCTimestamp,
        open: candle.open,
        high: candle.high,
        low: candle.low,
        close: candle.close,
        volume: candle.volume,           // 数量（BTC/股数）
        quoteVolume: candle.quoteVolume, // 成交额（USDT/USD）
      }))

      // 按时间排序并去重（lightweight-charts 要求数据按时间升序且无重复）
      const sortedData = rawData.sort((a: any, b: any) => a.time - b.time)
      const dedupedData = sortedData.filter((item: any, index: number, arr: any[]) =>
        index === 0 || item.time !== arr[index - 1].time
      )

      if (rawData.length !== dedupedData.length) {
        console.warn('[AdvancedChart] Removed', rawData.length - dedupedData.length, 'duplicate klines')
      }

      return dedupedData
    } catch (err) {
      console.error('[AdvancedChart] Error fetching kline:', err)
      throw err
    }
  }

  // 解析时间：支持 Unix 时间戳（数字）或字符串格式
  const parseCustomTime = (time: any): number => {
    if (!time) {
      console.warn('[AdvancedChart] Empty time value')
      return 0
    }

    // 如果已经是数字（Unix 时间戳）
    if (typeof time === 'number') {
      // 判断是毫秒还是秒：如果大于 10^12 则认为是毫秒（2001年之后的毫秒时间戳）
      if (time > 1000000000000) {
        const seconds = Math.floor(time / 1000)
        console.log('[AdvancedChart] ✅ Unix timestamp (ms→s):', time, '→', seconds, '(', new Date(time).toISOString(), ')')
        return seconds
      }
      console.log('[AdvancedChart] ✅ Unix timestamp (s):', time, '(', new Date(time * 1000).toISOString(), ')')
      return time
    }

    const timeStr = String(time)
    console.log('[AdvancedChart] Parsing time string:', timeStr)

    // 尝试标准ISO格式
    const isoTime = new Date(timeStr).getTime()
    if (!isNaN(isoTime) && isoTime > 0) {
      const timestamp = Math.floor(isoTime / 1000)
      console.log('[AdvancedChart] ✅ Parsed as ISO:', timeStr, '→', timestamp, '(', new Date(timestamp * 1000).toISOString(), ')')
      return timestamp
    }

    // 解析自定义格式 "MM-DD HH:mm UTC" (兼容旧数据)
    const match = timeStr.match(/(\d{2})-(\d{2})\s+(\d{2}):(\d{2})\s+UTC/)
    if (match) {
      const currentYear = new Date().getFullYear()
      const [_, month, day, hour, minute] = match
      const date = new Date(Date.UTC(
        currentYear,
        parseInt(month) - 1,
        parseInt(day),
        parseInt(hour),
        parseInt(minute)
      ))
      const timestamp = Math.floor(date.getTime() / 1000)
      console.log('[AdvancedChart] ✅ Parsed as custom format:', timeStr, '→', timestamp, '(', new Date(timestamp * 1000).toISOString(), ')')
      return timestamp
    }

    console.error('[AdvancedChart] ❌ Failed to parse time:', timeStr)
    return 0
  }

  // 获取订单数据
  const fetchOrders = async (traderID: string, symbol: string): Promise<OrderMarker[]> => {
    try {
      console.log('[AdvancedChart] Fetching orders for trader:', traderID, 'symbol:', symbol)
      // 获取已成交的订单，增加到200条以显示更多历史订单
      const result = await httpClient.get(`/api/orders?trader_id=${traderID}&symbol=${symbol}&status=FILLED&limit=200`)

      console.log('[AdvancedChart] Orders API response:', result)

      if (!result.success || !result.data) {
        console.warn('[AdvancedChart] No orders found, result:', result)
        return []
      }

      const orders = result.data
      console.log('[AdvancedChart] Raw orders data:', orders)
      const markers: OrderMarker[] = []

      orders.forEach((order: any) => {
        console.log('[AdvancedChart] Processing order:', order)

        // 处理字段名：支持PascalCase和snake_case
        const filledAt = order.filled_at || order.FilledAt || order.created_at || order.CreatedAt
        const avgPrice = order.avg_fill_price || order.AvgFillPrice || order.price || order.Price
        const orderAction = order.order_action || order.OrderAction
        const side = (order.side || order.Side)?.toLowerCase() // BUY/SELL
        const symbol = order.symbol || order.Symbol
        const closePosition = order.close_position ?? order.ClosePosition ?? false
        const reduceOnly = order.reduce_only ?? order.ReduceOnly ?? false
        const positionSideFromOrder = (order.position_side || order.PositionSide)?.toLowerCase()

        // 跳过没有成交时间或价格的订单
        if (!filledAt || !avgPrice || avgPrice === 0) {
          console.warn('[AdvancedChart] Skipping order - missing data:', { filledAt, avgPrice })
          return
        }

        const timeSeconds = parseCustomTime(filledAt)
        if (timeSeconds === 0) {
          console.warn('[AdvancedChart] Skipping order - invalid time:', filledAt)
          return
        }

        // 根据 order_action / close_position / reduce_only 判断开仓还是平仓（兼容大小写与 snake_case）
        let action: 'open' | 'close' = 'open'
        let positionSide: 'long' | 'short' = 'long'
        const actionUpper = typeof orderAction === 'string' ? orderAction.toUpperCase() : ''

        if (actionUpper) {
          if (actionUpper.includes('OPEN')) {
            action = 'open'
            positionSide = actionUpper.includes('LONG') ? 'long' : 'short'
          } else if (actionUpper.includes('CLOSE')) {
            action = 'close'
            positionSide = actionUpper.includes('LONG') ? 'long' : 'short'
          }
        }
        if (action === 'open' && (closePosition || reduceOnly)) {
          action = 'close'
          if (positionSideFromOrder === 'long' || positionSideFromOrder === 'short') {
            positionSide = positionSideFromOrder as 'long' | 'short'
          } else {
            positionSide = side === 'buy' ? 'long' : 'short'
          }
        }
        if (positionSide === 'long' && positionSideFromOrder === 'short') positionSide = 'short'
        if (positionSide === 'short' && positionSideFromOrder === 'long') positionSide = 'long'
        if (!actionUpper && action === 'open') positionSide = side === 'buy' ? 'long' : 'short'

        const pnl = order.realized_pnl ?? order.realizedPnl ?? order.realized_pnl_pct

        markers.push({
          time: timeSeconds,
          price: avgPrice,
          side: positionSide,
          rawSide: side,
          action,
          pnl: typeof pnl === 'number' ? pnl : undefined,
          symbol,
        })
      })

      // 启发式成对：按时间 FIFO，同一 symbol+side 的开与平配对，分配 pairIndex
      markers.sort((a, b) => a.time - b.time)
      const openStack: { long: number[]; short: number[] } = { long: [], short: [] }
      let nextPairIndex = 1
      for (let i = 0; i < markers.length; i++) {
        const m = markers[i]
        if (m.action === 'open') {
          openStack[m.side].push(i)
          // 先不分配，等平仓时一起分配
        } else {
          const stack = openStack[m.side]
          if (stack.length > 0) {
            const openIdx = stack.shift()!
            const pairId = nextPairIndex++
            markers[openIdx].pairIndex = pairId
            m.pairIndex = pairId
          } else {
            m.pairIndex = nextPairIndex++
          }
        }
      }
      for (let i = 0; i < markers.length; i++) {
        if (markers[i].action === 'open' && markers[i].pairIndex == null) {
          markers[i].pairIndex = nextPairIndex++
        }
      }

      console.log('[AdvancedChart] Final markers (with pairIndex):', markers)
      return markers
    } catch (err) {
      console.error('[AdvancedChart] Error fetching orders:', err)
      return []
    }
  }

  // 获取交易所挂单 (止盈止损订单)
  const fetchOpenOrders = async (traderID: string, symbol: string): Promise<OpenOrder[]> => {
    try {
      console.log('[AdvancedChart] Fetching open orders for trader:', traderID, 'symbol:', symbol)
      const result = await httpClient.get(`/api/open-orders?trader_id=${traderID}&symbol=${symbol}`)

      console.log('[AdvancedChart] Open orders API response:', result)

      if (!result.success || !result.data) {
        console.warn('[AdvancedChart] No open orders found')
        return []
      }

      // 后端直接返回 OpenOrder[] 数组，httpClient 会包装为 {success: true, data: OpenOrder[]}
      return result.data as OpenOrder[]
    } catch (err) {
      console.error('[AdvancedChart] Error fetching open orders:', err)
      return []
    }
  }

  // 当主题变化时更新图表颜色
  useEffect(() => {
    if (chartRef.current) {
      const colors = getChartColors()
      chartRef.current.applyOptions({
        layout: {
          background: { color: colors.background },
          textColor: colors.textColor,
        },
        grid: {
          vertLines: { color: colors.gridColor },
          horzLines: { color: colors.gridColor },
        },
        crosshair: {
          vertLine: { color: colors.crosshairColor },
          horzLine: { color: colors.crosshairColor },
        },
        rightPriceScale: {
          borderColor: colors.borderColor,
        },
        timeScale: {
          borderColor: colors.borderColor,
        },
      })
    }
  }, [theme])

  // 初始化图表
  useEffect(() => {
    if (!chartContainerRef.current) return

    const chart = createChart(chartContainerRef.current, {
      width: chartContainerRef.current.clientWidth || 800,
      height: chartContainerRef.current.clientHeight || height,
      layout: {
        background: { color: chartColors.background },
        textColor: chartColors.textColor,
        fontSize: 12,
      },
      grid: {
        vertLines: {
          color: chartColors.gridColor,
          style: 1,
          visible: true,
        },
        horzLines: {
          color: chartColors.gridColor,
          style: 1,
          visible: true,
        },
      },
      crosshair: {
        mode: 1,
        vertLine: {
          color: chartColors.crosshairColor,
          width: 1,
          style: 2,
          labelBackgroundColor: '#F0B90B',
        },
        horzLine: {
          color: chartColors.crosshairColor,
          width: 1,
          style: 2,
          labelBackgroundColor: '#F0B90B',
        },
      },
      rightPriceScale: {
        borderColor: chartColors.borderColor,
        scaleMargins: {
          top: 0.1,
          bottom: 0.25,
        },
        borderVisible: true,
        entireTextOnly: false,
      },
      timeScale: {
        borderColor: chartColors.borderColor,
        timeVisible: true,
        secondsVisible: false,
        borderVisible: true,
        rightOffset: 5,
        barSpacing: 8,
      },
      handleScroll: {
        mouseWheel: true,
        pressedMouseMove: true,
        horzTouchDrag: true,
        vertTouchDrag: true,
      },
      handleScale: {
        axisPressedMouseMove: true,
        mouseWheel: true,
        pinch: true,
      },
      localization: {
        timeFormatter: (time: number) => {
          const date = new Date(time * 1000)
          return date.toLocaleString('zh-CN', {
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit',
            hour12: false,
          })
        },
      },
    })

    chartRef.current = chart

    // 创建K线系列
    const candlestickSeries = chart.addSeries(CandlestickSeries, {
      upColor: '#0ECB81',
      downColor: '#F6465D',
      borderUpColor: '#0ECB81',
      borderDownColor: '#F6465D',
      wickUpColor: '#0ECB81',
      wickDownColor: '#F6465D',
    })
    candlestickSeriesRef.current = candlestickSeries as any

    // 创建成交量系列
    const volumeSeries = chart.addSeries(HistogramSeries, {
      color: '#26a69a',
      priceFormat: {
        type: 'volume',
      },
      priceScaleId: '',
      lastValueVisible: false,
      priceLineVisible: false,
    })
    volumeSeriesRef.current = volumeSeries as any

    // 成对买卖点连线（悬停时显示）
    const pairLineSeries = chart.addSeries(LineSeries, {
      color: 'rgba(240, 185, 11, 0.75)',
      lineWidth: 2,
      lastValueVisible: false,
      priceLineVisible: false,
      crosshairMarkerVisible: false,
    })
    pairLineSeriesRef.current = pairLineSeries as any

    // 响应式调整 (ResizeObserver)
    const resizeObserver = new ResizeObserver((entries) => {
      if (entries.length === 0 || !entries[0].contentRect) return
      const { width, height } = entries[0].contentRect
      chart.applyOptions({ width, height })
    })

    if (chartContainerRef.current) {
      resizeObserver.observe(chartContainerRef.current)
    }

    // 监听鼠标移动，显示 OHLC 信息
    chart.subscribeCrosshairMove((param) => {
      if (!param.time || !param.point || !candlestickSeriesRef.current) {
        setTooltipData(null)
        return
      }

      const data = param.seriesData.get(candlestickSeriesRef.current as any)
      if (!data) {
        setTooltipData(null)
        return
      }

      const candleData = data as any

      // 从存储的数据中获取 volume 和 quoteVolume
      const klineExtra = klineDataRef.current.get(param.time as number) || { volume: 0, quoteVolume: 0 }

      setTooltipData({
        time: param.time,
        open: candleData.open,
        high: candleData.high,
        low: candleData.low,
        close: candleData.close,
        volume: klineExtra.volume,
        quoteVolume: klineExtra.quoteVolume,
        x: param.point.x,
        y: param.point.y,
      })

      // 悬停检测：找到最近的买卖点，高亮并连线
      const cursorTime = param.time as number
      let cursorPrice: number
      try {
        cursorPrice = (candlestickSeriesRef.current as any).coordinateToPrice(param.point.y)
      } catch {
        applyMarkerHover(null)
        return
      }
      const points = currentOrderPointsRef.current
      if (points.length === 0) {
        applyMarkerHover(null)
        return
      }
      let bestIdx = -1
      let bestDist = Infinity
      for (let i = 0; i < points.length; i++) {
        const p = points[i]
        const timeDist = Math.abs(cursorTime - p.time)
        const priceDist = p.price !== 0 ? Math.abs((cursorPrice - p.price) / p.price) : 1
        const dist = timeDist / 60 + priceDist * 500
        if (dist < bestDist) {
          bestDist = dist
          bestIdx = i
        }
      }
      const hoverThreshold = 20
      if (bestIdx >= 0 && bestDist < hoverThreshold) {
        applyMarkerHover(points[bestIdx].pairIndex)
      } else {
        applyMarkerHover(null)
      }
    })

    function applyMarkerHover(hoveredPairIndex: number | null | undefined) {
      if (lastHoveredPairRef.current === hoveredPairIndex) return
      lastHoveredPairRef.current = hoveredPairIndex

      const baseMarkers = currentMarkersDataRef.current
      const points = currentOrderPointsRef.current
      const showMarkers = showOrderMarkersRef.current

      if (!seriesMarkersRef.current || baseMarkers.length === 0) {
        if (pairLineSeriesRef.current) pairLineSeriesRef.current.setData([])
        setHoveredPairPnlRef.current(null)
        return
      }

      if (hoveredPairIndex == null) {
        seriesMarkersRef.current.setMarkers(showMarkers ? baseMarkers : [])
        if (pairLineSeriesRef.current) pairLineSeriesRef.current.setData([])
        setHoveredPairPnlRef.current(null)
        return
      }

      const highlighted = baseMarkers.map((m, i) => {
        const pt = points[i]
        const isInPair = pt && pt.pairIndex === hoveredPairIndex
        return { ...m, size: isInPair ? 1.6 : 1 }
      })
      seriesMarkersRef.current.setMarkers(showMarkers ? highlighted : [])

      const pairPoints = points.filter((p) => p.pairIndex === hoveredPairIndex)
      if (pairLineSeriesRef.current && pairPoints.length >= 2) {
        const sorted = [...pairPoints].sort((a, b) => a.time - b.time)
        pairLineSeriesRef.current.setData(
          sorted.map((p) => ({ time: p.time as Time, value: p.price }))
        )
        const closePoint = sorted[sorted.length - 1]
        const pnl = closePoint.pnl != null ? closePoint.pnl : null
        setHoveredPairPnlRef.current(pnl)
      } else {
        if (pairLineSeriesRef.current) pairLineSeriesRef.current.setData([])
        setHoveredPairPnlRef.current(null)
      }
    }

    const chartEl = chartContainerRef.current
    const onChartMouseLeave = () => applyMarkerHover(null)
    chartEl?.addEventListener('mouseleave', onChartMouseLeave)

    return () => {
      chartEl?.removeEventListener('mouseleave', onChartMouseLeave)
      resizeObserver.disconnect()
      chart.remove()
    }
  }, []) // Chart is created once, ResizeObserver handles dimension changes


  // 加载数据和指标
  useEffect(() => {
    // 当 symbol 或 interval 改变时，重置初始加载标志（以便自动适配新数据）
    isInitialLoadRef.current = true

    // 清除旧的标记数据，避免旧数据影响新图表
    currentMarkersDataRef.current = []
    if (seriesMarkersRef.current) {
      try {
        seriesMarkersRef.current.setMarkers([])
      } catch (e) {
        // 忽略错误，稍后会重新创建
      }
      seriesMarkersRef.current = null
    }

    const loadData = async (isRefresh = false) => {
      if (!candlestickSeriesRef.current) return

      console.log('[AdvancedChart] Loading data for', symbol, interval, isRefresh ? '(refresh)' : '')
      // 只在首次加载时显示 loading，刷新时不显示避免闪烁
      if (!isRefresh) {
        setLoading(true)
      }
      setError(null)

      try {
        // 1. 获取K线数据
        const klineData = await fetchKlineData(symbol, interval)
        console.log('[AdvancedChart] Loaded', klineData.length, 'klines')
        candlestickSeriesRef.current.setData(klineData)

        // 存储 volume/quoteVolume 数据供 tooltip 使用
        klineDataRef.current.clear()
        klineData.forEach((k: any) => {
          klineDataRef.current.set(k.time, { volume: k.volume || 0, quoteVolume: k.quoteVolume || 0 })
        })

        // 1.5 计算行情统计数据
        if (klineData.length > 1) {
          const latestKline = klineData[klineData.length - 1]
          const prevKline = klineData[klineData.length - 2]

          // 涨跌幅：当前K线收盘价 vs 前一根K线收盘价
          const priceChange = latestKline.close - prevKline.close
          const priceChangePercent = (priceChange / prevKline.close) * 100

          setMarketStats({
            price: latestKline.close,
            priceChange,
            priceChangePercent,
            high: latestKline.high,
            low: latestKline.low,
            volume: latestKline.volume || 0,
            quoteVolume: latestKline.quoteVolume || 0,
          })
        } else if (klineData.length === 1) {
          const latestKline = klineData[0]
          setMarketStats({
            price: latestKline.close,
            priceChange: 0,
            priceChangePercent: 0,
            high: latestKline.high,
            low: latestKline.low,
            volume: latestKline.volume || 0,
            quoteVolume: latestKline.quoteVolume || 0,
          })
        }

        // 保存 K 线数据供指标更新使用
        currentKlineDataRef.current = klineData

        // 2. 显示成交量
        if (volumeSeriesRef.current) {
          const volumeEnabled = indicators.find(i => i.id === 'volume')?.enabled
          if (volumeEnabled) {
            const volumeData = klineData.map((k: Kline) => ({
              time: k.time,
              value: k.volume || 0,
              color: k.close >= k.open ? 'rgba(14, 203, 129, 0.5)' : 'rgba(246, 70, 93, 0.5)',
            }))
            volumeSeriesRef.current.setData(volumeData)
          } else {
            // 关闭成交量时清空数据
            volumeSeriesRef.current.setData([])
          }
        }

        // 3. 添加指标
        updateIndicators(klineData)

        // 4. 获取并显示订单标记（含启发式成对 + 持仓开仓补充）
        if (traderID && candlestickSeriesRef.current) {
          console.log('[AdvancedChart] Starting to fetch orders...')
          let orders = await fetchOrders(traderID, symbol)
          console.log('[AdvancedChart] Received orders:', orders)

          const klineTimes = klineData.map((k: any) => k.time as number)
          const klineMinTime = klineTimes[0] || 0
          const klineMaxTime = klineTimes[klineTimes.length - 1] || 0
          const lastCandleTime = klineTimes.length > 0 ? klineTimes[klineTimes.length - 1] : 0

          // 补充当前持仓的开仓标记（无历史订单时也显示持仓入口价）
          if (positions && positions.length > 0 && lastCandleTime) {
            const norm = (s: string) => (s || '').toUpperCase().replace(/USDT$/, '') + 'USDT'
            const symNorm = norm(symbol)
            positions.forEach((pos: Position) => {
              if (norm(pos.symbol || '') !== symNorm) return
              const side = (pos.side || '').toLowerCase() as 'long' | 'short'
              if (side !== 'long' && side !== 'short') return
              const entryPrice = pos.entry_price ?? (pos as any).entryPrice
              if (entryPrice == null || entryPrice === 0) return
              orders.push({
                time: lastCandleTime,
                price: entryPrice,
                side,
                rawSide: side === 'long' ? 'buy' : 'sell',
                action: 'open',
                symbol: pos.symbol || symbol,
                fromPosition: true,
              })
            })
          }

          if (orders.length > 0) {
            console.log('[AdvancedChart] Creating markers from', orders.length, 'orders (incl. position opens)')

            console.log('[AdvancedChart] Kline time range:', klineMinTime, '-', klineMaxTime, '(', klineTimes.length, 'candles)')

            const findCandleTime = (orderTime: number): number | null => {
              if (orderTime < klineMinTime || orderTime > klineMaxTime) return null
              let left = 0
              let right = klineTimes.length - 1
              while (left < right) {
                const mid = Math.ceil((left + right + 1) / 2)
                if (klineTimes[mid] <= orderTime) left = mid
                else right = mid - 1
              }
              return klineTimes[left]
            }

            const markers: Array<{
              time: Time
              position: 'belowBar' | 'aboveBar'
              color: string
              shape: 'circle'
              text: string
              size: number
            }> = []
            const orderPoints: Array<{ time: number; price: number; pairIndex?: number; pnl?: number }> = []

            const isZh = String(language).startsWith('zh')
            const labelClose = isZh ? '平' : 'Close'

            orders.forEach((order) => {
              const candleTime = order.fromPosition ? lastCandleTime : findCandleTime(order.time)
              if (candleTime === null) return

              const pnlVal = order.pnl
              orderPoints.push({
                time: candleTime as number,
                price: order.price,
                pairIndex: order.pairIndex,
                pnl: typeof pnlVal === 'number' ? pnlVal : undefined,
              })

              const priceStr = formatPriceWithDynamicPrecision(order.price)
              const isOpen = order.action === 'open'
              const isLong = order.side === 'long'
              const pairSuffix =
                order.fromPosition
                  ? (isZh ? ' (持仓)' : ' (Pos)')
                  : order.pairIndex != null
                    ? ` (${order.pairIndex})`
                    : ''

              let text: string
              let position: 'belowBar' | 'aboveBar'
              let color: string

              if (isOpen) {
                text = isLong
                  ? (isZh ? `开多 ${priceStr}` : `Long ${priceStr}`) + pairSuffix
                  : (isZh ? `开空 ${priceStr}` : `Short ${priceStr}`) + pairSuffix
                position = isLong ? 'belowBar' : 'aboveBar'
                color = isLong ? '#0ECB81' : '#F6465D'
              } else {
                const pnlStr =
                  order.pnl != null
                    ? (order.pnl >= 0 ? ` +$${order.pnl.toFixed(2)}` : ` -$${Math.abs(order.pnl).toFixed(2)}`)
                    : ''
                text = `${labelClose} ${priceStr}${pnlStr}`.trim() + pairSuffix
                position = isLong ? 'aboveBar' : 'belowBar'
                color =
                  order.pnl != null ? (order.pnl >= 0 ? '#0ECB81' : '#F6465D') : isLong ? '#0ECB81' : '#F6465D'
              }

              markers.push({
                time: candleTime as Time,
                position,
                color,
                shape: 'circle' as const,
                text,
                size: 1,
              })
            })

            // 按时间排序，保持 markers 与 orderPoints 一一对应
            const combined = markers.map((m, i) => ({ marker: m, point: orderPoints[i] }))
            combined.sort((a, b) => (a.marker.time as number) - (b.marker.time as number))
            markers.length = 0
            orderPoints.length = 0
            combined.forEach(({ marker, point }) => {
              markers.push(marker)
              orderPoints.push(point)
            })
            currentOrderPointsRef.current = orderPoints

            console.log('[AdvancedChart] Valid markers:', markers.length, 'out of', orders.length)

            console.log('[AdvancedChart] Setting', markers.length, 'markers on candlestick series')
            console.log('[AdvancedChart] Markers data:', JSON.stringify(markers, null, 2))

            try {
              // 存储标记数据供后续切换使用
              currentMarkersDataRef.current = markers

              // 使用 v5 API: createSeriesMarkers
              const markersToShow = showOrderMarkers ? markers : []

              if (seriesMarkersRef.current) {
                // 如果已经存在，更新标记
                seriesMarkersRef.current.setMarkers(markersToShow)
              } else {
                // 首次创建标记
                seriesMarkersRef.current = createSeriesMarkers(candlestickSeriesRef.current, markersToShow)
              }
              console.log('[AdvancedChart] ✅ Markers updated! Count:', markersToShow.length, 'Visible:', showOrderMarkers)
            } catch (err) {
              console.error('[AdvancedChart] ❌ Failed to set markers:', err)
            }
          } else {
            console.log('[AdvancedChart] No orders found, clearing markers')
            currentOrderPointsRef.current = []
            try {
              if (seriesMarkersRef.current) {
                seriesMarkersRef.current.setMarkers([])
              }
              pairLineSeriesRef.current?.setData([])
            } catch (err) {
              console.error('[AdvancedChart] Failed to clear markers:', err)
            }
          }
        } else {
          console.log('[AdvancedChart] Skipping markers:', {
            hasTraderID: !!traderID,
            hasSeries: !!candlestickSeriesRef.current
          })
        }

        // 只在初始加载时自动适配视图，避免刷新时抖动
        if (isInitialLoadRef.current) {
          chartRef.current?.timeScale().fitContent()
          isInitialLoadRef.current = false
        }
        setLoading(false)
      } catch (err: any) {
        console.error('[AdvancedChart] Error loading data:', err)
        setError(err.message || 'Failed to load chart data')
        setLoading(false)
      }
    }

    loadData(false) // 首次加载

    // 实时自动刷新 (5秒更新一次)
    const refreshInterval = setInterval(() => loadData(true), 5000)
    return () => clearInterval(refreshInterval)
  }, [symbol, interval, traderID, exchange])

  // 单独刷新挂单价格线 (60秒刷新一次，避免频繁调用交易所API)
  // 同时从持仓数据中获取止盈止损价格
  useEffect(() => {
    if (!traderID || !candlestickSeriesRef.current) return

    // 加载挂单并显示价格线
    const loadOpenOrders = async () => {
      try {
        // 先清除旧的价格线
        priceLinesRef.current.forEach(line => {
          try {
            candlestickSeriesRef.current?.removePriceLine(line)
          } catch (e) {
            // 忽略清除错误
          }
        })
        priceLinesRef.current = []

        // 1. 从持仓数据中获取当前币种的止盈止损价格
        if (positions && positions.length > 0) {
          const currentPosition = positions.find(p => p.symbol === symbol)
          if (currentPosition) {
            // 添加止损价格线（持仓）
            if (currentPosition.stop_loss && currentPosition.stop_loss > 0) {
              const formattedPrice = formatPriceWithDynamicPrecision(currentPosition.stop_loss)
              const stopLossLine = candlestickSeriesRef.current?.createPriceLine({
                price: currentPosition.stop_loss,
                color: '#F6465D', // 红色 - 止损
                lineWidth: 3, // 持仓线更粗，便于区分
                lineStyle: 0, // 实线 - 持仓使用实线
                axisLabelVisible: true,
                title: `SL ${formattedPrice} [持仓]`, // 使用中文标签，更清晰
              })
              if (stopLossLine) {
                priceLinesRef.current.push(stopLossLine)
              }
            }

            // 添加止盈价格线（持仓）
            if (currentPosition.take_profit && currentPosition.take_profit > 0) {
              const formattedPrice = formatPriceWithDynamicPrecision(currentPosition.take_profit)
              const takeProfitLine = candlestickSeriesRef.current?.createPriceLine({
                price: currentPosition.take_profit,
                color: '#0ECB81', // 绿色 - 止盈
                lineWidth: 3, // 持仓线更粗，便于区分
                lineStyle: 0, // 实线 - 持仓使用实线
                axisLabelVisible: true,
                title: `TP ${formattedPrice} [持仓]`, // 使用中文标签，更清晰
              })
              if (takeProfitLine) {
                priceLinesRef.current.push(takeProfitLine)
              }
            }
          }
        }

        // 2. 从交易所挂单API获取止盈止损订单
        const openOrders = await fetchOpenOrders(traderID, symbol)
        console.log('[AdvancedChart] Open orders for price lines:', openOrders)

        if (openOrders.length > 0 && candlestickSeriesRef.current) {
          openOrders.forEach(order => {
            // 获取触发价格 (止损/止盈用 stop_price，限价单用 price)
            const linePrice = order.stop_price > 0 ? order.stop_price : order.price
            if (linePrice <= 0) return

            // 判断订单类型
            const isStopLoss = order.type.includes('STOP') || order.type.includes('SL')
            const isTakeProfit = order.type.includes('TAKE_PROFIT') || order.type.includes('TP')
            const isLimit = order.type === 'LIMIT'

            // 格式化价格用于标题显示
            const formattedPrice = formatPriceWithDynamicPrecision(linePrice)

            // 设置价格线样式
            let lineColor = '#F0B90B' // 默认黄色
            let title = ''

            if (isStopLoss) {
              lineColor = '#FF6B6B' // 稍浅的红色 - 挂单止损（与持仓区分）
              title = `SL ${formattedPrice} [挂单 ${order.quantity}]` // 使用中文标签，更清晰
            } else if (isTakeProfit) {
              lineColor = '#51CF66' // 稍浅的绿色 - 挂单止盈（与持仓区分）
              title = `TP ${formattedPrice} [挂单 ${order.quantity}]` // 使用中文标签，更清晰
            } else if (isLimit) {
              lineColor = '#F0B90B' // 黄色 - 限价单
              title = `Limit ${order.side} ${formattedPrice} [挂单 ${order.quantity}]`
            } else {
              title = `${order.type} ${formattedPrice} [挂单 ${order.quantity}]`
            }

            const priceLine = candlestickSeriesRef.current?.createPriceLine({
              price: linePrice,
              color: lineColor,
              lineWidth: 1, // 挂单线较细
              lineStyle: 2, // 虚线 - 挂单使用虚线
              axisLabelVisible: true,
              title: title,
            })

            if (priceLine) {
              priceLinesRef.current.push(priceLine)
            }
          })
          console.log('[AdvancedChart] ✅ Created', priceLinesRef.current.length, 'price lines for pending orders')
        }
      } catch (err) {
        console.error('[AdvancedChart] Error loading open orders:', err)
      }
    }

    // 初始加载 (延迟1秒等待图表初始化完成)
    const initialTimeout = setTimeout(loadOpenOrders, 1000)

    // 60秒刷新一次挂单
    const openOrdersInterval = setInterval(loadOpenOrders, 60000)

    return () => {
      clearTimeout(initialTimeout)
      clearInterval(openOrdersInterval)
    }
  }, [symbol, traderID, positions])

  // 单独处理订单标记的显示/隐藏，避免重新加载数据
  useEffect(() => {
    if (!seriesMarkersRef.current) return

    try {
      const markersToShow = showOrderMarkers ? currentMarkersDataRef.current : []
      seriesMarkersRef.current.setMarkers(markersToShow)
      console.log('[AdvancedChart] 🔄 Toggled markers visibility:', showOrderMarkers, 'Count:', markersToShow.length)
    } catch (err) {
      console.error('[AdvancedChart] ❌ Failed to toggle markers:', err)
    }
  }, [showOrderMarkers])

  // 更新指标
  const updateIndicators = (klineData: Kline[]) => {
    if (!chartRef.current) return

    // 清除旧指标
    indicatorSeriesRef.current.forEach(series => {
      chartRef.current?.removeSeries(series as any)
    })
    indicatorSeriesRef.current.clear()

    // 添加启用的指标
    indicators.forEach(indicator => {
      if (!indicator.enabled || !chartRef.current) return

      if (indicator.id.startsWith('ma')) {
        const maData = calculateSMA(klineData, indicator.params.period)
        const series = chartRef.current.addSeries(LineSeries, {
          color: indicator.color,
          lineWidth: 2,
          title: indicator.name,
        })
        series.setData(maData as any)
        indicatorSeriesRef.current.set(indicator.id, series)
      } else if (indicator.id.startsWith('ema')) {
        const emaData = calculateEMA(klineData, indicator.params.period)
        const series = chartRef.current.addSeries(LineSeries, {
          color: indicator.color,
          lineWidth: 2,
          title: indicator.name,
          lineStyle: 2, // 虚线
        })
        series.setData(emaData as any)
        indicatorSeriesRef.current.set(indicator.id, series)
      } else if (indicator.id === 'bb') {
        const bbData = calculateBollingerBands(klineData)

        const upperSeries = chartRef.current.addSeries(LineSeries, {
          color: indicator.color,
          lineWidth: 1,
          title: 'BB Upper',
        })
        upperSeries.setData(bbData.map(d => ({ time: d.time as any, value: d.upper })))

        const middleSeries = chartRef.current.addSeries(LineSeries, {
          color: indicator.color,
          lineWidth: 1,
          lineStyle: 2,
          title: 'BB Middle',
        })
        middleSeries.setData(bbData.map(d => ({ time: d.time as any, value: d.middle })))

        const lowerSeries = chartRef.current.addSeries(LineSeries, {
          color: indicator.color,
          lineWidth: 1,
          title: 'BB Lower',
        })
        lowerSeries.setData(bbData.map(d => ({ time: d.time as any, value: d.lower })))

        indicatorSeriesRef.current.set(indicator.id + '_upper', upperSeries)
        indicatorSeriesRef.current.set(indicator.id + '_middle', middleSeries)
        indicatorSeriesRef.current.set(indicator.id + '_lower', lowerSeries)
      }
    })
  }

  // 切换指标
  const toggleIndicator = (id: string) => {
    setIndicators(prev =>
      prev.map(ind => (ind.id === id ? { ...ind, enabled: !ind.enabled } : ind))
    )
  }

  // 监听指标状态变化，自动更新图表上的指标
  useEffect(() => {
    if (currentKlineDataRef.current.length > 0 && chartRef.current) {
      updateIndicators(currentKlineDataRef.current)
    }
  }, [indicators])

  return (
    <div
      className="relative shadow-xl"
      style={{
        background: 'linear-gradient(180deg, #0F1215 0%, #0B0E11 100%)',
        borderRadius: '12px',
        overflow: 'hidden',
        border: '1px solid rgba(43, 49, 57, 0.5)',
        height: '100%',
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      {/* Compact Professional Header */}
      <div
        className="flex items-center justify-between px-3 py-2"
        style={{ 
          borderBottom: `1px solid var(--panel-border)`, 
          background: isDark ? '#0D1117' : 'var(--panel-bg)', 
          flexShrink: 0 
        }}
      >
        {/* Left: Symbol Info + Price */}
        <div className="flex items-center gap-4">
          {/* Symbol & Interval */}
          <div className="flex items-center gap-2">
            <span className="text-sm sm:text-base font-bold" style={{ color: 'var(--text-primary)' }}>{symbol}</span>
            <span className="text-[10px] sm:text-xs px-1.5 py-0.5 rounded font-medium" style={{ background: 'var(--panel-bg-hover)', color: 'var(--text-secondary)', border: `1px solid var(--panel-border)` }}>{interval}</span>
            <span
              className="text-[10px] sm:text-xs px-1.5 py-0.5 rounded font-medium uppercase"
              style={{
                background: exchange === 'hyperliquid' ? 'rgba(80, 227, 194, 0.1)' : 'rgba(243, 186, 47, 0.1)',
                color: exchange === 'hyperliquid' ? '#50E3C2' : '#F3BA2F',
                border: `1px solid var(--panel-border)`,
              }}
            >
              {exchange?.toUpperCase()}
            </span>
          </div>

          {/* Price Display */}
          {marketStats && (
            <div className="flex items-center gap-2 sm:gap-3 pl-3 border-l" style={{ borderColor: 'var(--panel-border)' }}>
              <span
                className="text-sm sm:text-base font-bold tabular-nums"
                style={{ color: marketStats.priceChange >= 0 ? '#10B981' : '#EF4444' }}
              >
                {formatPriceWithDynamicPrecision(marketStats.price)}
              </span>
              <span
                className="text-xs sm:text-sm font-medium px-1.5 py-0.5 rounded tabular-nums"
                style={{
                  background: marketStats.priceChange >= 0 ? 'rgba(16, 185, 129, 0.1)' : 'rgba(239, 68, 68, 0.1)',
                  color: marketStats.priceChange >= 0 ? '#10B981' : '#EF4444',
                }}
              >
                {marketStats.priceChange >= 0 ? '+' : ''}{marketStats.priceChangePercent.toFixed(2)}%
              </span>

              {/* Compact H/L */}
              <div className="flex items-center gap-2 text-[10px] sm:text-xs" style={{ color: 'var(--text-secondary)' }}>
                <span>H <span style={{ color: 'var(--text-primary)' }}>{formatPriceWithDynamicPrecision(marketStats.high)}</span></span>
                <span>L <span style={{ color: 'var(--text-primary)' }}>{formatPriceWithDynamicPrecision(marketStats.low)}</span></span>
                {marketStats.volume > 0 && baseUnit && (
                  <span>Vol <span style={{ color: 'var(--text-primary)' }}>{formatVolume(marketStats.volume)}</span></span>
                )}
              </div>
            </div>
          )}
        </div>

        {/* Right: Controls */}
        <div className="flex items-center gap-1.5">
          {loading && (
            <span className="text-[10px] sm:text-xs text-yellow-400 animate-pulse mr-1">
              {language === 'zh' ? '更新中...' : 'Updating...'}
            </span>
          )}
          <button
            onClick={() => setShowIndicatorPanel(!showIndicatorPanel)}
            className="flex items-center gap-1 px-2 py-1 rounded text-xs sm:text-sm font-medium transition-all"
            style={{
              background: showIndicatorPanel ? 'rgba(96, 165, 250, 0.15)' : 'transparent',
              color: showIndicatorPanel ? '#60A5FA' : 'var(--text-secondary)',
              border: `1px solid ${showIndicatorPanel ? 'rgba(96, 165, 250, 0.3)' : 'transparent'}`,
            }}
            onMouseEnter={(e) => {
              if (!showIndicatorPanel) {
                e.currentTarget.style.color = 'var(--text-primary)'
                e.currentTarget.style.background = 'var(--panel-bg-hover)'
              }
            }}
            onMouseLeave={(e) => {
              if (!showIndicatorPanel) {
                e.currentTarget.style.color = 'var(--text-secondary)'
                e.currentTarget.style.background = 'transparent'
              }
            }}
          >
            <Settings className="w-3.5 h-3.5 sm:w-4 sm:h-4" />
            <span>{language === 'zh' ? '指标' : 'Indicators'}</span>
          </button>

          <button
            onClick={() => setShowOrderMarkers(!showOrderMarkers)}
            className="flex items-center gap-1 px-2 py-1 rounded text-xs sm:text-sm font-medium transition-all"
            style={{
              background: showOrderMarkers ? 'rgba(16, 185, 129, 0.15)' : 'transparent',
              color: showOrderMarkers ? '#10B981' : 'var(--text-secondary)',
              border: `1px solid ${showOrderMarkers ? 'rgba(16, 185, 129, 0.3)' : 'transparent'}`,
            }}
            title={language === 'zh' ? '订单标记' : 'Order Markers'}
            onMouseEnter={(e) => {
              if (!showOrderMarkers) {
                e.currentTarget.style.color = 'var(--text-primary)'
                e.currentTarget.style.background = 'var(--panel-bg-hover)'
              }
            }}
            onMouseLeave={(e) => {
              if (!showOrderMarkers) {
                e.currentTarget.style.color = 'var(--text-secondary)'
                e.currentTarget.style.background = 'transparent'
              }
            }}
          >
            <span>B/S</span>
          </button>
        </div>
      </div>

      {/* 指标面板 - 专业化设计 */}
      {showIndicatorPanel && (
        <div
          className="absolute top-14 right-3 z-10 rounded-lg shadow-2xl backdrop-blur-sm"
          style={{
            background: isDark ? 'linear-gradient(135deg, #1A1E23 0%, #0F1215 100%)' : 'var(--panel-bg)',
            border: `1px solid var(--panel-border)`,
            maxHeight: '400px',
            minWidth: '240px',
            maxWidth: '280px',
            overflowY: 'auto',
          }}
        >
          {/* 标题栏 */}
          <div
            className="flex items-center justify-between px-3 py-2 border-b"
            style={{ borderColor: 'var(--panel-border)' }}
          >
            <div className="flex items-center gap-1.5">
              <BarChart2 className="w-4 h-4" style={{ color: 'var(--nofx-gold)' }} />
              <h4 className="text-sm font-bold" style={{ color: 'var(--text-primary)' }}>
                {language === 'zh' ? '技术指标' : 'Indicators'}
              </h4>
            </div>
            <button
              onClick={() => setShowIndicatorPanel(false)}
              className="transition-colors"
              style={{ color: 'var(--text-secondary)' }}
              onMouseEnter={(e) => {
                e.currentTarget.style.color = 'var(--text-primary)'
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.color = 'var(--text-secondary)'
              }}
            >
              <span className="text-lg">×</span>
            </button>
          </div>

          {/* 指标列表 */}
          <div className="p-2 space-y-1">
            {indicators.map(indicator => (
              <label
                key={indicator.id}
                className="flex items-center gap-2.5 p-2 rounded-md cursor-pointer transition-all group"
                style={{
                  background: indicator.enabled 
                    ? (isDark ? 'rgba(240, 185, 11, 0.15)' : 'rgba(240, 185, 11, 0.25)')
                    : 'transparent',
                  border: `1px solid ${indicator.enabled 
                    ? (isDark ? 'rgba(240, 185, 11, 0.4)' : 'rgba(240, 185, 11, 0.6)')
                    : 'transparent'}`,
                }}
                onMouseEnter={(e) => {
                  if (!indicator.enabled) {
                    e.currentTarget.style.background = 'var(--panel-bg-hover)'
                    e.currentTarget.style.borderColor = 'var(--panel-border)'
                  } else {
                    e.currentTarget.style.background = isDark 
                      ? 'rgba(240, 185, 11, 0.2)' 
                      : 'rgba(240, 185, 11, 0.3)'
                    e.currentTarget.style.borderColor = isDark 
                      ? 'rgba(240, 185, 11, 0.5)' 
                      : 'rgba(240, 185, 11, 0.7)'
                  }
                }}
                onMouseLeave={(e) => {
                  if (!indicator.enabled) {
                    e.currentTarget.style.background = 'transparent'
                    e.currentTarget.style.borderColor = 'transparent'
                  } else {
                    e.currentTarget.style.background = isDark 
                      ? 'rgba(240, 185, 11, 0.15)' 
                      : 'rgba(240, 185, 11, 0.25)'
                    e.currentTarget.style.borderColor = isDark 
                      ? 'rgba(240, 185, 11, 0.4)' 
                      : 'rgba(240, 185, 11, 0.6)'
                  }
                }}
              >
                <div className="relative">
                  <input
                    type="checkbox"
                    checked={indicator.enabled}
                    onChange={() => toggleIndicator(indicator.id)}
                    className="w-4 h-4 rounded border focus:ring-2 focus:ring-yellow-500/50"
                    style={{ 
                      borderColor: indicator.enabled 
                        ? 'var(--nofx-gold)' 
                        : 'var(--panel-border)',
                      backgroundColor: indicator.enabled 
                        ? 'var(--nofx-gold)' 
                        : 'transparent',
                      accentColor: 'var(--nofx-gold)',
                    }}
                  />
                </div>
                <div
                  className="w-8 h-3 rounded-sm border"
                  style={{ 
                    backgroundColor: indicator.color,
                    borderColor: indicator.enabled 
                      ? 'var(--nofx-gold)' 
                      : 'var(--panel-border)',
                    opacity: indicator.enabled ? 1 : 0.6,
                  }}
                ></div>
                <span 
                  className="text-xs sm:text-sm transition-colors flex-1 font-medium" 
                  style={{ 
                    color: indicator.enabled 
                      ? 'var(--nofx-gold)' 
                      : 'var(--text-primary)',
                  }}
                  onMouseEnter={(e) => {
                    if (!indicator.enabled) {
                      e.currentTarget.style.color = 'var(--nofx-gold)'
                    }
                  }}
                  onMouseLeave={(e) => {
                    if (!indicator.enabled) {
                      e.currentTarget.style.color = 'var(--text-primary)'
                    }
                  }}
                >
                  {indicator.name}
                </span>
                {indicator.enabled && (
                  <span className="text-xs font-bold" style={{ color: 'var(--nofx-gold)' }}>●</span>
                )}
              </label>
            ))}
          </div>

          {/* 底部提示 */}
          <div
            className="px-3 py-2 text-xs border-t"
            style={{ 
              borderColor: 'var(--panel-border)',
              color: 'var(--text-secondary)',
            }}
          >
            {language === 'zh' ? '点击切换指标' : 'Click to toggle'}
          </div>
        </div>
      )}

      {/* 图表容器 */}
      <div style={{ position: 'relative', flex: 1, minHeight: 0 }}>
        <div ref={chartContainerRef} style={{ height: '100%', width: '100%' }} />

        {/* OHLC Tooltip */}
        {tooltipData && (
          <div
            ref={tooltipRef}
            style={{
              position: 'absolute',
              left: '10px',
              top: '10px',
              padding: '8px 12px',
              background: 'rgba(15, 18, 21, 0.95)',
              border: '1px solid rgba(240, 185, 11, 0.3)',
              borderRadius: '6px',
              color: '#EAECEF',
              fontSize: '12px',
              fontFamily: 'monospace',
              pointerEvents: 'none',
              zIndex: 10,
              backdropFilter: 'blur(10px)',
              boxShadow: '0 4px 12px rgba(0, 0, 0, 0.5)',
            }}
          >
            <div style={{ marginBottom: '6px', color: '#F0B90B', fontWeight: 'bold', fontSize: '11px' }}>
              {new Date((tooltipData.time as number) * 1000).toLocaleString(language === 'zh' ? 'zh-CN' : 'en-US', {
                month: 'short',
                day: 'numeric',
                hour: '2-digit',
                minute: '2-digit',
              })}
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '4px 12px', fontSize: '11px' }}>
              <span style={{ color: '#848E9C' }}>O:</span>
              <span style={{ color: '#EAECEF', fontWeight: '500' }}>{tooltipData.open ? formatPriceWithDynamicPrecision(tooltipData.open) : '-'}</span>

              <span style={{ color: '#848E9C' }}>H:</span>
              <span style={{ color: '#0ECB81', fontWeight: '500' }}>{tooltipData.high ? formatPriceWithDynamicPrecision(tooltipData.high) : '-'}</span>

              <span style={{ color: '#848E9C' }}>L:</span>
              <span style={{ color: '#F6465D', fontWeight: '500' }}>{tooltipData.low ? formatPriceWithDynamicPrecision(tooltipData.low) : '-'}</span>

              <span style={{ color: '#848E9C' }}>C:</span>
              <span style={{
                color: tooltipData.close >= tooltipData.open ? '#0ECB81' : '#F6465D',
                fontWeight: 'bold'
              }}>
                {tooltipData.close ? formatPriceWithDynamicPrecision(tooltipData.close) : '-'}
              </span>

              {tooltipData.volume > 0 && baseUnit && (
                <>
                  <span style={{ color: '#848E9C' }}>V({baseUnit}):</span>
                  <span style={{ color: '#3B82F6', fontWeight: '500' }}>
                    {formatVolume(tooltipData.volume)}
                  </span>
                </>
              )}

              {tooltipData.quoteVolume > 0 && quoteUnit && (
                <>
                  <span style={{ color: '#848E9C' }}>V({quoteUnit}):</span>
                  <span style={{ color: '#3B82F6', fontWeight: '500' }}>
                    {formatVolume(tooltipData.quoteVolume)}
                  </span>
                </>
              )}
            </div>
          </div>
        )}

        {/* 成对连线时的盈亏显示 */}
        {hoveredPairPnl !== null && (
          <div
            style={{
              position: 'absolute',
              left: '10px',
              bottom: '10px',
              padding: '6px 10px',
              background: 'rgba(15, 18, 21, 0.95)',
              border: `1px solid ${hoveredPairPnl >= 0 ? 'rgba(14, 203, 129, 0.5)' : 'rgba(246, 70, 93, 0.5)'}`,
              borderRadius: '6px',
              color: hoveredPairPnl >= 0 ? '#0ECB81' : '#F6465D',
              fontSize: '13px',
              fontWeight: 'bold',
              fontFamily: 'monospace',
              pointerEvents: 'none',
              zIndex: 10,
              backdropFilter: 'blur(10px)',
              boxShadow: '0 4px 12px rgba(0, 0, 0, 0.5)',
            }}
          >
            {String(language).startsWith('zh') ? '盈亏: ' : 'PnL: '}
            {hoveredPairPnl >= 0 ? '+' : ''}${hoveredPairPnl.toFixed(2)}
          </div>
        )}

        {/* NOFX 水印 */}
        <div
          style={{
            position: 'absolute',
            bottom: '20%',
            right: '5%',
            pointerEvents: 'none',
            userSelect: 'none',
            zIndex: 1,
          }}
        >
          <div
            style={{
              fontSize: '56px',
              fontWeight: '700',
              color: 'rgba(240, 185, 11, 0.12)',
              letterSpacing: '4px',
              fontFamily: 'system-ui, -apple-system, BlinkMacSystemFont, sans-serif',
              textShadow: '0 2px 30px rgba(240, 185, 11, 0.2)',
            }}
          >
            NOFX
          </div>
        </div>
      </div>

      {/* 错误提示 */}
      {error && (
        <div
          className="absolute inset-0 flex items-center justify-center"
          style={{ background: 'rgba(11, 14, 17, 0.9)' }}
        >
          <div className="text-center">
            <div className="text-2xl mb-2">⚠️</div>
            <div style={{ color: '#F6465D' }}>{error}</div>
          </div>
        </div>
      )}

    </div>
  )
}
