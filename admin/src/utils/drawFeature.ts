import type { DrawObservation } from '@/types/drawHistory'
import { drawComparisonStages } from './drawFrontend'

export const DRAW_FEATURE_FORMULA = '体彩差值＝威廉－竞彩；均赔差值＝威廉－平均欧赔'
export const DRAW_FEATURE_NOTE = '初盘看平项绝对值最大；即时依次看平项绝对值最大、负数含平、正数含平。包含取三项绝对值升序的最后两项，边界并列保留。'
export const DRAW_FEATURE_EPS = 1e-9

export type Prices = [number, number, number]
export type DrawFeaturePhase = 'opening' | 'latest'
export type DrawFeatureKind = 'largest' | 'negative' | 'positive' | 'none' | 'missing'
export type DrawFeatureClassification = {
  kind: DrawFeatureKind
  differences: Prices | null
  drawDifference: number | null
  largest: number[]
  topTwo: number[]
  largestTied: boolean
  topTwoTied: boolean
}
export type DrawFeatureComparison = DrawFeatureClassification & {
  source: 'sporttery' | 'average'
  label: '体彩' | '均赔'
  william: Prices | null
  comparison: Prices | null
}

function prices(value: number[] | null | undefined): Prices | null {
  return value?.length === 3 && value.every((item) => typeof item === 'number' && Number.isFinite(item) && item > 0) ? value as Prices : null
}

/** Rank the unrounded signed differences by magnitude. Boundary ties stay in
 * the selection; one comparison contributes only its first matching category. */
export function classifyDrawDifferences(values: Prices | null, phase: DrawFeaturePhase = 'latest'): DrawFeatureClassification {
  if (!values || values.length !== 3 || !values.every((value) => typeof value === 'number' && Number.isFinite(value))) {
    return { kind: 'missing', differences: null, drawDifference: null, largest: [], topTwo: [], largestTied: false, topTwoTied: false }
  }
  const absolute = values.map(Math.abs)
  const sorted = [...absolute].sort((a, b) => a - b)
  const largest = absolute.flatMap((value, index) => value >= sorted[2]! - DRAW_FEATURE_EPS ? [index] : [])
  const topTwo = absolute.flatMap((value, index) => value >= sorted[1]! - DRAW_FEATURE_EPS ? [index] : [])
  const drawDifference = values[1]
  let kind: DrawFeatureKind = 'none'
  if (largest.includes(1)) kind = 'largest'
  else if (phase === 'latest' && topTwo.includes(1)) {
    if (drawDifference < -DRAW_FEATURE_EPS) kind = 'negative'
    else if (drawDifference > DRAW_FEATURE_EPS) kind = 'positive'
  }
  return { kind, differences: values, drawDifference, largest, topTwo, largestTied: largest.length > 1, topTwoTied: topTwo.length > 2 }
}

/** Each phase and company pair needs its own three real prices. Never borrow
 * opening/latest prices or another bookmaker to complete a missing pair. */
export function drawFeatureSignal(row: DrawObservation): Record<DrawFeaturePhase, DrawFeatureComparison[]> {
  const stages = drawComparisonStages(row)
  const phases = ['opening', 'latest'] as const
  const priority: Record<DrawFeatureKind, number> = { largest: 0, negative: 1, positive: 2, none: 3, missing: 4 }
  return Object.fromEntries(phases.map((phase) => {
    const stage = stages.find((item) => item.key === phase)
    const william = prices(stage?.william)
    const rows = (['sporttery', 'average'] as const).map((source): DrawFeatureComparison => {
      const comparison = prices(stage?.[source])
      const differences = william && comparison ? william.map((value, index) => value - comparison[index]!) as Prices : null
      return { source, label: source === 'sporttery' ? '体彩' : '均赔', william, comparison, ...classifyDrawDifferences(differences, phase) }
    })
    rows.sort((left, right) => priority[left.kind] - priority[right.kind])
    return [phase, rows]
  })) as Record<DrawFeaturePhase, DrawFeatureComparison[]>
}
