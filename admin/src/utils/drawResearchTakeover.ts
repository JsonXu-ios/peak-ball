import type { DrawResearchDecision, DrawResearchTakeover } from '@/types/drawHistory'

const positiveId = (value: unknown) => Number.isSafeInteger(value) && (value as number) > 0
const count = (value: unknown) => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0

/** Missing/partial older responses must not be presented as a successful cutover. */
export function researchTakeoverPresentation(value?: DrawResearchTakeover | null) {
  if (!value || !value.version?.trim() || !['research', 'mixed', 'fallback'].includes(value.mode)
    || ![value.served, value.persisted, value.fallback, value.unavailable].every(count)) {
    return { state: 'missing', color: 'info' as const, label: '当前接口尚未返回研究库接管状态', detail: '下方仍显示现有查询结果；加载新版分析服务后可核对实际读取来源。' }
  }
  const labels = { research: '研究库读取已启用', mixed: '部分比赛使用原链路回退', fallback: '当前使用原链路回退' }
  const color = value.mode === 'research' ? 'success' as const : 'warning' as const
  const detail = `本页库读取 ${value.served} 场 · 新写入 ${value.persisted} 场 · 回退 ${value.fallback} 场${value.unavailable ? ` · 数据不足 ${value.unavailable} 场` : ''}`
  return { state: value.mode, color, label: labels[value.mode], detail: [detail, value.reason].filter(Boolean).join('。') }
}

/** Only an explicit backend source and valid archive IDs earn a stored badge. */
export function researchDecisionPresentation(value?: DrawResearchDecision | null) {
  if (!value || !value.version?.trim()) return { state: 'missing', label: '来源待确认', color: undefined, detail: '当前接口未提供这场比赛的研究库读取来源。' }
  if (value.source === 'legacy_fallback') return { state: value.source, label: '原链路回退', color: 'warning', detail: value.reason || '研究库记录暂不可用，本场保留原链路计算结果。' }
  if (value.source === 'not_applicable') return { state: value.source, label: '不适用接管', color: undefined, detail: value.reason || '本场不在研究模型适用范围。' }
  if (!['research', 'research_write_through'].includes(value.source)
    || !positiveId(value.snapshotId) || !positiveId(value.predictionId)) {
    return { state: 'invalid', label: '归档来源待核对', color: 'warning', detail: '研究库来源或档案编号不完整，不能确认已从库读取。' }
  }
  return {
    state: value.source,
    label: value.source === 'research' ? '库读取' : '已入库',
    color: 'info',
    detail: [`特征快照 #${value.snapshotId} · 预测 #${value.predictionId}`, value.reason].filter(Boolean).join('。'),
  }
}
