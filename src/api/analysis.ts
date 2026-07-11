import apiClient from './request'
import type { AnalysisMatch } from '@/types/analysis'

export interface AnalysisQueryParams {
  date?: string
  scope?: 'sporttery' | 'all'
}

export interface AnalysisRuleSnapshot {
  version: number
  updatedAt?: string
  sourceRange?: {
    startDate?: string
    endDate?: string
  }
  total?: number
  commonRows?: Array<{
    label: string
    sample: number
    rules: Array<{
      value: string
      sample: number
      bothCorrect: number
      rate: number
    }>
  }>
  notes?: string
}

export default {
  getAnalysisMatches(params?: string | AnalysisQueryParams) {
    const query = typeof params === 'string' ? { date: params } : (params || {})
    return apiClient.get<AnalysisMatch[]>('/analysis/matches', { params: query })
  },

  getAnalysisRuleSnapshot() {
    return apiClient.get<AnalysisRuleSnapshot>('/analysis/rule-snapshot')
  },

  getAnalysisDetail(matchId: string, params?: AnalysisQueryParams) {
    return apiClient.get<AnalysisMatch>(`/analysis/match/${matchId}`, { params: params || {} })
  },
}
