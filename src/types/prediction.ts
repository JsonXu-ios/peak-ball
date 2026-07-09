/** 预测 */
export interface Prediction {
  id: number
  userId: number
  matchId: string
  pick: string
  odds: number
  stake: number
  profit: number
  status: string
  settledAt: string | null
  createdAt: string
  updatedAt: string
  match?: Record<string, unknown>
}

/** 用户统计（后端计算，非存库） */
export interface UserStats {
  totalPredictions: number
  won: number
  lost: number
  ongoing: number
  accuracy: number
  totalProfit: number
  profitChange: number
}

/** 排行榜条目 */
export interface LeaderboardEntry {
  id: number
  userId: number
  period: string
  periodKey: string
  points: number
  accuracy: number
  rank: number
  trend: string
  createdAt: string
  updatedAt: string
  user?: {
    id: number
    username: string
    nickname: string
    avatar: string
    country: string
  }
}
