import api from './request'
import type { AxiosResponse } from 'axios'
import type { DrawObservationQuery, DrawObservationsResponse } from '@/types/drawHistory'

interface ObservationCacheEntry {
  response?: AxiosResponse<DrawObservationsResponse>
  expires?: number
  pending?: Promise<AxiosResponse<DrawObservationsResponse>>
}
const observationCache = new Map<string, ObservationCacheEntry>()
const cacheTTL = 30000
const cacheLimit = 32

export function drawObservationCacheKey(params: DrawObservationQuery) {
  return JSON.stringify(Object.entries(params).filter(([key, value]) => key !== 'refresh' && value !== undefined).sort(([left], [right]) => left.localeCompare(right)))
}

export function clearDrawObservationCache() {
  observationCache.clear()
}

export function evictDrawObservationCache(params: DrawObservationQuery) {
  observationCache.delete(drawObservationCacheKey(params))
}

function copyResponse(response: AxiosResponse<DrawObservationsResponse>) {
  return { ...response, data: structuredClone(response.data) }
}

export function getDrawObservations(params: DrawObservationQuery) {
  const submitted = { ...params }
  const key = drawObservationCacheKey(submitted)
  if (submitted.refresh === 1) clearDrawObservationCache()
  const cached = observationCache.get(key)
  if (cached?.pending) return cached.pending.then(copyResponse)
  if (cached?.response && Number(cached.expires) > Date.now()) {
    observationCache.delete(key)
    observationCache.set(key, cached)
    return Promise.resolve(copyResponse(cached.response))
  }
  const entry: ObservationCacheEntry = {}
  observationCache.delete(key)
  observationCache.set(key, entry)
  while (observationCache.size > cacheLimit) observationCache.delete(observationCache.keys().next().value!)
  entry.pending = api.get<DrawObservationsResponse>('/admin/statistics/draw/observations', { params: submitted, timeout: 120000 }).then((response) => {
    if (observationCache.get(key) === entry) {
      if (response.data.dataVersion) {
        // Generated results live in Redis. Recheck their source revision on
        // each query; only coalesce requests currently in flight here.
        observationCache.delete(key)
      } else {
        entry.response = response
        entry.expires = Date.now() + cacheTTL
        entry.pending = undefined
      }
    }
    return response
  }, (error) => {
    if (observationCache.get(key) === entry) observationCache.delete(key)
    throw error
  })
  return entry.pending.then(copyResponse)
}
