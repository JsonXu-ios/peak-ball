import apiClient from './request'
import type { WalletTransaction, Reward, WalletBalance } from '@/types/wallet'

export default {
  /** 获取钱包余额 */
  getBalance() {
    return apiClient.get<WalletBalance>('/wallet/balance')
  },

  /** 获取交易记录 */
  getTransactions(type?: string) {
    const params = type ? { type } : {}
    return apiClient.get<WalletTransaction[]>('/wallet/transactions', { params })
  },

  /** 获取可兑换奖品列表 */
  getRewards() {
    return apiClient.get<Reward[]>('/wallet/rewards')
  },
}
