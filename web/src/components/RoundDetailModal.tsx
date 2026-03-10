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
                {data.analyst_report && (
                  <div className="mt-3 space-y-3">
                    <h4 className="text-xs font-semibold" style={{ color: 'var(--nofx-gold)' }}>
                      {language === 'zh' ? '分析师 系统提示词 / 用户提示词' : 'Analyst system & user prompts'}
                    </h4>
                    <div className="space-y-2">
                      <div className="text-xs font-medium" style={{ color: 'var(--nofx-gold)' }}>
                        {language === 'zh' ? '系统提示词' : 'System'}
                      </div>
                      <div
                        className="rounded-lg p-4 text-sm whitespace-pre-wrap break-words overflow-x-auto"
                        style={{
                          background: 'var(--panel-bg-hover)',
                          border: '1px solid var(--panel-border)',
                          color: 'var(--text-secondary)',
                          maxHeight: '200px',
                          overflowY: 'auto',
                          wordBreak: 'break-word',
                          overflowWrap: 'break-word',
                        }}
                      >
                        {(data.analyst_report.system_prompt ?? (data.analyst_report as unknown as Record<string, string>).systemPrompt) || (language === 'zh' ? '本轮回测未记录' : 'Not recorded for this round')}
                      </div>
                      <div className="text-xs font-medium" style={{ color: 'var(--nofx-gold)' }}>
                        {language === 'zh' ? '用户提示词' : 'User'}
                      </div>
                      <div
                        className="rounded-lg p-4 text-sm whitespace-pre-wrap break-words overflow-x-auto"
                        style={{
                          background: 'var(--panel-bg-hover)',
                          border: '1px solid var(--panel-border)',
                          color: 'var(--text-secondary)',
                          maxHeight: '200px',
                          overflowY: 'auto',
                          wordBreak: 'break-word',
                          overflowWrap: 'break-word',
                        }}
                      >
                        {(data.analyst_report.user_prompt ?? (data.analyst_report as unknown as Record<string, string>).userPrompt) || (language === 'zh' ? '本轮回测未记录' : 'Not recorded for this round')}
                      </div>
                    </div>
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
                    <div
                      className="whitespace-pre-wrap break-words"
                      style={{ color: 'var(--text-secondary)', wordBreak: 'break-word', overflowWrap: 'break-word' }}
                    >
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

              {/* 3. 风控审计（单条审批：开/平可分别通过或驳回） */}
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
                    (() => {
                      const raw = (data.compliance_audit as { decisions_audit_json?: string }).decisions_audit_json?.trim()
                      let decisionsAudit: { index: number; approved: boolean; reason: string }[] = []
                      if (raw) {
                        try {
                          const parsed = JSON.parse(raw) as unknown
                          if (Array.isArray(parsed)) {
                            decisionsAudit = parsed.map((x: unknown) => ({
                              index: Number((x as { index?: number }).index) ?? 0,
                              approved: Boolean((x as { approved?: boolean }).approved),
                              reason: String((x as { reason?: string }).reason ?? ''),
                            }))
                          }
                        } catch {
                          /* ignore */
                        }
                      }
                      const hasPerDecision = decisionsAudit.length > 0
                      return (
                    <>
                      {/* 单条审批说明 */}
                      {hasPerDecision ? (
                        <div className="mb-3 text-xs" style={{ color: 'var(--text-muted)' }}>
                          {language === 'zh'
                            ? `单条审批：本轮 ${decisionsAudit.length} 条决策分别通过/驳回，仅通过项会执行。`
                            : `Per-decision audit: ${decisionsAudit.length} decision(s); only approved items are executed.`}
                        </div>
                      ) : (
                        data.decision_record?.decisions?.length != null && data.decision_record.decisions.length > 0 && (
                          <div className="mb-3 text-xs" style={{ color: 'var(--text-muted)' }}>
                            {language === 'zh'
                              ? `本审计针对本轮 ${data.decision_record.decisions.length} 条决策（整批通过/驳回）`
                              : `This audit applies to ${data.decision_record.decisions.length} decision(s) (batch).`}
                          </div>
                        )
                      )}
                      <div className="flex flex-wrap items-center gap-2 mb-2">
                        <span
                          className="px-2.5 py-1 rounded text-xs font-medium"
                          style={{
                            background: data.compliance_audit.approved ? 'rgba(14, 203, 129, 0.2)' : 'rgba(246, 70, 93, 0.2)',
                            color: data.compliance_audit.approved ? '#0ECB81' : '#F6465D',
                            border: `1px solid ${data.compliance_audit.approved ? 'rgba(14, 203, 129, 0.4)' : 'rgba(246, 70, 93, 0.4)'}`,
                          }}
                        >
                          {data.compliance_audit.approved
                            ? (language === 'zh' ? '通过' : 'Approved')
                            : (language === 'zh' ? '驳回' : 'Rejected')}
                        </span>
                      </div>
                      {data.compliance_audit.reason ? (
                        <div className="mb-2">
                          <span className="text-xs font-medium" style={{ color: 'var(--text-primary)' }}>
                            {language === 'zh' ? '结论 / 原因：' : 'Reason: '}
                          </span>
                          <span className="whitespace-pre-wrap">{data.compliance_audit.reason}</span>
                        </div>
                      ) : null}
                      {!data.compliance_audit.reason && !data.compliance_audit.violations_json && (
                        <div className="text-xs opacity-80">{t('phaseNone', language)}</div>
                      )}
                      {/* 单条审批列表：决策 + 通过/驳回 + 原因 */}
                      {hasPerDecision && (
                        <div className="mt-3">
                          <div className="text-xs font-medium mb-2" style={{ color: 'var(--text-primary)' }}>
                            {language === 'zh' ? '单条审批结果' : 'Per-decision result'}
                          </div>
                          <ul className="space-y-2">
                            {decisionsAudit.map((da, i) => {
                              const pendingList = data.pending_decisions ?? []
                              const orig = pendingList[da.index] as Record<string, unknown> | undefined
                              const action = orig?.action ?? ''
                              const symbol = (orig?.symbol as string) ?? ''
                              const label = symbol ? `${String(action)} ${String(symbol).replace('USDT', '')}` : `#${da.index + 1}`
                              return (
                                <li
                                  key={i}
                                  className="flex flex-wrap items-center gap-2 rounded px-2 py-1.5 text-xs"
                                  style={{
                                    background: da.approved ? 'rgba(14, 203, 129, 0.08)' : 'rgba(246, 70, 93, 0.08)',
                                    border: `1px solid ${da.approved ? 'rgba(14, 203, 129, 0.25)' : 'rgba(246, 70, 93, 0.25)'}`,
                                  }}
                                >
                                  <span className="font-medium" style={{ color: 'var(--text-primary)' }}>{label}</span>
                                  <span
                                    className="px-1.5 py-0.5 rounded font-medium"
                                    style={{
                                      background: da.approved ? 'rgba(14, 203, 129, 0.2)' : 'rgba(246, 70, 93, 0.2)',
                                      color: da.approved ? '#0ECB81' : '#F6465D',
                                    }}
                                  >
                                    {da.approved ? (language === 'zh' ? '通过' : 'Approved') : (language === 'zh' ? '驳回' : 'Rejected')}
                                  </span>
                                  {!da.approved && da.reason && (
                                    <span className="flex-1 text-left opacity-90">{da.reason}</span>
                                  )}
                                </li>
                              )
                            })}
                          </ul>
                        </div>
                      )}
                      {/* 违规项：优先解析为 JSON 数组逐条展示 */}
                      {data.compliance_audit.violations_json && (() => {
                        const raw = data.compliance_audit.violations_json.trim()
                        let items: string[] = []
                        try {
                          const parsed = JSON.parse(raw) as unknown
                          if (Array.isArray(parsed)) items = parsed.filter((x): x is string => typeof x === 'string')
                          else items = [raw]
                        } catch {
                          items = [raw]
                        }
                        return (
                          <div className="mt-2">
                            <div className="text-xs font-medium mb-1" style={{ color: 'var(--text-primary)' }}>
                              {language === 'zh' ? '违规项：' : 'Violations:'}
                            </div>
                            <ul className="list-disc list-inside space-y-0.5 text-xs">
                              {items.map((v, i) => (
                                <li key={i}>{v}</li>
                              ))}
                            </ul>
                          </div>
                        )
                      })()}
                    </>
                      )
                    })()
                  ) : (
                    t('phaseNone', language)
                  )}
                </div>
                {data.compliance_audit && (
                  <div className="mt-3 space-y-3">
                    <h4 className="text-xs font-semibold" style={{ color: 'var(--nofx-gold)' }}>
                      {language === 'zh' ? '风控官 系统提示词 / 用户提示词' : 'Compliance system & user prompts'}
                    </h4>
                    <div className="space-y-2">
                      <div className="text-xs font-medium" style={{ color: 'var(--nofx-gold)' }}>
                        {language === 'zh' ? '系统提示词' : 'System'}
                      </div>
                      <div
                        className="rounded-lg p-4 text-sm whitespace-pre-wrap break-words overflow-x-auto"
                        style={{
                          background: 'var(--panel-bg-hover)',
                          border: '1px solid var(--panel-border)',
                          color: 'var(--text-secondary)',
                          maxHeight: '200px',
                          overflowY: 'auto',
                          wordBreak: 'break-word',
                          overflowWrap: 'break-word',
                        }}
                      >
                        {(data.compliance_audit.system_prompt ?? (data.compliance_audit as unknown as Record<string, string>).systemPrompt) || (language === 'zh' ? '本轮回测未记录' : 'Not recorded for this round')}
                      </div>
                      <div className="text-xs font-medium" style={{ color: 'var(--nofx-gold)' }}>
                        {language === 'zh' ? '用户提示词' : 'User'}
                      </div>
                      <div
                        className="rounded-lg p-4 text-sm whitespace-pre-wrap break-words overflow-x-auto"
                        style={{
                          background: 'var(--panel-bg-hover)',
                          border: '1px solid var(--panel-border)',
                          color: 'var(--text-secondary)',
                          maxHeight: '200px',
                          overflowY: 'auto',
                          wordBreak: 'break-word',
                          overflowWrap: 'break-word',
                        }}
                      >
                        {(data.compliance_audit.user_prompt ?? (data.compliance_audit as unknown as Record<string, string>).userPrompt) || (language === 'zh' ? '本轮回测未记录' : 'Not recorded for this round')}
                      </div>
                    </div>
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
