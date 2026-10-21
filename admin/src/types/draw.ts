/** Probabilities and rates are percentage points (0–100); missing values remain null. */
export interface DrawHistoryQuery {
  start_date?: string
  end_date?: string
  league?: string
  search?: string
  outcome?: string
  split?: string
  page?: number
  page_size?: number
  threshold?: number
}

export interface DrawForm {
  sample: number
  draws: number
  drawRate: number | null
  smoothedDrawRate: number | null
  goalsFor: number | null
  goalsAgainst: number | null
  lowScoreRate: number | null
  venue: 'all' | 'home' | 'away'
}

export interface DrawHistoryRow {
  matchId: string
  date: string
  matchTime: string
  league: string
  home: string
  guest: string
  homeScore: number
  awayScore: number
  isDraw: boolean
  probability: number | null
  rawProbability?: number | null
  marketProbability: number | null
  selected: boolean
  outcome: string
  split: string
  sources: string[]
  warnings: string[]
  signals?: Array<{ key: string; label: string; market: string; direction: string; value: number | null }>
  outOfTime?: boolean
  exploratory?: boolean
  eligible?: boolean
  groupProbabilities?: Record<string, number>
  groupPartitions?: Record<string, string>
  homeRecent?: DrawForm
  awayRecent?: DrawForm
  homeVenue?: DrawForm
  awayVenue?: DrawForm
  homeExpectedGoals?: number | null
  awayExpectedGoals?: number | null
  baselineProbability?: number | null
  baselineSample?: number
  oddsProvenance?: string
}

export interface DrawHistorySummary {
  total: number
  eligible: number
  actualDraws: number
  selected: number
  hits: number
  falsePositives: number
  falseNegatives: number
  trueNegatives: number
  precision: number | null
  recall: number | null
  coverage: number | null
  drawRate: number | null
  brier: number | null
  marketBrier: number | null
}

export interface DrawCalibrationBin {
  from: number
  to: number
  count: number
  predicted: number | null
  actual: number | null
}

export interface DrawModelComparison {
  key: string
  label: string
  sample: number
  hits: number
  selected: number
  precision: number | null
  coverage: number | null
  recall: number | null
  brier: number | null
  exploratory: boolean
  notes: string
  independentSplit?: boolean
  commonSample?: number
  commonBrier?: number | null
}

export interface DrawSourceCoverage {
  key: string
  label: string
  records: number
  matches: number
  usable: number
  notes: string
}

export interface DrawDataContribution {
  key: string
  label: string
  sample: number
  drawRate: number | null
  notes: string
}

export interface DrawJobProgress { done: number; total: number; phase: string }

export interface DrawHistoryReport {
  running: boolean
  progress?: DrawJobProgress
  modelVersion: string
  generatedAt: string
  range: { start: string; end: string }
  total: number
  eligible: number
  page: number
  pageSize: number
  threshold: number
  items: DrawHistoryRow[]
  summary: DrawHistorySummary
  testSummary?: DrawHistorySummary
  bins: DrawCalibrationBin[]
  comparisons: DrawModelComparison[]
  contributions: DrawDataContribution[]
  signalsSummary?: Array<{ key: string; label: string; sample: number; hits: number; precision: number | null; drawRate: number | null; notes: string }>
  sources: DrawSourceCoverage[]
  leagues: string[]
  model?: {
    calibrated: boolean
    trainingSamples: number
    calibrationSamples: number
    testSamples: number
    canCalibrate?: boolean
    minSamples?: number
    activeGroup?: string
    recommendedThreshold?: number
    message?: string
  }
  notes: string[]
}

export interface DrawActionResponse {
  running?: boolean
  progress?: DrawJobProgress
  message?: string
  modelVersion?: string
  samples?: number
  created?: number
  skipped?: number
}

export interface DrawUpcomingMatch {
  matchId: string
  league: string
  home: string
  guest: string
  matchTime: string
  status: string
  probability: number | null
  modelProbability: number | null
  marketProbability: number | null
  candidate: boolean
  quality: string
  warnings: string[]
  reasons: string[]
  modelVersion: string
  frozen: boolean
}

export interface DrawUpcomingReport {
  date: string
  scheduleTimezone: string
  modelVersion: string
  calibrated: boolean
  asOf: string
  items: DrawUpcomingMatch[]
  summary: { total: number; candidates: number; insufficient: number; frozen: number }
}
