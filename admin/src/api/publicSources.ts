import api from './request'
import type { DrawObservation } from '@/types/drawHistory'

export interface PublicModels {
  version?: string; state: string; reason?: string; warnings?: string[]
  observation?: DrawObservation; snapshotIds?: Record<string, number>
}

export interface PublicMatch {
  id: string; business_date: string; official_id: string; home: string; away: string
  kickoff: string | null; lottery_number: string | null; zgzcw_id: string | null
  updated_at: string; parts: string[]; models?: PublicModels
}
export interface PublicJob {
  id: number; status: string; config: { date: string; limit: number }; created_at: string
  processed: number; errors: { stage: string; message: string }[]; message: string
}
export interface PublicQuote {
  sourceLabel: string; company?: string; opening: string[]; latest: string[]
  probabilities: string[]; kelly: string[]; openingKelly: string[] | null; openingKellyTime?: string; returnRatio: string
}
export interface PublicOutcome {
  outcome: string; odds: number | null; support: number | null; probability: number | null
  profitRate: number | null; heatDifference: number | null
}
export interface PublicDetail {
  match: PublicMatch
  parts: {
    bonus?: { spf?: { latest: number[]; updatedAt: string }; rqspf?: { latest: number[]; goal: string; updatedAt: string }; drawScores?: Record<string, number | null>; totalGoals?: (number | null)[]; history: Record<string, Record<string, unknown>[]> }
    euro?: PublicQuote[]; asian?: PublicQuote[]; totals?: PublicQuote[]
    history?: { text: string; tables: string[][][] }
  }
  provenance: Record<string, { id: number; source: string; url: string; received_at: string; source_updated_at: string | null; sha256: string }>
  analysis: Record<string, { rows: PublicOutcome[]; source: string; receivedAt: string; latestQuote?: { latest: number[]; updatedAt: string } }>
  cutoff: { rule: string; time: string | null; stopped: boolean | null }
  missing: string[]
  models?: PublicModels
}
export const publicSourceOverview = (date: string) => api.get<{ matches: PublicMatch[]; jobs: PublicJob[]; storage: string }>('/admin/public-sources/overview', { params: { date } })
export const publicSourceDetail = (id: string) => api.get<PublicDetail>('/admin/public-sources/match', { params: { id } })
export const publicSourceCollect = (payload: { date: string; limit: number; officialId?: string; zgzcwId?: string }) => api.post('/admin/public-sources/collect', payload)
export const publicSourceMap = (id: string, zgzcwId: string) => api.put('/admin/public-sources/mapping', { id, zgzcwId })
export const publicSourceCompanyMap = (label: string, company: string) => api.put('/admin/public-sources/company-mapping', { label, company })
export const publicSourceRaw = (id: string, snapshotId: number) => api.get<{ payload: string; source: string; received_at: string; sha256: string }>('/admin/public-sources/raw', { params: { id, snapshotId } })
