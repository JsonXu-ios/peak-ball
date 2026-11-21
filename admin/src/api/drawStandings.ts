import { getCrawlerMatchDetail } from '@/api'

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
  const number = Number(value)
  return Number.isSafeInteger(number) && number >= minimum ? number : null
}

/** The history summary already identifies the fixture's home/away teams. */
export function parseDrawStandings(history: unknown): DrawStandings {
  const source = record(history)
  const summary = record(source.league_summary ?? source.leagueSummary)
  const side = (key: 'home' | 'guest'): DrawTeamStanding => {
    const all = record(record(summary[key]).all)
    return { rank: integer(all.rank, 1), played: integer(all.game, 0) }
  }
  return { home: side('home'), away: side('guest') }
}

interface CachedStanding { value?: DrawStandings; expires?: number; pending?: Promise<DrawStandings> }
const cache = new Map<string, CachedStanding>()
const ttl = 30000
const cacheLimit = 300
const concurrency = 4
let active = 0
const waiting: Array<() => void> = []

function acquireSlot(): Promise<() => void> {
  return new Promise((resolve) => {
    const start = () => {
      active++
      resolve(() => { active--; waiting.shift()?.() })
    }
    if (active < concurrency) start()
    else waiting.push(start)
  })
}

export function clearDrawStandingsCache() {
  cache.clear()
}

/** Reuse the authenticated, same-origin history detail endpoint in every deployment. */
export function loadDrawStandings(matchId: string): Promise<DrawStandings> {
  const existing = cache.get(matchId)
  if (existing?.pending) return existing.pending
  if (existing?.value && Number(existing.expires) > Date.now()) return Promise.resolve(existing.value)
  const entry: CachedStanding = {}
  cache.set(matchId, entry)
  for (const [key, value] of cache) {
    if (cache.size <= cacheLimit) break
    if (!value.pending && key !== matchId) cache.delete(key)
  }
  entry.pending = (async () => {
    const release = await acquireSlot()
    try {
      const { data } = await getCrawlerMatchDetail(matchId)
      const value = parseDrawStandings(record(data).history)
      if (cache.get(matchId) === entry) {
        entry.value = value
        entry.expires = Date.now() + ttl
        entry.pending = undefined
      }
      return value
    } catch (error) {
      if (cache.get(matchId) === entry) cache.delete(matchId)
      throw error
    } finally {
      release()
    }
  })()
  return entry.pending
}
