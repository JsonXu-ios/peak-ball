import type { DrawObservation } from '@/types/drawHistory'
import { drawAverageEuroOdds } from './drawFrontend'

export const DRAW_ALTERNATIVE_OUTCOME_VERSION = 'draw-average-alternative-v1'
const SAME_PRICE_EPSILON = 1e-9

type AlternativeInput = Pick<DrawObservation, 'jingcaiId' | 'frozenDrawSignal' | 'frontendAnalysis'>
type AlternativeResultInput = AlternativeInput & Pick<DrawObservation, 'settled' | 'regulationSettled' | 'homeScore' | 'awayScore'>

export interface DrawAlternativeOutcome {
  version: string
  state: 'recommended' | 'not_selected' | 'out_of_scope' | 'unavailable'
  direction: 'home' | 'away' | null
  homeOdds: number | null
  awayOdds: number | null
  source: 'average_euro_latest'
  reason: string
}

/** A market alternative to the original draw pick, not a second model class.
 * Only this row's real latest average home/away prices determine the direction.
 * Secondary screening and the target result never enter this calculation. */
export function recommendDrawAlternativeOutcome(row: AlternativeInput): DrawAlternativeOutcome {
  const base: DrawAlternativeOutcome = {
    version: DRAW_ALTERNATIVE_OUTCOME_VERSION,
    state: 'unavailable', direction: null, homeOdds: null, awayOdds: null,
    source: 'average_euro_latest', reason: '等待原模型入选结果',
  }
  if (!row.jingcaiId?.trim() || row.frozenDrawSignal?.state === 'out_of_scope') {
    return { ...base, state: 'out_of_scope', reason: '仅为原模型入选的竞彩比赛提供胜负备选' }
  }
  if (row.frozenDrawSignal?.state === 'not_selected') {
    return { ...base, state: 'not_selected', reason: '原模型未入选平局' }
  }
  if (row.frozenDrawSignal?.state !== 'selected') return base

  const average = drawAverageEuroOdds(row.frontendAnalysis)
  const validPrice = (value: number | null | undefined): value is number => typeof value === 'number' && Number.isFinite(value) && value > 1
  const homeOdds = validPrice(average[0]) ? average[0] : null
  const awayOdds = validPrice(average[2]) ? average[2] : null
  const prices = { ...base, homeOdds, awayOdds }
  if (homeOdds === null || awayOdds === null) {
    return { ...prices, reason: '缺少有效的即时平均欧赔主胜或客胜报价' }
  }
  if (Math.abs(homeOdds - awayOdds) <= SAME_PRICE_EPSILON) {
    return { ...prices, reason: '即时均赔主客同价，暂无明确胜负备选' }
  }
  return {
    ...prices, state: 'recommended', direction: homeOdds < awayOdds ? 'home' : 'away',
    reason: '排除平后，取即时平均欧赔主客赔率较低的一方',
  }
}

/** Result fields annotate an already determined alternative only after an
 * explicitly confirmed regulation-time result, including a genuine 0:0. */
export function drawAlternativeOutcomePresentation(row: AlternativeResultInput) {
  const recommendation = recommendDrawAlternativeOutcome(row)
  const empty = { color: '', outcomeLabel: '', outcomeColor: '', outcomeState: 'none' as const }
  if (recommendation.state !== 'recommended') {
    const label = recommendation.state === 'out_of_scope' ? '非竞彩' : recommendation.state === 'not_selected' ? '未入选' : '暂无'
    return { ...recommendation, ...empty, label }
  }
  const label = recommendation.direction === 'home' ? '次选 主胜' : '次选 客胜'
  const color = recommendation.direction === 'home' ? 'primary' : 'deep-purple'
  const settled = row.settled === true && row.regulationSettled === true
    && Number.isInteger(row.homeScore) && row.homeScore >= 0
    && Number.isInteger(row.awayScore) && row.awayScore >= 0
  if (!settled) {
    return { ...recommendation, label, color, outcomeLabel: '待赛果核对', outcomeColor: '', outcomeState: 'pending' as const }
  }
  if (row.homeScore === row.awayScore) {
    return { ...recommendation, label, color, outcomeLabel: '主选平命中', outcomeColor: 'success', outcomeState: 'primary_hit' as const }
  }
  const actual = row.homeScore > row.awayScore ? 'home' : 'away'
  const hit = recommendation.direction === actual
  return {
    ...recommendation, label, color,
    outcomeLabel: hit ? '次选命中' : '主次均未命中',
    outcomeColor: hit ? 'success' : 'warning', outcomeState: hit ? 'alternative_hit' as const : 'miss' as const,
  }
}
