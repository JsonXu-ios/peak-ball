/** 用户 */
export interface User {
  id: number
  username: string
  nickname: string
  avatar: string
  email: string
  badge: string
  balance: number
  joinedAt: string
  country: string
}

/** 关注的球队 */
export interface FollowedTeam {
  id: number
  userId: number
  teamId: number
  teamName: string
  teamLogo: string
  createdAt: string
}
