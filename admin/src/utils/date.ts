export const BEIJING_TIME_ZONE = 'Asia/Shanghai'

const dateFormatter = new Intl.DateTimeFormat('en-CA', {
  timeZone: BEIJING_TIME_ZONE,
  year: 'numeric', month: '2-digit', day: '2-digit',
})

const dateTimeFormatter = new Intl.DateTimeFormat('en-CA', {
  timeZone: BEIJING_TIME_ZONE,
  year: 'numeric', month: '2-digit', day: '2-digit',
  hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23',
})

function parts(formatter: Intl.DateTimeFormat, date: Date) {
  return Object.fromEntries(formatter.formatToParts(date).map((part) => [part.type, part.value]))
}

/** The crawler's business date is a Beijing calendar day, independent of browser timezone. */
export function beijingToday(now = new Date()): string {
  const value = parts(dateFormatter, now)
  return `${value.year}-${value.month}-${value.day}`
}

/** Source timestamps without an offset are Beijing wall clocks; offset-qualified values are instants. */
export function formatBeijingDateTime(value: string | null | undefined, empty = '—'): string {
  if (!value) return empty
  const raw = value.trim()
  if (!raw) return empty
  if (/^\d{4}-\d{2}-\d{2}$/.test(raw)) return raw

  let normalized = raw
  if (!/(?:z|[+-]\d{2}:?\d{2})$/i.test(raw)) {
    const wallClock = raw.match(/^(\d{4})[-/](\d{1,2})[-/](\d{1,2})[T ](\d{1,2}):(\d{2})(?::(\d{2})(\.\d+)?)?$/)
    if (!wallClock) return raw
    const [, year, month, day, hour, minute, second = '00', fraction = ''] = wallClock
    normalized = `${year}-${month!.padStart(2, '0')}-${day!.padStart(2, '0')}T${hour!.padStart(2, '0')}:${minute}:${second}${fraction}+08:00`
  } else {
    normalized = raw.replace(' ', 'T')
  }

  const date = new Date(normalized)
  if (!Number.isFinite(date.getTime())) return raw
  const result = parts(dateTimeFormatter, date)
  return `${result.year}-${result.month}-${result.day} ${result.hour}:${result.minute}:${result.second}（北京时间）`
}
