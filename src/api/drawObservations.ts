import apiClient from './request'
import type { DrawObservationDatesResponse, DrawObservationQuery, DrawObservationsResponse } from '@/types/drawHistory'
import { validateDrawObservationFilters } from '../../admin/src/utils/drawObservationFilters'
import { BEIJING_TIME_ZONE } from '../../admin/src/utils/date'

export async function getDrawObservations(params: DrawObservationQuery, signal?: AbortSignal) {
  const query = { ...params }
  const response = await apiClient.get<DrawObservationsResponse>('/draw/observations', { params: query, signal, timeout: 120000 })
  const data = response.data
  if (!Array.isArray(data.items) || data.mode !== (query.mode || 'today')
    || (query.date && (data.date !== query.date || data.items.some(row => row.date !== query.date)))
    || (data.scope !== undefined && data.scope !== (query.scope || 'all'))
    || (query.scope === 'sporttery' && (data.scope !== 'sporttery' || data.items.some(row => !row.jingcaiId?.trim())))
    || (data.scheduleTimezone && data.scheduleTimezone !== BEIJING_TIME_ZONE)) {
    throw new Error('返回比赛与所选日期或赛事范围不一致，请重新查询')
  }
  validateDrawObservationFilters(data, query)
  return response
}

export async function getDrawObservationDates(params: { month: string; mode: 'today' | 'history'; scope: 'all' | 'sporttery' }, signal?: AbortSignal) {
  const response = await apiClient.get<DrawObservationDatesResponse>('/draw/observations/dates', { params: { ...params }, signal, timeout: 120000 })
  const data = response.data
  if (!Array.isArray(data.dates) || data.month !== params.month || data.mode !== params.mode || data.scope !== params.scope
    || data.scheduleTimezone !== BEIJING_TIME_ZONE) {
    throw new Error('日期列表与所选赛事范围不一致，请重试')
  }
  return response
}

export interface DrawTeamStanding { rank: number | null; played: number | null }
export interface DrawStandings { home: DrawTeamStanding; away: DrawTeamStanding }

function record(value: unknown): Record<string, unknown> {
  if (typeof value === 'string') {
    try { value = JSON.parse(value) } catch { return {} }
  }
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function integer(value: unknown, minimum: number): number | null {
  if (typeof value !== 'number' && typeof value !== 'string') return null
  if (typeof value === 'string' && !value.trim()) return null
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) && parsed >= minimum ? parsed : null
}

export function parseDrawStandings(history: unknown): DrawStandings {
  const source = record(history)
  const summary = record(source.leagueSummary ?? source.league_summary)
  const side = (key: 'home' | 'guest'): DrawTeamStanding => {
    const all = record(record(summary[key]).all)
    return { rank: integer(all.rank, 1), played: integer(all.game, 0) }
  }
  return { home: side('home'), away: side('guest') }
}

interface CachedStanding { value?: DrawStandings; expires?: number; pending?: Promise<DrawStandings> }
const standingsCache = new Map<string, CachedStanding>()
const waiting: Array<() => void> = []
let active = 0

function acquireSlot(): Promise<() => void> {
  return new Promise(resolve => {
    const start = () => {
      active++
      resolve(() => { active--; waiting.shift()?.() })
    }
    if (active < 4) start()
    else waiting.push(start)
  })
}

export function clearDrawStandingsCache() {
  standingsCache.clear()
}

/** Public match-history endpoint, without admin credentials or data mutations. */
export function loadDrawStandings(matchId: string): Promise<DrawStandings> {
  const existing = standingsCache.get(matchId)
  if (existing?.pending) return existing.pending
  if (existing?.value && Number(existing.expires) > Date.now()) return Promise.resolve(existing.value)
  const entry: CachedStanding = {}
  standingsCache.set(matchId, entry)
  for (const [key, value] of standingsCache) {
    if (standingsCache.size <= 300) break
    if (!value.pending && key !== matchId) standingsCache.delete(key)
  }
  entry.pending = (async () => {
    const release = await acquireSlot()
    try {
      const { data } = await apiClient.get(`/match/${encodeURIComponent(matchId)}/history`, { timeout: 30000 })
      const value = parseDrawStandings(data)
      if (standingsCache.get(matchId) === entry) {
        entry.value = value
        entry.expires = Date.now() + 30000
        entry.pending = undefined
      }
      return value
    } catch (error) {
      if (standingsCache.get(matchId) === entry) standingsCache.delete(matchId)
      throw error
    } finally { release() }
  })()
  return entry.pending
}
