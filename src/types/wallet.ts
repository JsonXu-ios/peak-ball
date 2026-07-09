/** 钱包交易 */
export interface WalletTransaction {
  id: number
  userId: number
  type: string
  amount: number
  description: string
  detail: string
  createdAt: string
}

/** 可兑换奖品 */
export interface Reward {
  id: number
  name: string
  description: string
  icon: string
  iconColor: string
  cost: number
  isActive: boolean
  createdAt: string
  updatedAt: string
}

/** 钱包余额摘要 */
export interface WalletBalance {
  balance: number
  lifetimeEarned: number
}
