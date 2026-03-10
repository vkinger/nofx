import { useState } from 'react'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  ReferenceLine,
} from 'recharts'
import useSWR from 'swr'
import { api } from '../lib/api'
import { useLanguage } from '../contexts/LanguageContext'
import { useAuth } from '../contexts/AuthContext'
import { useTheme } from '../contexts/ThemeContext'
import { t } from '../i18n/translations'
import {
  AlertTriangle,
  BarChart3,
  DollarSign,
  Percent,
  TrendingUp as ArrowUp,
  TrendingDown as ArrowDown,
} from 'lucide-react'

interface EquityPoint {
  timestamp: string
  total_equity: number
  pnl: number
  pnl_pct: number
  cycle_number: number
}

interface EquityChartProps {
  traderId?: string
  embedded?: boolean // 嵌入模式（不显示外层卡片）
}

export function EquityChart({ traderId, embedded = false }: EquityChartProps) {
  const { language } = useLanguage()
  const { user, token } = useAuth()
  const { theme } = useTheme()
  const isDark = theme === 'dark'
  const [displayMode, setDisplayMode] = useState<'dollar' | 'percent'>('dollar')
  
  // 根据主题获取图表颜色
  const getChartColors = () => {
    const isDark = theme === 'dark'
    return {
      gridColor: isDark ? '#2B3139' : 'rgba(0, 0, 0, 0.1)',
      axisColor: isDark ? '#5E6673' : 'rgba(0, 0, 0, 0.3)',
      tickColor: isDark ? '#848E9C' : 'rgba(0, 0, 0, 0.5)',
      tickLineColor: isDark ? '#2B3139' : 'rgba(0, 0, 0, 0.15)',
      referenceLineColor: isDark ? '#474D57' : 'rgba(0, 0, 0, 0.2)',
      textColor: isDark ? '#848E9C' : 'rgba(0, 0, 0, 0.6)',
      tooltipBg: isDark ? '#1E2329' : 'rgba(255, 255, 255, 0.98)',
      tooltipBorder: isDark ? '#2B3139' : 'rgba(0, 0, 0, 0.1)',
      tooltipText: isDark ? '#EAECEF' : '#111827',
      tooltipLabel: isDark ? '#848E9C' : 'rgba(0, 0, 0, 0.6)',
    }
  }
  
  const chartColors = getChartColors()

  const { data: history, error, isLoading } = useSWR<EquityPoint[]>(
    user && token && traderId ? `equity-history-${traderId}` : null,
    () => api.getEquityHistory(traderId),
    {
      refreshInterval: 30000, // 30秒刷新（历史数据更新频率较低）
      revalidateOnFocus: false,
      dedupingInterval: 20000,
    }
  )

  const { data: account } = useSWR(
    user && token && traderId ? `account-${traderId}` : null,
    () => api.getAccount(traderId),
    {
      refreshInterval: 15000, // 15秒刷新（配合后端缓存）
      revalidateOnFocus: false,
      dedupingInterval: 10000,
    }
  )

  // Loading state - show skeleton
  if (isLoading) {
    return (
      <div className={embedded ? 'p-6' : 'binance-card p-6'}>
        {!embedded && (
          <h3 className="text-lg font-semibold mb-6" style={{ color: '#EAECEF' }}>
            {t('accountEquityCurve', language)}
          </h3>
        )}
        <div className="animate-pulse">
          <div className="skeleton h-64 w-full rounded"></div>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className={embedded ? 'p-6' : 'binance-card p-6'}>
        <div
          className="flex items-center gap-3 p-4 rounded"
          style={{
            background: 'rgba(246, 70, 93, 0.1)',
            border: '1px solid rgba(246, 70, 93, 0.2)',
          }}
        >
          <AlertTriangle className="w-6 h-6" style={{ color: '#F6465D' }} />
          <div>
            <div className="font-semibold" style={{ color: '#F6465D' }}>
              {t('loadingError', language)}
            </div>
            <div className="text-sm" style={{ color: '#848E9C' }}>
              {error.message}
            </div>
          </div>
        </div>
      </div>
    )
  }

  // 过滤掉无效数据：total_equity为0或小于1的数据点（API失败导致）
  const validHistory = history?.filter((point) => point.total_equity > 1) || []

  if (!validHistory || validHistory.length === 0) {
    return (
      <div className={embedded ? 'p-6' : 'binance-card p-6'}>
        {!embedded && (
          <h3 className="text-lg font-semibold mb-6" style={{ color: '#EAECEF' }}>
            {t('accountEquityCurve', language)}
          </h3>
        )}
        <div className="text-center py-16" style={{ color: '#848E9C' }}>
          <div className="mb-4 flex justify-center opacity-50">
            <BarChart3 className="w-16 h-16" />
          </div>
          <div className="text-lg font-semibold mb-2">
            {t('noHistoricalData', language)}
          </div>
          <div className="text-sm">{t('dataWillAppear', language)}</div>
        </div>
      </div>
    )
  }

  // 限制显示最近的数据点（性能优化）
  // 如果数据超过2000个点，只显示最近2000个
  const MAX_DISPLAY_POINTS = 2000
  const displayHistory =
    validHistory.length > MAX_DISPLAY_POINTS
      ? validHistory.slice(-MAX_DISPLAY_POINTS)
      : validHistory

  // 计算初始余额（优先从 account 获取配置的初始余额，备选从历史数据反推）
  const initialBalance =
    account?.initial_balance || // 从交易员配置读取真实初始余额
    (validHistory[0]
      ? validHistory[0].total_equity - validHistory[0].pnl
      : undefined) || // 备选：淨值 - 盈亏
    1000 // 默认值（与创建交易员时的默认配置一致）

  // 转换数据格式，保留完整时间供 tooltip 显示；cycle_number 可能未定义，用序号兜底
  const chartData = displayHistory.map((point, index) => {
    const pnl = point.total_equity - initialBalance
    const pnlPct = ((pnl / initialBalance) * 100).toFixed(2)
    const d = new Date(point.timestamp)
    const timeLabel =
      language === 'zh'
        ? `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}:${String(d.getSeconds()).padStart(2, '0')}`
        : d.toLocaleString(undefined, { dateStyle: 'short', timeStyle: 'medium' })
    const cycle =
      point.cycle_number != null && Number.isFinite(point.cycle_number)
        ? point.cycle_number
        : index + 1
    return {
      time: d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }),
      timeLabel,
      timestamp: point.timestamp,
      value: displayMode === 'dollar' ? point.total_equity : parseFloat(pnlPct),
      cycle,
      raw_equity: point.total_equity,
      raw_pnl: pnl,
      raw_pnl_pct: parseFloat(pnlPct),
    }
  })

  // 打点周期：数据点多时按间隔打点（约 25～40 个点），少时每个点都打
  const dotInterval = chartData.length <= 50 ? 1 : Math.max(1, Math.floor(chartData.length / 30))

  const currentValue = chartData[chartData.length - 1]
  const isProfit = currentValue.raw_pnl >= 0

  // 计算Y轴范围
  const calculateYDomain = () => {
    if (displayMode === 'percent') {
      // 百分比模式：找到最大最小值，留20%余量
      const values = chartData.map((d) => d.value)
      const minVal = Math.min(...values)
      const maxVal = Math.max(...values)
      const range = Math.max(Math.abs(maxVal), Math.abs(minVal))
      const padding = Math.max(range * 0.2, 1) // 至少留1%余量
      return [Math.floor(minVal - padding), Math.ceil(maxVal + padding)]
    } else {
      // 美元模式：以初始余额为基准，上下留10%余量
      const values = chartData.map((d) => d.value)
      const minVal = Math.min(...values, initialBalance)
      const maxVal = Math.max(...values, initialBalance)
      const range = maxVal - minVal
      const padding = Math.max(range * 0.15, initialBalance * 0.01) // 至少留1%余量
      return [Math.floor(minVal - padding), Math.ceil(maxVal + padding)]
    }
  }

  // 自定义 Tooltip：显示准确时间、周期、净值、盈亏（鼠标悬停时的具体坐标信息）
  const CustomTooltip = ({ active, payload, label }: any) => {
    if (active && payload && payload.length) {
      const data = payload[0].payload
      const rows: { label: string; value: string; color?: string }[] = [
        {
          label: language === 'zh' ? '时间' : 'Time',
          value: data.timeLabel ?? data.timestamp ?? label,
        },
        {
          label: language === 'zh' ? '周期' : 'Cycle',
          value:
            data.cycle != null && data.cycle !== ''
              ? `#${Number(data.cycle)}`
              : language === 'zh'
                ? '—'
                : '—',
        },
        {
          label: language === 'zh' ? '净值' : 'Equity',
          value: `${data.raw_equity.toFixed(2)} USDT`,
        },
        {
          label: language === 'zh' ? '盈亏' : 'PnL',
          value: `${data.raw_pnl >= 0 ? '+' : ''}${data.raw_pnl.toFixed(2)} USDT (${data.raw_pnl_pct >= 0 ? '+' : ''}${data.raw_pnl_pct}%)`,
          color: data.raw_pnl >= 0 ? '#0ECB81' : '#F6465D',
        },
      ]
      return (
        <div
          className="rounded-lg p-3 shadow-xl min-w-[200px]"
          style={{
            background: chartColors.tooltipBg,
            border: `1px solid ${chartColors.tooltipBorder}`,
          }}
        >
          {rows.map((row) => (
            <div
              key={row.label}
              className="flex justify-between gap-4 text-sm mb-1.5 last:mb-0"
            >
              <span style={{ color: chartColors.tooltipLabel }}>{row.label}</span>
              <span
                className="font-mono font-semibold"
                style={{ color: row.color ?? chartColors.tooltipText }}
              >
                {row.value}
              </span>
            </div>
          ))}
        </div>
      )
    }
    return null
  }

  // 按周期打点：仅在第 0, dotInterval, 2*dotInterval... 处绘制圆点（Recharts dot 不能返回 null，不显示时用 r=0）
  const renderDot = (props: any) => {
    const { cx, cy, index } = props
    const show = index % dotInterval === 0
    return (
      <circle
        cx={cx}
        cy={cy}
        r={show ? 3 : 0}
        fill="#F0B90B"
        stroke={show ? (isDark ? '#1E2329' : '#fff') : 'transparent'}
        strokeWidth={show ? 1 : 0}
      />
    )
  }

  return (
    <div className={embedded ? 'p-3 sm:p-5' : 'binance-card p-3 sm:p-5 animate-fade-in'}>
      {/* Header */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between mb-4">
        <div className="flex-1">
          {!embedded && (
            <h3
              className="text-base sm:text-lg font-bold mb-2"
              style={{ color: 'var(--text-primary)' }}
            >
              {t('accountEquityCurve', language)}
            </h3>
          )}
          <div className="flex flex-col sm:flex-row sm:items-baseline gap-2 sm:gap-4">
            <span
              className="text-2xl sm:text-3xl font-bold mono"
              style={{ color: 'var(--text-primary)' }}
            >
              {account?.total_equity.toFixed(2) || '0.00'}
              <span
                className="text-base sm:text-lg ml-1"
                style={{ color: 'var(--text-secondary)' }}
              >
                USDT
              </span>
            </span>
            <div className="flex items-center gap-2 flex-wrap">
              <span
                className="text-sm sm:text-lg font-bold mono px-2 sm:px-3 py-1 rounded flex items-center gap-1"
                style={{
                  color: isProfit ? '#0ECB81' : '#F6465D',
                  background: isProfit
                    ? 'rgba(14, 203, 129, 0.1)'
                    : 'rgba(246, 70, 93, 0.1)',
                  border: `1px solid ${
                    isProfit
                      ? 'rgba(14, 203, 129, 0.2)'
                      : 'rgba(246, 70, 93, 0.2)'
                  }`,
                }}
              >
                {isProfit ? (
                  <ArrowUp className="w-4 h-4" />
                ) : (
                  <ArrowDown className="w-4 h-4" />
                )}
                {isProfit ? '+' : ''}
                {currentValue.raw_pnl_pct}%
              </span>
              <span
                className="text-xs sm:text-sm mono"
                style={{ color: 'var(--text-secondary)' }}
              >
                ({isProfit ? '+' : ''}
                {currentValue.raw_pnl.toFixed(2)} USDT)
              </span>
            </div>
          </div>
        </div>

        {/* Display Mode Toggle */}
        <div
          className="flex gap-1.5 sm:gap-2 rounded p-1 sm:p-1.5 self-start sm:self-auto"
          style={{ 
            background: isDark ? '#0B0E11' : 'var(--panel-bg-hover)', 
            border: `1px solid var(--panel-border)` 
          }}
        >
          <button
            onClick={() => setDisplayMode('dollar')}
            className="px-4 sm:px-5 py-2 sm:py-2.5 rounded text-sm sm:text-base font-bold transition-all flex items-center gap-2"
            style={
              displayMode === 'dollar'
                ? {
                    background: '#F0B90B',
                    color: '#000',
                    boxShadow: '0 2px 8px rgba(240, 185, 11, 0.4)',
                  }
                : { 
                    background: 'transparent', 
                    color: 'var(--text-secondary)' 
                  }
            }
            onMouseEnter={(e) => {
              if (displayMode !== 'dollar') {
                e.currentTarget.style.color = 'var(--text-primary)'
                e.currentTarget.style.background = isDark ? 'rgba(255, 255, 255, 0.05)' : 'var(--panel-bg)'
              }
            }}
            onMouseLeave={(e) => {
              if (displayMode !== 'dollar') {
                e.currentTarget.style.color = 'var(--text-secondary)'
                e.currentTarget.style.background = 'transparent'
              }
            }}
          >
            <DollarSign className="w-4 h-4 sm:w-5 sm:h-5" /> USDT
          </button>
          <button
            onClick={() => setDisplayMode('percent')}
            className="px-4 sm:px-5 py-2 sm:py-2.5 rounded text-sm sm:text-base font-bold transition-all flex items-center gap-2"
            style={
              displayMode === 'percent'
                ? {
                    background: '#F0B90B',
                    color: '#000',
                    boxShadow: '0 2px 8px rgba(240, 185, 11, 0.4)',
                  }
                : { 
                    background: 'transparent', 
                    color: 'var(--text-secondary)' 
                  }
            }
            onMouseEnter={(e) => {
              if (displayMode !== 'percent') {
                e.currentTarget.style.color = 'var(--text-primary)'
                e.currentTarget.style.background = isDark ? 'rgba(255, 255, 255, 0.05)' : 'var(--panel-bg)'
              }
            }}
            onMouseLeave={(e) => {
              if (displayMode !== 'percent') {
                e.currentTarget.style.color = 'var(--text-secondary)'
                e.currentTarget.style.background = 'transparent'
              }
            }}
          >
            <Percent className="w-4 h-4 sm:w-5 sm:h-5" /> %
          </button>
        </div>
      </div>

      {/* Chart */}
      <div
        className="my-2"
        style={{
          borderRadius: '8px',
          overflow: 'hidden',
          position: 'relative',
        }}
      >
        {/* NOFX Watermark */}
        <div
          style={{
            position: 'absolute',
            top: '15px',
            right: '15px',
            fontSize: '20px',
            fontWeight: 'bold',
            color: 'rgba(240, 185, 11, 0.15)',
            zIndex: 10,
            pointerEvents: 'none',
            fontFamily: 'monospace',
          }}
        >
          NOFX
        </div>
        <ResponsiveContainer width="100%" height={280}>
          <LineChart
            data={chartData}
            margin={{ top: 10, right: 20, left: 5, bottom: 30 }}
          >
            <defs>
              <linearGradient id="colorGradient" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor="#F0B90B" stopOpacity={0.8} />
                <stop offset="95%" stopColor="#FCD535" stopOpacity={0.2} />
              </linearGradient>
            </defs>
            <CartesianGrid strokeDasharray="3 3" stroke="#2B3139" />
            <XAxis
              dataKey="time"
              stroke="#5E6673"
              tick={{ fill: '#848E9C', fontSize: 11 }}
              tickLine={{ stroke: '#2B3139' }}
              interval={Math.floor(chartData.length / 10)}
              angle={-15}
              textAnchor="end"
              height={60}
            />
            <YAxis
              stroke="#5E6673"
              tick={{ fill: '#848E9C', fontSize: 12 }}
              tickLine={{ stroke: '#2B3139' }}
              domain={calculateYDomain()}
              tickFormatter={(value) =>
                displayMode === 'dollar' ? `$${value.toFixed(0)}` : `${value}%`
              }
            />
            <Tooltip content={<CustomTooltip />} />
            <ReferenceLine
              y={displayMode === 'dollar' ? initialBalance : 0}
              stroke="#474D57"
              strokeDasharray="3 3"
              label={{
                value:
                  displayMode === 'dollar'
                    ? t('initialBalance', language).split(' ')[0]
                    : '0%',
                fill: '#848E9C',
                fontSize: 12,
              }}
            />
            <Line
              type="natural"
              dataKey="value"
              stroke="url(#colorGradient)"
              strokeWidth={3}
              dot={renderDot}
              activeDot={{
                r: 6,
                fill: '#FCD535',
                stroke: '#F0B90B',
                strokeWidth: 2,
              }}
              connectNulls={true}
            />
          </LineChart>
        </ResponsiveContainer>
      </div>

      {/* Footer Stats */}
      <div
        className="mt-3 grid grid-cols-2 sm:grid-cols-4 gap-2 sm:gap-3 pt-3"
        style={{ borderTop: `1px solid var(--panel-border)` }}
      >
        <div
          className="p-3 rounded transition-all"
          style={{ 
            background: isDark ? 'rgba(240, 185, 11, 0.05)' : 'var(--panel-bg-hover)',
            border: `1px solid var(--panel-border)`,
          }}
        >
          <div
            className="text-xs sm:text-sm mb-1.5 uppercase tracking-wider font-medium"
            style={{ color: 'var(--text-secondary)' }}
          >
            {t('initialBalance', language)}
          </div>
          <div
            className="text-sm sm:text-base font-bold mono"
            style={{ color: 'var(--text-primary)' }}
          >
            {initialBalance.toFixed(2)} USDT
          </div>
        </div>
        <div
          className="p-3 rounded transition-all"
          style={{ 
            background: isDark ? 'rgba(240, 185, 11, 0.05)' : 'var(--panel-bg-hover)',
            border: `1px solid var(--panel-border)`,
          }}
        >
          <div
            className="text-xs sm:text-sm mb-1.5 uppercase tracking-wider font-medium"
            style={{ color: 'var(--text-secondary)' }}
          >
            {t('currentEquity', language)}
          </div>
          <div
            className="text-sm sm:text-base font-bold mono"
            style={{ color: 'var(--text-primary)' }}
          >
            {currentValue.raw_equity.toFixed(2)} USDT
          </div>
        </div>
        <div
          className="p-3 rounded transition-all"
          style={{ 
            background: isDark ? 'rgba(240, 185, 11, 0.05)' : 'var(--panel-bg-hover)',
            border: `1px solid var(--panel-border)`,
          }}
        >
          <div
            className="text-xs sm:text-sm mb-1.5 uppercase tracking-wider font-medium"
            style={{ color: 'var(--text-secondary)' }}
          >
            {t('historicalCycles', language)}
          </div>
          <div
            className="text-sm sm:text-base font-bold mono"
            style={{ color: 'var(--text-primary)' }}
          >
            {validHistory.length} {t('cycles', language)}
          </div>
        </div>
        <div
          className="p-3 rounded transition-all"
          style={{ 
            background: isDark ? 'rgba(240, 185, 11, 0.05)' : 'var(--panel-bg-hover)',
            border: `1px solid var(--panel-border)`,
          }}
        >
          <div
            className="text-xs sm:text-sm mb-1.5 uppercase tracking-wider font-medium"
            style={{ color: 'var(--text-secondary)' }}
          >
            {t('displayRange', language)}
          </div>
          <div
            className="text-sm sm:text-base font-bold mono"
            style={{ color: 'var(--text-primary)' }}
          >
            {validHistory.length > MAX_DISPLAY_POINTS
              ? `${t('recent', language)} ${MAX_DISPLAY_POINTS}`
              : t('allData', language)}
          </div>
        </div>
      </div>
    </div>
  )
}
