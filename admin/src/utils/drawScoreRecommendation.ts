import type { DrawObservation } from '@/types/drawHistory'
import { classifySecondaryDraw } from './drawSecondarySignal'

export const DRAW_SCORE_RECOMMENDATION_VERSION = 'draw-score-dynamic-v2'

export interface DrawScoreRecommendation {
  version: string
  state: 'recommended' | 'not_selected' | 'out_of_scope' | 'unavailable'
  scores: string[]
  reason: string
  sourceLabel?: string
  needsUpdate?: boolean
}

type ScoreInput = Pick<DrawObservation, 'jingcaiId' | 'frozenDrawSignal' | 'secondaryDecision' | 'scoreRecommendation'>
type ScoreResultInput = ScoreInput & Pick<DrawObservation, 'settled' | 'regulationSettled' | 'homeScore' | 'awayScore'>
const sourceLabels: Record<string, string> = {
  sporttery_score_odds: '竞彩比分报价',
  goal_estimate: '近期进球、交锋与大小球估计',
}

/** Preserve the backend's two-score order. Missing/legacy payloads have no
 * browser fallback, and neither choice reads the target match result. */
export function recommendDrawScores(row: ScoreInput): DrawScoreRecommendation {
  const base = { version: DRAW_SCORE_RECOMMENDATION_VERSION, scores: [] as string[] }
  if (!row.jingcaiId?.trim()) return { ...base, state: 'out_of_scope', reason: '仅对二次保留的竞彩比赛推荐' }
  const secondary = classifySecondaryDraw(row)
  if (secondary.decision === 'out_of_scope') return { ...base, state: 'out_of_scope', reason: secondary.reason }
  if (secondary.decision === 'unavailable') return { ...base, state: 'unavailable', reason: secondary.reason, needsUpdate: secondary.needsUpdate }
  if (secondary.decision !== 'retained') return { ...base, state: 'not_selected', reason: '未二次入选，不推荐比分' }
  const recorded = row.scoreRecommendation
  if (!recorded || recorded.version !== DRAW_SCORE_RECOMMENDATION_VERSION) {
    return { ...base, state: 'unavailable', needsUpdate: true, reason: '比分推荐待更新' }
  }
  const reason = typeof recorded.reason === 'string' ? recorded.reason.trim() : ''
  if (recorded.state === 'unavailable') return { ...base, state: 'unavailable', reason: reason || '暂无可用的比分推荐' }
  const validScores = Array.isArray(recorded.scores) && recorded.scores.length === 2 && recorded.scores.every((score) => score
    && Number.isInteger(score.home) && score.home >= 0 && score.home <= 6
    && Number.isInteger(score.away) && score.away === score.home)
  const scores = validScores ? recorded.scores.map((score) => `${score.home}:${score.away}`) : []
  if (recorded.state !== 'recommended' || scores.length !== 2 || scores[0] === scores[1] || !reason) {
    return { ...base, state: 'unavailable', needsUpdate: true, reason: '比分推荐结果待更新，请重新查询' }
  }
  return { version: recorded.version, state: 'recommended', scores, reason,
    sourceLabel: recorded.source ? sourceLabels[recorded.source] : undefined }
}

/** Only the settlement annotation reads confirmed regulation goals. */
export function drawScorePresentation(row: ScoreResultInput) {
  const recommendation = recommendDrawScores(row)
  if (recommendation.state !== 'recommended') {
    return { ...recommendation, label: recommendation.state === 'out_of_scope' ? '非竞彩'
      : recommendation.state === 'unavailable' ? recommendation.needsUpdate ? '待更新' : '暂无推荐' : '未二次入选', outcomeLabel: '', outcomeColor: '' }
  }
  const settled = row.settled === true && row.regulationSettled === true
    && Number.isInteger(row.homeScore) && row.homeScore >= 0
    && Number.isInteger(row.awayScore) && row.awayScore >= 0
  const hitIndex = settled ? recommendation.scores.indexOf(`${row.homeScore}:${row.awayScore}`) : -1
  const outcomeLabel = !settled ? '待赛果核对' : hitIndex === 0 ? '首选命中' : hitIndex === 1 ? '次选命中（前二命中）' : '前二未命中'
  return { ...recommendation, label: recommendation.sourceLabel || '推荐比分', outcomeLabel, outcomeColor: settled ? hitIndex >= 0 ? 'success' : 'warning' : '' }
}
