import type { AnalysisMatch, BookmakerMarket, MatchRoiSimulation } from '../../../src/types/analysis'

export type { BookmakerMarket, BookmakerOutcome, DirectionValues } from '../../../src/types/analysis'

/** Read-only projection of the existing frontend analysis; no separate calculation. */
export type DrawFrontendAnalysis = Pick<AnalysisMatch,
  | 'yapanpankou1' | 'yapanpankou2' | 'qiushupankou1' | 'qiushupankou2'
  | 'yapantouzhu' | 'newyapantouzhu' | 'qiushutouzhu' | 'newqiushutouzhu'
  | 'winProbability' | 'drawProbability' | 'loseProbability'
  | 'sanhuxinli' | 'kailiresult' | 'ticairesult'
> & {
  basis?: { asian: string; goals: string; kelly: string; ticai: string }
  roiSimulation: MatchRoiSimulation | null
  localMarket: BookmakerMarket | null
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
  homeRecent: DrawRecentHistory
  awayRecent: DrawRecentHistory
  headToHead?: DrawRecentHistory
  asianHandicap: DrawObservedLine
  goalsLine: DrawObservedLine
  euroOdds: DrawObservedEuroOdds
  companyQuotes?: DrawCompanyQuote[] | null
  frontendAnalysis?: DrawFrontendAnalysis | null
  sources: string[]
  warnings: string[]
}

export interface DrawObservationsResponse {
  dataVersion?: string
  generatedAt?: string
  scheduleTimezone?: string | null
  scope?: 'all' | 'sporttery'
  mode: 'today' | 'history'
  date: string
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
