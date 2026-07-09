import apiClient from './request'
import type { Notification } from '@/types/notification'

export default {
  /** 获取通知列表 */
  getNotifications(type?: string) {
    const params = type ? { type } : {}
    return apiClient.get<Notification[]>('/notifications', { params })
  },

  /** 获取未读通知数 */
  getUnreadCount() {
    return apiClient.get<{ count: number }>('/notifications/unread-count')
  },

  /** 标记单条通知已读 */
  markAsRead(id: number) {
    return apiClient.put(`/notifications/${id}/read`)
  },

  /** 全部标为已读 */
  markAllAsRead() {
    return apiClient.put('/notifications/read-all')
  },
}
