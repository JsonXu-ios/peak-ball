import apiClient from './request'
import type { Prediction, UserStats, LeaderboardEntry } from '@/types/prediction'

export default {
  /** 获取用户预测列表 */
  getPredictions(status?: string) {
    return apiClient.get<Prediction[]>('/predictions', { params: status ? { status } : {} })
  },

  /** 获取用户统计 */
  getStats() {
    return apiClient.get<UserStats>('/predictions/stats')
  },

  /** 创建新预测 */
  createPrediction(data: { matchId: string; pick: string; odds: number; stake: number }) {
    return apiClient.post<Prediction>('/predictions', data)
  },

  /** 获取排行榜 */
  getLeaderboard(period?: string) {
    return apiClient.get<LeaderboardEntry[]>('/leaderboard', { params: period ? { period } : {} })
  },
}
