import { defineStore } from 'pinia'
import { ref } from 'vue'
import userApi from '@/api/user'
import type { User, FollowedTeam } from '@/types/user'

export const useUserStore = defineStore('user', () => {
  const user = ref<User | null>(null)
  const followedTeams = ref<FollowedTeam[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchUser() {
    loading.value = true
    error.value = null
    try {
      const res = await userApi.getUser()
      user.value = res.data ?? null
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : '获取用户信息失败'
    } finally {
      loading.value = false
    }
  }

  async function fetchFollowedTeams() {
    try {
      const res = await userApi.getFollowedTeams()
      followedTeams.value = res.data ?? []
    } catch (e: unknown) {
      console.error('获取关注球队失败', e)
    }
  }

  return { user, followedTeams, loading, error, fetchUser, fetchFollowedTeams }
})
