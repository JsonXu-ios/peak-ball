export interface DrawObservationQuery {
  mode?: 'today' | 'history'
  date?: string
  start_date?: string
  end_date?: string
  league?: string
  search?: string
  page?: number
  page_size?: number
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

export interface DrawObservation {
  matchId: string
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
  asianHandicap: DrawObservedLine
  goalsLine: DrawObservedLine
  euroOdds: DrawObservedEuroOdds
  sources: string[]
  warnings: string[]
}

export interface DrawObservationsResponse {
  mode: 'today' | 'history'
  date: string
  summary: {
    /** All deduplicated money/moneys rows, including fixtures not yet completed. */
    historicalMatches: number
    /** Completed regulation draws in money/moneys. */
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
