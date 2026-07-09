import { defineStore } from 'pinia'
import { ref } from 'vue'
import matchApi from '@/api/match'
import type { Match, MatchHistory, MatchOddsEuro, MatchOddsPankou } from '@/types/match'

function getLocalDateString(date = new Date()): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

export const useMatchStore = defineStore('match', () => {
  /* ---------- state ---------- */
  const matches = ref<Match[]>([])
  const currentMatch = ref<Match | null>(null)
  const currentHistory = ref<MatchHistory | null>(null)
  const currentOddsEuro = ref<MatchOddsEuro | null>(null)
  const currentOddsPankou = ref<MatchOddsPankou | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)
  const selectedDate = ref<string>(getLocalDateString())

  /* ---------- actions ---------- */

  /** 获取比赛列表 */
  async function fetchMatches(date?: string) {
    loading.value = true
    error.value = null
    try {
      const res = await matchApi.getMatches(date ?? selectedDate.value)
      matches.value = res.data ?? []
    } catch (e: any) {
      error.value = e.message ?? '获取比赛列表失败'
    } finally {
      loading.value = false
    }
  }

  /** 获取单场比赛详情 */
  async function fetchMatchDetail(id: string) {
    loading.value = true
    error.value = null
    try {
      const res = await matchApi.getMatchDetail(id)
      currentMatch.value = res.data
    } catch (e: any) {
      error.value = e.message ?? '获取比赛详情失败'
    } finally {
      loading.value = false
    }
  }

  /** 获取历史对阵数据 */
  async function fetchHistory(id: string) {
    try {
      const res = await matchApi.getMatchHistory(id)
      currentHistory.value = res.data
    } catch (e: any) {
      console.error('获取历史数据失败', e)
    }
  }

  /** 获取欧赔数据 */
  async function fetchOddsEuro(id: string) {
    try {
      const res = await matchApi.getMatchOddsEuro(id)
      currentOddsEuro.value = res.data
    } catch (e: any) {
      console.error('获取欧赔数据失败', e)
    }
  }

  /** 获取亚盘数据 */
  async function fetchOddsPankou(id: string) {
    try {
      const res = await matchApi.getMatchOddsPankou(id)
      currentOddsPankou.value = res.data
    } catch (e: any) {
      console.error('获取亚盘数据失败', e)
    }
  }

  /** 获取比赛全部数据（详情 + 历史 + 赔率） */
  async function fetchMatchAll(id: string) {
    loading.value = true
    error.value = null
    try {
      await Promise.all([
        fetchMatchDetail(id),
        fetchHistory(id),
        fetchOddsEuro(id),
        fetchOddsPankou(id),
      ])
    } catch (e: any) {
      error.value = e.message ?? '获取数据失败'
    } finally {
      loading.value = false
    }
  }

  /** 重置当前比赛数据 */
  function resetCurrent() {
    currentMatch.value = null
    currentHistory.value = null
    currentOddsEuro.value = null
    currentOddsPankou.value = null
  }

  return {
    matches,
    currentMatch,
    currentHistory,
    currentOddsEuro,
    currentOddsPankou,
    loading,
    error,
    selectedDate,
    fetchMatches,
    fetchMatchDetail,
    fetchHistory,
    fetchOddsEuro,
    fetchOddsPankou,
    fetchMatchAll,
    resetCurrent,
  }
})
