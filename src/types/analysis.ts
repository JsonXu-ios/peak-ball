export interface AnalysisDetail {
  date: string
  matchId: string
  home: string
  test1: string[]
  test2: unknown[]
  test3: string[]
  test4: string[]
  test5: string[]
  test6: string[]
  test7: number[]
  test8: string[]
  test9: unknown[]
  test10: string[]
  test11: string[]
  test14: unknown[]
  test15: unknown[]
  test16: string[]
  test17: unknown[]
  test19: unknown[]
  test20: unknown[]
  test21: string
  test22: string
  test23: string[]
}

export interface DirectionValues {
  home: number
  draw: number
  away: number
}

export interface BookmakerOutcome {
  outcome: 'home' | 'draw' | 'away'
  outcomeLabel: string
  retailShare: number
  probability?: number
  error?: number
  betStake: number
  totalStake: number
  odds: number
  payout: number
  bookmakerProfit: number
  bookmakerLoss: number
  bookmakerRoi: number
  officialProfitRate?: number
  hotColdIndex?: number
  bookmakerOutcome: string
  available: boolean
}

export interface BookmakerMarket {
  key: 'sporttery' | 'sportteryRqspf' | 'william' | 'bet365' | string
  name: string
  companyId?: string
  goal?: string
  odds: DirectionValues
  oddsAvailable: boolean
  retailDistribution: DirectionValues
  psychologyError?: number
  psychologyErrorLabel?: string
  bettingRatio?: BookmakerOutcome[]
  bookmakerByOutcome: BookmakerOutcome[]
}

export interface MatchRoiSimulation {
  totalStake: number
  retailDistribution: DirectionValues
  markets: BookmakerMarket[]
}

export interface AnalysisTeamProfile {
  teamName: string
  league: string
  summary: string
  sourceTitle: string
  sourceUrl: string
  fetchedAt: string
}

export interface AnalysisTeamProfiles {
  home: AnalysisTeamProfile
  guest: AnalysisTeamProfile
}

export interface GoddessWomanDimensionScore {
  label: string
  home: number
  guest: number
}

export interface GoddessWomanPrediction {
  title: string
  prediction: string
  confidence: string
  homeScore: number
  guestScore: number
  probabilities: DirectionValues
  formula: string
  reasonSummary: string
  reasons: string[]
  dimensionScores: GoddessWomanDimensionScore[]
  seventhSenseLabel: string
}

export interface AnalysisMatch {
  matchId: string
  date: string
  league: string
  home: string
  guest: string
  matchTime: string
  displayState: string
  status: number
  jingcaiId: string
  homeScore: number
  guestScore: number
  homeLogo: string
  guestLogo: string
  homeRank: string
  guestRank: string
  winProbability: number
  drawProbability: number
  loseProbability: number
  prediction: string
  qiuprediction: string
  confidence: string
  tags: string[]
  warnings: string[]
  sportteryOdds: number[]
  roiSimulation?: MatchRoiSimulation
  teamProfiles?: AnalysisTeamProfiles
  goddessWoman?: GoddessWomanPrediction
  sanhuxinli: string[]
  kaijuresult: string[]
  kailiresult: string[]
  ticairesult: string[]
  liangduilishi: string[]
  liangduibisai: unknown[]
  homezuijinbisai: unknown[]
  guestzuijinbisai: unknown[]
  touzhue: number[]
  changguiyapan: string
  changguiqiushu: string
  yapantouzhu: unknown[]
  newyapantouzhu: unknown[]
  qiushutouzhu: unknown[]
  newqiushutouzhu: unknown[]
  qiushuAll: unknown[]
  liangduiqiushu: unknown[]
  yapanpankou1: number
  yapanpankou2: number
  newpankou: number
  qiushupankou1: number
  qiushupankou2: number
  newqiushu: number
  yapanai: number[]
  qiushuai: number[]
  oddsCompanyCount: number
  asiaCount: number
  dxqCount: number
  detail: AnalysisDetail
}
