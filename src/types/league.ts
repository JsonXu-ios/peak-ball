/** 联赛积分榜条目 */
export interface LeagueStanding {
  id: number
  leagueId: number
  league: string
  season: string
  teamId: number
  teamName: string
  teamLogo: string
  rank: number
  played: number
  won: number
  drawn: number
  lost: number
  goalsFor: number
  goalsAgainst: number
  goalDiff: number
  points: number
  form: string
  zone: string
  createdAt: string
  updatedAt: string
}

/** 射手榜 / 助攻榜条目 */
export interface TopScorer {
  id: number
  leagueId: number
  league: string
  season: string
  playerName: string
  teamName: string
  avatar: string
  goals: number
  assists: number
  rank: number
  createdAt: string
  updatedAt: string
}
