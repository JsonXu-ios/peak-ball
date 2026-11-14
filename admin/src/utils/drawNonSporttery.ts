import type { DrawObservation } from '@/types/drawHistory'
import { drawAverageEuroOdds } from './drawFrontend'

export const NON_SPORTTERY_OBSERVATION_VERSION = 'draw-nonsporttery-observation-v1'
const SAME_PRICE_EPSILON = 1e-9

type SignalInput = Pick<DrawObservation, 'jingcaiId' | 'nonSportterySignal'>
type AlternativeInput = SignalInput & Pick<DrawObservation, 'frontendAnalysis'>
type ResultInput = Pick<DrawObservation, 'settled' | 'regulationSettled' | 'homeScore' | 'awayScore'>

const confirmedResult = (row: ResultInput) => row.settled === true && row.regulationSettled === true
  && Number.isInteger(row.homeScore) && row.homeScore >= 0
  && Number.isInteger(row.awayScore) && row.awayScore >= 0

/** The backend's separate signal alone selects a non-JC fixture. */
export function nonSportteryDrawPresentation(row: SignalInput & ResultInput) {
  if (row.jingcaiId?.trim()) return { state: 'out_of_scope', label: '—', color: '', detail: '竞彩比赛使用原有选平及次选列' }
  const signal = row.nonSportterySignal
  if (!signal) return { state: 'unavailable', label: '等待服务更新', color: '', detail: '当前接口尚未返回非竞彩选平结果' }
  if (signal.state === 'unavailable') return { state: signal.state, label: '数据不足', color: '', detail: signal.reason || '本场缺少模型所需输入' }
  if (signal.state === 'not_selected') return { state: signal.state, label: '未入选', color: '', detail: signal.reason || '' }
  if (signal.state !== 'selected') return { state: signal.state, label: '暂无', color: '', detail: signal.reason || '非竞彩选平状态无法识别' }
  if (!confirmedResult(row)) return { state: signal.state, label: '入选 · 待赛果', color: 'primary', detail: signal.reason || '' }
  return row.homeScore === row.awayScore
    ? { state: signal.state, label: '命中平局', color: 'success', detail: signal.reason || '' }
    : { state: signal.state, label: '入选 · 未打平', color: 'warning', detail: signal.reason || '' }
}

export interface NonSportteryAlternative {
  version: string
  state: 'recommended' | 'not_selected' | 'out_of_scope' | 'unavailable'
  direction: 'home' | 'away' | null
  homeOdds: number | null
  awayOdds: number | null
  reason: string
}

/** A separate market alternative. Never changes or reads the JC signal. */
export function recommendNonSportteryAlternative(row: AlternativeInput): NonSportteryAlternative {
  const base: NonSportteryAlternative = {
    version: NON_SPORTTERY_OBSERVATION_VERSION,
    state: 'unavailable', direction: null, homeOdds: null, awayOdds: null,
    reason: '等待非竞彩选平结果',
  }
  if (row.jingcaiId?.trim()) return { ...base, state: 'out_of_scope', reason: '竞彩比赛使用原有次选列' }
  if (!row.nonSportterySignal) return { ...base, reason: '当前接口尚未返回非竞彩选平结果' }
  if (row.nonSportterySignal.state === 'not_selected') return { ...base, state: 'not_selected', reason: '非竞彩模型未入选平局' }
  if (row.nonSportterySignal.state !== 'selected') return { ...base, reason: row.nonSportterySignal.reason || '等待非竞彩选平结果' }
  const average = drawAverageEuroOdds(row.frontendAnalysis)
  const validPrice = (value: number | null | undefined): value is number => typeof value === 'number' && Number.isFinite(value) && value > 1
  const homeOdds = validPrice(average[0]) ? average[0] : null
  const awayOdds = validPrice(average[2]) ? average[2] : null
  const prices = { ...base, homeOdds, awayOdds }
  if (homeOdds === null || awayOdds === null) return { ...prices, reason: '缺少有效的即时平均欧赔主胜或客胜报价' }
  if (Math.abs(homeOdds - awayOdds) <= SAME_PRICE_EPSILON) return { ...prices, reason: '即时均赔主客同价，暂无明确胜负备选' }
  return { ...prices, state: 'recommended', direction: homeOdds < awayOdds ? 'home' : 'away', reason: '取即时平均欧赔主客赔率较低的一方' }
}

export function nonSportteryAlternativePresentation(row: AlternativeInput & ResultInput) {
  const recommendation = recommendNonSportteryAlternative(row)
  const base = { ...recommendation, label: recommendation.direction === 'home' ? '次选 主胜' : '次选 客胜',
    color: recommendation.direction === 'home' ? 'primary' : 'deep-purple', outcomeLabel: '', outcomeColor: '' }
  if (recommendation.state !== 'recommended') return { ...base, label: '次选暂无', color: '' }
  if (!confirmedResult(row)) return { ...base, outcomeLabel: '待赛果核对' }
  if (row.homeScore === row.awayScore) return { ...base, outcomeLabel: '主选平命中', outcomeColor: 'success' }
  const hit = (row.homeScore > row.awayScore ? 'home' : 'away') === recommendation.direction
  return { ...base, outcomeLabel: hit ? '次选命中' : '主次均未命中', outcomeColor: hit ? 'success' : 'warning' }
}
