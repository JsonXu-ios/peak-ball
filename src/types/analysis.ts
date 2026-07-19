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
  /** 非竞彩比赛的本地模拟盘（指数/让球数/冷热指数由后端推算，非官方数据） */
  simulated?: boolean
  simulationNote?: string
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

// ---- unified backend decision block (computed by go_server, never client-side) ----

export interface PlatformGoalResult {
  label: string
  total: number
  tone: 'red' | 'green' | 'blue' | 'normal'
}

export interface PlatformGuidePrediction {
  outcome: 'home' | 'draw' | 'away'
  goal: PlatformGoalResult
  score: string
  secondaryScore: string
  warning?: string
  warningTone?: 'red' | 'green' | 'blue' | 'normal'
}

export interface PlatformWarningRow {
  value: string
  tone: 'red' | 'green' | 'blue' | 'normal'
}

export interface PlatformStatRow {
  label: string
  value: string
  tone: 'red' | 'green' | 'blue' | 'normal'
}

export interface PlatformGoalPair {
  home: number | null
  guest: number | null
}

export interface PlatformEvilCultRow {
  label: string
  primary: string
  secondary: string
  tone: string
  primaryTone: string
  secondaryTone: string
}

export interface PlatformEvilCultStep {
  label: string
  detail: string
  overDelta: number
  underDelta: number
  overScore: number
  underScore: number
}

export interface PlatformEvilCultPrediction {
  goal: string
  secondaryGoal: string
  total: string
  secondaryTotal: string
  underGoal: string
  overGoal: string
  underTotalText: string
  overTotalText: string
  underTotalValue: number
  overTotalValue: number
  underGoalLine: number
  overGoalLine: number
  underScore: string
  overScore: string
  underOutcome: 'home' | 'draw' | 'away'
  overOutcome: 'home' | 'draw' | 'away'
  firstPick: string
  firstDirection: 'over' | 'under'
  mainPick: string
  reversePick: string
  mainReason: string
  secondPassReason: string
  secondPassReversed: boolean
  secondPassForced: boolean
  secondOverScore: number
  secondUnderScore: number
  mainTotal: number
  secondaryTotalValue: number
  goalDirection: 'over' | 'under'
  secondaryGoalDirection: 'over' | 'under'
  goalLine: number
  secondaryGoalLine: number
  score: string
  secondaryScore: string
  outcome: 'home' | 'draw' | 'away'
  secondaryOutcome: 'home' | 'draw' | 'away'
  goalTone: string
  reverseTone: string
  note: string
  reason: string
}

export interface PlatformEvilCult {
  line: number
  rows: PlatformEvilCultRow[]
  prediction: PlatformEvilCultPrediction
  scores: {
    over: number
    under: number
    overPercent: number
    underPercent: number
    steps: PlatformEvilCultStep[]
  }
  inputs: Array<{ label: string; value: string; detail: string }>
}

export interface PlatformDecision {
  bookmaker: PlatformGuidePrediction
  platform: PlatformGuidePrediction
  warningRows: PlatformWarningRow[]
  warningAdjusted?: PlatformGuidePrediction
  warningAdjustedSummary: string
  professionalConflict?: PlatformWarningRow
  professionalConsensus: '' | 'home' | 'draw' | 'away'
  sportteryComfort: '' | 'home' | 'draw' | 'away'
  rqspfComfort: '' | 'home' | 'draw' | 'away'
  drawRisk: { score: number; reasons: string[] }
  handicapPressureLabel: string
  goalBalanceSignal: '' | 'under' | 'underHidden' | 'over' | 'overCorrected'
  goals: { under: PlatformGoalPair; main: PlatformGoalPair; over: PlatformGoalPair }
  zeroGoalAdvice: string
  handicapAlertRows: PlatformStatRow[]
  goalBalanceAlertRows: PlatformStatRow[]
  evilCult: PlatformEvilCult
  localMarket?: BookmakerMarket
}

// ---- 命中率统计与规则匹配（全部由 go_server 计算，前端只渲染） ----

export type AccuracyTone = 'red' | 'green' | 'blue' | 'normal'

export interface AccuracyStatsRow {
  label: string
  sample: number
  bookmakerCorrect: number
  platformCorrect: number
  bothCorrect: number
}

export interface EvilCultAccuracyRow {
  label: string
  sample: number
  underCorrect: number
  overCorrect: number
  firstCorrect: number
  mainCorrect: number
  reverseCorrect: number
}

export interface AccuracyOverallStats {
  sample: number
  bookmakerCorrect: number
  platformCorrect: number
}

export interface AccuracyCommonRule {
  value: string
  sample: number
  bothCorrect: number
  rate: number
}

export interface AccuracyCommonRow {
  label: string
  sample: number
  rules: AccuracyCommonRule[]
}

export interface AccuracyFitSummary {
  label: string
  tone: AccuracyTone
  score: number
  ruleCount: number
  rate: number
  sample: number
}

export interface AccuracyMatchRow {
  matchId: string
  date: string
  matchTitle: string
  league: string
  time: string
  outcomeFit: AccuracyFitSummary
  goalFit: AccuracyFitSummary
  scoreFit: AccuracyFitSummary
  conclusion: string
  tone: AccuracyTone
  evidence: string
  resultSummary: string
  resultTone: AccuracyTone
}

export interface AccuracyStatsSummary {
  startDate: string
  endDate: string
  total: number
  overall: AccuracyOverallStats
  rows: AccuracyStatsRow[]
  evilCultRows: EvilCultAccuracyRow[]
  commonRows: AccuracyCommonRow[]
  generatedCommonRows: AccuracyCommonRow[]
  matchRows: AccuracyMatchRow[]
  settledFitRows: AccuracyMatchRow[]
}

export interface MyAngleMarket {
  bucket: string
  sample: number
  hit: number
  accuracy: number
  verdict: 'red' | 'black' | 'neutral'
}

export interface MyAngleBlock {
  totalPicks: number
  spf: MyAngleMarket
  rqspf: MyAngleMarket
  dxq: MyAngleMarket
}

export interface UserPick {
  id: number
  matchId: string
  market: 'spf' | 'rqspf' | 'dxq' | 'score'
  pick: string
  line: number | null
  direction: 'follow' | 'fade' | 'self'
  confidence: number
  note: string
  source: string
  createdAt: string
  updatedAt: string
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
  /** 综合均值与命中标记：后端算好（null = 样本不足），前端只展示 */
  historyGoalAverage: number | null
  recentGoalAverage: number | null
  combinedGoalAverage: number | null
  combinedHandicapAverage: number | null
  predictionHit: boolean | null
  detail: AnalysisDetail
  platform?: PlatformDecision
  myAngle?: MyAngleBlock
  accuracyFit?: AccuracyMatchRow
}

/** 补录页返回：完赛已隐藏比分；未开赛保持原状态（赛前记录 source=live） */
export interface PickEntryMatch extends AnalysisMatch {
  settled: boolean
  picks: UserPick[]
}
