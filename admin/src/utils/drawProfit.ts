import type { BookmakerMarket, BookmakerOutcome, DrawObservation } from '@/types/drawHistory'

export type DrawProfitKind = 'sporttery' | 'simulated'
export interface DrawProfitOutcome {
  outcome: BookmakerOutcome['outcome']
  label: string
  profit: number | null
  rate: number | null
  unavailableReason: string
}
export interface DrawProfitMarket {
  key: string
  name: string
  goal: string
  simulated: boolean
  note: string
  totalStake: number | null
  outcomes: DrawProfitOutcome[]
}

const outcomes = [
  { outcome: 'home', label: '胜' },
  { outcome: 'draw', label: '平' },
  { outcome: 'away', label: '负' },
] as const

function finite(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

/** Both normal and fallback handicap simulations use these canonical keys. */
export function isDrawSimulatedResultMarket(market: BookmakerMarket): boolean {
  return market.simulated === true && !['sportteryRqspf', 'sportteryRqspfSim'].includes(market.key)
}

export function isDrawHandicapProfitMarket(market: Pick<BookmakerMarket, 'key'>): boolean {
  return ['sportteryRqspf', 'sportteryRqspfSim'].includes(market.key)
}

/** Display scope only; keep the result-market selector used by existing decisions unchanged. */
export function isDrawProfitDisplayMarket(market: BookmakerMarket, kind: DrawProfitKind, includeHandicap = false): boolean {
  return kind === 'simulated'
    ? isDrawSimulatedResultMarket(market) || (includeHandicap && market.simulated === true && isDrawHandicapProfitMarket(market))
    : !market.simulated && (market.key === 'sporttery' || (includeHandicap && market.key === 'sportteryRqspf'))
}

/** Read existing frontend values only. In particular a simulated sporttery market
 * never supplies the official column, and localMarket is a different calculation. */
export function drawProfitMarkets(row: DrawObservation, kind: DrawProfitKind, includeHandicap = false): DrawProfitMarket[] {
  const simulation = row.frontendAnalysis?.roiSimulation
  const markets = Array.isArray(simulation?.markets) ? simulation.markets : []
  return markets
    .filter((market) => market && isDrawProfitDisplayMarket(market, kind, includeHandicap))
    .map((market) => profitMarket(market, simulation?.totalStake))
}

function profitMarket(market: BookmakerMarket, totalStake: number | undefined): DrawProfitMarket {
  const rows = Array.isArray(market.bookmakerByOutcome) ? market.bookmakerByOutcome : []
  const rowBases = rows.map((row) => row.totalStake).filter((value) => finite(value) && value > 0)
  const commonBase = rowBases.length === rows.length && rowBases.length > 0 && rowBases.every((value) => value === rowBases[0]) ? rowBases[0]! : null
  return {
    key: market.key,
    name: market.name || market.key,
    goal: market.goal || '',
    simulated: market.simulated === true,
    note: market.simulationNote || '',
    totalStake: commonBase ?? (finite(totalStake) && totalStake > 0 ? totalStake : null),
    outcomes: outcomes.map(({ outcome, label }) => {
      const row = rows.find((entry) => entry.outcome === outcome)
      const priced = market.oddsAvailable === true && !!row && finite(row.odds) && row.odds > 0
      const amountAvailable = priced && row!.available === true
      const rate = market.simulated ? amountAvailable ? row!.bookmakerRoi : undefined : priced ? row!.officialProfitRate : undefined
      return {
        outcome,
        label: isDrawHandicapProfitMarket(market) ? `让${label}` : label,
        profit: amountAvailable && finite(row!.bookmakerProfit) ? row!.bookmakerProfit : null,
        rate: finite(rate) ? rate : null,
        unavailableReason: !row ? '暂无本项明细' : !priced ? '赔率不足' : !row.available ? '测算数据不足' : '暂无',
      }
    }),
  }
}

export function drawProfitAmount(value: number | null, signed = false): string {
  if (!finite(value)) return '暂无'
  const rounded = Number(value.toFixed(2)) || 0
  return `${signed && rounded > 0 ? '+' : ''}${rounded.toLocaleString('zh-CN', { maximumFractionDigits: 2 })}元`
}

export function drawProfitRate(value: number | null): string {
  if (!finite(value)) return '暂无'
  const rounded = Number(value.toFixed(2)) || 0
  return `${rounded > 0 ? '+' : ''}${rounded.toFixed(2)}%`
}

/** Same bookmaker-view convention as DrawProfitTable: red positive, green negative. */
export function drawProfitClass(value: number | null): string {
  if (!finite(value) || Math.abs(value) < 0.005) return 'text-medium-emphasis'
  return value > 0 ? 'text-error' : 'text-success'
}
