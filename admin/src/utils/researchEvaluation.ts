import type { ResearchEvaluation } from '@/types/researchStore'

/** Historical model scores and null denominators must never look like rates. */
export function researchRate(value: number | null | undefined) {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 && value <= 1
    ? `${(value * 100).toFixed(2)}%` : '暂无'
}

/** Evaluation generations describe different score methods and cannot be
 * presented as a single evolving accuracy measurement. */
export function isResearchScoreRankMetrics(evaluation: Pick<ResearchEvaluation, 'version' | 'metrics'>) {
  if (evaluation.version === 'research-evaluation-v2-score-ranks') {
    return !evaluation.metrics.scoreMethod || evaluation.metrics.scoreMethod === 'draw-score-baseline-v1'
  }
  if (evaluation.version === 'research-evaluation-v3-dynamic-scores') {
    return !evaluation.metrics.scoreMethod || evaluation.metrics.scoreMethod === 'draw-score-dynamic-v2'
  }
  return false
}

export function researchScoreMethod(evaluation: Pick<ResearchEvaluation, 'version' | 'metrics'>) {
  if (isResearchScoreRankMetrics(evaluation) && evaluation.version === 'research-evaluation-v3-dynamic-scores') {
    return { kind: 'current-dynamic', label: '本场数据比分推荐', note: '首选、次选按各场竞彩比分报价或进球数据排序；二次候选要求报价完整且赛前交锋至少5场。' }
  }
  if (isResearchScoreRankMetrics(evaluation) && evaluation.version === 'research-evaluation-v2-score-ranks') {
    return { kind: 'historical-baseline', label: '历史固定比分：1:1 / 0:0', note: '这是旧版固定比分及旧版二次规则的评估，保留用于对照；不代表当前推荐的正确率。' }
  }
  return { kind: 'unknown', label: '比分评估口径待确认', note: '该记录的比分方法尚未确认，首选与前二统计暂不展示。' }
}

export function researchEvaluationBasis(evaluation: Pick<ResearchEvaluation, 'scope' | 'metrics'>) {
  if (evaluation.metrics.historicalAssociationOnly === true) {
    return { label: '历史重建关联', color: 'warning', note: '包含历史重建输入，仅用于回看与比较；不能视为独立的赛前预测验证。' }
  }
  if (evaluation.metrics.independentOfHistoricalReconstruction === true) {
    return { label: '真实赛前首次预测', color: 'success', note: '每场仅采用首次可用的真实赛前预测；历史重建排除，未完赛比赛不计入命中率。' }
  }
  return { label: '验证口径待确认', color: 'info', note: '该记录未声明历史重建或真实赛前验证口径，请结合版本信息审查。' }
}
