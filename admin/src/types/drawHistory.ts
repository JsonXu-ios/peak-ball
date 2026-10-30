import type { AnalysisMatch, BookmakerMarket, MatchRoiSimulation } from '../../../src/types/analysis'

export type { BookmakerMarket, BookmakerOutcome, DirectionValues } from '../../../src/types/analysis'

/** Read-only projection of the existing frontend analysis; no separate calculation. */
export type DrawFrontendAnalysis = Pick<AnalysisMatch,
  | 'yapanpankou1' | 'yapanpankou2' | 'qiushupankou1' | 'qiushupankou2'
  | 'yapantouzhu' | 'newyapantouzhu' | 'qiushutouzhu' | 'newqiushutouzhu'
  | 'winProbability' | 'drawProbability' | 'loseProbability'
  | 'sanhuxinli' | 'kailiresult' | 'ticairesult'
> & {
  /** Same current average European odds used by the frontend calculation; null means unavailable. */
  averageEuroOdds?: number[] | null
  comparisonStages?: DrawComparisonStage[]
  predictionAvailability?: { kelly: DrawPredictionState; ticai: DrawPredictionState }
  basis?: { asian: string; goals: string; kelly: string; ticai: string }
  /** Strict original inputs for the weighted support score; missing is never zero. */
  weightedFeatureInputs?: {
    homeHeat: number | null
    officialProfitRates: [number | null, number | null, number | null]
  }
  roiSimulation: MatchRoiSimulation | null
  localMarket: BookmakerMarket | null
}

export interface DrawPredictionState {
  available: boolean
  reason: string
  /** Win/draw/loss order. Kelly indices, or signed William minus sporttery prices. */
  values?: number[] | null
}

export interface DrawComparisonMetric extends DrawPredictionState {
  result: string[]
  basis: string
}

export interface DrawComparisonStage {
  key: 'opening' | 'latest'
  label: string
  note: string
  kelly: DrawComparisonMetric
  ticai: DrawComparisonMetric
  williamAverage: DrawComparisonMetric
  william: number[] | null
  sporttery: number[] | null
  average: number[] | null
}

export interface DrawFrontendLoadState {
  loading: boolean
  error: string
}

export interface DrawObservationQuery {
  mode?: 'today' | 'history'
  scope?: 'all' | 'sporttery'
  date?: string
  /** Compatibility with older history handlers; both bounds are the same selected day. */
  start_date?: string
  end_date?: string
  refresh?: number
  league?: string
  search?: string
  page?: number
  page_size?: number
  home_draws_min?: number
  home_draws_max?: number
  away_draws_min?: number
  away_draws_max?: number
  h2h_draws_min?: number
  h2h_draws_max?: number
}

export interface DrawRecentMatch {
  matchId: string
  date: string
  matchTime: string
  league: string
  home: string
  guest: string
  homeScore: number
  awayScore: number
  venue: 'home' | 'away'
  result: 'draw' | 'win' | 'loss'
}

export interface DrawRecentHistory {
  sample: number
  draws: number
  matches: DrawRecentMatch[]
}

export interface DrawObservedLine {
  opening: string | null
  latest: string | null
  historical: string | null
  company: string
  source: 'pankou_moneys' | 'history_moneys' | 'missing'
  openingOdds: Array<string | null>
  latestOdds: Array<string | null>
  capturedAt?: string | null
}

export interface DrawObservedEuroOdds {
	openingStatus?: 'available' | 'unopened' | 'no_quotes' | 'not_collected'
  company: string
  opening: Array<string | null>
  latest: Array<string | null>
  source: 'odds_moneys' | 'missing'
  capturedAt?: string | null
}

export interface DrawCompanyQuote {
  key: 'sporttery' | 'william' | 'bet365'
  name: string
  euroOdds: DrawObservedEuroOdds
  asianHandicap: DrawObservedLine
  goalsLine: DrawObservedLine
}

/** Fixed model selection is computed without the target match result. */
export interface FrozenDrawSignal {
  modelVersion: string
  state: 'selected' | 'not_selected' | 'unavailable' | 'out_of_scope'
  score?: number
  threshold?: number
  reason?: string
}

/** Server-owned secondary screening; target results only annotate this decision. */
export interface DrawSecondaryDecision {
  version: string
  decision: 'retained' | 'rejected' | 'not_selected' | 'unavailable' | 'out_of_scope'
  threshold: number
  score?: number
  reason: string
  reasons?: string[]
  reasonCodes?: string[]
  headToHeadSample: number
  minimumHeadToHead: number
  sportteryQuotesAvailable: boolean
}

/** Ranked server recommendations are separate from match selection. */
export interface DrawScoreRecommendationSignal {
  version: string
  state: 'recommended' | 'not_selected' | 'out_of_scope' | 'unavailable'
  scores: Array<{ home: number; away: number }>
  reason: string
  source?: 'sporttery_score_odds' | 'goal_estimate' | string
}

/** Actual serving provenance; it never changes the frozen model decision. */
export interface DrawResearchDecision {
  version: string
  source: 'research' | 'research_write_through' | 'legacy_fallback' | 'not_applicable'
  snapshotId?: number
  predictionId?: number
  asOf?: string
  reason?: string
}

export interface DrawResearchTakeover {
  version: string
  mode: 'research' | 'mixed' | 'fallback'
  served: number
  persisted: number
  fallback: number
  unavailable: number
  reason?: string
}

export interface DrawObservationDatesResponse {
  month: string
  mode: 'today' | 'history'
  scope: 'all' | 'sporttery'
  scheduleTimezone: string
  dates: Array<{ date: string; count: number; matchCount: number; drawCount: number }>
}

export interface DrawObservation {
  matchId: string
  jingcaiId?: string
  date: string
  matchTime: string
  league: string
  home: string
  guest: string
  homeScore: number
  awayScore: number
  displayState: string
  status: number
  settled: boolean
  /** Explicit confirmation of a usable regulation-time result; absent on older responses. */
  regulationSettled?: boolean
  homeRecent: DrawRecentHistory
  awayRecent: DrawRecentHistory
  headToHead?: DrawRecentHistory
  asianHandicap: DrawObservedLine
  goalsLine: DrawObservedLine
  euroOdds: DrawObservedEuroOdds
  companyQuotes?: DrawCompanyQuote[] | null
  frontendAnalysis?: DrawFrontendAnalysis | null
  frozenDrawSignal?: FrozenDrawSignal | null
  secondaryDecision?: DrawSecondaryDecision | null
  scoreRecommendation?: DrawScoreRecommendationSignal | null
  researchDecision?: DrawResearchDecision | null
  sources: string[]
  warnings: string[]
}

/** Complete query-window totals, independent of list mode, count filters and pagination. */
export interface DrawObservationMetricGroup {
  selected: number
  settled: number
  hits: number
  misses: number
  pending: number
  /** Confirmed regulation draws / confirmed regulation settled selections. */
  precision: number | null
  /** Confirmed regulation draws selected / all eligible confirmed draws in the window. */
  coverage: number | null
}

/** A descriptive split of the existing secondary pool, never a new selector. */
export interface DrawProfitSupportMetrics {
  version: string
  status: 'available' | 'incomplete'
  secondaryComparable: DrawObservationMetricGroup
  secondarySupported: DrawObservationMetricGroup
  secondaryNotSupported: DrawObservationMetricGroup
  secondaryUnavailable: DrawObservationMetricGroup
}

export interface DrawObservationMetrics {
  status: 'available' | 'incomplete'
  modelVersion: string
  secondaryVersion: string
  secondaryThreshold: number
  scopeMatches: number
  settledMatches: number
  settledDraws: number
  eligibleMatches: number
  eligibleSettled: number
  eligibleDraws: number
  modelAvailable: number
  modelUnavailable: number
  outOfScope: number
  secondaryUnavailable: number
  original: DrawObservationMetricGroup
  secondary: DrawObservationMetricGroup
  rejected: DrawObservationMetricGroup
  /** Absent on services that have not loaded profit-support-v1 aggregation. */
  profitSupport?: DrawProfitSupportMetrics | null
}

export interface DrawObservationsResponse {
  dataVersion?: string
  generatedAt?: string
  scheduleTimezone?: string | null
  scope?: 'all' | 'sporttery'
  mode: 'today' | 'history'
  date: string
  /** Absent on services that have not loaded the current-window metrics update. */
  observationMetrics?: DrawObservationMetrics | null
  /** Absent until the analysis service loads the research serving integration. */
  researchTakeover?: DrawResearchTakeover | null
  summary: {
    /** All deduplicated money/moneys rows in the response scope, including fixtures not yet completed. */
    historicalMatches: number
    /** Completed regulation draws in money/moneys within the response scope. */
    drawMatches: number
    /** Coverage of the mode/date fixture pool, before league/search pagination. */
    asianMatches: number
    totalMatches: number
  }
  total: number
  page: number
  pageSize: number
  leagues: string[]
  items: DrawObservation[]
  notes: string[]
}
