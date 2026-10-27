<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { evictDrawObservationCache, getDrawObservations } from '@/api/drawHistory'
import { clearDrawFrontendCache, loadDrawCompanyQuotes, loadDrawFrontendAnalysis } from '@/api/drawFrontend'
import { BEIJING_TIME_ZONE, beijingToday, formatBeijingDateTime } from '@/utils/date'
import type { DrawFrontendAnalysis, DrawFrontendLoadState, DrawObservation, DrawObservationQuery, DrawObservedEuroOdds, DrawObservedLine, DrawRecentMatch, DrawObservationsResponse } from '@/types/drawHistory'
import DrawFrontendData from './DrawFrontendData.vue'
import DrawFeatureSignal from './DrawFeatureSignal.vue'
import DrawProfitSummary from './DrawProfitSummary.vue'
import DrawFrozenSignal from './DrawFrozenSignal.vue'
import DrawSecondarySignal from './DrawSecondarySignal.vue'
import DrawScheduleDate from './DrawScheduleDate.vue'
import { DRAW_FEATURE_NOTE } from '@/utils/drawFeature'
import { classifySecondaryDraw, SECONDARY_DRAW_THRESHOLD, SECONDARY_DRAW_VERSION } from '@/utils/drawSecondarySignal'

const loading = ref(false)
const error = ref('')
const report = ref<DrawObservationsResponse | null>(null)
const mode = ref<'today' | 'history' | 'counts'>('today')
const scope = ref<'all' | 'sporttery'>('all')
const scopeOptions = [{ title: '全部赛事', value: 'all' }, { title: '竞彩赛事', value: 'sporttery' }]
const scheduleDate = ref(beijingToday())
const calendarRevision = ref(0)
const league = ref<string | null>(null)
const leagueOptions = ref<string[]>([])
const search = ref('')
const page = ref(1)
const pageSize = ref(50)
const expanded = ref<string[]>([])
const frontendStates = reactive<Record<string, DrawFrontendLoadState>>({})
const quoteStates = reactive<Record<string, DrawFrontendLoadState>>({})
const legacyAnalysisObserved = ref(false)
type AnalysisPrefetch = Promise<{ value: Map<string, DrawFrontendAnalysis> } | { error: unknown }>
let analysisRequest = 0
let quoteRequest = 0
const quoteRequests = new Map<string, number>()
const countRanges = reactive({
  home: { min: '', max: '' },
  away: { min: '', max: '' },
  h2h: { min: '', max: '' },
})
const countErrors = reactive({ home: '', away: '', h2h: '' })
const countGroups = [
  { key: 'home', title: '主队赛前近 10 场平局', maximum: 10, minKey: 'home_draws_min', maxKey: 'home_draws_max' },
  { key: 'away', title: '客队赛前近 10 场平局', maximum: 10, minKey: 'away_draws_min', maxKey: 'away_draws_max' },
  { key: 'h2h', title: '两队赛前历史交锋平局', maximum: undefined, minKey: 'h2h_draws_min', maxKey: 'h2h_draws_max' },
] as const
let sequence = 0

const pages = computed(() => Math.max(1, Math.ceil((report.value?.total || 0) / pageSize.value)))
const legacyHistoryCoverage = computed(() => !!report.value && mode.value !== 'today' && !report.value.scheduleTimezone)
const summaryPrefix = computed(() => scope.value === 'sporttery' ? '竞彩主比赛表' : '主比赛表')
const secondarySummary = computed(() => {
  const counts = { selected: 0, retained: 0, rejected: 0, unavailable: 0 }
  for (const row of report.value?.items || []) {
    if (row.frozenDrawSignal?.state !== 'selected') continue
    counts.selected++
    const decision = classifySecondaryDraw(row).decision
    if (decision === 'retained') counts.retained++
    else if (decision === 'rejected') counts.rejected++
    else counts.unavailable++
  }
  return counts
})
const emptyMessage = computed(() => {
  if (loading.value) return '正在读取本地比赛…'
  if (error.value) return '查询失败，请查看上方错误提示后重试'
  if (!report.value) return mode.value === 'counts' ? '条件尚未查询，请点击“按次数筛选”' : '条件尚未查询，请点击“查询”'
  if (report.value.total > 0) return '当前页没有比赛，点击“查询”返回第一页'
  return mode.value === 'today' ? '所选日期暂无比赛' : mode.value === 'counts' ? '所选日期没有满足次数条件的历史平局比赛' : '所选日期暂无已完赛平局比赛'
})
const teams = (row: DrawObservation) => [
  { key: 'home', label: row.home, recent: row.homeRecent },
  { key: 'away', label: row.guest, recent: row.awayRecent },
]
const lines = (row: DrawObservation) => [
  { key: 'asian', label: '亚盘', line: row.asianHandicap, labels: ['主', '客'] },
  { key: 'goals', label: '大小球', line: row.goalsLine, labels: ['大', '小'] },
]

function count(value: number | undefined) {
  return value === undefined ? '—' : value.toLocaleString('zh-CN')
}

function dateTime(raw: string) {
  return formatBeijingDateTime(raw, '暂无')
}

function resultLabel(value: DrawRecentMatch['result']) {
  return { draw: '平', win: '胜', loss: '负' }[value]
}

function recentTitle(match: DrawRecentMatch) {
  return `${dateTime(match.matchTime || match.date)} · ${match.home} ${match.homeScore}:${match.awayScore} ${match.guest} · ${match.venue === 'home' ? '主场' : '客场'} · ${resultLabel(match.result)}`
}

function sourceLabel(source: string) {
  return ({ money: '旧版比赛', moneys: '比赛数据', history_moneys: '历史战绩', pankou_moneys: '盘口数据', odds_moneys: '欧赔数据' } as Record<string, string>)[source] || source
}

function hasLine(line: DrawObservedLine) {
  return !!line && ((line.opening != null && line.opening !== '') || (line.latest != null && line.latest !== '') || [...(line.openingOdds || []), ...(line.latestOdds || [])].some((value) => value != null && value !== ''))
}

function hasEuro(euro: DrawObservedEuroOdds | undefined) {
  return !!euro && [...(euro.opening || []), ...(euro.latest || [])].some((value) => value != null && value !== '')
}

function quote(value: string | null | undefined) {
  return value === null || value === undefined || value === '' ? '暂无' : value
}

function needsSportteryOpening(row: DrawObservation) {
  if (!row.jingcaiId?.trim()) return false
  const values = row.companyQuotes?.find((company) => company.key === 'sporttery')?.euroOdds.opening
  const status = row.companyQuotes?.find((company) => company.key === 'sporttery')?.euroOdds.openingStatus
  if (status === 'unopened' || status === 'no_quotes') return false
  return !values || values.length !== 3 || values.some((raw) => raw == null || !Number.isFinite(Number(raw)) || Number(raw) <= 0)
}

function toggle(row: DrawObservation) {
  const closing = expanded.value.includes(row.matchId)
  expanded.value = closing
    ? expanded.value.filter((id) => id !== row.matchId)
    : [...expanded.value, row.matchId]
  if (!closing && (row.companyQuotes === undefined || (needsSportteryOpening(row) && !quoteStates[row.matchId]))) void supplementQuotes(row)
}

function supplementError(error: unknown) {
  return error instanceof Error ? error.message : '读取前台补充数据失败，请重试'
}

async function supplementAnalysis(target = report.value, request = sequence, refresh = false, prefetched?: AnalysisPrefetch) {
  if (!target || request !== sequence) return
  const missing = target.items.filter((row) => row.frontendAnalysis === undefined)
  if (!missing.length) return
  const pending = ++analysisRequest
  for (const row of missing) frontendStates[row.matchId] = { loading: true, error: '' }
  try {
    const prepared = prefetched ? await prefetched : { value: await loadDrawFrontendAnalysis(target.date, refresh) }
    if ('error' in prepared) throw prepared.error
    const analysis = prepared.value
    if (request !== sequence || pending !== analysisRequest || report.value !== target) return
    for (const row of missing) {
      const value = analysis.get(String(row.matchId))
      if (row.frontendAnalysis === undefined && value) row.frontendAnalysis = value
      frontendStates[row.matchId] = { loading: false, error: value || row.frontendAnalysis != null ? '' : '前台批量接口未返回本场比赛的分析数据' }
    }
  } catch (error) {
    if (request !== sequence || pending !== analysisRequest || report.value !== target) return
    for (const row of missing) frontendStates[row.matchId] = { loading: false, error: supplementError(error) }
  }
}

async function supplementQuotes(row: DrawObservation, refresh = false) {
  if (row.companyQuotes !== undefined && !needsSportteryOpening(row) && !refresh) return
  const target = report.value
  if (!target?.items.includes(row)) return
  const request = sequence
  const pending = ++quoteRequest
  quoteRequests.set(row.matchId, pending)
  quoteStates[row.matchId] = { loading: true, error: '' }
  try {
    const quotes = await loadDrawCompanyQuotes(String(row.matchId), refresh)
    if (request !== sequence || quoteRequests.get(row.matchId) !== pending || report.value !== target) return
    row.companyQuotes = quotes
    quoteStates[row.matchId] = { loading: false, error: '' }
  } catch (error) {
    if (request !== sequence || quoteRequests.get(row.matchId) !== pending || report.value !== target) return
    quoteStates[row.matchId] = { loading: false, error: supplementError(error) }
  }
}

function clearSupplementStates() {
  analysisRequest++
  quoteRequests.clear()
  for (const key of Object.keys(frontendStates)) delete frontendStates[key]
  for (const key of Object.keys(quoteStates)) delete quoteStates[key]
}

function countFilters(): DrawObservationQuery | null {
  const filters: DrawObservationQuery = {}
  for (const group of countGroups) {
    countErrors[group.key] = ''
    const minimum = String(countRanges[group.key].min ?? '').trim()
    const maximum = String(countRanges[group.key].max ?? '').trim()
    const min = minimum === '' ? undefined : Number(minimum)
    const max = maximum === '' ? undefined : Number(maximum)
    if ([min, max].some((value) => value !== undefined && (!Number.isSafeInteger(value) || value < 0))) {
      countErrors[group.key] = '请输入非负整数，留空表示不限'
    } else if (group.maximum !== undefined && [min, max].some((value) => value !== undefined && value > group.maximum!)) {
      countErrors[group.key] = '主、客队近 10 场的平局次数不能超过 10'
    } else if (min !== undefined && max !== undefined && min > max) {
      countErrors[group.key] = '最少次数不能大于最多次数'
    } else {
      if (min !== undefined) filters[group.minKey] = min
      if (max !== undefined) filters[group.maxKey] = max
    }
  }
  if (Object.values(countErrors).some(Boolean)) {
    error.value = '请修正平局次数范围后查询'
    return null
  }
  return filters
}

function validateResponse(data: DrawObservationsResponse, submitted: DrawObservationQuery, counts: boolean) {
  const expectedScope = submitted.scope || 'all'
  if ((data.scope !== undefined && data.scope !== expectedScope) || (expectedScope === 'sporttery' && (data.scope !== 'sporttery' || !Array.isArray(data.items) || data.items.some((row) => typeof row.jingcaiId !== 'string' || !row.jingcaiId.trim())))) {
    throw new Error(expectedScope === 'sporttery' ? '当前服务尚未加载竞彩范围筛选，已停止展示。请切换“全部赛事”或加载更新后再查询。' : '返回结果不符合所选赛事范围，已停止展示。请加载更新后再查询。')
  }
  if (data.scheduleTimezone && data.scheduleTimezone !== BEIJING_TIME_ZONE) {
    throw new Error('返回结果不符合所选北京时间日期条件，已停止展示。分析服务尚未加载更新，请加载更新后再查询。')
  }
  if (data.mode !== submitted.mode || data.date !== submitted.date || !Array.isArray(data.items) || data.items.some((row) => row.date !== submitted.date)) {
    throw new Error('返回结果不符合所选日期条件，已停止展示。分析服务尚未加载更新，请加载更新后再查询。')
  }
  if (counts) {
    for (const row of data.items) {
      const groups = [
        { window: row.homeRecent, min: submitted.home_draws_min, max: submitted.home_draws_max },
        { window: row.awayRecent, min: submitted.away_draws_min, max: submitted.away_draws_max },
        { window: row.headToHead, min: submitted.h2h_draws_min, max: submitted.h2h_draws_max },
      ]
      for (const group of groups) {
        if (group.min === undefined && group.max === undefined) continue
        const value = group.window
        if (!value || !Number.isInteger(value.sample) || value.sample <= 0 || !Number.isInteger(value.draws) || value.draws < 0 || value.draws > value.sample || (group.min !== undefined && value.draws < group.min) || (group.max !== undefined && value.draws > group.max)) {
          throw new Error('返回结果不符合所选平局次数条件，已停止展示。分析服务尚未加载更新，请加载更新后再查询。')
        }
      }
    }
  }
}

function invalidateResults() {
  sequence++
  clearSupplementStates()
  loading.value = false
  page.value = 1
  report.value = null
  expanded.value = []
  error.value = ''
}

function invalidateDate() {
  invalidateResults()
  league.value = null
  leagueOptions.value = []
}

function changeScope() {
  invalidateResults()
  league.value = null
  leagueOptions.value = []
  return load(true)
}

async function load(resetPage = false, refresh = false) {
  if (!scheduleDate.value) {
    error.value = '请选择赛程日期'
    return
  }
  const filters = mode.value === 'counts' ? countFilters() : {}
  if (filters === null) return
  if (resetPage) page.value = 1
  const requestedTab = mode.value
  const submitted: DrawObservationQuery = {
    mode: requestedTab === 'today' ? 'today' : 'history',
    scope: scope.value,
    date: scheduleDate.value,
    start_date: scheduleDate.value,
    end_date: scheduleDate.value,
    ...(refresh ? { refresh: 1 } : {}),
    league: league.value || undefined,
    search: search.value.trim() || undefined,
    page: page.value,
    page_size: pageSize.value,
    ...filters,
  }
  const request = ++sequence
  if (refresh) {
    clearDrawFrontendCache()
    calendarRevision.value++
  }
  const prefetched: AnalysisPrefetch | undefined = legacyAnalysisObserved.value
    ? loadDrawFrontendAnalysis(submitted.date!).then((value) => ({ value }), (error: unknown) => ({ error }))
    : undefined
  loading.value = true
  error.value = ''
  try {
    const { data } = await getDrawObservations(submitted)
    if (request === sequence) {
      validateResponse(data, submitted, requestedTab === 'counts')
      clearSupplementStates()
      report.value = data
      leagueOptions.value = data.leagues || []
      expanded.value = []
      if (data.items.length) legacyAnalysisObserved.value = data.items.some((row) => row.frontendAnalysis === undefined)
      void supplementAnalysis(report.value, request, false, prefetched)
    }
  } catch (err) {
    if (request !== sequence) return
    evictDrawObservationCache(submitted)
    clearSupplementStates()
    report.value = null
    expanded.value = []
    const e = err as { response?: { data?: { error?: string; message?: string } }; message?: string }
    error.value = e.response?.data?.error || e.response?.data?.message || e.message || '读取本地比赛失败，请稍后重试'
  } finally {
    if (request === sequence) loading.value = false
  }
}

function reset() {
  invalidateResults()
  leagueOptions.value = []
  league.value = null
  search.value = ''
  scope.value = 'all'
  scheduleDate.value = beijingToday()
  if (mode.value === 'counts') {
    for (const group of countGroups) {
      countRanges[group.key].min = ''
      countRanges[group.key].max = ''
      countErrors[group.key] = ''
    }
  }
  load(true)
}

function changeMode() {
  invalidateResults()
  leagueOptions.value = []
  page.value = 1
  league.value = null
  load(true)
}

onMounted(() => load())
onBeforeUnmount(() => { sequence++ })
</script>

<template>
  <div class="draw-history">
    <div class="d-flex flex-wrap align-center ga-3 mb-4">
      <div>
        <h2 class="text-h5 font-weight-bold"><v-icon color="primary" class="mr-1">mdi-soccer-field</v-icon>平局比赛观察</h2>
        <div class="text-body-2 text-medium-emphasis mt-1">查看今日全部赛程与历史实际平局，核对双方赛前近 10 场、欧赔和盘口。</div>
      </div>
      <v-spacer />
      <v-btn variant="outlined" prepend-icon="mdi-refresh" :loading="loading" @click="load(false, true)">刷新本地数据</v-btn>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4" closable @click:close="error = ''">{{ error }}</v-alert>
    <v-alert v-if="loading && report" type="info" variant="tonal" density="compact" class="mb-4">正在查询或刷新，当前表格与展开内容为上次读取的结果。</v-alert>

    <v-card class="mb-4">
      <v-tabs v-model="mode" color="primary" @update:model-value="changeMode">
        <v-tab value="today" prepend-icon="mdi-calendar-today">今日比赛</v-tab>
        <v-tab value="history" prepend-icon="mdi-history">历史平局</v-tab>
        <v-tab value="counts" prepend-icon="mdi-filter-variant">平局次数</v-tab>
      </v-tabs>
    </v-card>

    <v-row dense class="mb-2">
      <v-col cols="6" md="3"><v-card variant="tonal"><v-card-text><div class="text-caption">{{ summaryPrefix }}全部比赛</div><div class="text-h5 font-weight-bold">{{ count(report?.summary.historicalMatches) }}</div><div class="text-caption text-medium-emphasis">含未赛，按比赛去重</div></v-card-text></v-card></v-col>
      <v-col cols="6" md="3"><v-card variant="tonal" color="primary"><v-card-text><div class="text-caption">{{ summaryPrefix }}历史完赛平局</div><div class="text-h5 font-weight-bold">{{ count(report?.summary.drawMatches) }}</div><div class="text-caption">常规时间实际平局</div></v-card-text></v-card></v-col>
      <v-col cols="6" md="3"><v-card variant="tonal"><v-card-text><div class="text-caption">{{ mode === 'today' ? '当日赛程' : '历史平局' }}亚盘有记录</div><div class="text-h5 font-weight-bold">{{ legacyHistoryCoverage ? '—' : count(report?.summary.asianMatches) }}</div><div class="text-caption text-medium-emphasis">{{ legacyHistoryCoverage ? '当前接口未提供单日覆盖数' : '仅实际盘口记录' }}</div></v-card-text></v-card></v-col>
      <v-col cols="6" md="3"><v-card variant="tonal"><v-card-text><div class="text-caption">{{ mode === 'today' ? '当日赛程' : '历史平局' }}大小球有记录</div><div class="text-h5 font-weight-bold">{{ legacyHistoryCoverage ? '—' : count(report?.summary.totalMatches) }}</div><div class="text-caption text-medium-emphasis">{{ legacyHistoryCoverage ? '当前接口未提供单日覆盖数' : '仅实际盘口记录' }}</div></v-card-text></v-card></v-col>
    </v-row>
    <div class="text-caption text-medium-emphasis mb-4">前两项总数统计本地主比赛表全历史，随赛事范围变化，不受日期、联赛、球队和次数条件影响。竞彩范围按本地竞彩编号识别；缺少编号的比赛不会计入，场数不代表竞彩数据已采集完整。赛前近 10 场及交锋仍使用双方全部赛事记录。{{ legacyHistoryCoverage ? '当前接口未提供历史单日盘口覆盖数，已留空。' : '盘口覆盖按所选赛事范围与单日的目标比赛统计，不受联赛、球队和分页筛选影响。' }}<span v-if="mode === 'counts'">筛选后场数见下方结果。</span></div>

    <v-card class="mb-4">
      <v-card-text class="d-flex flex-wrap align-center ga-3">
        <DrawScheduleDate v-model="scheduleDate" :mode="mode === 'today' ? 'today' : 'history'" :scope="scope" :revision="calendarRevision" class="filter-date" @update:model-value="invalidateDate" @change="load(true)" />
        <v-select v-model="scope" :items="scopeOptions" label="赛事范围" density="compact" variant="outlined" hide-details class="filter-scope" @update:model-value="changeScope" />
        <v-autocomplete v-model="league" :items="leagueOptions" label="全部联赛" clearable density="compact" variant="outlined" hide-details class="filter-league" @update:model-value="invalidateResults" />
        <v-text-field v-model="search" label="球队 / 比赛 ID" prepend-inner-icon="mdi-magnify" density="compact" variant="outlined" hide-details class="filter-search" @update:model-value="invalidateResults" @keyup.enter="load(true)" />
        <v-btn color="primary" :loading="loading" @click="load(true)">查询</v-btn>
        <v-btn variant="text" :disabled="loading" @click="reset">{{ mode === 'counts' ? '重置为今天' : '今天' }}</v-btn>
      </v-card-text>
    </v-card>

    <v-card v-if="mode === 'counts'" class="mb-4">
      <v-card-title class="text-subtitle-1">按平局次数筛选历史实际平局</v-card-title>
      <v-card-text>
        <v-row>
          <v-col v-for="group in countGroups" :key="group.key" cols="12" md="4">
            <div class="text-subtitle-2 mb-3">{{ group.title }}</div>
            <div class="d-flex align-center ga-2">
              <v-text-field v-model="countRanges[group.key].min" type="number" :min="0" :max="group.maximum" :step="1" label="最少次数" placeholder="不限" density="compact" variant="outlined" hide-details :error="!!countErrors[group.key]" @update:model-value="countErrors[group.key] = ''; invalidateResults()" @keyup.enter="load(true)" />
              <span class="text-medium-emphasis">至</span>
              <v-text-field v-model="countRanges[group.key].max" type="number" :min="0" :max="group.maximum" :step="1" label="最多次数" placeholder="不限" density="compact" variant="outlined" hide-details :error="!!countErrors[group.key]" @update:model-value="countErrors[group.key] = ''; invalidateResults()" @keyup.enter="load(true)" />
            </div>
            <div v-if="countErrors[group.key]" class="text-caption text-error mt-2">{{ countErrors[group.key] }}</div>
            <div v-else class="text-caption text-medium-emphasis mt-2">{{ group.key === 'h2h' ? '全部可用赛前交锋，不限最近 10 场' : '双方分别统计，合计主客场，最多 10 场' }}</div>
          </v-col>
        </v-row>
        <div class="d-flex flex-wrap align-center ga-3 mt-2"><div class="text-caption text-medium-emphasis flex-grow-1">留空表示不限，最少与最多相同即精确筛选。三组条件同时满足；只要某组设了次数条件，该组没有记录的比赛就不匹配，“暂无”不当作 0 次平局。筛选作用于所选日期的全部历史平局结果，再分页。</div><v-btn color="primary" :loading="loading" prepend-icon="mdi-filter-check" @click="load(true)">按次数筛选</v-btn></div>
      </v-card-text>
    </v-card>

    <v-alert type="info" variant="tonal" density="compact" class="mb-4">
      所有标签仅查看所选赛程日与赛事范围：今日比赛列出当日赛程，历史平局和平局次数仅列已完赛平局。双方近 10 场合计主客场并排除本场，最新在左，不足 10 场按实际数量显示。赔率与水位缺失显示“暂无”，即时盘展示当前已获取的最新报价；日期按源站赛程日查询。
    </v-alert>

    <v-card variant="outlined" class="mb-4 secondary-observation-note" :data-secondary-version="SECONDARY_DRAW_VERSION">
      <v-card-text class="py-3">
        <div class="text-body-2"><strong>二次筛选观察 v1：</strong>原模型入选后，模型分数低于 {{ SECONDARY_DRAW_THRESHOLD.toFixed(3) }} 剔除，达到门槛保留。分数不是平局概率，赛果仅用于核对。</div>
        <div v-if="report" class="d-flex flex-wrap ga-3 text-caption mt-2" aria-live="polite">
          <span>本页原入选 {{ secondarySummary.selected }} 场</span>
          <span class="text-primary">二次保留 {{ secondarySummary.retained }} 场</span>
          <span class="text-warning">二次剔除 {{ secondarySummary.rejected }} 场</span>
          <span v-if="secondarySummary.unavailable">待计算 {{ secondarySummary.unavailable }} 场</span>
        </div>
        <div class="text-caption text-medium-emphasis mt-1">按当前快照判断，原模型标记保留供对照。本页数量不代表全部筛选结果；查看误选和误剔请使用“今日比赛”并切换赛程日。</div>
      </v-card-text>
    </v-card>

    <v-card>
      <v-card-title class="d-flex flex-wrap align-center ga-3 text-subtitle-1">
        <v-icon size="20">mdi-table-search</v-icon>{{ report?.date || scheduleDate }} {{ mode === 'today' ? '全部赛程' : mode === 'counts' ? '平局次数筛选结果' : '历史平局明细' }}
        <span class="text-body-2 text-medium-emphasis">共 {{ count(report?.total) }} 场</span>
        <v-spacer />
        <span class="text-caption text-medium-emphasis">点击比赛展开公司报价、前台分析及双方近况与历史交锋</span>
        <span class="result-badge result-draw">平</span><span class="result-badge result-win">胜</span><span class="result-badge result-loss">负</span>
      </v-card-title>
      <v-progress-linear v-if="loading" indeterminate color="primary" />
      <v-divider v-else />
      <v-table density="comfortable" class="observation-table">
        <thead><tr><th>时间 / 联赛</th><th>主队 / 客队</th><th>比分 / 状态</th><th title="固定竞彩选平模型按输入决定是否入选，完赛后才核对是否命中平局">模型选平 ⓘ</th><th title="二次筛选 v1：原模型入选且分数达到0.225保留，低于门槛剔除；原标记保留对照">二次筛选 ⓘ</th><th>主队赛前近 10 场</th><th>客队赛前近 10 场</th><th>赛前历史交锋</th><th>欧赔（主 / 平 / 客）</th><th>亚盘（主 / 客水位）</th><th>大小球（大 / 小水位）</th><th>投注热度</th><th>初盘对比</th><th>即时盘对比</th><th :title="DRAW_FEATURE_NOTE">平局特征 ⓘ</th><th>竞彩庄家盈亏</th><th>模拟庄家盈亏</th><th>核查</th></tr></thead>
        <tbody>
          <template v-for="row in report?.items || []" :key="row.matchId">
            <tr class="match-row" :class="{ 'is-expanded': expanded.includes(row.matchId) }" tabindex="0" role="button" :aria-expanded="expanded.includes(row.matchId)" @click="toggle(row)" @keydown.enter.prevent="toggle(row)" @keydown.space.prevent="toggle(row)">
              <td><div>{{ dateTime(row.matchTime || row.date) }}</div><div class="text-caption text-medium-emphasis">{{ row.league }}</div><div v-if="row.jingcaiId?.trim()" class="text-caption text-medium-emphasis">竞彩：{{ row.jingcaiId }}</div></td>
              <td><div>{{ row.home }}</div><div class="text-medium-emphasis">{{ row.guest }}</div></td>
              <td><div v-if="row.settled" class="font-weight-bold text-primary">{{ row.homeScore }} : {{ row.awayScore }}</div><div v-else class="text-medium-emphasis">未结算</div><div class="text-caption text-medium-emphasis">{{ row.displayState || (row.settled ? '已完赛' : '未完赛') }}</div></td>
              <td class="frozen-signal-cell"><DrawFrozenSignal :row="row" /></td>
              <td class="secondary-signal-cell"><DrawSecondarySignal :row="row" /></td>
              <td v-for="team in teams(row)" :key="team.key">
                <div v-if="mode !== 'counts' || team.recent.sample > 0" class="text-body-2 mb-1"><strong>{{ team.recent.draws }} / {{ team.recent.sample }}</strong><span class="text-caption text-medium-emphasis ml-1">场平局</span></div>
                <div v-if="team.recent.matches.length" class="result-strip"><span v-for="match in team.recent.matches" :key="match.matchId" class="result-badge" :class="`result-${match.result}`" :title="recentTitle(match)">{{ resultLabel(match.result) }}</span></div>
                <div v-else class="text-caption text-medium-emphasis">暂无赛前战绩</div>
              </td>
              <td><template v-if="row.headToHead && row.headToHead.sample > 0"><div><strong>{{ row.headToHead.draws }} / {{ row.headToHead.sample }}</strong><span class="text-caption text-medium-emphasis ml-1">场平局</span></div><div class="text-caption text-medium-emphasis">全部可用赛前交锋</div></template><span v-else class="text-caption text-medium-emphasis">{{ row.headToHead ? '暂无交锋记录' : '当前接口未提供交锋数据' }}</span></td>
              <td class="euro-cell">
                <template v-if="hasEuro(row.euroOdds)">
                  <div class="quote-row"><span class="text-caption text-medium-emphasis">初盘</span><span v-for="index in 3" :key="index">{{ quote(row.euroOdds?.opening?.[index - 1]) }}</span></div>
                  <div class="quote-row"><span class="text-caption text-medium-emphasis">即时盘</span><span v-for="index in 3" :key="index">{{ quote(row.euroOdds?.latest?.[index - 1]) }}</span></div>
                  <div class="text-caption text-medium-emphasis mt-1">{{ row.euroOdds?.company || '公司暂无' }}</div>
                </template>
                <span v-else class="text-medium-emphasis">暂无</span>
              </td>
              <td v-for="item in lines(row)" :key="item.key" class="line-cell">
                <template v-if="hasLine(item.line)">
                  <div><span class="text-caption text-medium-emphasis mr-2">初盘</span>{{ quote(item.line.opening) }}<span class="water-values"><span v-for="(label, index) in item.labels" :key="label">{{ label }} {{ quote(item.line.openingOdds?.[index]) }}</span></span></div>
                  <div><span class="text-caption text-medium-emphasis mr-2">即时盘</span>{{ quote(item.line.latest) }}<span class="water-values"><span v-for="(label, index) in item.labels" :key="label">{{ label }} {{ quote(item.line.latestOdds?.[index]) }}</span></span></div>
                  <div class="text-caption text-medium-emphasis mt-1">{{ item.line.company || '公司暂无' }}</div>
                </template>
                <span v-else class="text-medium-emphasis">暂无</span>
              </td>
              <td class="heat-cell"><DrawFrontendData :row="row" :analysis-state="frontendStates[row.matchId]" compact heat-only @retry-analysis="supplementAnalysis(report, sequence, true)" /></td>
              <td v-for="stage in (['opening', 'latest'] as const)" :key="stage" class="comparison-cell" :data-comparison-column="stage"><DrawFrontendData :row="row" :analysis-state="frontendStates[row.matchId]" compact :comparison-stage="stage" @retry-analysis="supplementAnalysis(report, sequence, true)" /></td>
              <td class="feature-cell"><DrawFeatureSignal :row="row" :analysis-state="frontendStates[row.matchId]" @retry-analysis="supplementAnalysis(report, sequence, true)" /></td>
              <td class="profit-cell"><DrawProfitSummary :row="row" kind="sporttery" :analysis-state="frontendStates[row.matchId]" @retry-analysis="supplementAnalysis(report, sequence, true)" /></td>
              <td class="profit-cell"><DrawProfitSummary :row="row" kind="simulated" :analysis-state="frontendStates[row.matchId]" @retry-analysis="supplementAnalysis(report, sequence, true)" /></td>
              <td><v-icon :icon="expanded.includes(row.matchId) ? 'mdi-chevron-up' : 'mdi-chevron-down'" size="20" /><div v-if="row.warnings?.length" class="text-caption text-warning">{{ row.warnings.length }} 项说明</div></td>
            </tr>
            <tr v-if="expanded.includes(row.matchId)" class="detail-row">
              <td colspan="18" class="pa-4">
                <div class="d-flex flex-wrap align-center ga-2 mb-3"><span class="text-subtitle-2">赛前历史战绩与交锋</span><span class="text-caption text-medium-emphasis">比赛 ID：{{ row.matchId }}</span><v-spacer /><span class="text-caption text-medium-emphasis">仅列本场之前的有效完赛记录</span></div>
                <v-row>
                  <v-col v-for="team in teams(row)" :key="team.key" cols="12" xl="6">
                    <v-card variant="outlined">
                      <v-card-title class="text-subtitle-2">{{ team.label }}<span v-if="mode !== 'counts' || team.recent.sample > 0" class="text-body-2 font-weight-regular ml-2">{{ team.recent.draws }} 平 / {{ team.recent.sample }} 场</span><span v-else class="text-caption text-medium-emphasis ml-2">暂无赛前记录</span></v-card-title>
                      <v-table density="compact" class="recent-table"><thead><tr><th>日期 / 联赛</th><th>对手</th><th>主客</th><th>比分（主 : 客）</th><th>结果</th></tr></thead><tbody><tr v-for="match in team.recent.matches" :key="match.matchId"><td>{{ dateTime(match.matchTime || match.date) }}<div class="text-caption text-medium-emphasis">{{ match.league }}</div></td><td>{{ match.venue === 'home' ? match.guest : match.home }}</td><td>{{ match.venue === 'home' ? '主场' : '客场' }}</td><td><span class="text-caption text-medium-emphasis">{{ match.home }}</span> {{ match.homeScore }} : {{ match.awayScore }} <span class="text-caption text-medium-emphasis">{{ match.guest }}</span></td><td><span class="result-badge" :class="`result-${match.result}`">{{ resultLabel(match.result) }}</span></td></tr><tr v-if="!team.recent.matches.length"><td colspan="5" class="text-center py-6 text-medium-emphasis">暂无赛前战绩</td></tr></tbody></v-table>
                    </v-card>
                  </v-col>
                </v-row>
                <v-card variant="outlined" class="mt-4">
                  <v-card-title class="text-subtitle-2">两队赛前历史交锋<span v-if="row.headToHead && row.headToHead.sample > 0" class="text-body-2 font-weight-regular ml-2">{{ row.headToHead.draws }} 平 / {{ row.headToHead.sample }} 场</span><span v-else class="text-caption text-medium-emphasis ml-2">{{ row.headToHead ? '暂无赛前交锋记录' : '当前接口未提供交锋数据' }}</span></v-card-title>
                  <v-card-subtitle v-if="row.headToHead" class="pb-3">全部可用交锋，最新在前，排除本场；胜平负以本场主队 {{ row.home }} 为视角。</v-card-subtitle>
                  <v-table density="compact" class="recent-table">
                    <thead><tr><th>日期 / 联赛</th><th>当时主队</th><th>比分（主 : 客）</th><th>当时客队</th><th>本场主队当时主客</th><th>本场主队结果</th></tr></thead>
                    <tbody><tr v-for="match in row.headToHead?.matches || []" :key="match.matchId"><td>{{ dateTime(match.matchTime || match.date) }}<div class="text-caption text-medium-emphasis">{{ match.league }}</div></td><td>{{ match.home }}</td><td>{{ match.homeScore }} : {{ match.awayScore }}</td><td>{{ match.guest }}</td><td>{{ match.venue === 'home' ? '主场' : '客场' }}</td><td><span class="result-badge" :class="`result-${match.result}`">{{ resultLabel(match.result) }}</span></td></tr><tr v-if="!row.headToHead?.matches?.length"><td colspan="6" class="text-center py-6 text-medium-emphasis">{{ row.headToHead ? '暂无赛前交锋记录' : '当前接口未提供交锋数据' }}</td></tr></tbody>
                  </v-table>
                </v-card>
                <DrawFrontendData :row="row" :analysis-state="frontendStates[row.matchId]" :quotes-state="quoteStates[row.matchId]" class="mt-5 mb-5" @retry-analysis="supplementAnalysis(report, sequence, true)" @retry-quotes="supplementQuotes(row, true)" />
                <div v-if="row.sources?.length" class="mt-3 d-flex flex-wrap align-center ga-2"><span class="text-caption text-medium-emphasis">本地来源</span><v-chip v-for="source in row.sources" :key="source" size="x-small" variant="outlined">{{ sourceLabel(source) }}</v-chip></div>
                <div v-for="warning in row.warnings || []" :key="warning" class="text-caption text-warning mt-2">{{ warning }}</div>
              </td>
            </tr>
          </template>
          <tr v-if="!report?.items?.length"><td colspan="18" class="text-center py-12 text-medium-emphasis">{{ emptyMessage }}</td></tr>
        </tbody>
      </v-table>
      <v-divider />
      <div class="d-flex flex-wrap justify-end align-center ga-3 pa-3"><v-select v-model="pageSize" :items="[50, 100]" label="每页" density="compact" variant="outlined" hide-details style="max-width: 100px" :disabled="loading" @update:model-value="load(true)" /><v-pagination v-model="page" :length="pages" :total-visible="6" density="compact" :disabled="loading" @update:model-value="load()" /></div>
    </v-card>
    <div v-if="report?.notes?.length" class="text-caption text-medium-emphasis mt-4"><div v-for="note in report.notes" :key="note" class="mb-1">{{ note }}</div></div>
  </div>
</template>

<style scoped>
.filter-date { flex: 0 1 180px; min-width: 160px; }
.filter-scope { flex: 0 1 170px; min-width: 150px; }
.filter-league { flex: 0 1 210px; min-width: 160px; }
.filter-search { flex: 1 1 220px; min-width: 180px; }
.observation-table :deep(th), .observation-table :deep(td) { white-space: nowrap; }
.match-row { cursor: pointer; }
.match-row:hover, .is-expanded { background: rgba(var(--v-theme-primary), .04); }
.match-row:focus-visible { outline: 2px solid rgb(var(--v-theme-primary)); outline-offset: -2px; }
.result-strip { display: flex; gap: 3px; }
.result-badge { display: inline-flex; align-items: center; justify-content: center; width: 20px; height: 23px; border-radius: 4px; font-size: 12px; font-weight: 600; flex-shrink: 0; }
.result-draw { color: #9a5c00; background: #fff0c7; }
.result-win { color: #15723c; background: #dff5e7; }
.result-loss { color: #b13737; background: #fce5e5; }
.line-cell { min-width: 260px; }
.euro-cell { min-width: 230px; }
.comparison-cell :deep(.frontend-summary) { min-width: 310px; }
.quote-row { display: grid; grid-template-columns: 60px repeat(3, minmax(40px, 1fr)); gap: 8px; align-items: baseline; }
.water-values { display: inline-flex; gap: 10px; margin-left: 12px; font-size: 12px; color: rgba(var(--v-theme-on-surface), .7); }
.detail-row { background: rgba(var(--v-theme-primary), .025); }
.recent-table :deep(td) { white-space: nowrap; }
</style>
