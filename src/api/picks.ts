import apiClient from './request'
import type { PickEntryMatch, UserPick } from '@/types/analysis'

export interface PickEntryParams {
  date?: string
  scope?: 'sporttery' | 'all'
}

export interface SavePickPayload {
  matchId: string
  market: 'spf' | 'rqspf' | 'dxq' | 'score'
  pick: string
  line?: number | null
  direction?: 'follow' | 'fade' | 'self'
  confidence?: number
  note?: string
  source?: string
}

export default {
  /** 补录页比赛列表：仅完赛，后端已隐藏真实比分 */
  getPickEntryMatches(params?: PickEntryParams) {
    return apiClient.get<PickEntryMatch[]>('/picks/entry', { params: params || {} })
  },

  /** 保存/覆盖一条选择（按 matchId+market 唯一） */
  savePick(payload: SavePickPayload) {
    return apiClient.post<UserPick>('/picks', payload)
  },

  /** 删除一条选择 */
  deletePick(id: number) {
    return apiClient.delete<{ deleted: boolean }>(`/picks/${id}`)
  },

  /** 全部选择记录 */
  listPicks(matchId?: string) {
    return apiClient.get<UserPick[]>('/picks', { params: matchId ? { match_id: matchId } : {} })
  },
}
