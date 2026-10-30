import type { DrawObservation } from '@/types/drawHistory'

export const SECONDARY_DRAW_VERSION = 'secondary-v2-input-guards'
export const SECONDARY_DRAW_THRESHOLD = 0.225

export type SecondaryDrawDecision = 'retained' | 'rejected' | 'not_selected' | 'unavailable' | 'out_of_scope'

export interface SecondaryDrawClassification {
  decision: SecondaryDrawDecision
  version: string
  threshold: number
  score?: number
  reason: string
  reasons: string[]
  needsUpdate?: boolean
}

export interface SecondaryDrawPresentation extends SecondaryDrawClassification {
  label: string
  color?: string
  outcomeLabel: string
  outcomeColor?: string
}

type SecondaryDrawInput = Pick<DrawObservation, 'frozenDrawSignal' | 'secondaryDecision'>
type SecondaryDrawResultInput = SecondaryDrawInput & Pick<DrawObservation, 'settled' | 'regulationSettled' | 'homeScore' | 'awayScore'>
const validScore = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value) && value >= 0 && value <= 1
const decisions: SecondaryDrawDecision[] = ['retained', 'rejected', 'not_selected', 'unavailable', 'out_of_scope']

/** The server owns quote/history guards. The browser never reconstructs a
 * secondary decision from the model score, current-page odds or final result. */
export function classifySecondaryDraw(row: SecondaryDrawInput): SecondaryDrawClassification {
  const base = { version: SECONDARY_DRAW_VERSION, threshold: SECONDARY_DRAW_THRESHOLD, reasons: [] as string[] }
  const recorded = row.secondaryDecision
  if (!recorded || recorded.version !== SECONDARY_DRAW_VERSION) {
    return { ...base, decision: 'unavailable', needsUpdate: true, reason: '二次筛选结果待更新' }
  }
  if (!decisions.includes(recorded.decision) || recorded.threshold !== SECONDARY_DRAW_THRESHOLD) {
    return { ...base, decision: 'unavailable', needsUpdate: true, reason: '二次筛选结果待更新，请重新查询' }
  }
  if (recorded.decision === 'retained' && row.frozenDrawSignal?.state !== 'selected') {
    return { ...base, decision: 'unavailable', needsUpdate: true, reason: '原模型与二次筛选结果尚未一致，请重新查询' }
  }
  const reasons = Array.isArray(recorded.reasons) ? recorded.reasons.filter((reason): reason is string => typeof reason === 'string' && !!reason.trim()) : []
  const reason = reasons.length ? reasons.join('；') : typeof recorded.reason === 'string' ? recorded.reason.trim() : ''
  if (!reason) return { ...base, decision: 'unavailable', needsUpdate: true, reason: '二次筛选说明待更新' }
  const score = validScore(recorded.score) ? recorded.score : validScore(row.frozenDrawSignal?.score) ? row.frozenDrawSignal.score : undefined
  return { ...base, decision: recorded.decision, score, reason, reasons: reasons.length ? [...reasons] : [reason] }
}

/** Results annotate an existing server decision and cannot change selection. */
export function secondaryDrawPresentation(row: SecondaryDrawResultInput): SecondaryDrawPresentation {
  const classified = classifySecondaryDraw(row)
  if (classified.decision === 'out_of_scope') return { ...classified, label: '非竞彩', outcomeLabel: '' }
  if (classified.decision === 'not_selected') return { ...classified, label: '原模型未入选', outcomeLabel: '' }
  if (classified.decision === 'unavailable') return { ...classified, label: classified.needsUpdate ? '待更新' : '数据不足', outcomeLabel: '' }
  const settled = row.settled === true && row.regulationSettled === true
    && Number.isInteger(row.homeScore) && row.homeScore >= 0 && Number.isInteger(row.awayScore) && row.awayScore >= 0
  if (classified.decision === 'rejected') {
    if (!settled) return { ...classified, label: '二次剔除', color: 'warning', outcomeLabel: '待赛果核对' }
    return row.homeScore === row.awayScore
      ? { ...classified, label: '二次剔除', color: 'warning', outcomeLabel: '误剔平局', outcomeColor: 'error' }
      : { ...classified, label: '二次剔除', color: 'warning', outcomeLabel: '剔除正确', outcomeColor: 'success' }
  }
  if (!settled) return { ...classified, label: '二次入选 · 待赛果', color: 'primary', outcomeLabel: '' }
  return row.homeScore === row.awayScore
    ? { ...classified, label: '命中平局', color: 'success', outcomeLabel: '二次入选' }
    : { ...classified, label: '二次入选 · 未打平', color: 'warning', outcomeLabel: '' }
}
