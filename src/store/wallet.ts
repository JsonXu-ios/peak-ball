import { defineStore } from 'pinia'
import { ref } from 'vue'
import walletApi from '@/api/wallet'
import type { WalletTransaction, Reward, WalletBalance } from '@/types/wallet'

export const useWalletStore = defineStore('wallet', () => {
  const balance = ref<WalletBalance>({ balance: 0, lifetimeEarned: 0 })
  const transactions = ref<WalletTransaction[]>([])
  const rewards = ref<Reward[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchBalance() {
    try {
      const res = await walletApi.getBalance()
      balance.value = res.data ?? { balance: 0, lifetimeEarned: 0 }
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : '获取余额失败'
    }
  }

  async function fetchTransactions(type?: string) {
    loading.value = true
    error.value = null
    try {
      const res = await walletApi.getTransactions(type)
      transactions.value = res.data ?? []
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : '获取交易记录失败'
    } finally {
      loading.value = false
    }
  }

  async function fetchRewards() {
    try {
      const res = await walletApi.getRewards()
      rewards.value = res.data ?? []
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : '获取奖品列表失败'
    }
  }

  return { balance, transactions, rewards, loading, error, fetchBalance, fetchTransactions, fetchRewards }
})
