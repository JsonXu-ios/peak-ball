import apiClient from './request'
import type { Expert } from '@/types/expert'

export default {
  /** 获取专家列表 */
  getExperts() {
    return apiClient.get<Expert[]>('/experts')
  },
}
