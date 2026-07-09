import apiClient from './request'
import type { Match, MatchHistory, MatchOddsEuro, MatchOddsPankou } from '@/types/match'

export default {
  /** 获取比赛列表，可按日期筛选 */
  getMatches(date?: string) {
    const params = date ? { date } : {}
    return apiClient.get<Match[]>('/matches', { params })
  },

  /** 获取单场比赛详情 */
  getMatchDetail(id: string) {
    return apiClient.get<Match>(`/match/${id}`)
  },

  /** 获取历史交锋数据 */
  getMatchHistory(id: string) {
    return apiClient.get<MatchHistory>(`/match/${id}/history`)
  },

  /** 获取欧赔数据 */
  getMatchOddsEuro(id: string) {
    return apiClient.get<MatchOddsEuro>(`/match/${id}/odds/euro`)
  },

  /** 获取亚盘/大小球数据 */
  getMatchOddsPankou(id: string) {
    return apiClient.get<MatchOddsPankou>(`/match/${id}/odds/pankou`)
  },
}
