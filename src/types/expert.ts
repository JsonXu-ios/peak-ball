/** 专家 */
export interface Expert {
  id: number
  userId: number
  name: string
  avatar: string
  specialty: string
  accuracy: number
  streak: number
  followers: number
  verified: boolean
  createdAt: string
  updatedAt: string
}
