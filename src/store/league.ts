import { defineStore } from 'pinia'
import { ref } from 'vue'
import leagueApi from '@/api/league'
import type { LeagueStanding, TopScorer } from '@/types/league'

export const useLeagueStore = defineStore('league', () => {
  const standings = ref<LeagueStanding[]>([])
  const topScorers = ref<TopScorer[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchStandings(leagueId?: number, season?: string) {
    loading.value = true
    error.value = null
    try {
      const res = await leagueApi.getStandings(leagueId, season)
      standings.value = res.data ?? []
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : '获取积分榜失败'
    } finally {
      loading.value = false
    }
  }

  async function fetchTopScorers(leagueId?: number, season?: string) {
    try {
      const res = await leagueApi.getTopScorers(leagueId, season)
      topScorers.value = res.data ?? []
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : '获取射手榜失败'
    }
  }

  return { standings, topScorers, loading, error, fetchStandings, fetchTopScorers }
})
