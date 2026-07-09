/** 通知 */
export interface Notification {
  id: number
  userId: number
  type: string
  title: string
  message: string
  icon: string
  matchId: string
  isRead: boolean
  readAt: string | null
  createdAt: string
  updatedAt: string
}
