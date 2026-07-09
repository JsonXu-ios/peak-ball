import { defineStore } from 'pinia'
import { ref } from 'vue'
import expertApi from '@/api/expert'
import type { Expert } from '@/types/expert'

export const useExpertStore = defineStore('expert', () => {
  const experts = ref<Expert[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchExperts() {
    loading.value = true
    error.value = null
    try {
      const res = await expertApi.getExperts()
      experts.value = res.data ?? []
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : '获取专家列表失败'
    } finally {
      loading.value = false
    }
  }

  return { experts, loading, error, fetchExperts }
})
