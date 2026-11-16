import type { PublicMatch, PublicResult } from '@/api/publicSources'
import { frozenDrawPresentation } from './drawFrozenSignal'
import { secondaryDrawPresentation } from './drawSecondarySignal'
import { drawScorePresentation } from './drawScoreRecommendation'
import { drawTotalsPredictionPresentation } from './drawTotalsPrediction'

const score = (value: unknown): value is number => typeof value === 'number' && Number.isInteger(value) && value >= 0
export function confirmedPublicResult(result?: PublicResult) {
  return result?.state === 'finished' && result.regulationSettled === true && score(result.homeScore) && score(result.awayScore)
}
export function publicResultPresentation(result?: PublicResult) {
  const confirmed = confirmedPublicResult(result)
  const labels = { finished: '比分待核实', pending: '未完赛', unverified: '比分待核实', unavailable: '赛果待更新', cancelled: '已取消', postponed: '已延期' }
  return {
    confirmed,
    label: confirmed ? '已完赛 · 常规时间' : result ? labels[result.state] || '比分待核实' : '赛果待更新',
    score: result && score(result.homeScore) && score(result.awayScore) ? `${result.homeScore} : ${result.awayScore}` : '',
    color: confirmed ? 'success' : result?.state === 'unverified' ? 'warning' : undefined,
    reason: result?.reason || '点击“更新赛果”获取已保存比赛的最新结果。',
  }
}

/** Count only confirmed results paired with the server's saved predictions. */
export function publicResultMetrics(matches: PublicMatch[]) {
  const metrics = {
    confirmed: 0, pending: 0,
    draw: { hits: 0, total: 0 }, secondary: { hits: 0, total: 0 }, scores: { hits: 0, total: 0 },
    totals: { full_win: 0, half_win: 0, push: 0, half_loss: 0, full_loss: 0 },
  }
  for (const match of matches) {
    if (!confirmedPublicResult(match.result)) { metrics.pending++; continue }
    metrics.confirmed++
    const row = match.models?.observation
    // A stale/mismatched observation cannot settle a newly retrieved result.
    if (!row || row.settled !== true || row.regulationSettled !== true || row.homeScore !== match.result!.homeScore || row.awayScore !== match.result!.awayScore) continue
    const draw = frozenDrawPresentation(row)
    if (draw.label === '命中平局' || draw.label === '入选 · 未打平') {
      metrics.draw.total++; if (row.homeScore === row.awayScore) metrics.draw.hits++
    }
    const secondary = secondaryDrawPresentation(row)
    if (secondary.decision === 'retained') {
      metrics.secondary.total++; if (row.homeScore === row.awayScore) metrics.secondary.hits++
    }
    const scores = drawScorePresentation(row)
    if (scores.state === 'recommended') {
      metrics.scores.total++; if (scores.scores.includes(`${row.homeScore}:${row.awayScore}`)) metrics.scores.hits++
    }
    const totals = drawTotalsPredictionPresentation(row)
    if (totals.outcomeState !== 'none' && totals.outcomeState !== 'pending') metrics.totals[totals.outcomeState]++
  }
  return metrics
}
