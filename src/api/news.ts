import apiClient from './request'
import type { News, TransferRumor } from '@/types/news'

export default {
  /** 获取新闻列表 */
  getNews(category?: string) {
    const params = category ? { category } : {}
    return apiClient.get<News[]>('/news', { params })
  },

  /** 获取转会传闻列表 */
  getTransferRumors() {
    return apiClient.get<TransferRumor[]>('/transfers')
  },
}
