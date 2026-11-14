import type { DrawObservation } from '@/types/drawHistory'

type Input = Pick<DrawObservation, 'comprehensiveSignal' | 'settled' | 'regulationSettled' | 'homeScore' | 'awayScore'>

const confirmedResult = (row: Input) => row.settled === true && row.regulationSettled === true
  && Number.isInteger(row.homeScore) && row.homeScore >= 0
  && Number.isInteger(row.awayScore) && row.awayScore >= 0

/** The server alone selects. Scores, old signals and prices cannot promote a row. */
export function comprehensiveDrawPresentation(row: Input) {
  const signal = row.comprehensiveSignal
  if (!signal) return { state: 'missing', label: '等待服务更新', color: '', reason: '当前接口尚未返回全量决策树结果' }
  if (signal.state === 'unavailable') return { state: signal.state, label: '新树暂不可用', color: '', reason: signal.reason || '模型或有效输入暂不可用' }
  if (signal.state === 'not_selected') return { state: signal.state, label: '新树未选平', color: '', reason: signal.reason || '' }
  if (signal.state !== 'selected') return { state: 'unavailable', label: '新树状态待核对', color: '', reason: '服务未返回有效的新树判断' }
  if (!confirmedResult(row)) return { state: signal.state, label: '新树入选 · 待赛果', color: 'primary', reason: signal.reason || '' }
  return row.homeScore === row.awayScore
    ? { state: signal.state, label: '新树命中平局', color: 'success', reason: signal.reason || '' }
    : { state: signal.state, label: '新树入选 · 未打平', color: 'warning', reason: signal.reason || '' }
}

/** A second server-owned tree supplies the side; never infer it from prices. */
export function comprehensiveAlternativePresentation(row: Input) {
  const signal = row.comprehensiveSignal
  const base = { state: 'unavailable', direction: null as 'home' | 'away' | null, label: '次选暂无', color: '', reason: '', outcomeLabel: '', outcomeColor: '' }
  if (!signal || signal.state !== 'selected') return { ...base, state: 'not_selected' }
  if (signal.alternativeDirection !== 'home' && signal.alternativeDirection !== 'away') {
    return { ...base, reason: signal.alternativeReason || '胜负树尚无明确方向' }
  }
  const direction = signal.alternativeDirection
  const result = { ...base, state: 'recommended', direction,
    label: direction === 'home' ? '树次选 主胜' : '树次选 客胜',
    color: direction === 'home' ? 'primary' : 'deep-purple', reason: signal.alternativeReason || '' }
  if (!confirmedResult(row)) return { ...result, outcomeLabel: '待赛果核对' }
  if (row.homeScore === row.awayScore) return { ...result, outcomeLabel: '主选平命中', outcomeColor: 'success' }
  const actual = row.homeScore > row.awayScore ? 'home' : 'away'
  return actual === direction
    ? { ...result, outcomeLabel: '树次选命中', outcomeColor: 'success' }
    : { ...result, outcomeLabel: '新树主次均未命中', outcomeColor: 'warning' }
}

export function comprehensiveNumber(value: number | null | undefined, digits = 4) {
  return typeof value === 'number' && Number.isFinite(value)
    ? value.toLocaleString('zh-CN', { maximumFractionDigits: digits }) : '暂无'
}
