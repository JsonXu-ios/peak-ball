/** 比赛基本信息 */
export interface Match {
  id: number
  matchId: string
  date: string
  league: string
  leagueName: string
  leagueId: number
  home: string
  guest: string
  homeTeamId: number
  guestTeamId: number
  matchTime: string
  homeScore: number
  guestScore: number
  homeHalfScore: number
  guestHalfScore: number
  homeRank: string
  guestRank: string
  status: number
  displayState: string
  homeLogo: string
  guestLogo: string
}

/** 历史战绩 - 汇总 */
export interface HistorySummary {
  win: number
  lose: number
  draw: number
  all: number
  winGoal: number
  loseGoal: number
  homeWin: number
  homeLose: number
  homeDraw: number
  homeAll: number
  guestWin: number
  guestDraw: number
  guestLose: number
  guestAll: number
}

/** 历史战绩 - 单场记录 */
export interface HistoryMatch {
  matchTime: string
  home: string
  guest: string
  homeId: number
  guestId: number
  goal: number[]
  halfGoal: number[]
  league: string
  scheduleId: number
  matchId: number
  letGoal: number | null
  separate: number
}

/** 联赛统计 */
export interface LeagueStat {
  sid: string
  spf: number[]
  prevSpf: number[]
  league: string
}

/** 排名条目 */
export interface RankItem {
  rank: number
  name: string
  game: number
  win: number
  draw: number
  lose: number
  winGoal: number
  lostGoal: number
  totalScore: number
  color: string
  teamId: number
}

/** 排名数据 */
export interface RankData {
  list: RankItem[]
  color: Array<{
    leagueId: number
    season: string
    color: string
    nameJ: string
    nameE: string
    nameF: string
    shengjiang: number
  }>
}

/** 未来赛程 */
export interface FutureMatch {
  matchTime: string
  home: string
  guest: string
  homeId: number
  guestId: number
  goal: number[]
  halfGoal: number[]
  league: string
  scheduleId: number
  matchId: number
  letGoal: number | null
  separate: number
}

/** 历史战绩完整响应 */
export interface MatchHistory {
  id: number
  matchId: string
  date: string
  leagueStat: LeagueStat | null
  againstSummary: HistorySummary | null
  againstList: HistoryMatch[] | null
  recentHomeSummary: HistorySummary | null
  recentHomeList: HistoryMatch[] | null
  recentGuestSummary: HistorySummary | null
  recentGuestList: HistoryMatch[] | null
  rankData: RankData | null
  futureHome: FutureMatch[] | null
  futureGuest: FutureMatch[] | null
}

/** 欧赔 - 单条 */
export interface EuroOdd {
  companyId: string
  companyName: string
  odds: string[]
  firstOdds: string[]
  returnRatio: string
  firstReturnRatio: string
  ratio: string[]
  firstRatio: string[]
  oddsTrend: number[]
  ratioTrend: number[]
  firstKelly: number[] | string[] | null
  kelly: number[] | string[] | null
}

/** 欧赔数据 */
export interface MatchOddsEuro {
  id: number
  matchId: string
  date: string
  data: EuroOdd[] | null
  riseAndFall: unknown | null
  avgOdds: EuroOdd | null
  william: EuroOdd | null
  bet365: EuroOdd | null
  pinnacle: EuroOdd | null
  companyCount: number
}

/** 盘口 - 单条 */
export interface PankouItem {
  companyId: number
  companyName: string
  oddsTrend: number[]
  odds: string[]
  firstOdds: string[]
  firstPankou: string
  pankou: string
  firstReturnRatio: string
  returnRatio: string
}

/** 盘口数据 */
export interface MatchOddsPankou {
  id: number
  matchId: string
  date: string
  asiaData: PankouItem[] | null
  dxqData: PankouItem[] | null
  bet365Asia: PankouItem | null
  bet365Dxq: PankouItem | null
  asiaCount: number
  dxqCount: number
}
