/** 新闻文章 */
export interface News {
  id: number
  title: string
  summary: string
  content: string
  imageUrl: string
  category: string
  source: string
  club: string
  isHot: boolean
  createdAt: string
  updatedAt: string
}

/** 转会传闻 */
export interface TransferRumor {
  id: number
  playerName: string
  fromClub: string
  toClub: string
  fromClubLogo: string
  toClubLogo: string
  value: string
  trustLevel: number
  tier: string
  status: string
  createdAt: string
  updatedAt: string
}
