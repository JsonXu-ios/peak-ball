import type { DrawObservation, DrawTotalsPrediction } from '@/types/drawHistory'
import { recommendDrawAlternativeOutcome } from './drawAlternativeOutcome'

type TotalsInput = Pick<DrawObservation, 'jingcaiId' | 'totalsPrediction'>
type TotalsResultInput = TotalsInput & Pick<DrawObservation,
  'settled' | 'regulationSettled' | 'homeScore' | 'awayScore' | 'frozenDrawSignal' | 'frontendAnalysis'>

export type DrawTotalsOutcome = 'full_win' | 'half_win' | 'push' | 'half_loss' | 'full_loss'

const quarterParts = (line: unknown): number[] | null => {
  if (typeof line !== 'number' || !Number.isFinite(line) || line <= 0) return null
  const quarters = Math.round(line * 4)
  if (!Number.isSafeInteger(quarters) || quarters <= 0 || Math.abs(line * 4 - quarters) > 1e-9) return null
  return quarters % 2 === 0 ? [quarters / 4] : [(quarters - 1) / 4, (quarters + 1) / 4]
}
const validWater = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value) && value > 0
const confirmedResult = (row: Pick<DrawObservation, 'settled' | 'regulationSettled' | 'homeScore' | 'awayScore'>) =>
  row.settled === true && row.regulationSettled === true
  && Number.isInteger(row.homeScore) && row.homeScore >= 0
  && Number.isInteger(row.awayScore) && row.awayScore >= 0

/** Exact equal-stake split for quarter lines; target goals never change a pick. */
export function settleDrawTotals(direction: 'over' | 'under', line: number, totalGoals: number): DrawTotalsOutcome | null {
  const parts = quarterParts(line)
  if (!parts || !['over', 'under'].includes(direction) || !Number.isInteger(totalGoals) || totalGoals < 0) return null
  let wins = 0, losses = 0
  for (const part of parts) {
    const difference = direction === 'over' ? totalGoals - part : part - totalGoals
    if (difference > 0) wins++
    if (difference < 0) losses++
  }
  if (wins === parts.length) return 'full_win'
  if (losses === parts.length) return 'full_loss'
  if (wins > 0) return 'half_win'
  if (losses > 0) return 'half_loss'
  return 'push'
}

/** The backend alone selects direction. Older API fields never substitute. */
export function drawTotalsRecommendation(row: TotalsInput) {
  const base = {
    version: '', state: 'unavailable' as DrawTotalsPrediction['state'], direction: null as 'over' | 'under' | null,
    line: null as number | null, water: null as number | null, parts: [] as number[],
    label: '等待分析服务更新', color: '', reason: '当前接口尚未返回大小球推荐',
  }
  if (!row.jingcaiId?.trim()) return { ...base, state: 'out_of_scope' as const, label: '非竞彩', reason: '仅竞彩比赛提供大小球推荐' }
  const signal = row.totalsPrediction
  if (!signal) return base
  const version = typeof signal.modelVersion === 'string' ? signal.modelVersion : ''
  if (signal.state === 'out_of_scope') return { ...base, version, state: signal.state, label: '不适用', reason: signal.reason || '本场不在大小球推荐范围' }
  if (signal.state === 'unavailable') return { ...base, version, label: '暂无', reason: signal.reason || '大小球数据不足' }
  if (signal.state !== 'recommended' || !version.trim()) return { ...base, version, reason: '大小球推荐格式待更新' }
  const parts = quarterParts(signal.line)
  const direction = signal.direction
  const water = direction === 'over' ? signal.overWater : direction === 'under' ? signal.underWater : null
  if (!parts || !['over', 'under'].includes(direction || '') || !validWater(water)) {
    return { ...base, version, label: '暂无', reason: '缺少有效的大小球推荐方向、盘口或对应水位' }
  }
  const line = Math.round(signal.line! * 4) / 4
  return {
    ...base, version, state: 'recommended' as const, direction: direction as 'over' | 'under', line, water, parts,
    label: `${direction === 'over' ? '大' : '小'} ${line}`, color: direction === 'over' ? 'deep-orange' : 'indigo',
    reason: signal.reason || '按本场盘口推荐',
  }
}

const outcomes: Record<DrawTotalsOutcome, { label: string; color: string }> = {
  full_win: { label: '全赢', color: 'success' }, half_win: { label: '赢半', color: 'success' },
  push: { label: '走盘', color: '' }, half_loss: { label: '输半', color: 'error' },
  full_loss: { label: '全输', color: 'error' },
}

export function drawTotalsPredictionPresentation(row: TotalsResultInput) {
  const recommendation = drawTotalsRecommendation(row)
  const base = { ...recommendation, outcomeState: 'none' as 'none' | 'pending' | DrawTotalsOutcome, outcomeLabel: '', outcomeColor: '', jointLabel: '' }
  if (recommendation.state !== 'recommended') return base
  if (!confirmedResult(row)) return { ...base, outcomeState: 'pending' as const, outcomeLabel: '待赛果核对' }
  const outcome = settleDrawTotals(recommendation.direction!, recommendation.line!, row.homeScore + row.awayScore)
  if (!outcome) return { ...base, outcomeState: 'pending' as const, outcomeLabel: '待赛果核对' }
  const alternative = recommendDrawAlternativeOutcome(row)
  const alternativeHit = row.homeScore !== row.awayScore && alternative.state === 'recommended'
    && alternative.direction === (row.homeScore > row.awayScore ? 'home' : 'away')
  return {
    ...base, outcomeState: outcome, outcomeLabel: outcomes[outcome].label, outcomeColor: outcomes[outcome].color,
    jointLabel: alternativeHit && (outcome === 'full_win' || outcome === 'half_win') ? '次选＋大小球同中' : '',
  }
}
