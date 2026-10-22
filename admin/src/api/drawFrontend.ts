import type { DrawCompanyQuote, DrawFrontendAnalysis } from '@/types/drawHistory'
import { parseDrawCompanyQuotes, parseDrawFrontendAnalysis, parseDrawRawJSON } from '@/utils/drawFrontend'

const ttl = 30000
interface Cached<T> { value?: T; expires?: number; pending?: Promise<T> }
const analysisCache = new Map<string, Cached<Map<string, DrawFrontendAnalysis>>>()
const quoteCache = new Map<string, Cached<DrawCompanyQuote[]>>()

/** Legacy supplement is opt-in to local development; it never uses admin credentials. */
export function drawFrontendBaseURL(): string {
  if (!import.meta.env.DEV || typeof window === 'undefined' || !['localhost', '127.0.0.1'].includes(window.location.hostname)) {
    throw new Error('当前环境无法访问旧接口的前台补充数据，请使用本机开发页面或加载已更新的分析服务')
  }
  const configured = String(import.meta.env.VITE_ANALYSIS_API_BASE_URL || '').trim()
  const base = new URL(configured || `${window.location.protocol}//${window.location.hostname}:28080/api`)
  if (!['http:', 'https:'].includes(base.protocol) || !['localhost', '127.0.0.1'].includes(base.hostname) || base.username || base.password || base.search || base.hash) {
    throw new Error('前台补充接口地址无效：仅支持本机开发环境的本地接口')
  }
  return base.toString().replace(/\/$/, '')
}

async function request(path: string, raw = false): Promise<unknown> {
  const base = drawFrontendBaseURL()
  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), 120000)
  try {
    const response = await fetch(base + path, { method: 'GET', headers: { Accept: 'application/json' }, credentials: 'omit', redirect: 'error', signal: controller.signal })
    if (raw && response.status === 404) return null
    if (!response.ok) throw new Error(`前台补充数据读取失败（HTTP ${response.status}）`)
    const body = await response.text()
    return raw ? parseDrawRawJSON(body) : JSON.parse(body)
  } catch (error) {
    if (error instanceof Error && error.name === 'AbortError') throw new Error('读取前台补充数据超时，请重试')
    if (error instanceof TypeError) throw new Error('无法连接现有前台数据接口，请检查连接后重试')
    throw error
  } finally {
    window.clearTimeout(timeout)
  }
}

function cached<T>(cache: Map<string, Cached<T>>, key: string, loader: () => Promise<T>, refresh: boolean): Promise<T> {
  if (refresh) cache.delete(key)
  const existing = cache.get(key)
  if (existing?.pending) return existing.pending
  if (existing?.value !== undefined && Number(existing.expires) > Date.now()) return Promise.resolve(existing.value)
  const entry: Cached<T> = {}
  cache.set(key, entry)
  entry.pending = loader().then((value) => {
    if (cache.get(key) === entry) { entry.value = value; entry.expires = Date.now() + ttl; entry.pending = undefined }
    return value
  }, (error) => {
    if (cache.get(key) === entry) cache.delete(key)
    throw error
  })
  return entry.pending
}

export function loadDrawFrontendAnalysis(date: string, refresh = false) {
  return cached(analysisCache, date, async () => parseDrawFrontendAnalysis(await request(`/analysis/matches?date=${encodeURIComponent(date)}&scope=all`), date), refresh)
}

export function loadDrawCompanyQuotes(matchId: string, refresh = false) {
  return cached(quoteCache, matchId, async () => {
    const [euro, pankou] = await Promise.all([request(`/match/${encodeURIComponent(matchId)}/odds/euro`, true), request(`/match/${encodeURIComponent(matchId)}/odds/pankou`, true)])
    return parseDrawCompanyQuotes(matchId, euro, pankou)
  }, refresh)
}

export function clearDrawFrontendCache() {
  analysisCache.clear()
  quoteCache.clear()
}
