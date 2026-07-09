import apiClient from './request'
import type { SearchResult, SearchHistoryItem } from '@/types/search'

export default {
  /** 综合搜索 */
  search(q: string) {
    return apiClient.get<SearchResult>('/search', { params: { q } })
  },

  /** 获取搜索历史 */
  getHistory() {
    return apiClient.get<SearchHistoryItem[]>('/search/history')
  },

  /** 保存搜索历史 */
  saveHistory(query: string) {
    return apiClient.post('/search/history', { query })
  },

  /** 清空搜索历史 */
  clearHistory() {
    return apiClient.delete('/search/history')
  },
}
