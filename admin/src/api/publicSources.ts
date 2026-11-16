import api from './request'
import type { DrawObservation } from '@/types/drawHistory'

export interface PublicModels {
  version?: string; state: string; reason?: string; warnings?: string[]
  observation?: DrawObservation; snapshotIds?: Record<string, number>
  marketContext?: PublicMarketContext
  inputSources?: PublicModelInputSource[]
}

export interface PublicWaterValues {
  line: number; leftWater: number; rightWater: number; waterGap: number
  overWater?: number; underWater?: number; companyCount?: number; companies?: string[]
}
export interface PublicMarketContext {
  version: string; usedByFrozenModels: boolean; note: string; averaging: string; asianSign?: string
  markets: { key: 'asian' | 'totals'; leftMeaning: string; rightMeaning: string; phases: {
    phase: 'opening' | 'latest'; macau: PublicWaterValues | null; averages: PublicWaterValues[]
    macauVsAverage: { leftWaterDifference: number; rightWaterDifference: number; waterGapDifference: number; companyCount: number } | null
  }[] }[]
  features: Record<string, number>
}
export interface PublicModelInputSource {
  company: string; source: string; sourceCompanyId: string; sourceMatchId: string; receivedAt?: string
  opening: string[]; latest: string[]; kelly: string[] | null; openingKelly: string[] | null; reason: string
  openingSource?: string; openingAt?: string
}

export interface PublicAcquisition {
  mode?: 'api' | 'api_with_page_fields' | 'page' | 'legacy'
  sourcePolicy?: string
  interfaces?: { url: string; method: string; transport: string; form?: Record<string, string | number> | null; receivedAt?: string; fields?: string[] }[]
  pageFields?: { fields: string[]; url: string; reason: string }[]
  fieldWarnings?: { company: string; message: string; apiPrices: string[]; pagePrices: string[]; fields: string[] }[]
}

export interface PublicMatch {
  id: string; business_date: string; official_id: string; home: string; away: string
  kickoff: string | null; lottery_number: string | null; zgzcw_id: string | null
  updated_at: string; parts: string[]; missing?: string[]; models?: PublicModels; analysis?: Record<string, PublicVoteMarket>
  acquisition?: Record<string, PublicAcquisition>
  result?: PublicResult
}
export interface PublicResult {
  state: 'finished' | 'pending' | 'unverified' | 'unavailable' | 'cancelled' | 'postponed'
  homeScore: number | null; awayScore: number | null; regulationSettled: boolean; reason: string
  source?: string; sourceMatchId?: string; receivedAt?: string; snapshotId?: number; url?: string
  lastAttemptError?: string
}
export interface PublicJob {
  id: number; status: string; config: { date: string; limit?: number; kind?: 'results'; officialId?: string }; created_at: string
  processed: number; errors: { stage: string; message: string }[]; message: string
}
export interface PublicQuote {
  sourceLabel: string; company?: string; opening: string[]; latest: string[]
  probabilities: string[] | null; kelly: string[] | null; openingKelly: string[] | null; openingKellyTime?: string; returnRatio: string | null
  openingKellyPrices?: string[]; openingKellyNote?: string; fieldWarnings?: string[]
}
export interface PublicOutcome {
  outcome: string; odds: number | null; support: number | null; probability: number | null
  profitRate: number | null; profitAmount: number | null; heatDifference: number | null
}
export interface PublicVoteMarket {
  rows: PublicOutcome[]; totalStake: number; source: string; receivedAt: string
  basis?: 'paired_vote_odds'
  goal?: string | number | null
  inputSource?: { snapshotId: number; source: string; url: string; mode: PublicAcquisition['mode'] }
  latestQuote?: { latest: number[]; updatedAt: string }
}
export interface PublicDetail {
  match: PublicMatch
  result?: PublicResult
  parts: {
    bonus?: { spf?: { latest: number[]; updatedAt: string }; rqspf?: { latest: number[]; goal: string; updatedAt: string }; drawScores?: Record<string, number | null>; totalGoals?: (number | null)[]; history: Record<string, Record<string, unknown>[]> }
    euro?: PublicQuote[]; asian?: PublicQuote[]; totals?: PublicQuote[]
    history?: { text?: string; tables: string[][][]; source?: string }
  }
  provenance: Record<string, { id: number; source: string; url: string; received_at: string; source_updated_at: string | null; sha256: string; acquisition?: PublicAcquisition }>
  analysis: Record<string, PublicVoteMarket>
  cutoff: { rule: string; time: string | null; stopped: boolean | null }
  missing: string[]
  models?: PublicModels
}
export const publicSourceOverview = (date: string) => api.get<{ matches: PublicMatch[]; jobs: PublicJob[]; storage: string; network?: { proxyCount: number; rotationConfigured: boolean; intervalSeconds?: number; jitterSeconds?: number; forwardedForRandomized?: boolean; error?: string } }>('/admin/public-sources/overview', { params: { date } })
export const publicSourceDetail = (id: string) => api.get<PublicDetail>('/admin/public-sources/match', { params: { id } })
export const publicSourceCollect = (payload: { date: string; limit: number; officialId?: string; zgzcwId?: string }) => api.post('/admin/public-sources/collect', payload)
export const publicSourceUpdateResults = (payload: { date: string; officialId?: string }) => api.post<{ id: number; status: string }>('/admin/public-sources/results', payload)
export const publicSourceResultRaw = (id: string, snapshotId: number) => api.get<{ payload: string; source: string; received_at: string }>('/admin/public-sources/results/raw', { params: { id, snapshotId } })
export const publicSourceMap = (id: string, zgzcwId: string) => api.put('/admin/public-sources/mapping', { id, zgzcwId })
export const publicSourceCompanyMap = (label: string, company: string) => api.put('/admin/public-sources/company-mapping', { label, company })
export const publicSourceRaw = (id: string, snapshotId: number) => api.get<{ payload: string; source: string; received_at: string; sha256: string }>('/admin/public-sources/raw', { params: { id, snapshotId } })
