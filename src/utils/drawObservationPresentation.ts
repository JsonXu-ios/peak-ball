import type { DrawObservation } from '@/types/drawHistory'
import { frozenDrawPresentation, hasCurrentJcDrawSelectionMetadata } from '../../admin/src/utils/drawFrozenSignal'
import { drawAlternativeOutcomePresentation } from '../../admin/src/utils/drawAlternativeOutcome'
import { nonSportteryDrawPresentation, nonSportteryAlternativePresentation } from '../../admin/src/utils/drawNonSporttery'
import { secondaryDrawPresentation } from '../../admin/src/utils/drawSecondarySignal'
import { drawTotalsPredictionPresentation } from '../../admin/src/utils/drawTotalsPrediction'
import { drawScorePresentation } from '../../admin/src/utils/drawScoreRecommendation'
import { formatBeijingDateTime } from '../../admin/src/utils/date'

export { beijingToday } from '../../admin/src/utils/date'

export interface DrawCardPresentation {
  selected: boolean
  primary: { label: string; detail: string; color: string }
  alternative: { label: string; detail: string; outcomeLabel: string; color: string }
  totals: { label: string; outcomeLabel: string; reason: string }
  score: { label: string; detail: string }
  secondary: { label: string; detail: string }
  /** Existing server probabilities in percent, never the draw selection score. */
  probabilities: [number | null, number | null, number | null]
}

export function isConfirmed(row: Pick<DrawObservation, 'settled' | 'regulationSettled' | 'homeScore' | 'awayScore'>): boolean {
  return row.settled === true && row.regulationSettled === true
    && Number.isInteger(row.homeScore) && row.homeScore >= 0
    && Number.isInteger(row.awayScore) && row.awayScore >= 0
}

/** Adapt existing server decisions and shared display rules; fit no new model. */
export function cardPresentation(row: DrawObservation): DrawCardPresentation {
  const safe = isConfirmed(row) ? row : { ...row, settled: false, regulationSettled: false }
  const jc = !!row.jingcaiId?.trim()
  const primary = jc ? frozenDrawPresentation(safe) : nonSportteryDrawPresentation(safe)
  const alternative = jc ? drawAlternativeOutcomePresentation(safe) : nonSportteryAlternativePresentation(safe)
  const totals = drawTotalsPredictionPresentation(safe)
  const score = drawScorePresentation(safe)
  const secondary = secondaryDrawPresentation(safe)
  const raw = [row.frontendAnalysis?.winProbability, row.frontendAnalysis?.drawProbability, row.frontendAnalysis?.loseProbability]
  const probabilities = raw.map(value => typeof value === 'number' && Number.isFinite(value) && value >= 0 && value <= 100 ? value : null) as DrawCardPresentation['probabilities']
  if (!probabilities.some(value => value !== null && value > 0)) probabilities.fill(null)
  return {
    selected: jc ? row.frozenDrawSignal?.state === 'selected' && hasCurrentJcDrawSelectionMetadata(row.frozenDrawSignal) : row.nonSportterySignal?.state === 'selected',
    primary: { label: primary.label, detail: primary.detail, color: primary.color || '' },
    alternative: { label: alternative.label, detail: alternative.reason, outcomeLabel: alternative.outcomeLabel, color: alternative.color || '' },
    totals: { label: totals.label, outcomeLabel: totals.outcomeLabel, reason: totals.reason },
    score: { label: score.scores.length ? score.scores.map((value, index) => `${index === 0 ? '首选比分' : '备选比分'} ${value}`).join(' / ') : score.label,
      detail: [score.reason, score.outcomeLabel].filter(Boolean).join('；') },
    secondary: jc ? { label: secondary.label, detail: [secondary.reason, secondary.outcomeLabel].filter(Boolean).join('；') }
      : { label: '不适用', detail: '二次筛选仅适用于竞彩，非竞彩使用独立选平信号' },
    probabilities,
  }
}

export function beijingDay(raw: string): string {
  const formatted = formatBeijingDateTime(raw, '')
  return /^\d{4}-\d{2}-\d{2}(?: |$)/.test(formatted) ? formatted.slice(0, 10) : ''
}

export function kickoffTime(raw: string): string {
  const formatted = formatBeijingDateTime(raw, '')
  return /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}/.test(formatted) ? formatted.slice(11, 16) : '—'
}

/** Calendar arithmetic independent of the browser's timezone and DST. */
export function shiftDay(day: string, offset: number): string {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(day) || !Number.isInteger(offset)) return day
  const instant = new Date(`${day}T12:00:00Z`)
  if (!Number.isFinite(instant.getTime()) || instant.toISOString().slice(0, 10) !== day) return day
  instant.setUTCDate(instant.getUTCDate() + offset)
  return instant.toISOString().slice(0, 10)
}
