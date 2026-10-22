import type { AnalysisMatch } from '../../../src/types/analysis'
import type { DrawCompanyQuote, DrawFrontendAnalysis, DrawObservedEuroOdds, DrawObservedLine } from '@/types/drawHistory'

type RecordValue = Record<string, unknown>
type CompanyKey = DrawCompanyQuote['key']
const companies: Array<{ key: CompanyKey; name: string }> = [{ key: 'sporttery', name: '竞彩' }, { key: 'william', name: '威廉' }, { key: 'bet365', name: 'Bet365' }]
const own = (record: RecordValue, key: string) => Object.prototype.hasOwnProperty.call(record, key)

/** Signed, per-outcome price differences. Missing/unopened prices are not zero. */
export function drawWilliamSportteryDifferences(quotes: DrawCompanyQuote[] | null | undefined) {
  const william = quotes?.find((quote) => quote.key === 'william')?.euroOdds.latest
  const sporttery = quotes?.find((quote) => quote.key === 'sporttery')?.euroOdds.latest
  const price = (raw: string | null | undefined) => {
    const value = raw == null || raw.trim() === '' ? NaN : Number(raw)
    return Number.isFinite(value) && value > 0 ? value : null
  }
  return ['胜', '平', '负'].map((outcome, index) => {
    const w = price(william?.[index]), s = price(sporttery?.[index])
    return { outcome, william: w, sporttery: s, difference: w === null || s === null ? null : Math.round((w - s) * 100) / 100 }
  })
}

/** Keep JSON numeric quote text (including 0.00) without changing quoted strings. */
export function parseDrawRawJSON(text: string): unknown {
  return JSON.parse(text.replace(/"(?:\\[\s\S]|[^"\\])*"|(-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/g, (token, numeric: string | undefined) => numeric === undefined ? token : JSON.stringify(numeric)))
}

function decoded(value: unknown): unknown {
  if (typeof value !== 'string') return value
  const text = value.trim()
  if (!text.startsWith('{') && !text.startsWith('[')) return value
  try { return parseDrawRawJSON(text) } catch { return null }
}

function record(value: unknown): RecordValue {
  const parsed = decoded(value)
  return parsed !== null && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed as RecordValue : {}
}

function field(value: RecordValue, camel: string, snake?: string) {
  return own(value, camel) ? value[camel] : snake ? value[snake] : undefined
}

function text(value: unknown): string | null {
  if (typeof value !== 'string' && typeof value !== 'number') return null
  if (typeof value === 'number' && !Number.isFinite(value)) return null
  const result = String(value).trim()
  return !result || ['-', '—', 'null', 'undefined'].includes(result.toLowerCase()) ? null : result
}

function values(value: unknown, length: number): Array<string | null> {
  const source = decoded(value)
  return Array.from({ length }, (_, index) => Array.isArray(source) ? text(source[index]) : null)
}

function marketRows(value: unknown, market: string) {
  const parsed = decoded(value)
  if (Array.isArray(parsed)) return { present: true, rows: parsed.map(record) }
  const source = record(parsed)
  if (own(source, market)) {
    const rows = decoded(source[market])
    return { present: true, rows: Array.isArray(rows) ? rows.map(record) : [] }
  }
  return { present: false, rows: [] as RecordValue[] }
}

function nameKey(raw: unknown): CompanyKey | '' {
  const name = String(raw ?? '').replace(/\s/g, '').toLowerCase()
  if (name.includes('bet365') || name === 'bet3**') return 'bet365'
  if (name.includes('william') || name.includes('威廉')) return 'william'
  if (name.includes('sporttery') || name.includes('竞彩')) return 'sporttery'
  return ''
}

function identity(row: RecordValue, key: CompanyKey, market: 'euro' | 'pankou', dedicated = false) {
  const rawID = Number(field(row, 'companyId', 'company_id'))
  const id = Number.isSafeInteger(rawID) && rawID > 0 ? String(rawID) : ''
  if (market === 'euro' ? ['8', '9'].includes(id) : ['281', '115'].includes(id)) return { matches: false, canonical: false }
  const known = market === 'euro' ? ({ '281': 'bet365', '115': 'william' } as Record<string, string>)[id] : ({ '8': 'bet365', '9': 'william' } as Record<string, string>)[id]
  if (known) return { matches: known === key, canonical: true }
  const name = field(row, 'companyName', 'company_name')
  const named = nameKey(name)
  return { matches: named ? named === key : dedicated && !id && !String(name ?? '').trim(), canonical: false }
}

function captured(value: RecordValue) {
  return text(field(value, 'capturedAt', 'captured_at'))
}

function missingEuro(capturedAt: string | null): DrawObservedEuroOdds {
  return { company: '', opening: [null, null, null], latest: [null, null, null], source: 'missing', capturedAt }
}

function missingLine(capturedAt: string | null): DrawObservedLine {
  return { company: '', opening: null, latest: null, historical: null, openingOdds: [null, null], latestOdds: [null, null], source: 'missing', capturedAt }
}

function euroQuote(odds: RecordValue, key: CompanyKey, name: string): DrawObservedEuroOdds {
  const result = missingEuro(captured(odds))
  if (key === 'sporttery') {
    let trade = record(field(odds, 'sportteryTrade', 'sporttery_trade'))
    const recorded = record(trade.sportteryOdds)
    const official = record(recorded.data)
    const history = record(official.spf).odds
    const nested = record(trade.data)
    if (own(nested, 'tzbl')) trade = nested
    const ratio = record(trade.tzbl)
    result.latest = ['h', 'd', 'a'].map((key) => text(ratio[key]))
    if (Array.isArray(history) && history.length && Number(recorded.code ?? 0) === 0) {
      result.opening = ['spf3', 'spf1', 'spf0'].map((key) => text(record(history[0])[key]))
      result.latest = ['spf3', 'spf1', 'spf0'].map((key) => text(record(history[history.length - 1])[key]))
    }
    if (result.latest.some((value) => value !== null)) result.company = name
    if (result.latest.every((value) => value !== null && Number.isFinite(Number(value)) && Number(value) > 0)) result.source = 'odds_moneys'
    return result
  }
  const source = marketRows(odds.data, 'odds')
  const rows = source.present ? source.rows : [record(odds[key])]
  for (const row of rows) {
    const id = identity(row, key, 'euro', !source.present)
    if (!id.matches) continue
    const opening = values(field(row, 'firstOdds', 'first_odds'), 3)
    const latest = values(row.odds, 3)
    if (![...opening, ...latest].some((value) => value !== null)) continue
    return { ...result, company: id.canonical ? name : text(field(row, 'companyName', 'company_name')) || name, opening, latest, source: 'odds_moneys' }
  }
  return result
}

function lineQuote(rows: RecordValue[], preferred: unknown, key: CompanyKey, name: string, capturedAt: string | null): DrawObservedLine {
  const result = missingLine(capturedAt)
  const dedicated = preferred !== null && preferred !== undefined
  for (const row of dedicated ? [record(preferred)] : rows) {
    const id = identity(row, key, 'pankou', dedicated)
    if (!id.matches) continue
    const opening = text(field(row, 'firstPankou', 'first_pankou'))
    const latest = text(row.pankou)
    const openingOdds = values(field(row, 'firstOdds', 'first_odds'), 2)
    const latestOdds = values(row.odds, 2)
    if (opening === null && latest === null && ![...openingOdds, ...latestOdds].some((value) => value !== null)) continue
    return { ...result, company: id.canonical ? name : text(field(row, 'companyName', 'company_name')) || name, opening, latest, openingOdds, latestOdds, source: 'pankou_moneys' }
  }
  return result
}

export function assertDrawRawMatch(value: unknown, matchId: string) {
  if (value === null) return
  const payload = record(value)
  if (String(field(payload, 'matchId', 'match_id') ?? '').trim() !== matchId) throw new Error('前台原始赔率返回了不同的比赛 ID，已停止补充')
}

/** Only adapts captured raw data; never borrows frontend estimates/simulated prices. */
export function parseDrawCompanyQuotes(matchId: string, euro: unknown, pankou: unknown): DrawCompanyQuote[] {
  assertDrawRawMatch(euro, matchId)
  assertDrawRawMatch(pankou, matchId)
  const odds = record(euro)
  const lines = record(pankou)
  const asiaData = field(lines, 'asiaData', 'asia_data')
  const asian = marketRows(asiaData, 'asia')
  const combined = record(asiaData)
  const total = own(combined, 'dxq') ? { present: true, rows: marketRows(combined.dxq, 'dxq').rows } : marketRows(field(lines, 'dxqData', 'dxq_data'), 'dxq')
  return companies.map(({ key, name }) => ({
    key, name, euroOdds: euroQuote(odds, key, name),
    asianHandicap: lineQuote(asian.rows, key === 'bet365' && !asian.present ? field(lines, 'bet365Asia', 'bet365_asia') : undefined, key, name, captured(lines)),
    goalsLine: lineQuote(total.rows, key === 'bet365' && !total.present ? field(lines, 'bet365Dxq', 'bet365_dxq') : undefined, key, name, captured(lines)),
  }))
}

export function parseDrawFrontendAnalysis(payload: unknown, date: string): Map<string, DrawFrontendAnalysis> {
  if (!Array.isArray(payload)) throw new Error('前台批量分析返回格式不正确')
  const result = new Map<string, DrawFrontendAnalysis>()
  for (const entry of payload) {
    const row = entry as AnalysisMatch
    const sourceDate = typeof row?.date === 'string' ? row.date.match(/^(\d{4}-\d{2}-\d{2})(?:T| |$)/)?.[1] : undefined
    if (!row || sourceDate !== date || !row.matchId) throw new Error('前台批量分析返回的日期或比赛 ID 不符合查询条件')
    result.set(String(row.matchId), {
      yapanpankou1: row.yapanpankou1, yapanpankou2: row.yapanpankou2, qiushupankou1: row.qiushupankou1, qiushupankou2: row.qiushupankou2,
      yapantouzhu: row.yapantouzhu, newyapantouzhu: row.newyapantouzhu, qiushutouzhu: row.qiushutouzhu, newqiushutouzhu: row.newqiushutouzhu,
      winProbability: row.winProbability, drawProbability: row.drawProbability, loseProbability: row.loseProbability,
      sanhuxinli: row.sanhuxinli, kailiresult: row.kailiresult, ticairesult: row.ticairesult,
      roiSimulation: row.roiSimulation ?? null, localMarket: row.platform?.localMarket ?? null,
    })
  }
  return result
}
