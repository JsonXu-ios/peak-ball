import { defineStore } from 'pinia'
import { ref } from 'vue'
import newsApi from '@/api/news'
import type { News, TransferRumor } from '@/types/news'

export const useNewsStore = defineStore('news', () => {
  const newsList = ref<News[]>([])
  const transferRumors = ref<TransferRumor[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchNews(category?: string) {
    loading.value = true
    error.value = null
    try {
      const res = await newsApi.getNews(category)
      newsList.value = res.data ?? []
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : '获取新闻失败'
    } finally {
      loading.value = false
    }
  }

  async function fetchTransferRumors() {
    loading.value = true
    error.value = null
    try {
      const res = await newsApi.getTransferRumors()
      transferRumors.value = res.data ?? []
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : '获取转会传闻失败'
    } finally {
      loading.value = false
    }
  }

  return { newsList, transferRumors, loading, error, fetchNews, fetchTransferRumors }
})
