import type { DrawMatchStatusFilter, DrawMatchStatusOption, DrawObservation, DrawObservationQuery, DrawObservationsResponse } from '@/types/drawHistory'

const commonMatchStatuses: DrawMatchStatusOption[] = [
  { title: '全部状态', value: 'all' },
  { title: '未开始', value: 'state:未开始' },
  { title: '进行中', value: 'state:进行中' },
  { title: '上半场', value: 'state:上半场' },
  { title: '中场', value: 'state:中场' },
  { title: '下半场', value: 'state:下半场' },
  { title: '加时', value: 'state:加时' },
  { title: '点球', value: 'state:点球' },
  { title: '完赛', value: 'finished' },
  { title: '未完赛（汇总）', value: 'unfinished' },
  { title: '推迟', value: 'state:推迟' },
  { title: '延期', value: 'state:延期' },
  { title: '取消', value: 'state:取消' },
  { title: '腰斩', value: 'state:腰斩' },
  { title: '中断', value: 'state:中断' },
  { title: '暂停', value: 'state:暂停' },
  { title: '待定', value: 'state:待定' },
  { title: '状态未知', value: 'unknown' },
]

/** Keep common phases visible and include every additional real source state. */
export function drawMatchStatusOptions(observed: DrawMatchStatusOption[] = [], selected: DrawMatchStatusFilter = 'all') {
  const options = new Map(commonMatchStatuses.map((option) => [option.value, { ...option }]))
  for (const option of observed) {
    if (typeof option?.value !== 'string' || !option.value.startsWith('state:')) continue
    const title = option.value.slice(6).trim()
    if (title && !options.has(option.value)) options.set(option.value, { title, value: option.value })
  }
  // Preserve the selected source label while changing date/scope or loading.
  if (!options.has(selected) && selected.startsWith('state:')) options.set(selected, { title: selected.slice(6), value: selected })
  return [...options.values()]
}

export function drawObservationFilterError(message: string) {
  if (message.startsWith('match_status 应为 ')) return '当前运行的 28080 分析服务尚未加载完整比赛状态筛选，请自行重启分析服务后重试。'
  return message
}

/** Check server-side filtering; never filter a paginated page in the browser. */
export function validateDrawObservationFilters(data: DrawObservationsResponse, query: DrawObservationQuery) {
  const status = query.match_status || 'all'
  const selection = query.selection || 'all'
  if ((status !== 'all' && data.matchStatus !== status) || (selection !== 'all' && data.selection !== selection)) {
    throw new Error('分析服务尚未加载比赛状态和入选筛选，请加载更新后再查询。')
  }
  if (data.items.some((row) => !matchesDrawObservationFilters(row, query))) {
    throw new Error('返回结果不符合比赛状态或入选条件，请加载分析服务更新后再查询。')
  }
}

export function matchesDrawObservationFilters(row: DrawObservation, query: DrawObservationQuery) {
  if (query.match_status === 'finished' && row.settled !== true) return false
  if (query.match_status === 'unfinished' && row.settled !== false) return false
  if (query.match_status === 'unknown' && (row.settled !== false || !!row.displayState?.trim())) return false
  if (query.match_status?.startsWith('state:') && row.displayState?.trim() !== query.match_status.slice(6).trim()) return false
  if (query.selection === 'sporttery') return !!row.jingcaiId?.trim() && row.frozenDrawSignal?.state === 'selected'
  if (query.selection === 'non_sporttery') return !row.jingcaiId?.trim() && row.nonSportterySignal?.state === 'selected'
  return true
}
