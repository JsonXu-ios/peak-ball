import api from './request'
import type { ResearchAuditPage, ResearchEvaluation, ResearchFeatureDefinition, ResearchOverview, ResearchPage, ResearchSyncStatus } from '@/types/researchStore'

const base = '/admin/statistics/research'

export function getResearchOverview() {
  return api.get<ResearchOverview>(`${base}/overview`)
}

export function getResearchSyncStatus() {
  return api.get<ResearchSyncStatus>(`${base}/sync/status`)
}

/** Synchronization is an explicit user action; reading the page never starts it. */
export function syncResearchStore() {
  return api.post<ResearchSyncStatus>(`${base}/sync`, {})
}

export function getResearchFeatures(params: { version?: string; search?: string; page: number; page_size: number }) {
  return api.get<ResearchPage<ResearchFeatureDefinition>>(`${base}/features`, { params })
}

export function getResearchAuditIssues(params: { severity?: string; page: number; page_size: number }) {
  return api.get<ResearchAuditPage>(`${base}/audit/issues`, { params })
}

export function getResearchEvaluations() {
  return api.get<{ items: ResearchEvaluation[] }>(`${base}/evaluations`)
}
