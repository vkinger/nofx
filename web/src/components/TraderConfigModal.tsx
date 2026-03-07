import { useState, useEffect } from 'react'
import type { AIModel, Exchange, CreateTraderRequest, Strategy } from '../types'
import { useLanguage } from '../contexts/LanguageContext'
import { useTheme } from '../contexts/ThemeContext'
import { t } from '../i18n/translations'
import { toast } from 'sonner'
import { Pencil, Plus, X as IconX, Sparkles, ExternalLink, UserPlus } from 'lucide-react'
import { httpClient } from '../lib/httpClient'

// 提取下划线后面的名称部分
function getShortName(fullName: string): string {
  const parts = fullName.split('_')
  return parts.length > 1 ? parts[parts.length - 1] : fullName
}

// 交易所注册链接配置
const EXCHANGE_REGISTRATION_LINKS: Record<string, { url: string; hasReferral?: boolean }> = {
  binance: { url: 'https://www.binance.com/join?ref=NOFXENG', hasReferral: true },
  okx: { url: 'https://www.okx.com/join/1865360', hasReferral: true },
  bybit: { url: 'https://partner.bybit.com/b/83856', hasReferral: true },
  hyperliquid: { url: 'https://app.hyperliquid.xyz/join/AITRADING', hasReferral: true },
  aster: { url: 'https://www.asterdex.com/en/referral/fdfc0e', hasReferral: true },
  lighter: { url: 'https://app.lighter.xyz/?referral=68151432', hasReferral: true },
}

import type { TraderConfigData } from '../types'

// 表单内部状态类型
interface FormState {
  trader_id?: string
  trader_name: string
  ai_model: string
  exchange_id: string
  strategy_id: string
  is_cross_margin: boolean
  show_in_competition: boolean
  scan_interval_minutes: number
  initial_balance?: number
  use_analyst_flow: boolean
  use_compliance_flow: boolean
  analyst_model_id: string   // 分析师专用模型，空=与交易员同模型
  compliance_model_id: string // 风控官专用模型，空=与交易员同模型
}

interface TraderConfigModalProps {
  isOpen: boolean
  onClose: () => void
  traderData?: TraderConfigData | null
  isEditMode?: boolean
  availableModels?: AIModel[]
  availableExchanges?: Exchange[]
  onSave?: (data: CreateTraderRequest) => Promise<void>
}

export function TraderConfigModal({
  isOpen,
  onClose,
  traderData,
  isEditMode = false,
  availableModels = [],
  availableExchanges = [],
  onSave,
}: TraderConfigModalProps) {
  const { language } = useLanguage()
  const { theme } = useTheme()
  const isDark = theme === 'dark'
  const [formData, setFormData] = useState<FormState>({
    trader_name: '',
    ai_model: '',
    exchange_id: '',
    strategy_id: '',
    is_cross_margin: true,
    show_in_competition: true,
    scan_interval_minutes: 3,
    use_analyst_flow: false,
    use_compliance_flow: false,
    analyst_model_id: '',
    compliance_model_id: '',
  })
  const [isSaving, setIsSaving] = useState(false)
  const [strategies, setStrategies] = useState<Strategy[]>([])
  const [isFetchingBalance, setIsFetchingBalance] = useState(false)
  const [balanceFetchError, setBalanceFetchError] = useState<string>('')

  // 获取用户的策略列表
  useEffect(() => {
    const fetchStrategies = async () => {
      try {
        const result = await httpClient.get<{ strategies: Strategy[] }>('/api/strategies')
        if (result.success && result.data?.strategies) {
          const strategyList = result.data.strategies
          setStrategies(strategyList)
          // 如果没有选择策略，默认选中激活的策略
          if (!formData.strategy_id && !isEditMode) {
            const activeStrategy = strategyList.find(s => s.is_active)
            if (activeStrategy) {
              setFormData(prev => ({ ...prev, strategy_id: activeStrategy.id }))
            } else if (strategyList.length > 0) {
              setFormData(prev => ({ ...prev, strategy_id: strategyList[0].id }))
            }
          }
        }
      } catch (error) {
        console.error('Failed to fetch strategies:', error)
      }
    }
    if (isOpen) {
      fetchStrategies()
    }
  }, [isOpen])

  useEffect(() => {
    if (traderData) {
      setFormData({
        ...traderData,
        strategy_id: traderData.strategy_id || '',
        use_analyst_flow: traderData.use_analyst_flow ?? false,
        use_compliance_flow: traderData.use_compliance_flow ?? false,
        analyst_model_id: traderData.analyst_model_id ?? '',
        compliance_model_id: traderData.compliance_model_id ?? '',
      })
    } else if (!isEditMode) {
      setFormData({
        trader_name: '',
        ai_model: availableModels[0]?.id || '',
        exchange_id: availableExchanges[0]?.id || '',
        strategy_id: '',
        is_cross_margin: true,
        show_in_competition: true,
        scan_interval_minutes: 3,
        use_analyst_flow: false,
        use_compliance_flow: false,
        analyst_model_id: '',
        compliance_model_id: '',
      })
    }
  }, [traderData, isEditMode, availableModels, availableExchanges])

  if (!isOpen) return null

  const selectedExchange = availableExchanges.find((e) => e.id === formData.exchange_id)
  const isPaperExchange = selectedExchange?.exchange_type === 'paper'
  const defaultPaperInitialBalance = 100

  const handleInputChange = (field: keyof FormState, value: any) => {
    setFormData((prev) => ({ ...prev, [field]: value }))
  }

  const handleFetchCurrentBalance = async () => {
    if (!isEditMode || !traderData?.trader_id) {
       setBalanceFetchError(t('fetchBalanceEditModeOnly', language))
      return
    }

    setIsFetchingBalance(true)
    setBalanceFetchError('')

    try {
      const result = await httpClient.get<{
        total_equity?: number
        balance?: number
      }>(`/api/account?trader_id=${traderData.trader_id}`)

      if (result.success && result.data) {
        const currentBalance =
          result.data.total_equity || result.data.balance || 0
        setFormData((prev) => ({ ...prev, initial_balance: currentBalance }))
        toast.success(t('balanceFetched', language))
      } else {
        throw new Error(result.message || t('balanceFetchFailed', language))
      }
    } catch (error) {
      console.error(t('balanceFetchFailed', language) + ':', error)
       setBalanceFetchError(t('balanceFetchNetworkError', language))
    } finally {
      setIsFetchingBalance(false)
    }
  }

  const handleSave = async () => {
    if (!onSave) return
    if (!isEditMode && !formData.strategy_id) {
      toast.error('请先选择交易策略，创建后交易员将使用该策略配置运行')
      return
    }

    setIsSaving(true)
    try {
      const saveData: CreateTraderRequest = {
        name: formData.trader_name,
        ai_model_id: formData.ai_model,
        exchange_id: formData.exchange_id,
        strategy_id: formData.strategy_id || undefined,
        is_cross_margin: formData.is_cross_margin,
        show_in_competition: formData.show_in_competition,
        scan_interval_minutes: formData.scan_interval_minutes,
        use_analyst_flow: formData.use_analyst_flow,
        use_compliance_flow: formData.use_compliance_flow,
        analyst_model_id: formData.analyst_model_id || undefined,
        compliance_model_id: formData.compliance_model_id || undefined,
      }

      // 编辑模式：提交已填写的初始余额；创建虚拟盘：必须带初始资金（默认 10000）
      if (isEditMode && formData.initial_balance !== undefined) {
        saveData.initial_balance = formData.initial_balance
      } else if (!isEditMode && isPaperExchange) {
        saveData.initial_balance = formData.initial_balance ?? defaultPaperInitialBalance
      }

      await toast.promise(onSave(saveData), {
        loading: t('saving', language),
        success: t('saveSuccess', language),
        error: t('saveFailed', language),
      })
      onClose()
    } catch (error) {
       console.error(t('saveFailed', language) + ':', error)
    } finally {
      setIsSaving(false)
    }
  }

  const selectedStrategy = strategies.find(s => s.id === formData.strategy_id)

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black bg-opacity-50 backdrop-blur-sm p-4 overflow-y-auto">
      <div
        className="rounded-xl shadow-2xl max-w-2xl w-full my-8 trader-config-modal flex flex-col"
        style={{
          maxHeight: 'calc(100vh - 4rem)',
          background: isDark
            ? 'linear-gradient(135deg, rgba(30, 36, 42, 0.95) 0%, rgba(24, 28, 33, 0.95) 100%)'
            : 'var(--panel-bg)',
          border: '1px solid var(--panel-border)',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div
          className="flex items-center justify-between p-6 border-b sticky top-0 z-10 rounded-t-xl"
          style={{
            borderBottom: '1px solid var(--panel-border)',
            background: isDark
              ? 'linear-gradient(135deg, rgba(30, 36, 42, 0.95) 0%, rgba(37, 43, 53, 0.95) 100%)'
              : 'var(--panel-bg)',
          }}
        >
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-lg bg-gradient-to-br from-[#F0B90B] to-[#E1A706] flex items-center justify-center text-black">
              {isEditMode ? (
                <Pencil className="w-5 h-5" />
              ) : (
                <Plus className="w-5 h-5" />
              )}
            </div>
            <div>
              <h2 className="text-xl font-bold text-[#EAECEF]">
                {isEditMode ? t('editTrader', language) : t('createTrader', language)}
              </h2>
              <p className="text-sm text-[#848E9C] mt-1">
                {isEditMode ? t('editTraderConfig', language) : t('selectStrategyAndConfigParams', language)}
              </p>
            </div>
          </div>
          <button
            onClick={onClose}
            className="w-8 h-8 rounded-lg transition-colors flex items-center justify-center"
            style={{
              color: 'var(--text-secondary)',
            }}
            onMouseEnter={(e) => {
              e.currentTarget.style.color = 'var(--text-primary)'
              e.currentTarget.style.background = 'var(--panel-bg-hover)'
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.color = 'var(--text-secondary)'
              e.currentTarget.style.background = 'transparent'
            }}
          >
            <IconX className="w-4 h-4" />
          </button>
        </div>

        {/* Content - flex-1 min-h-0 确保可滚动且底部按钮始终可见 */}
        <div
          className="p-6 space-y-6 overflow-y-auto flex-1 min-h-0"
        >
          <style>{`
            .trader-config-modal input::placeholder,
            .trader-config-modal textarea::placeholder {
              color: var(--text-secondary);
              opacity: 0.7;
            }
            .trader-config-modal select option {
              background: var(--panel-bg);
              color: var(--text-primary);
            }
            .trader-config-modal select option:checked {
              background: rgba(240, 185, 11, 0.2);
              color: var(--nofx-gold);
              font-weight: 600;
            }
            .trader-config-modal select:not([value=""]) {
              border-color: var(--nofx-gold);
              box-shadow: 0 0 0 1px rgba(240, 185, 11, 0.2);
            }
            .trader-config-modal select:not([value=""]) option:checked {
              background: rgba(240, 185, 11, 0.3);
              color: var(--nofx-gold);
            }
          `}</style>
          {/* Basic Info */}
          <div className="bg-[#0B0E11] border border-[#2B3139] rounded-lg p-5">
            <h3 className="text-lg font-semibold text-[#EAECEF] mb-5 flex items-center gap-2">
              <span className="text-[#F0B90B]">1</span> {t('basicConfig', language)}
            </h3>
            <div className="space-y-4">
              <div>
                <label className="text-sm text-[#EAECEF] block mb-2">
                  {t('traderNameRequired', language)}
                </label>
                <input
                  type="text"
                  value={formData.trader_name}
                  onChange={(e) =>
                    handleInputChange('trader_name', e.target.value)
                  }
                  className="w-full px-3 py-2 bg-[#0B0E11] border border-[#2B3139] rounded text-[#EAECEF] focus:border-[#F0B90B] focus:outline-none"
                   placeholder={t('enterTraderNamePlaceholder', language)}
                />
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="text-sm text-[#EAECEF] block mb-2">
                  {t('aiModelRequired', language)}
                  </label>
                  <select
                    value={formData.ai_model}
                    onChange={(e) =>
                      handleInputChange('ai_model', e.target.value)
                    }
                    className="w-full px-3 py-2 rounded transition-all focus:outline-none"
                    style={{
                      background: formData.ai_model
                        ? (isDark ? 'rgba(240, 185, 11, 0.1)' : 'rgba(240, 185, 11, 0.08)')
                        : 'var(--panel-bg)',
                      border: formData.ai_model
                        ? '1px solid var(--nofx-gold)'
                        : '1px solid var(--panel-border)',
                      color: 'var(--text-primary)',
                      boxShadow: formData.ai_model ? '0 0 0 1px rgba(240, 185, 11, 0.2)' : 'none',
                    }}
                    onFocus={(e) => {
                      e.currentTarget.style.borderColor = 'var(--nofx-gold)'
                      e.currentTarget.style.boxShadow = '0 0 0 2px rgba(240, 185, 11, 0.3)'
                    }}
                    onBlur={(e) => {
                      if (formData.ai_model) {
                        e.currentTarget.style.borderColor = 'var(--nofx-gold)'
                        e.currentTarget.style.boxShadow = '0 0 0 1px rgba(240, 185, 11, 0.2)'
                      } else {
                        e.currentTarget.style.borderColor = 'var(--panel-border)'
                        e.currentTarget.style.boxShadow = 'none'
                      }
                    }}
                  >
                    {availableModels.map((model) => (
                      <option key={model.id} value={model.id}>
                        {getShortName(model.name || model.id).toUpperCase()}
                      </option>
                    ))}
                  </select>
                </div>
                <div>
                  <label className="text-sm text-[#EAECEF] block mb-2">
                  {t('exchangeRequired', language)}
                  </label>
                  <select
                    value={formData.exchange_id}
                    onChange={(e) =>
                      handleInputChange('exchange_id', e.target.value)
                    }
                    className="w-full px-3 py-2 rounded transition-all focus:outline-none"
                    style={{
                      background: formData.exchange_id
                        ? (isDark ? 'rgba(240, 185, 11, 0.1)' : 'rgba(240, 185, 11, 0.08)')
                        : 'var(--panel-bg)',
                      border: formData.exchange_id
                        ? '1px solid var(--nofx-gold)'
                        : '1px solid var(--panel-border)',
                      color: 'var(--text-primary)',
                      boxShadow: formData.exchange_id ? '0 0 0 1px rgba(240, 185, 11, 0.2)' : 'none',
                    }}
                    onFocus={(e) => {
                      e.currentTarget.style.borderColor = 'var(--nofx-gold)'
                      e.currentTarget.style.boxShadow = '0 0 0 2px rgba(240, 185, 11, 0.3)'
                    }}
                    onBlur={(e) => {
                      if (formData.exchange_id) {
                        e.currentTarget.style.borderColor = 'var(--nofx-gold)'
                        e.currentTarget.style.boxShadow = '0 0 0 1px rgba(240, 185, 11, 0.2)'
                      } else {
                        e.currentTarget.style.borderColor = 'var(--panel-border)'
                        e.currentTarget.style.boxShadow = 'none'
                      }
                    }}
                  >
                    {availableExchanges.map((exchange) => (
                      <option key={exchange.id} value={exchange.id}>
                        {getShortName(exchange.name || exchange.exchange_type || exchange.id).toUpperCase()}
                        {exchange.account_name ? ` - ${exchange.account_name}` : ''}
                      </option>
                    ))}
                  </select>
                  {/* Exchange Registration Link */}
                  {formData.exchange_id && (() => {
                    // Find the selected exchange to get its type
                    const selectedExchange = availableExchanges.find(e => e.id === formData.exchange_id)
                    const exchangeType = selectedExchange?.exchange_type?.toLowerCase() || ''
                    const regLink = EXCHANGE_REGISTRATION_LINKS[exchangeType]
                    if (!regLink) return null
                    return (
                      <a
                        href={regLink.url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="mt-2 inline-flex items-center gap-1.5 text-xs transition-colors"
                        style={{ color: 'var(--text-secondary)' }}
                        onMouseEnter={(e) => {
                          e.currentTarget.style.color = 'var(--nofx-gold)'
                        }}
                        onMouseLeave={(e) => {
                          e.currentTarget.style.color = 'var(--text-secondary)'
                        }}
                      >
                        <UserPlus className="w-3.5 h-3.5" />
                        <span>{t('noExchangeAccount', language)}</span>
                        {regLink.hasReferral && (
                          <span className="px-1.5 py-0.5 bg-[#F0B90B]/10 text-[#F0B90B] rounded text-[10px]">
                            {t('discount', language)}
                          </span>
                        )}
                        <ExternalLink className="w-3 h-3" />
                      </a>
                    )
                  })()}
                </div>
              </div>
            </div>
          </div>

          {/* Strategy Selection */}
          <div className="bg-[#0B0E11] border border-[#2B3139] rounded-lg p-5">
            <h3 className="text-lg font-semibold text-[#EAECEF] mb-5 flex items-center gap-2">
              <span className="text-[#F0B90B]">2</span> {t('selectTradingStrategy', language)}
              <Sparkles className="w-4 h-4 text-[#F0B90B]" />
            </h3>
            <div className="space-y-4">
              <div>
                <label className="text-sm text-[#EAECEF] block mb-2">
                  {t('useStrategy', language)}
                </label>
                <select
                  value={formData.strategy_id}
                  onChange={(e) =>
                    handleInputChange('strategy_id', e.target.value)
                  }
                  className="w-full px-3 py-2 rounded transition-all focus:outline-none"
                  style={{
                    background: formData.strategy_id
                      ? (isDark ? 'rgba(240, 185, 11, 0.1)' : 'rgba(240, 185, 11, 0.08)')
                      : 'var(--panel-bg)',
                    border: formData.strategy_id
                      ? '1px solid var(--nofx-gold)'
                      : '1px solid var(--panel-border)',
                    color: 'var(--text-primary)',
                    boxShadow: formData.strategy_id ? '0 0 0 1px rgba(240, 185, 11, 0.2)' : 'none',
                  }}
                  onFocus={(e) => {
                    e.currentTarget.style.borderColor = 'var(--nofx-gold)'
                    e.currentTarget.style.boxShadow = '0 0 0 2px rgba(240, 185, 11, 0.3)'
                  }}
                  onBlur={(e) => {
                    if (formData.strategy_id) {
                      e.currentTarget.style.borderColor = 'var(--nofx-gold)'
                      e.currentTarget.style.boxShadow = '0 0 0 1px rgba(240, 185, 11, 0.2)'
                    } else {
                      e.currentTarget.style.borderColor = 'var(--panel-border)'
                      e.currentTarget.style.boxShadow = 'none'
                    }
                  }}
                >
                  <option value="">{t('noStrategyManual', language)}</option>
                  {strategies.map((strategy) => (
                    <option key={strategy.id} value={strategy.id}>
                      {strategy.name}
                      {strategy.is_active ? t('activeTag', language) : ''}
                      {strategy.is_default ? t('default', language) : ''}
                    </option>
                  ))}
                </select>
                {strategies.length === 0 && (
                    <p className="text-xs text-[#848E9C] mt-2">
                      {t('noStrategyHint', language)}
                  </p>
                )}
              </div>

              {/* Strategy Preview */}
              {selectedStrategy && (
                <div
                  className="mt-3 p-4 rounded-lg"
                  style={{
                    background: isDark ? 'rgba(30, 35, 41, 0.6)' : 'var(--panel-bg-hover)',
                    border: '1px solid var(--panel-border)',
                  }}
                >
                  <div className="flex items-center gap-2 mb-2">
                    <span className="text-[#F0B90B] text-sm font-medium">
                      {t('strategyDetails', language)}
                    </span>
                    {selectedStrategy.is_active && (
                      <span className="px-2 py-0.5 bg-green-500/20 text-green-400 text-xs rounded">
                        {t('activating', language)}
                      </span>
                    )}
                  </div>
                  <p className="text-sm text-[#848E9C] mb-2">
                    {selectedStrategy.description || (language === 'zh' ? '无描述' : 'No description')}
                  </p>
                  <div className="grid grid-cols-2 gap-2 text-xs" style={{ color: 'var(--text-secondary)' }}>
                    <div>
                      {t('coinSource', language)}: {selectedStrategy.config.coin_source.source_type === 'static' ? '固定币种' :
                        selectedStrategy.config.coin_source.source_type === 'ai500' ? 'AI500' :
                        selectedStrategy.config.coin_source.source_type === 'oi_top' ? 'OI Top' : '混合'}
                    </div>
                    <div>
                      {t('marginLimit', language)}: {((selectedStrategy.config.risk_control?.max_margin_usage || 0.9) * 100).toFixed(0)}%
                    </div>
                  </div>
                </div>
              )}
            </div>
          </div>

          {/* Agent 流程（多 Agent / 风控官） */}
          <div
            className="border rounded-lg p-5"
            style={{
              background: isDark ? 'rgba(11, 14, 17, 0.6)' : 'var(--panel-bg)',
              border: '1px solid var(--panel-border)',
            }}
          >
            <h3 className="text-lg font-semibold mb-5 flex items-center gap-2" style={{ color: 'var(--text-primary)' }}>
              <span style={{ color: 'var(--nofx-gold)' }}>2.5</span> Agent 流程
            </h3>
            <p className="text-sm mb-4" style={{ color: 'var(--text-secondary)' }}>
              策略配置决定风控、指标、币种来源等；此处选择是否启用多 Agent（分析师→交易员）与风控官审计。
            </p>
            <div className="space-y-3">
              <label className="flex items-center gap-3 cursor-pointer">
                <input
                  type="checkbox"
                  checked={formData.use_analyst_flow}
                  onChange={(e) => handleInputChange('use_analyst_flow', e.target.checked)}
                  className="w-4 h-4 rounded border-2"
                  style={{ accentColor: 'var(--nofx-gold)' }}
                />
                <span style={{ color: 'var(--text-primary)' }}>多 Agent（分析师 → 黑板 → 交易员）</span>
              </label>
              <p className="text-xs pl-7" style={{ color: 'var(--text-secondary)' }}>
                先跑宏观分析师写黑板，交易员再结合分析师报告与完整数据做决策
              </p>
              {formData.use_analyst_flow && (
                <div className="pl-7">
                  <label className="text-sm block mb-1" style={{ color: 'var(--text-primary)' }}>
                    分析师模型（可选）
                  </label>
                  <select
                    value={formData.analyst_model_id}
                    onChange={(e) => handleInputChange('analyst_model_id', e.target.value)}
                    className="w-full px-3 py-2 rounded text-sm"
                    style={{
                      background: 'var(--panel-bg)',
                      border: '1px solid var(--panel-border)',
                      color: 'var(--text-primary)',
                    }}
                  >
                    <option value="">与交易员同模型</option>
                    {availableModels.map((m) => (
                      <option key={m.id} value={m.id}>
                        {getShortName(m.name || m.id).toUpperCase()}
                      </option>
                    ))}
                  </select>
                  <p className="text-xs mt-1" style={{ color: 'var(--text-secondary)' }}>
                    不选则使用上方「AI 模型」；单独指定可避免推理模型占满 token 导致截断
                  </p>
                </div>
              )}
              <label className="flex items-center gap-3 cursor-pointer">
                <input
                  type="checkbox"
                  checked={formData.use_compliance_flow}
                  onChange={(e) => handleInputChange('use_compliance_flow', e.target.checked)}
                  className="w-4 h-4 rounded border-2"
                  style={{ accentColor: 'var(--nofx-gold)' }}
                />
                <span style={{ color: 'var(--text-primary)' }}>风控官审计（通过后才执行）</span>
              </label>
              <p className="text-xs pl-7" style={{ color: 'var(--text-secondary)' }}>
                交易员产出决策后先由风控官审计，通过后再执行
              </p>
              {formData.use_compliance_flow && (
                <div className="pl-7">
                  <label className="text-sm block mb-1" style={{ color: 'var(--text-primary)' }}>
                    风控官模型（可选）
                  </label>
                  <select
                    value={formData.compliance_model_id}
                    onChange={(e) => handleInputChange('compliance_model_id', e.target.value)}
                    className="w-full px-3 py-2 rounded text-sm"
                    style={{
                      background: 'var(--panel-bg)',
                      border: '1px solid var(--panel-border)',
                      color: 'var(--text-primary)',
                    }}
                  >
                    <option value="">与交易员同模型</option>
                    {availableModels.map((m) => (
                      <option key={m.id} value={m.id}>
                        {getShortName(m.name || m.id).toUpperCase()}
                      </option>
                    ))}
                  </select>
                </div>
              )}
            </div>
          </div>

          {/* Trading Parameters */}
          <div className="bg-[#0B0E11] border border-[#2B3139] rounded-lg p-5">
            <h3 className="text-lg font-semibold text-[#EAECEF] mb-5 flex items-center gap-2">
              <span className="text-[#F0B90B]">3</span> {t('tradingParams', language)}
            </h3>
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="text-sm text-[#EAECEF] block mb-2">
                    {t('marginMode', language)}
                  </label>
                  <div className="flex gap-2">
                    <button
                      type="button"
                      onClick={() => handleInputChange('is_cross_margin', true)}
                      className="flex-1 px-3 py-2 rounded text-sm transition-all font-medium"
                      style={{
                        background: formData.is_cross_margin
                          ? 'var(--nofx-gold)'
                          : 'var(--panel-bg)',
                        color: formData.is_cross_margin ? '#000' : 'var(--text-secondary)',
                        border: formData.is_cross_margin
                          ? '2px solid var(--nofx-gold)'
                          : '1px solid var(--panel-border)',
                        boxShadow: formData.is_cross_margin
                          ? '0 0 0 2px rgba(240, 185, 11, 0.2), 0 0 8px rgba(240, 185, 11, 0.3)'
                          : 'none',
                      }}
                    >
                      {t('crossMargin', language)}
                    </button>
                    <button
                      type="button"
                      onClick={() =>
                        handleInputChange('is_cross_margin', false)
                      }
                      className="flex-1 px-3 py-2 rounded text-sm transition-all font-medium"
                      style={{
                        background: !formData.is_cross_margin
                          ? 'var(--nofx-gold)'
                          : 'var(--panel-bg)',
                        color: !formData.is_cross_margin ? '#000' : 'var(--text-secondary)',
                        border: !formData.is_cross_margin
                          ? '2px solid var(--nofx-gold)'
                          : '1px solid var(--panel-border)',
                        boxShadow: !formData.is_cross_margin
                          ? '0 0 0 2px rgba(240, 185, 11, 0.2), 0 0 8px rgba(240, 185, 11, 0.3)'
                          : 'none',
                      }}
                    >
                      {t('isolatedMargin', language)}
                    </button>
                  </div>
                </div>
                <div>
                  <label className="text-sm block mb-2" style={{ color: 'var(--text-primary)' }}>
                    {t('aiScanInterval', language)}
                  </label>
                  <input
                    type="number"
                    value={formData.scan_interval_minutes}
                    onChange={(e) => {
                      const parsedValue = Number(e.target.value)
                      const safeValue = Number.isFinite(parsedValue)
                        ? Math.max(3, parsedValue)
                        : 3
                      handleInputChange('scan_interval_minutes', safeValue)
                    }}
                    className="w-full px-3 py-2 rounded transition-colors focus:outline-none"
                    style={{
                      background: 'var(--panel-bg)',
                      border: '1px solid var(--panel-border)',
                      color: 'var(--text-primary)',
                    }}
                    onFocus={(e) => {
                      e.currentTarget.style.borderColor = 'var(--nofx-gold)'
                    }}
                    onBlur={(e) => {
                      e.currentTarget.style.borderColor = 'var(--panel-border)'
                    }}
                    min="3"
                    max="60"
                    step="1"
                  />
                  <p className="text-xs mt-1" style={{ color: 'var(--text-secondary)' }}>
                    {t('scanIntervalRecommend', language)}
                  </p>
                </div>
              </div>

              {/* Competition visibility */}
              <div>
                <label className="text-sm text-[#EAECEF] block mb-2">
                  {t('competitionDisplay', language)}
                </label>
                <div className="flex gap-2">
                  <button
                    type="button"
                    onClick={() => handleInputChange('show_in_competition', true)}
                    className="flex-1 px-3 py-2 rounded text-sm transition-all font-medium"
                    style={{
                      background: formData.show_in_competition
                        ? 'var(--nofx-gold)'
                        : 'var(--panel-bg)',
                      color: formData.show_in_competition ? '#000' : 'var(--text-secondary)',
                      border: formData.show_in_competition
                        ? '2px solid var(--nofx-gold)'
                        : '1px solid var(--panel-border)',
                      boxShadow: formData.show_in_competition
                        ? '0 0 0 2px rgba(240, 185, 11, 0.2), 0 0 8px rgba(240, 185, 11, 0.3)'
                        : 'none',
                    }}
                  >
                    {t('show', language)}
                  </button>
                  <button
                    type="button"
                    onClick={() => handleInputChange('show_in_competition', false)}
                    className="flex-1 px-3 py-2 rounded text-sm transition-all font-medium"
                    style={{
                      background: !formData.show_in_competition
                        ? 'var(--nofx-gold)'
                        : 'var(--panel-bg)',
                      color: !formData.show_in_competition ? '#000' : 'var(--text-secondary)',
                      border: !formData.show_in_competition
                        ? '2px solid var(--nofx-gold)'
                        : '1px solid var(--panel-border)',
                      boxShadow: !formData.show_in_competition
                        ? '0 0 0 2px rgba(240, 185, 11, 0.2), 0 0 8px rgba(240, 185, 11, 0.3)'
                        : 'none',
                    }}
                  >
                    {t('hide', language)}
                  </button>
                </div>
                  <p className="text-xs text-[#848E9C] mt-1">
                    {t('hiddenInCompetition', language)}
                </p>
              </div>

              {/* Initial Balance: 编辑模式始终显示；创建时仅虚拟盘显示（必填，默认 10000） */}
              {(isEditMode || isPaperExchange) && (
                <div>
                  <div className="flex items-center justify-between mb-2">
                    <label className="text-sm text-[#EAECEF]">
                      {t('initialBalanceLabel', language)}
                    </label>
                    <button
                      type="button"
                      onClick={handleFetchCurrentBalance}
                      disabled={isFetchingBalance}
                      className="px-3 py-1 text-xs bg-[#F0B90B] text-black rounded hover:bg-[#E1A706] transition-colors disabled:bg-[#848E9C] disabled:cursor-not-allowed"
                    >
                      {isFetchingBalance ? t('fetching', language) : t('fetchCurrentBalance', language)}
                    </button>
                  </div>
                  <input
                    type="number"
                    value={
                      formData.initial_balance ??
                      (isPaperExchange && !isEditMode ? defaultPaperInitialBalance : 0)
                    }
                    onChange={(e) =>
                      handleInputChange(
                        'initial_balance',
                        Number(e.target.value) || undefined
                      )
                    }
                    className="w-full px-3 py-2 rounded transition-colors focus:outline-none"
                    style={{
                      background: 'var(--panel-bg)',
                      border: '1px solid var(--panel-border)',
                      color: 'var(--text-primary)',
                    }}
                    onFocus={(e) => {
                      e.currentTarget.style.borderColor = 'var(--nofx-gold)'
                    }}
                    onBlur={(e) => {
                      e.currentTarget.style.borderColor = 'var(--panel-border)'
                    }}
                    min={isPaperExchange && !isEditMode ? 1 : 100}
                    step="0.01"
                  />
                    <p className="text-xs text-[#848E9C] mt-1">
                      {t('balanceUpdateHint', language)}
                  <p className="text-xs mt-1" style={{ color: 'var(--text-secondary)' }}>
                    {isPaperExchange && !isEditMode
                      ? language === 'zh'
                        ? '虚拟盘模拟本金，默认 10000 USDT，可修改'
                        : 'Paper trading simulated balance, default 10000 USDT'
                      : '用于手动更新初始余额基准（例如充值/提现后）'}
                  </p>
                  {balanceFetchError && (
                    <p className="text-xs text-red-500 mt-1">
                      {balanceFetchError}
                    </p>
                  )}
                </div>
              )}

              {/* Create mode info */}
              {!isEditMode && (
                <div
                  className="p-3 rounded flex items-center gap-2"
                  style={{
                    background: isDark ? 'rgba(30, 35, 41, 0.6)' : 'var(--panel-bg-hover)',
                    border: '1px solid var(--panel-border)',
                  }}
                >
                  <svg
                    xmlns="http://www.w3.org/2000/svg"
                    className="w-4 h-4"
                    style={{ color: 'var(--nofx-gold)' }}
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  >
                    <circle cx="12" cy="12" r="10" />
                    <line x1="12" x2="12" y1="8" y2="12" />
                    <line x1="12" x2="12.01" y1="16" y2="16" />
                  </svg>
                  <span className="text-sm text-[#848E9C]">
                    {t('autoFetchBalanceInfo', language)}
                  </span>
                </div>
              )}
            </div>
          </div>

        </div>

        {/* Footer - flex-shrink-0 确保保存按钮始终可见 */}
        <div
          className="flex justify-end gap-3 p-6 border-t flex-shrink-0 rounded-b-xl"
          style={{
            borderTop: '1px solid var(--panel-border)',
            background: isDark
              ? 'linear-gradient(135deg, rgba(30, 36, 42, 0.95) 0%, rgba(37, 43, 53, 0.95) 100%)'
              : 'var(--panel-bg)',
          }}
        >
          <button
            onClick={onClose}
            className="px-6 py-3 rounded-lg transition-all duration-200"
            style={{
              background: 'var(--panel-bg-hover)',
              color: 'var(--text-primary)',
              border: '1px solid var(--panel-border)',
            }}
            onMouseEnter={(e) => {
              e.currentTarget.style.background = 'var(--panel-bg)'
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.background = 'var(--panel-bg-hover)'
            }}
          >
            {t('cancel', language)}
          </button>
          {onSave && (
            <button
              onClick={handleSave}
              disabled={
                isSaving ||
                !formData.trader_name ||
                !formData.ai_model ||
                !formData.exchange_id
              }
              className="px-8 py-3 rounded-lg transition-all duration-200 disabled:cursor-not-allowed font-medium shadow-lg"
              style={{
                background: (isSaving || !formData.trader_name || !formData.ai_model || !formData.exchange_id)
                  ? 'var(--text-disabled)'
                  : 'linear-gradient(to right, var(--nofx-gold), #E1A706)',
                color: '#000',
              }}
              onMouseEnter={(e) => {
                if (!isSaving && formData.trader_name && formData.ai_model && formData.exchange_id) {
                  e.currentTarget.style.background = 'linear-gradient(to right, #E1A706, #D4951E)'
                }
              }}
              onMouseLeave={(e) => {
                if (!isSaving && formData.trader_name && formData.ai_model && formData.exchange_id) {
                  e.currentTarget.style.background = 'linear-gradient(to right, var(--nofx-gold), #E1A706)'
                }
              }}
            >
              {isSaving ? t('saving', language) : isEditMode ? t('editTrader', language) : t('createTraderButton', language)}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
