import { useEffect, useState } from 'react'
import { X } from 'lucide-react'
import { api } from '../lib/api'
import { t, type Language } from '../i18n/translations'
import type { RoundDetailResponse } from '../types'

interface RoundDetailModalProps {
  traderId: string
  roundId: number
  onClose: () => void
  language: Language
}

export function RoundDetailModal({ traderId, roundId, onClose, language }: RoundDetailModalProps) {
  const [data, setData] = useState<RoundDetailResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    api
      .getRoundDetail(traderId, roundId)
      .then((res) => {
        if (!cancelled) setData(res)
      })
      .catch((e) => {
        if (!cancelled) setError(e?.message || 'Failed to load')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [traderId, roundId])

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      style={{ background: 'rgba(0,0,0,0.6)' }}
      onClick={onClose}
    >
      <div
        className="rounded-xl shadow-2xl w-full max-w-3xl max-h-[90vh] overflow-hidden flex flex-col"
        style={{
          background: 'var(--panel-bg)',
          border: '1px solid var(--panel-border)',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-6 py-4 border-b" style={{ borderColor: 'var(--panel-border)' }}>
          <h2 className="text-lg font-bold" style={{ color: 'var(--text-primary)' }}>
            {t('roundDetailView', language)} · #{data?.decision_record?.cycle_number ?? roundId}
          </h2>
          <button
            type="button"
            onClick={onClose}
            className="p-2 rounded-lg hover:opacity-80 transition-opacity"
            style={{ color: 'var(--text-secondary)' }}
            aria-label="Close"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto p-6 space-y-6">
          {loading && (
            <div className="py-12 text-center" style={{ color: 'var(--text-secondary)' }}>
              Loading…
            </div>
          )}
          {error && (
            <div className="py-8 text-center text-red-400">
              {error}
            </div>
          )}
          {!loading && !error && data && (
            <>
              {/* 1. 分析师报告 */}
              <section>
                <h3 className="text-sm font-semibold mb-2 flex items-center gap-2" style={{ color: 'var(--text-primary)' }}>
                  <span>📊</span> {t('analystReport', language)}
                </h3>
                <div
                  className="rounded-lg p-4 text-sm whitespace-pre-wrap"
                  style={{
                    background: 'var(--panel-bg-hover)',
                    border: '1px solid var(--panel-border)',
                    color: 'var(--text-secondary)',
                  }}
                >
                  {data.analyst_report ? (
                    <>
                      <div className="flex gap-3 mb-3">
                        <span className="font-medium" style={{ color: 'var(--text-primary)' }}>
                          {data.analyst_report.bias}
                        </span>
                        <span>Confidence: {data.analyst_report.confidence}%</span>
                      </div>
                      {data.analyst_report.report_text || t('phaseNone', language)}
                    </>
                  ) : (
                    t('phaseNone', language)
                  )}
                </div>
                {data.analyst_report && (data.analyst_report.system_prompt || data.analyst_report.user_prompt) && (
                  <div className="mt-3 space-y-3">
                    <div className="text-xs font-medium" style={{ color: 'var(--nofx-gold)' }}>
                      {language === 'zh' ? '分析师 系统提示词 / 用户提示词' : 'Analyst system & user prompts'}
                    </div>
                    {data.analyst_report.system_prompt && (
                      <div
                        className="rounded-lg p-4 text-sm whitespace-pre-wrap overflow-x-auto"
                        style={{
                          background: 'var(--panel-bg-hover)',
                          border: '1px solid var(--panel-border)',
                          color: 'var(--text-secondary)',
                          maxHeight: '200px',
                          overflowY: 'auto',
                        }}
                      >
                        <span className="text-xs font-medium" style={{ color: 'var(--nofx-gold)' }}>{language === 'zh' ? '系统提示词' : 'System'}: </span>
                        {data.analyst_report.system_prompt}
                      </div>
                    )}
                    {data.analyst_report.user_prompt && (
                      <div
                        className="rounded-lg p-4 text-sm whitespace-pre-wrap overflow-x-auto"
                        style={{
                          background: 'var(--panel-bg-hover)',
                          border: '1px solid var(--panel-border)',
                          color: 'var(--text-secondary)',
                          maxHeight: '200px',
                          overflowY: 'auto',
                        }}
                      >
                        <span className="text-xs font-medium" style={{ color: 'var(--nofx-gold)' }}>{language === 'zh' ? '用户提示词' : 'User'}: </span>
                        {data.analyst_report.user_prompt}
                      </div>
                    )}
                  </div>
                )}
              </section>

              {/* 2. 交易员推理与决策 */}
              <section>
                <h3 className="text-sm font-semibold mb-2 flex items-center gap-2" style={{ color: 'var(--text-primary)' }}>
                  <span>🤖</span> {t('traderCoTDecisions', language)}
                </h3>
                <div
                  className="rounded-lg p-4 text-sm space-y-4"
                  style={{
                    background: 'var(--panel-bg-hover)',
                    border: '1px solid var(--panel-border)',
                  }}
                >
                  {data.decision_record.cot_trace ? (
                    <div className="whitespace-pre-wrap" style={{ color: 'var(--text-secondary)' }}>
                      {data.decision_record.cot_trace}
                    </div>
                  ) : (
                    <div style={{ color: 'var(--text-muted)' }}>{t('cotEmpty', language)}</div>
                  )}
                  {data.decision_record.decisions && data.decision_record.decisions.length > 0 && (
                    <div>
                      <div className="text-xs font-medium mb-2" style={{ color: 'var(--text-secondary)' }}>
                        Decisions
                      </div>
                      <ul className="list-disc list-inside space-y-1 text-sm" style={{ color: 'var(--text-primary)' }}>
                        {data.decision_record.decisions.map((a, i) => (
                          <li key={i}>
                            {a.action} {a.symbol}
                            {a.reasoning ? ` — ${a.reasoning}` : ''}
                          </li>
                        ))}
                      </ul>
                    </div>
                  )}
                </div>
              </section>

              {/* 2.5 交易员 系统提示词 / 用户提示词（Agent 模式单轮详情） */}
              <section>
                <h3 className="text-sm font-semibold mb-2 flex items-center gap-2" style={{ color: 'var(--text-primary)' }}>
                  <span>📝</span> {language === 'zh' ? '交易员 系统提示词 / 用户提示词' : 'Trader system & user prompts'}
                </h3>
                <div className="space-y-3">
                  {data.decision_record.system_prompt && (
                    <div
                      className="rounded-lg p-4 text-sm whitespace-pre-wrap overflow-x-auto"
                      style={{
                        background: 'var(--panel-bg-hover)',
                        border: '1px solid var(--panel-border)',
                        color: 'var(--text-secondary)',
                        maxHeight: '240px',
                        overflowY: 'auto',
                      }}
                    >
                      <div className="text-xs font-medium mb-2" style={{ color: 'var(--nofx-gold)' }}>
                        {language === 'zh' ? '系统提示词' : 'System prompt'}
                      </div>
                      {data.decision_record.system_prompt}
                    </div>
                  )}
                  {data.decision_record.input_prompt && (
                    <div
                      className="rounded-lg p-4 text-sm whitespace-pre-wrap overflow-x-auto"
                      style={{
                        background: 'var(--panel-bg-hover)',
                        border: '1px solid var(--panel-border)',
                        color: 'var(--text-secondary)',
                        maxHeight: '240px',
                        overflowY: 'auto',
                      }}
                    >
                      <div className="text-xs font-medium mb-2" style={{ color: 'var(--nofx-gold)' }}>
                        {language === 'zh' ? '用户提示词' : 'User prompt'}
                      </div>
                      {data.decision_record.input_prompt}
                    </div>
                  )}
                  {!data.decision_record.system_prompt && !data.decision_record.input_prompt && (
                    <div className="rounded-lg p-4 text-sm" style={{ color: 'var(--text-muted)' }}>
                      {language === 'zh' ? '本轮回测未记录提示词' : 'Prompts not recorded for this round'}
                    </div>
                  )}
                </div>
              </section>

              {/* 3. 风控审计 */}
              <section>
                <h3 className="text-sm font-semibold mb-2 flex items-center gap-2" style={{ color: 'var(--text-primary)' }}>
                  <span>🛡️</span> {t('complianceAudit', language)}
                </h3>
                <div
                  className="rounded-lg p-4 text-sm"
                  style={{
                    background: 'var(--panel-bg-hover)',
                    border: '1px solid var(--panel-border)',
                    color: 'var(--text-secondary)',
                  }}
                >
                  {data.compliance_audit ? (
                    <>
                      <div className="flex items-center gap-2 mb-2">
                        <span
                          className="px-2 py-0.5 rounded text-xs font-medium"
                          style={{
                            background: data.compliance_audit.approved ? 'rgba(14, 203, 129, 0.2)' : 'rgba(246, 70, 93, 0.2)',
                            color: data.compliance_audit.approved ? '#0ECB81' : '#F6465D',
                          }}
                        >
                          {data.compliance_audit.approved
                            ? (language === 'zh' ? '通过' : 'Approved')
                            : (language === 'zh' ? '驳回' : 'Rejected')}
                        </span>
                      </div>
                      {data.compliance_audit.reason || t('phaseNone', language)}
                      {data.compliance_audit.violations_json && (
                        <pre className="mt-2 text-xs overflow-x-auto">
                          {data.compliance_audit.violations_json}
                        </pre>
                      )}
                    </>
                  ) : (
                    t('phaseNone', language)
                  )}
                </div>
                {data.compliance_audit && (data.compliance_audit.system_prompt || data.compliance_audit.user_prompt) && (
                  <div className="mt-3 space-y-3">
                    <div className="text-xs font-medium" style={{ color: 'var(--nofx-gold)' }}>
                      {language === 'zh' ? '风控官 系统提示词 / 用户提示词' : 'Compliance system & user prompts'}
                    </div>
                    {data.compliance_audit.system_prompt && (
                      <div
                        className="rounded-lg p-4 text-sm whitespace-pre-wrap overflow-x-auto"
                        style={{
                          background: 'var(--panel-bg-hover)',
                          border: '1px solid var(--panel-border)',
                          color: 'var(--text-secondary)',
                          maxHeight: '200px',
                          overflowY: 'auto',
                        }}
                      >
                        <span className="text-xs font-medium" style={{ color: 'var(--nofx-gold)' }}>{language === 'zh' ? '系统提示词' : 'System'}: </span>
                        {data.compliance_audit.system_prompt}
                      </div>
                    )}
                    {data.compliance_audit.user_prompt && (
                      <div
                        className="rounded-lg p-4 text-sm whitespace-pre-wrap overflow-x-auto"
                        style={{
                          background: 'var(--panel-bg-hover)',
                          border: '1px solid var(--panel-border)',
                          color: 'var(--text-secondary)',
                          maxHeight: '200px',
                          overflowY: 'auto',
                        }}
                      >
                        <span className="text-xs font-medium" style={{ color: 'var(--nofx-gold)' }}>{language === 'zh' ? '用户提示词' : 'User'}: </span>
                        {data.compliance_audit.user_prompt}
                      </div>
                    )}
                  </div>
                )}
              </section>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
