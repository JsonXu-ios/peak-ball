import type { DrawObservation } from '@/types/drawHistory'

export const SECONDARY_DRAW_VERSION = 'secondary-v1'
export const SECONDARY_DRAW_THRESHOLD = 0.225

export type SecondaryDrawDecision = 'retained' | 'rejected' | 'not_selected' | 'unavailable' | 'out_of_scope'

export interface SecondaryDrawClassification {
  decision: SecondaryDrawDecision
  version: string
  threshold: number
  score?: number
  reason: string
}

export interface SecondaryDrawPresentation extends SecondaryDrawClassification {
  label: string
  color?: string
  outcomeLabel: string
  outcomeColor?: string
}

type SecondaryDrawInput = Pick<DrawObservation, 'frozenDrawSignal'>
type SecondaryDrawResultInput = SecondaryDrawInput & Pick<DrawObservation, 'settled' | 'regulationSettled' | 'homeScore' | 'awayScore'>

/** Only frozen model inputs determine this decision; match results never select rows. */
export function classifySecondaryDraw(row: SecondaryDrawInput): SecondaryDrawClassification {
  const signal = row.frozenDrawSignal
  const base = { version: SECONDARY_DRAW_VERSION, threshold: SECONDARY_DRAW_THRESHOLD }
  if (!signal) return { ...base, decision: 'unavailable', reason: '等待接口返回原固定模型结果' }
  if (signal.state === 'out_of_scope') {
    return { ...base, decision: 'out_of_scope', reason: signal.reason || '仅对原固定模型入选的竞彩比赛做二次筛选' }
  }
  if (signal.state === 'not_selected') {
    return { ...base, decision: 'not_selected', reason: '原固定模型未入选，不进入二次筛选' }
  }
  if (signal.state !== 'selected') {
    return { ...base, decision: 'unavailable', reason: signal.reason || '原固定模型数据不足，不能判为二次剔除' }
  }
  const score = signal.score
  if (typeof score !== 'number' || !Number.isFinite(score) || score < 0 || score > 1) {
    return { ...base, decision: 'unavailable', reason: '原固定模型分数缺失或无效，不能判为二次剔除' }
  }
  if (score < SECONDARY_DRAW_THRESHOLD) {
    return { ...base, score, decision: 'rejected', reason: '原模型已入选，但模型分数低于固定二次门槛 0.225，二次剔除' }
  }
  return { ...base, score, decision: 'retained', reason: '原模型已入选，且模型分数达到固定二次门槛 0.225，二次保留' }
}

/** Results annotate a previously fixed decision, and cannot change it. */
export function secondaryDrawPresentation(row: SecondaryDrawResultInput): SecondaryDrawPresentation {
  const classified = classifySecondaryDraw(row)
  if (classified.decision === 'out_of_scope') {
    return { ...classified, label: '非竞彩', outcomeLabel: '' }
  }
  if (classified.decision === 'not_selected') {
    return { ...classified, label: '原模型未入选', outcomeLabel: '' }
  }
  if (classified.decision === 'unavailable') {
    return { ...classified, label: row.frozenDrawSignal ? '数据不足' : '待计算', outcomeLabel: '' }
  }
  const settled = row.settled === true && row.regulationSettled === true
    && Number.isFinite(row.homeScore) && Number.isFinite(row.awayScore)
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
