import { defineStore } from 'pinia'
import { ref } from 'vue'
import notificationApi from '@/api/notification'
import type { Notification } from '@/types/notification'

export const useNotificationStore = defineStore('notification', () => {
  const notifications = ref<Notification[]>([])
  const unreadCount = ref(0)
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchNotifications(type?: string) {
    loading.value = true
    error.value = null
    try {
      const res = await notificationApi.getNotifications(type)
      notifications.value = res.data ?? []
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : '获取通知失败'
    } finally {
      loading.value = false
    }
  }

  async function fetchUnreadCount() {
    try {
      const res = await notificationApi.getUnreadCount()
      unreadCount.value = res.data?.count ?? 0
    } catch {
      // 静默失败
    }
  }

  async function markAsRead(id: number) {
    try {
      await notificationApi.markAsRead(id)
      const item = notifications.value.find((n) => n.id === id)
      if (item) {
        item.isRead = true
        unreadCount.value = Math.max(0, unreadCount.value - 1)
      }
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : '标记失败'
    }
  }

  async function markAllAsRead() {
    try {
      await notificationApi.markAllAsRead()
      notifications.value.forEach((n) => (n.isRead = true))
      unreadCount.value = 0
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : '标记失败'
    }
  }

  return {
    notifications,
    unreadCount,
    loading,
    error,
    fetchNotifications,
    fetchUnreadCount,
    markAsRead,
    markAllAsRead,
  }
})
