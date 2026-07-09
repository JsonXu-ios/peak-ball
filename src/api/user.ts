import apiClient from './request'
import type { User, FollowedTeam } from '@/types/user'

export default {
  /** 获取当前用户信息 */
  getUser() {
    return apiClient.get<User>('/user')
  },

  /** 获取关注的球队 */
  getFollowedTeams() {
    return apiClient.get<FollowedTeam[]>('/user/followed-teams')
  },
}
