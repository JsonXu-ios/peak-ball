import type { DrawObservation } from '@/types/drawHistory'
import { drawAverageEuroOdds } from './drawFrontend'
import { hasCurrentJcDrawSelectionMetadata, JC_DRAW_MINIMUM_HEAD_TO_HEAD } from './drawFrozenSignal'

export const DRAW_MARKET_BASELINE_VERSION = 'draw-market-implied-v1'
/** Chosen only on JC rows before 2026-10-15 (training segment); never tuned on later results. */
export const DRAW_MARKET_BASELINE_THRESHOLD = 0.25

type BaselineInput = Pick<DrawObservation, 'jingcaiId' | 'frozenDrawSignal' | 'frontendAnalysis' | 'settled' | 'regulationSettled' | 'homeScore' | 'awayScore'>

export interface DrawMarketBaseline {
  version: string
  state: 'selected' | 'not_selected' | 'out_of_scope' | 'unavailable'
  impliedDraw: number | null
  modelSelected: boolean | null
  agreement: 'both' | 'market_only' | 'model_only' | 'neither' | null
  settled: boolean
  drawHit: boolean | null
  label: string
  reason: string
}

/** Market comparison: normalized 1/odds draw share of the latest average euro prices.
 * Same scope as the original JC pick (JC + at least one valid prior H2H). The result never enters the selection. */
export function drawMarketBaseline(row: BaselineInput): DrawMarketBaseline {
  const settled = row.settled === true && row.regulationSettled === true
    && Number.isInteger(row.homeScore) && row.homeScore >= 0 && Number.isInteger(row.awayScore) && row.awayScore >= 0
  const base: DrawMarketBaseline = {
    version: DRAW_MARKET_BASELINE_VERSION, state: 'unavailable', impliedDraw: null, modelSelected: null,
    agreement: null, settled, drawHit: null, label: '暂无', reason: '',
  }
  if (!row.jingcaiId?.trim()) return { ...base, state: 'out_of_scope', label: '非竞彩', reason: '对照列与原模型同口径，仅观察竞彩比赛' }
  const signal = row.frozenDrawSignal
  const odds = drawAverageEuroOdds(row.frontendAnalysis)
  if (odds.some((value) => value === null || value <= 1)) return { ...base, reason: '缺少有效的即时平均欧赔胜平负报价' }
  const inverse = odds.map((value) => 1 / value!)
  const impliedDraw = inverse[1] / (inverse[0] + inverse[1] + inverse[2])
  const withOdds = { ...base, impliedDraw }
  if (!signal || !hasCurrentJcDrawSelectionMetadata(signal)) return { ...withOdds, reason: '等待分析服务返回赛前交锋样本' }
  if ((signal.headToHeadSample ?? 0) < JC_DRAW_MINIMUM_HEAD_TO_HEAD) {
    return { ...withOdds, state: 'not_selected', label: '交锋不足', reason: '与原模型同口径：需至少1场有效赛前交锋' }
  }
  const selected = impliedDraw >= DRAW_MARKET_BASELINE_THRESHOLD
  const modelSelected = signal.state === 'selected' ? true : signal.state === 'not_selected' ? false : null
  const agreement = modelSelected === null ? null
    : selected && modelSelected ? 'both' : selected ? 'market_only' : modelSelected ? 'model_only' : 'neither'
  const drawHit = selected && settled ? row.homeScore === row.awayScore : null
  return {
    ...withOdds, state: selected ? 'selected' : 'not_selected', modelSelected, agreement, drawHit,
    label: !selected ? '未入选' : !settled ? '市场入选 · 待赛果' : drawHit ? '命中平局' : '入选 · 未打平',
    reason: selected ? `平局隐含概率达到${(DRAW_MARKET_BASELINE_THRESHOLD * 100).toFixed(0)}%门槛` : `平局隐含概率低于${(DRAW_MARKET_BASELINE_THRESHOLD * 100).toFixed(0)}%门槛`,
  }
}

export interface DrawMarketBaselineSummary {
  rows: number
  market: { selected: number; settled: number; draws: number }
  model: { selected: number; settled: number; draws: number }
  both: number
  marketOnly: number
  modelOnly: number
}

/** Current-page tally only; not a full-window hit rate. */
export function summarizeDrawMarketBaseline(rows: BaselineInput[]): DrawMarketBaselineSummary {
  const summary: DrawMarketBaselineSummary = { rows: 0, market: { selected: 0, settled: 0, draws: 0 }, model: { selected: 0, settled: 0, draws: 0 }, both: 0, marketOnly: 0, modelOnly: 0 }
  for (const row of rows) {
    const item = drawMarketBaseline(row)
    if (item.state !== 'selected' && item.state !== 'not_selected') continue
    summary.rows++
    const draw = item.settled && row.homeScore === row.awayScore
    if (item.state === 'selected') {
      summary.market.selected++
      if (item.settled) { summary.market.settled++; if (draw) summary.market.draws++ }
    }
    if (item.modelSelected) {
      summary.model.selected++
      if (item.settled) { summary.model.settled++; if (draw) summary.model.draws++ }
    }
    if (item.agreement === 'both') summary.both++
    else if (item.agreement === 'market_only') summary.marketOnly++
    else if (item.agreement === 'model_only') summary.modelOnly++
  }
  return summary
}
