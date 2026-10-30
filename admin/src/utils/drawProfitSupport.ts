import type { DrawObservation } from '@/types/drawHistory'

export const DRAW_PROFIT_SUPPORT_VERSION = 'profit-support-v1'
export const DRAW_PROFIT_SUPPORT_EPS = 1e-9

export type DrawProfitSupportState = 'supported' | 'not_supported' | 'unavailable' | 'out_of_scope'
export type DrawProfitDirection = 'home' | 'draw' | 'away'
export type DrawProfitRates = [number | null, number | null, number | null]

export interface DrawProfitSupportClassification {
  state: DrawProfitSupportState
  version: string
  officialRates: DrawProfitRates
  simulatedRates: DrawProfitRates
  officialMinimum: DrawProfitDirection[]
  simulatedMinimum: DrawProfitDirection[]
  reason: string
}

const directions: DrawProfitDirection[] = ['home', 'draw', 'away']
const finite = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value)
const emptyRates = (): DrawProfitRates => [null, null, null]

function minima(values: DrawProfitRates): DrawProfitDirection[] {
  if (!values.every(finite)) return []
  const lowest = Math.min(...values as number[])
  return directions.filter((_, index) => values[index]! <= lowest + DRAW_PROFIT_SUPPORT_EPS)
}

/** Only source-checked rates and non-handicap simulation inputs classify this
 * feature. Target scores, model selection and results never enter the rule. */
export function classifyDrawProfitSupport(row: Pick<DrawObservation, 'jingcaiId' | 'frontendAnalysis'>): DrawProfitSupportClassification {
  const result: DrawProfitSupportClassification = {
    state: 'unavailable', version: DRAW_PROFIT_SUPPORT_VERSION,
    officialRates: emptyRates(), simulatedRates: emptyRates(),
    officialMinimum: [], simulatedMinimum: [], reason: '',
  }
  if (typeof row.jingcaiId !== 'string' || !row.jingcaiId.trim()) {
    return { ...result, state: 'out_of_scope', reason: '仅观察有竞彩编号的比赛' }
  }
  const analysis = row.frontendAnalysis
  // This strict projection preserves genuine zero and source null. Do not
  // substitute raw officialProfitRate pointers which older APIs defaulted to 0.
  const official = analysis?.weightedFeatureInputs?.officialProfitRates
  if (Array.isArray(official) && official.length === 3) {
    result.officialRates = official.map(value => finite(value) ? value : null) as DrawProfitRates
  }
  result.officialMinimum = minima(result.officialRates)

  const markets = analysis?.roiSimulation?.markets
  const simulated = Array.isArray(markets) ? markets.filter(market => market?.simulated === true
    && !['sportteryRqspf', 'sportteryRqspfSim'].includes(market.key)) : []
  if (simulated.length === 1 && simulated[0]!.oddsAvailable === true) {
    const rows = simulated[0]!.bookmakerByOutcome
    if (Array.isArray(rows)) {
      result.simulatedRates = directions.map(direction => {
        const found = rows.filter(item => item?.outcome === direction)
        if (found.length !== 1) return null
        const item = found[0]!
        if (item.available !== true || !finite(item.odds) || item.odds <= 0
          || !finite(item.bookmakerProfit) || !finite(item.totalStake) || item.totalStake <= 0) return null
        const value = item.bookmakerProfit * 100 / item.totalStake
        return finite(value) ? value : null
      }) as DrawProfitRates
    }
  }
  result.simulatedMinimum = minima(result.simulatedRates)
  if (!result.officialMinimum.length || !result.simulatedMinimum.length) {
    const reasons = []
    if (!result.officialMinimum.length) reasons.push('缺少三项真实竞彩源站盈亏率')
    if (!result.simulatedMinimum.length) reasons.push(simulated.length > 1
      ? '非让球模拟来源不唯一' : '缺少完整有效的非让球模拟盈亏')
    return { ...result, reason: `${reasons.join('；')}，待判` }
  }
  const overlap = result.officialMinimum.some(direction => result.simulatedMinimum.includes(direction))
  return {
    ...result, state: overlap ? 'not_supported' : 'supported',
    reason: overlap ? '两套最低盈亏方向存在共同项，无此增强特征' : '两套最低盈亏方向没有交集，标记为平局观察增强',
  }
}
