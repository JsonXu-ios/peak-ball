import type { LeagueStanding } from './league'
import type { News } from './news'

/** 专家（搜索结果用） */
export interface SearchExpert {
  id: number
  name: string
  avatar: string
  specialty: string
  accuracy: number
  streak: number
  followers: number
  verified: boolean
}

/** 搜索综合结果 */
export interface SearchResult {
  teams: LeagueStanding[]
  experts: SearchExpert[]
  news: News[]
}

/** 搜索历史条目 */
export interface SearchHistoryItem {
  id: number
  userId: number
  query: string
  createdAt: string
}
