import { defineStore } from 'pinia'
import { ref } from 'vue'
import searchApi from '@/api/search'
import type { SearchResult, SearchHistoryItem } from '@/types/search'

export const useSearchStore = defineStore('search', () => {
  const result = ref<SearchResult>({ teams: [], experts: [], news: [] })
  const history = ref<SearchHistoryItem[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function search(q: string) {
    if (!q.trim()) return
    loading.value = true
    error.value = null
    try {
      const res = await searchApi.search(q)
      result.value = res.data ?? { teams: [], experts: [], news: [] }
      // 搜索成功后保存历史
      await searchApi.saveHistory(q)
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : '搜索失败'
    } finally {
      loading.value = false
    }
  }

  async function fetchHistory() {
    try {
      const res = await searchApi.getHistory()
      history.value = res.data ?? []
    } catch {
      // 静默失败
    }
  }

  async function clearHistory() {
    try {
      await searchApi.clearHistory()
      history.value = []
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : '清空失败'
    }
  }

  return { result, history, loading, error, search, fetchHistory, clearHistory }
})
