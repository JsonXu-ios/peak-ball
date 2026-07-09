import apiClient from './request'
import type { LeagueStanding, TopScorer } from '@/types/league'

export default {
  /** 获取联赛积分榜 */
  getStandings(leagueId?: number, season?: string) {
    const params: Record<string, string | number> = {}
    if (leagueId) params.league_id = leagueId
    if (season) params.season = season
    return apiClient.get<LeagueStanding[]>('/league/standings', { params })
  },

  /** 获取射手榜 */
  getTopScorers(leagueId?: number, season?: string) {
    const params: Record<string, string | number> = {}
    if (leagueId) params.league_id = leagueId
    if (season) params.season = season
    return apiClient.get<TopScorer[]>('/league/top-scorers', { params })
  },
}
