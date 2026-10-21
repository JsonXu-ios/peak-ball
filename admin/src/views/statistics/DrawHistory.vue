<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { getDrawObservations } from '@/api/drawHistory'
import type { DrawObservation, DrawObservedEuroOdds, DrawObservedLine, DrawRecentMatch, DrawObservationsResponse } from '@/types/drawHistory'

const loading = ref(false)
const error = ref('')
const report = ref<DrawObservationsResponse | null>(null)
const mode = ref<'today' | 'history'>('today')
const scheduleDate = ref(new Date().toISOString().slice(0, 10))
const startDate = ref('')
const endDate = ref('')
const league = ref<string | null>(null)
const search = ref('')
const page = ref(1)
const pageSize = ref(50)
const expanded = ref<string[]>([])
let sequence = 0

const pages = computed(() => Math.max(1, Math.ceil((report.value?.total || 0) / pageSize.value)))
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
  if (!raw) return '暂无'
  if (/^\d{4}-\d{2}-\d{2}$/.test(raw)) return raw
  const date = new Date(raw)
  if (!Number.isFinite(date.getTime())) return raw
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', hourCycle: 'h23', timeZoneName: 'short',
  }).format(date)
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

function toggle(row: DrawObservation) {
  expanded.value = expanded.value.includes(row.matchId)
    ? expanded.value.filter((id) => id !== row.matchId)
    : [...expanded.value, row.matchId]
}

async function load(resetPage = false) {
  if (mode.value === 'today' && !scheduleDate.value) {
    error.value = '请选择赛程日期'
    return
  }
  if (mode.value === 'history' && startDate.value && endDate.value && startDate.value > endDate.value) {
    error.value = '开始日期不能晚于结束日期'
    return
  }
  if (resetPage) page.value = 1
  const request = ++sequence
  loading.value = true
  error.value = ''
  try {
    const { data } = await getDrawObservations({
      mode: mode.value,
      date: mode.value === 'today' ? scheduleDate.value : undefined,
      start_date: mode.value === 'history' ? startDate.value || undefined : undefined,
      end_date: mode.value === 'history' ? endDate.value || undefined : undefined,
      league: league.value || undefined,
      search: search.value.trim() || undefined,
      page: page.value,
      page_size: pageSize.value,
    })
    if (request === sequence) {
      report.value = data
      expanded.value = []
    }
  } catch (err) {
    if (request !== sequence) return
    const e = err as { response?: { data?: { error?: string; message?: string } }; message?: string }
    error.value = e.response?.data?.error || e.response?.data?.message || e.message || '读取本地比赛失败，请稍后重试'
  } finally {
    if (request === sequence) loading.value = false
  }
}

function reset() {
  startDate.value = ''
  endDate.value = ''
  league.value = null
  search.value = ''
  if (mode.value === 'today') scheduleDate.value = new Date().toISOString().slice(0, 10)
  load(true)
}

function changeMode() {
  report.value = null
  expanded.value = []
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
      <v-btn variant="outlined" prepend-icon="mdi-refresh" :loading="loading" @click="load()">刷新本地数据</v-btn>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4" closable @click:close="error = ''">{{ error }}</v-alert>

    <v-card class="mb-4">
      <v-tabs v-model="mode" color="primary" @update:model-value="changeMode">
        <v-tab value="today" prepend-icon="mdi-calendar-today">今日比赛</v-tab>
        <v-tab value="history" prepend-icon="mdi-history">历史平局</v-tab>
      </v-tabs>
    </v-card>

    <v-row dense class="mb-2">
      <v-col cols="6" md="3"><v-card variant="tonal"><v-card-text><div class="text-caption">主比赛表全部比赛</div><div class="text-h5 font-weight-bold">{{ count(report?.summary.historicalMatches) }}</div><div class="text-caption text-medium-emphasis">含未赛，按比赛去重</div></v-card-text></v-card></v-col>
      <v-col cols="6" md="3"><v-card variant="tonal" color="primary"><v-card-text><div class="text-caption">主比赛表历史完赛平局</div><div class="text-h5 font-weight-bold">{{ count(report?.summary.drawMatches) }}</div><div class="text-caption">常规时间实际平局</div></v-card-text></v-card></v-col>
      <v-col cols="6" md="3"><v-card variant="tonal"><v-card-text><div class="text-caption">{{ mode === 'today' ? '当日赛程' : '历史平局' }}亚盘有记录</div><div class="text-h5 font-weight-bold">{{ count(report?.summary.asianMatches) }}</div><div class="text-caption text-medium-emphasis">仅实际盘口记录</div></v-card-text></v-card></v-col>
      <v-col cols="6" md="3"><v-card variant="tonal"><v-card-text><div class="text-caption">{{ mode === 'today' ? '当日赛程' : '历史平局' }}大小球有记录</div><div class="text-h5 font-weight-bold">{{ count(report?.summary.totalMatches) }}</div><div class="text-caption text-medium-emphasis">仅实际盘口记录</div></v-card-text></v-card></v-col>
    </v-row>
    <div class="text-caption text-medium-emphasis mb-4">比赛总数与平局数仅统计本地主比赛表；历史战绩仅用作双方近 10 场输入。盘口覆盖按当前标签的目标比赛统计，今日标签按所选日期统计，不受联赛、球队和分页筛选影响。</div>

    <v-card class="mb-4">
      <v-card-text class="d-flex flex-wrap align-center ga-3">
        <v-text-field v-if="mode === 'today'" v-model="scheduleDate" type="date" label="赛程日期（默认 UTC 今天）" density="compact" variant="outlined" hide-details class="filter-date" />
        <template v-else>
          <v-text-field v-model="startDate" type="date" label="开始日期（不限）" density="compact" variant="outlined" hide-details class="filter-date" />
          <v-text-field v-model="endDate" type="date" label="结束日期（不限）" density="compact" variant="outlined" hide-details class="filter-date" />
        </template>
        <v-autocomplete v-model="league" :items="report?.leagues || []" label="全部联赛" clearable density="compact" variant="outlined" hide-details class="filter-league" />
        <v-text-field v-model="search" label="球队 / 比赛 ID" prepend-inner-icon="mdi-magnify" density="compact" variant="outlined" hide-details class="filter-search" @keyup.enter="load(true)" />
        <v-btn color="primary" :loading="loading" @click="load(true)">查询</v-btn>
        <v-btn variant="text" :disabled="loading" @click="reset">{{ mode === 'today' ? '今天' : '全部历史' }}</v-btn>
      </v-card-text>
    </v-card>

    <v-alert type="info" variant="tonal" density="compact" class="mb-4">
      今日比赛列出所选赛程日的全部比赛；历史标签仅列已完赛平局。双方近 10 场合计主客场并排除本场，最新在左，不足 10 场按实际数量显示。赔率与水位缺失显示“暂无”，本地最新不代表终盘；日期按源站赛程日查询。
    </v-alert>

    <v-card>
      <v-card-title class="d-flex flex-wrap align-center ga-3 text-subtitle-1">
        <v-icon size="20">mdi-table-search</v-icon>{{ mode === 'today' ? `${report?.date || scheduleDate} 全部赛程` : '历史平局明细' }}
        <span class="text-body-2 text-medium-emphasis">共 {{ count(report?.total) }} 场</span>
        <v-spacer />
        <span class="text-caption text-medium-emphasis">点击比赛展开双方近 10 场</span>
        <span class="result-badge result-draw">平</span><span class="result-badge result-win">胜</span><span class="result-badge result-loss">负</span>
      </v-card-title>
      <v-progress-linear v-if="loading" indeterminate color="primary" />
      <v-divider v-else />
      <v-table density="comfortable" class="observation-table">
        <thead><tr><th>时间 / 联赛</th><th>主队 / 客队</th><th>比分 / 状态</th><th>主队赛前近 10 场</th><th>客队赛前近 10 场</th><th>欧赔（主 / 平 / 客）</th><th>亚盘（主 / 客水位）</th><th>大小球（大 / 小水位）</th><th>核查</th></tr></thead>
        <tbody>
          <template v-for="row in report?.items || []" :key="row.matchId">
            <tr class="match-row" :class="{ 'is-expanded': expanded.includes(row.matchId) }" tabindex="0" role="button" :aria-expanded="expanded.includes(row.matchId)" @click="toggle(row)" @keydown.enter.prevent="toggle(row)" @keydown.space.prevent="toggle(row)">
              <td><div>{{ dateTime(row.matchTime || row.date) }}</div><div class="text-caption text-medium-emphasis">{{ row.league }}</div></td>
              <td><div>{{ row.home }}</div><div class="text-medium-emphasis">{{ row.guest }}</div></td>
              <td><div v-if="row.settled" class="font-weight-bold text-primary">{{ row.homeScore }} : {{ row.awayScore }}</div><div v-else class="text-medium-emphasis">未结算</div><div class="text-caption text-medium-emphasis">{{ row.displayState || (row.settled ? '已完赛' : '未完赛') }}</div></td>
              <td v-for="team in teams(row)" :key="team.key">
                <div class="text-body-2 mb-1"><strong>{{ team.recent.draws }} / {{ team.recent.sample }}</strong><span class="text-caption text-medium-emphasis ml-1">场平局</span></div>
                <div v-if="team.recent.matches.length" class="result-strip"><span v-for="match in team.recent.matches" :key="match.matchId" class="result-badge" :class="`result-${match.result}`" :title="recentTitle(match)">{{ resultLabel(match.result) }}</span></div>
                <div v-else class="text-caption text-medium-emphasis">暂无赛前战绩</div>
              </td>
              <td class="euro-cell">
                <template v-if="hasEuro(row.euroOdds)">
                  <div class="quote-row"><span class="text-caption text-medium-emphasis">初盘</span><span v-for="index in 3" :key="index">{{ quote(row.euroOdds?.opening?.[index - 1]) }}</span></div>
                  <div class="quote-row"><span class="text-caption text-medium-emphasis">本地最新</span><span v-for="index in 3" :key="index">{{ quote(row.euroOdds?.latest?.[index - 1]) }}</span></div>
                  <div class="text-caption text-medium-emphasis mt-1">{{ row.euroOdds?.company || '公司暂无' }}</div>
                  <div class="text-caption text-medium-emphasis">采集 {{ row.euroOdds?.capturedAt ? dateTime(row.euroOdds.capturedAt) : '时间未知' }}</div>
                </template>
                <span v-else class="text-medium-emphasis">暂无</span>
              </td>
              <td v-for="item in lines(row)" :key="item.key" class="line-cell">
                <template v-if="hasLine(item.line)">
                  <div><span class="text-caption text-medium-emphasis mr-2">初盘</span>{{ quote(item.line.opening) }}<span class="water-values"><span v-for="(label, index) in item.labels" :key="label">{{ label }} {{ quote(item.line.openingOdds?.[index]) }}</span></span></div>
                  <div><span class="text-caption text-medium-emphasis mr-2">本地最新</span>{{ quote(item.line.latest) }}<span class="water-values"><span v-for="(label, index) in item.labels" :key="label">{{ label }} {{ quote(item.line.latestOdds?.[index]) }}</span></span></div>
                  <div class="text-caption text-medium-emphasis mt-1">{{ item.line.company || '公司暂无' }}</div>
                  <div class="text-caption text-medium-emphasis">采集 {{ item.line.capturedAt ? dateTime(item.line.capturedAt) : '时间未知' }}</div>
                </template>
                <span v-else class="text-medium-emphasis">暂无</span>
              </td>
              <td><v-icon :icon="expanded.includes(row.matchId) ? 'mdi-chevron-up' : 'mdi-chevron-down'" size="20" /><div v-if="row.warnings?.length" class="text-caption text-warning">{{ row.warnings.length }} 项说明</div></td>
            </tr>
            <tr v-if="expanded.includes(row.matchId)" class="detail-row">
              <td colspan="9" class="pa-4">
                <div class="d-flex flex-wrap align-center ga-2 mb-3"><span class="text-subtitle-2">赛前战绩核查</span><span class="text-caption text-medium-emphasis">比赛 ID：{{ row.matchId }}</span><v-spacer /><span class="text-caption text-medium-emphasis">仅列本场之前的有效完赛记录</span></div>
                <v-row>
                  <v-col v-for="team in teams(row)" :key="team.key" cols="12" xl="6">
                    <v-card variant="outlined">
                      <v-card-title class="text-subtitle-2">{{ team.label }}<span class="text-body-2 font-weight-regular ml-2">{{ team.recent.draws }} 平 / {{ team.recent.sample }} 场</span></v-card-title>
                      <v-table density="compact" class="recent-table"><thead><tr><th>日期 / 联赛</th><th>对手</th><th>主客</th><th>比分（主 : 客）</th><th>结果</th></tr></thead><tbody><tr v-for="match in team.recent.matches" :key="match.matchId"><td>{{ dateTime(match.matchTime || match.date) }}<div class="text-caption text-medium-emphasis">{{ match.league }}</div></td><td>{{ match.venue === 'home' ? match.guest : match.home }}</td><td>{{ match.venue === 'home' ? '主场' : '客场' }}</td><td><span class="text-caption text-medium-emphasis">{{ match.home }}</span> {{ match.homeScore }} : {{ match.awayScore }} <span class="text-caption text-medium-emphasis">{{ match.guest }}</span></td><td><span class="result-badge" :class="`result-${match.result}`">{{ resultLabel(match.result) }}</span></td></tr><tr v-if="!team.recent.matches.length"><td colspan="5" class="text-center py-6 text-medium-emphasis">暂无赛前战绩</td></tr></tbody></v-table>
                    </v-card>
                  </v-col>
                </v-row>
                <div v-if="row.sources?.length" class="mt-3 d-flex flex-wrap align-center ga-2"><span class="text-caption text-medium-emphasis">本地来源</span><v-chip v-for="source in row.sources" :key="source" size="x-small" variant="outlined">{{ sourceLabel(source) }}</v-chip></div>
                <div v-for="warning in row.warnings || []" :key="warning" class="text-caption text-warning mt-2">{{ warning }}</div>
              </td>
            </tr>
          </template>
          <tr v-if="!report?.items?.length"><td colspan="9" class="text-center py-12 text-medium-emphasis">{{ loading ? '正在读取本地比赛…' : mode === 'today' ? '所选日期暂无比赛' : '当前范围暂无已完赛平局比赛' }}</td></tr>
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
.quote-row { display: grid; grid-template-columns: 60px repeat(3, minmax(40px, 1fr)); gap: 8px; align-items: baseline; }
.water-values { display: inline-flex; gap: 10px; margin-left: 12px; font-size: 12px; color: rgba(var(--v-theme-on-surface), .7); }
.detail-row { background: rgba(var(--v-theme-primary), .025); }
.recent-table :deep(td) { white-space: nowrap; }
</style>
