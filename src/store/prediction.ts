import { defineStore } from 'pinia'
import { ref } from 'vue'
import predictionApi from '@/api/prediction'
import type { Prediction, UserStats, LeaderboardEntry } from '@/types/prediction'

export const usePredictionStore = defineStore('prediction', () => {
  const predictions = ref<Prediction[]>([])
  const stats = ref<UserStats | null>(null)
  const leaderboard = ref<LeaderboardEntry[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchPredictions(status?: string) {
    loading.value = true
    error.value = null
    try {
      const res = await predictionApi.getPredictions(status)
      predictions.value = res.data ?? []
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : '获取预测列表失败'
    } finally {
      loading.value = false
    }
  }

  async function fetchStats() {
    try {
      const res = await predictionApi.getStats()
      stats.value = res.data ?? null
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : '获取统计失败'
    }
  }

  async function fetchLeaderboard(period?: string) {
    loading.value = true
    error.value = null
    try {
      const res = await predictionApi.getLeaderboard(period)
      leaderboard.value = res.data ?? []
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : '获取排行榜失败'
    } finally {
      loading.value = false
    }
  }

  return { predictions, stats, leaderboard, loading, error, fetchPredictions, fetchStats, fetchLeaderboard }
})
