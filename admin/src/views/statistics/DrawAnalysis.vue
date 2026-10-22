<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import drawApi from '@/api/draw'
import { beijingToday, formatBeijingDateTime } from '@/utils/date'
import type { DrawForm, DrawHistoryQuery, DrawHistoryReport, DrawHistoryRow, DrawJobProgress, DrawUpcomingReport } from '@/types/draw'

const tab = ref('history')
const loading = ref(false)
const action = ref('')
const error = ref('')
const message = ref('')
const report = ref<DrawHistoryReport | null>(null)
const date = ref(beijingToday())
const loadedDate = ref('')
const search = ref('')
const league = ref('')
const outcome = ref('all')
const split = ref('all')
const threshold = ref<number | null>(null)
const thresholdExploring = ref(false)
const page = ref(1)
const pageSize = ref(30)
const selectedRow = ref<DrawHistoryRow | null>(null)
const detailsOpen = ref(false)
const running = ref(false)
const progress = ref<DrawJobProgress | null>(null)
const autoRefresh = ref(true)
let pollTimer: ReturnType<typeof setTimeout> | undefined
const upcoming = ref<DrawUpcomingReport | null>(null)
const upcomingLoading = ref(false)
const scheduleDate = ref(beijingToday())
let requestSequence = 0

const busy = computed(() => loading.value || !!action.value || running.value)
const summary = computed(() => report.value?.summary)
const testSummary = computed(() => report.value?.testSummary)
const detailFormRows = computed(() => {
  const row = selectedRow.value
  if (!row) return []
  const forms: Array<{ label: string; form?: DrawForm }> = [
    { label: '主队近 10 场', form: row.homeRecent },
    { label: '客队近 10 场', form: row.awayRecent },
    { label: '主队近 10 个主场', form: row.homeVenue },
    { label: '客队近 10 个客场', form: row.awayVenue },
  ]
  return forms.filter((entry): entry is { label: string; form: DrawForm } => !!entry.form)
})
const thresholdLabel = computed(() => threshold.value === null ? '校准建议' : `≥ ${percentage(threshold.value, 2)}`)
const pages = computed(() => Math.max(1, Math.ceil((report.value?.total || 0) / pageSize.value)))
const sourceDate = computed(() => loadedDate.value || date.value)
const outcomeItems = [
  { title: '全部比赛', value: 'all' },
  { title: '选平命中', value: 'hit' },
  { title: '误判：选平但未平', value: 'false_positive' },
  { title: '漏选：实际平局未选', value: 'missed' },
  { title: '实际平局', value: 'draw' },
  { title: '模型选平', value: 'selected' },
  { title: '数据不足', value: 'unavailable' },
]
const splitItems = [
  { title: '全部分区', value: 'all' },
  { title: '训练区', value: 'train' },
  { title: '校准区', value: 'calibration' },
  { title: '测试区', value: 'test' },
  { title: '未纳入建模', value: 'excluded' },
]

function percentage(value: number | null | undefined, digits = 1) {
  return value === null || value === undefined || !Number.isFinite(value) ? '—' : `${value.toFixed(digits)}%`
}

function decimal(value: number | null | undefined) {
  return value === null || value === undefined || !Number.isFinite(value) ? '—' : value.toFixed(4)
}

function goals(value: number | null | undefined) {
  return value === null || value === undefined || !Number.isFinite(value) ? '—' : value.toFixed(2)
}

function modelLabel(key: string) {
  return report.value?.comparisons?.find((item) => item.key === key)?.label || key
}

function number(value: number | undefined) {
  return value === undefined ? '—' : value.toLocaleString('zh-CN')
}

function dateTime(value: string | undefined) {
  return formatBeijingDateTime(value)
}

function splitLabel(value: string) {
  return ({ train: '训练', calibration: '校准', test: '测试', excluded: '未纳入', unassigned: '未分区', warmup: '预热' } as Record<string, string>)[value] || value || '未分区'
}

function outcomeLabel(row: DrawHistoryRow) {
  if (row.probability === null || row.eligible === false) return '未纳入'
  if (row.selected) return row.isDraw ? '选平命中' : '误判'
  return row.isDraw ? '漏选' : '正确排除'
}

function outcomeColor(row: DrawHistoryRow) {
  if (row.probability === null || row.eligible === false) return 'default'
  return row.selected ? (row.isDraw ? 'success' : 'error') : (row.isDraw ? 'warning' : 'grey')
}

function errorText(err: unknown) {
  const e = err as { response?: { data?: { error?: string; message?: string } }; message?: string }
  return e.response?.data?.error || e.response?.data?.message || e.message || '请求失败，请稍后重试'
}

function query(): DrawHistoryQuery {
  return {
    start_date: date.value,
    end_date: date.value,
    league: league.value || undefined,
    search: search.value.trim() || undefined,
    outcome: outcome.value,
    split: split.value,
    page: page.value,
    page_size: pageSize.value,
    threshold: thresholdExploring.value ? threshold.value ?? undefined : undefined,
  }
}

function schedulePoll() {
  clearTimeout(pollTimer)
  if (running.value && autoRefresh.value) pollTimer = setTimeout(() => fetchHistory(false, true), 3000)
}

function toggleAutoRefresh() {
  autoRefresh.value = !autoRefresh.value
  if (autoRefresh.value) fetchHistory(false, true)
  else clearTimeout(pollTimer)
}

async function fetchHistory(resetPage = false, quiet = false) {
  if (resetPage) page.value = 1
  if (!date.value) {
    error.value = '请选择日期'
    return
  }
  const requestedDate = date.value
  const sequence = ++requestSequence
  if (!quiet) loading.value = true
  error.value = ''
  try {
    const { data } = await drawApi.history(query())
    if (sequence === requestSequence) {
      report.value = data
      loadedDate.value = requestedDate
      if (!thresholdExploring.value || threshold.value === null) threshold.value = data.threshold ?? data.model?.recommendedThreshold ?? null
      running.value = !!data.running
      progress.value = data.progress || null
      schedulePoll()
    }
  } catch (err) {
    if (sequence === requestSequence) error.value = errorText(err)
  } finally {
    if (sequence === requestSequence) {
      loading.value = false
      schedulePoll()
    }
  }
}

function resetFilters() {
  date.value = beijingToday()
  search.value = ''
  league.value = ''
  outcome.value = 'all'
  split.value = 'all'
  fetchHistory(true)
}

function setThreshold(value: number) {
  threshold.value = value
  thresholdExploring.value = true
}

function restoreRecommendedThreshold() {
  threshold.value = null
  thresholdExploring.value = false
  fetchHistory(true)
}

async function runAction(kind: 'rebuild' | 'calibrate') {
  if (!date.value) {
    error.value = '请选择日期'
    return
  }
  action.value = kind
  error.value = ''
  message.value = ''
  try {
    const { data } = await drawApi[kind]()
    running.value = !!data.running
    progress.value = data.progress || null
    autoRefresh.value = true
    message.value = data.message || (kind === 'rebuild' ? '历史模型已重建' : '历史样本校准已完成')
    await fetchHistory(true)
    upcoming.value = null
  } catch (err) {
    error.value = errorText(err)
  } finally {
    action.value = ''
  }
}

function showDetails(row: DrawHistoryRow) {
  selectedRow.value = row
  detailsOpen.value = true
}

async function fetchUpcoming() {
  if (!scheduleDate.value) {
    error.value = '请选择赛程日期'
    return
  }
  upcomingLoading.value = true
  error.value = ''
  try {
    upcoming.value = (await drawApi.matches(scheduleDate.value)).data
  } catch (err) {
    error.value = errorText(err)
  } finally {
    upcomingLoading.value = false
  }
}

async function freezeUpcoming() {
  if (!scheduleDate.value) {
    error.value = '请选择赛程日期'
    return
  }
  action.value = 'generate'
  error.value = ''
  try {
    const { data } = await drawApi.generate(scheduleDate.value)
    message.value = data.message || `新增 ${data.created ?? 0} 场赛前记录，跳过 ${data.skipped ?? 0} 场`
    await fetchUpcoming()
  } catch (err) {
    error.value = errorText(err)
  } finally {
    action.value = ''
  }
}

function resetUpcomingDate() {
  scheduleDate.value = beijingToday()
  fetchUpcoming()
}

function changeTab(value: unknown) {
  if (value === 'upcoming' && !upcoming.value) fetchUpcoming()
}

onMounted(() => fetchHistory())
onBeforeUnmount(() => { clearTimeout(pollTimer); requestSequence++ })
</script>

<template>
  <div class="draw-analysis">
    <div class="d-flex flex-wrap align-center ga-3 mb-4">
      <div>
        <h2 class="text-h5 font-weight-bold"><v-icon color="primary" class="mr-1">mdi-scale-balance</v-icon>平局模型验证</h2>
        <div class="text-body-2 text-medium-emphasis mt-1">用已有历史逐场检验模型，比较选平命中、误判与漏选，再校准下一场的概率。</div>
      </div>
      <v-spacer />
      <v-chip v-if="report" size="small" variant="tonal" color="primary">{{ report.modelVersion }}</v-chip>
      <v-btn variant="outlined" prepend-icon="mdi-refresh" :disabled="loading || !!action" @click="tab === 'upcoming' ? fetchUpcoming() : fetchHistory()">刷新</v-btn>
      <v-btn color="warning" prepend-icon="mdi-database-sync" :loading="action === 'rebuild'" :disabled="busy && action !== 'rebuild'" @click="runAction('rebuild')">重建全量历史</v-btn>
      <v-btn color="primary" prepend-icon="mdi-tune" :loading="action === 'calibrate'" :disabled="busy || !report || report.model?.canCalibrate === false" @click="runAction('calibrate')">用历史样本校准</v-btn>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4" closable @click:close="error = ''">{{ error }}</v-alert>
    <v-alert v-if="message" type="success" variant="tonal" class="mb-4" closable @click:close="message = ''">{{ message }}</v-alert>
    <v-card v-if="running" class="mb-4" variant="outlined" color="primary"><v-card-text class="py-3"><div class="d-flex flex-wrap align-center ga-3"><v-icon>mdi-database-sync</v-icon><span>后台任务：{{ progress?.phase || '正在处理历史数据' }} · {{ number(progress?.done) }} / {{ number(progress?.total) }}</span><v-spacer /><v-btn size="small" variant="text" @click="toggleAutoRefresh">{{ autoRefresh ? '停止自动刷新' : '继续自动刷新' }}</v-btn></div><v-progress-linear :model-value="progress?.total ? progress.done / progress.total * 100 : 0" :indeterminate="!progress?.total" color="primary" class="mt-2" /><div class="text-caption mt-2">{{ autoRefresh ? '每 3 秒更新进度，完成后自动加载结果。' : '自动刷新已停止，后台任务继续执行，可手动刷新查看结果。' }}</div></v-card-text></v-card>

    <v-card class="mb-4">
      <v-tabs v-model="tab" color="primary" @update:model-value="changeTab">
        <v-tab value="history" prepend-icon="mdi-history">历史验证与校准</v-tab>
        <v-tab value="upcoming" prepend-icon="mdi-calendar-clock">未来赛程</v-tab>
      </v-tabs>
    </v-card>

    <template v-if="tab === 'history'">
      <v-alert type="info" variant="tonal" density="compact" class="mb-4">
        历史回算用于检验和校准。数据采集晚于开赛的分析与盘口只作探索对照；这类结果不计作真实赛前成绩。训练、校准、测试按时间分开，调整阈值后请重点查看测试区。
      </v-alert>

      <v-card class="mb-4">
        <v-card-text>
          <div class="d-flex flex-wrap align-center ga-3">
            <v-text-field v-model="date" type="date" label="日期（北京时间）" density="compact" variant="outlined" hide-details class="filter-date" />
            <v-autocomplete v-model="league" :items="report?.leagues || []" label="全部联赛" clearable density="compact" variant="outlined" hide-details class="filter-select" />
            <v-text-field v-model="search" label="球队 / 比赛 ID" prepend-inner-icon="mdi-magnify" density="compact" variant="outlined" hide-details class="filter-search" @keyup.enter="fetchHistory(true)" />
            <v-btn color="primary" :loading="loading" :disabled="!!action" @click="fetchHistory(true)">查询</v-btn>
            <v-btn variant="text" :disabled="busy" @click="resetFilters">今天</v-btn>
          </div>
          <div class="d-flex flex-wrap align-center ga-3 mt-4">
            <v-select v-model="split" :items="splitItems" label="验证分区" density="compact" variant="outlined" hide-details class="filter-select" @update:model-value="fetchHistory(true)" />
            <v-select v-model="outcome" :items="outcomeItems" label="明细筛选" density="compact" variant="outlined" hide-details class="filter-select" @update:model-value="fetchHistory(true)" />
            <div class="threshold-control">
              <div class="d-flex align-center text-body-2"><span>{{ thresholdExploring ? '阈值探索' : '选平阈值（来自校准区）' }}</span><v-spacer /><strong>{{ thresholdLabel }}</strong></div>
              <v-slider :model-value="threshold ?? 0" :min="0" :max="100" :step="0.01" color="primary" hide-details density="compact" :disabled="busy || threshold === null" @update:model-value="setThreshold" @end="fetchHistory(true)" />
            </div>
            <v-btn size="small" variant="text" color="primary" :disabled="busy" @click="restoreRecommendedThreshold">采用校准建议阈值</v-btn>
          </div>
          <div class="text-caption text-medium-emphasis mt-2">默认阈值仅由校准区计算，不使用测试赛果。手动调节属于阈值探索；查看测试结果后再调参的结果，不能作为独立验证结论。<span v-if="report?.model?.recommendedThreshold !== undefined"> 校准建议：{{ percentage(report.model.recommendedThreshold, 2) }}。</span></div>
          <div class="text-caption text-medium-emphasis mt-3">日期：{{ sourceDate }} · 更新于 {{ dateTime(report?.generatedAt) }} · 下方历史对照与分箱按当前全部筛选条件计算；独立测试卡片不受明细结果和分区筛选影响。</div>
        </v-card-text>
      </v-card>

      <v-progress-linear v-if="loading" indeterminate color="primary" class="mb-3" />

      <template v-if="report && summary">
        <v-card v-if="testSummary" class="mb-4" variant="outlined" color="primary">
          <v-card-title class="text-subtitle-1 d-flex flex-wrap align-center ga-2"><v-icon size="20">mdi-check-decagram-outline</v-icon>{{ thresholdExploring ? '测试区对照（阈值探索）' : '独立测试区效果' }}<span class="text-caption font-weight-regular ml-2">{{ thresholdExploring ? '当前为手动阈值，结果用于探索' : '按时间留出的后 20% 样本' }} · 不受明细结果筛选影响</span></v-card-title>
          <v-card-text><v-row dense><v-col cols="6" md="3"><div class="text-caption">测试命中率（碰撞）</div><div class="text-h5 font-weight-bold">{{ percentage(testSummary.precision) }}</div><div class="text-caption">命中 {{ number(testSummary.hits) }} / 选平 {{ number(testSummary.selected) }}</div></v-col><v-col cols="6" md="3"><div class="text-caption">测试覆盖率</div><div class="text-h5 font-weight-bold">{{ percentage(testSummary.coverage) }}</div><div class="text-caption">可计算 {{ number(testSummary.eligible) }} 场</div></v-col><v-col cols="6" md="3"><div class="text-caption">测试平局找回率</div><div class="text-h5 font-weight-bold">{{ percentage(testSummary.recall) }}</div><div class="text-caption">误判 {{ number(testSummary.falsePositives) }} · 漏选 {{ number(testSummary.falseNegatives) }}</div></v-col><v-col cols="6" md="3"><div class="text-caption">测试概率误差 Brier</div><div class="text-h5 font-weight-bold">{{ decimal(testSummary.brier) }}</div><div class="text-caption">越低越好 · 探索数据仍需真实赛前验证</div></v-col></v-row></v-card-text>
        </v-card>
        <div class="text-subtitle-2 mb-2">当前筛选子集历史对照<span class="text-caption text-medium-emphasis font-weight-regular ml-2">{{ split === 'all' ? '包含训练、校准和测试区' : splitLabel(split) + '区' }} · 训练内表现用于诊断<span v-if="outcome !== 'all'"> · 已按结果筛选，不代表整体命中率</span></span></div>
        <v-row dense class="mb-3">
          <v-col cols="6" md="2"><v-card variant="tonal"><v-card-text><div class="text-caption">筛选比赛 / 可计算</div><div class="text-h6 font-weight-bold">{{ number(summary.total) }} / {{ number(summary.eligible) }}</div><div class="text-caption text-medium-emphasis">实际平局 {{ percentage(summary.drawRate) }}</div></v-card-text></v-card></v-col>
          <v-col cols="6" md="2"><v-card variant="tonal" color="primary"><v-card-text><div class="text-caption">历史命中率（碰撞）</div><div class="text-h6 font-weight-bold">{{ percentage(summary.precision) }}</div><div class="text-caption">命中 {{ number(summary.hits) }} / 选平 {{ number(summary.selected) }}</div></v-card-text></v-card></v-col>
          <v-col cols="6" md="2"><v-card variant="tonal"><v-card-text><div class="text-caption">筛选覆盖率</div><div class="text-h6 font-weight-bold">{{ percentage(summary.coverage) }}</div><div class="text-caption text-medium-emphasis">选平 / 可计算比赛</div></v-card-text></v-card></v-col>
          <v-col cols="6" md="2"><v-card variant="tonal"><v-card-text><div class="text-caption">平局找回率</div><div class="text-h6 font-weight-bold">{{ percentage(summary.recall) }}</div><div class="text-caption text-medium-emphasis">命中 / 可计算的实际平局</div></v-card-text></v-card></v-col>
          <v-col cols="6" md="2"><v-card variant="tonal" color="error"><v-card-text><div class="text-caption">误判 / 漏选</div><div class="text-h6 font-weight-bold">{{ number(summary.falsePositives) }} / {{ number(summary.falseNegatives) }}</div><div class="text-caption">选平未平 / 平局未选</div></v-card-text></v-card></v-col>
          <v-col cols="6" md="2"><v-card variant="tonal"><v-card-text><div class="text-caption">概率误差 Brier</div><div class="text-h6 font-weight-bold">{{ decimal(summary.brier) }}</div><div class="text-caption text-medium-emphasis">越低越好 · 比较时看相同样本</div></v-card-text></v-card></v-col>
        </v-row>

        <v-card v-if="report.model" class="mb-4" variant="outlined">
          <v-card-text class="d-flex flex-wrap align-center ga-4 py-3">
            <v-chip size="small" :color="report.model.calibrated ? 'success' : 'warning'" variant="tonal">{{ report.model.calibrated ? '历史样本已校准' : '基础模型 / 待校准' }}</v-chip>
            <span class="text-body-2">训练 {{ number(report.model.trainingSamples) }} 场</span><span class="text-body-2">校准 {{ number(report.model.calibrationSamples) }} 场</span><span class="text-body-2">测试 {{ number(report.model.testSamples) }} 场</span>
            <span v-if="report.model.message" class="text-caption text-medium-emphasis">{{ report.model.message }}</span>
          </v-card-text>
        </v-card>

        <v-expansion-panels class="mb-4" multiple>
          <v-expansion-panel title="已有数据来源与覆盖量" value="sources">
            <v-expansion-panel-text>
              <v-table density="compact"><thead><tr><th>已有数据来源</th><th>记录数</th><th>覆盖比赛</th><th>可用比赛</th><th>使用方式和限制</th></tr></thead><tbody><tr v-for="source in report.sources || []" :key="source.key"><td class="font-weight-medium">{{ source.label }}</td><td>{{ number(source.records) }}</td><td>{{ number(source.matches) }}</td><td>{{ number(source.usable) }}</td><td class="wrap-cell text-medium-emphasis">{{ source.notes || '—' }}</td></tr><tr v-if="!report.sources?.length"><td colspan="5" class="text-medium-emphasis">暂未返回来源覆盖统计</td></tr></tbody></v-table>
            </v-expansion-panel-text>
          </v-expansion-panel>
          <v-expansion-panel title="模型对比与数据贡献" value="comparisons">
            <v-expansion-panel-text>
              <div class="text-body-2 text-medium-emphasis mb-3">命中率 = 命中数 / 选平数，样本数表示该组可计算的全部比赛。不同数据组可能使用各自的历史时间拆分；横向比较概率误差时，请查看固定共同样本的 Brier 和场数。相关性不能直接证明数据的独立贡献。</div>
              <v-table density="compact">
                <thead><tr><th>模型 / 基准</th><th>该组样本</th><th>命中 / 选平</th><th>命中率</th><th>覆盖率</th><th>找回率</th><th>该组 Brier</th><th>固定共同样本</th><th>共同样本 Brier</th><th>时间拆分 / 说明</th></tr></thead>
                <tbody>
                  <tr v-for="model in report.comparisons || []" :key="model.key">
                    <td class="font-weight-medium">{{ model.label }}<v-chip v-if="model.exploratory" size="x-small" color="warning" variant="tonal" class="ml-2">探索</v-chip></td>
                    <td>{{ number(model.sample) }}</td><td>{{ number(model.hits) }} / {{ number(model.selected) }}</td><td>{{ percentage(model.precision) }}</td><td>{{ percentage(model.coverage) }}</td><td>{{ percentage(model.recall) }}</td><td>{{ decimal(model.brier) }}</td><td>{{ number(model.commonSample) }}</td><td>{{ decimal(model.commonBrier) }}</td>
                    <td class="wrap-cell text-caption"><div v-if="model.independentSplit" class="text-primary mb-1">该组自己的历史时间拆分</div>{{ model.notes || '—' }}</td>
                  </tr>
                  <tr v-if="!report.comparisons?.length"><td colspan="10">暂无可比较的模型样本</td></tr>
                </tbody>
              </v-table>
              <div v-if="report.contributions?.length" class="mt-5">
                <div class="text-subtitle-2 mb-2">现有分析数据的历史表现</div>
                <v-table density="compact"><thead><tr><th>数据 / 分析特征</th><th>覆盖样本</th><th>样本实际平局率</th><th>使用情况</th></tr></thead><tbody><tr v-for="item in report.contributions" :key="item.key"><td>{{ item.label }}</td><td>{{ number(item.sample) }}</td><td>{{ percentage(item.drawRate) }}</td><td class="wrap-cell text-caption">{{ item.notes }}</td></tr></tbody></v-table>
              </div>
            </v-expansion-panel-text>
          </v-expansion-panel>
          <v-expansion-panel v-if="report.signalsSummary?.length" title="已有分析信号碰撞" value="signals">
            <v-expansion-panel-text>
              <div class="text-body-2 text-medium-emphasis mb-3">复用已有分析的方向和数值，查看信号触发后实际是否平局。采集时点不完整的信号标为探索对照，不能视为赛前验证。</div>
              <v-table density="compact"><thead><tr><th>分析信号</th><th>触发样本</th><th>平局命中</th><th>选平命中率</th><th>触发样本平局率</th><th>使用口径</th></tr></thead><tbody><tr v-for="signal in report.signalsSummary" :key="signal.key"><td>{{ signal.label }}</td><td>{{ number(signal.sample) }}</td><td>{{ number(signal.hits) }}</td><td>{{ percentage(signal.precision) }}</td><td>{{ percentage(signal.drawRate) }}</td><td class="wrap-cell text-caption">{{ signal.notes }}</td></tr></tbody></v-table>
            </v-expansion-panel-text>
          </v-expansion-panel>
          <v-expansion-panel title="概率分箱与混淆矩阵" value="calibration">
            <v-expansion-panel-text>
              <v-row>
                <v-col cols="12" md="7">
                  <div class="text-subtitle-2 mb-2">预测概率与实际平局率</div>
                  <v-table density="compact"><thead><tr><th>概率区间</th><th>场数</th><th>平均预测</th><th>实际平局率</th><th style="min-width: 160px">预测 / 实际</th></tr></thead><tbody><tr v-for="bin in report.bins || []" :key="bin.from"><td>{{ bin.from }}–{{ bin.to }}%</td><td>{{ number(bin.count) }}</td><td>{{ percentage(bin.predicted) }}</td><td>{{ percentage(bin.actual) }}</td><td><v-progress-linear :model-value="bin.predicted || 0" color="primary" height="5" class="mb-1" /><v-progress-linear :model-value="bin.actual || 0" color="success" height="5" /></td></tr><tr v-if="!report.bins?.length"><td colspan="5">暂无分箱数据</td></tr></tbody></v-table>
                  <div class="text-caption text-medium-emphasis mt-2"><span class="text-primary">蓝色：模型预测</span> · <span class="text-success">绿色：实际平局率</span> · 样本少的分箱波动更大。</div>
                </v-col>
                <v-col cols="12" md="5">
                  <div class="text-subtitle-2 mb-2">选平阈值 {{ thresholdLabel }}</div>
                  <v-table density="comfortable" class="confusion-table"><thead><tr><th></th><th>实际平局</th><th>实际非平局</th></tr></thead><tbody><tr><th>模型选平</th><td class="text-success"><strong>{{ number(summary.hits) }}</strong><div class="text-caption">命中</div></td><td class="text-error"><strong>{{ number(summary.falsePositives) }}</strong><div class="text-caption">误判</div></td></tr><tr><th>模型未选</th><td class="text-warning"><strong>{{ number(summary.falseNegatives) }}</strong><div class="text-caption">漏选</div></td><td><strong>{{ number(summary.trueNegatives) }}</strong><div class="text-caption">正确排除</div></td></tr></tbody></v-table>
                  <div class="text-caption text-medium-emphasis mt-2">数据不足的比赛保留在历史列表中，不进入模型效果分母。</div>
                </v-col>
              </v-row>
            </v-expansion-panel-text>
          </v-expansion-panel>
        </v-expansion-panels>

        <v-card>
          <v-card-title class="d-flex flex-wrap align-center ga-2 text-subtitle-1"><v-icon size="20">mdi-table-search</v-icon>逐场历史碰撞明细<v-spacer /><span class="text-body-2 text-medium-emphasis">筛选后 {{ number(report.total) }} 场</span></v-card-title>
          <v-divider />
          <v-table density="compact" fixed-header height="600" class="history-table">
            <thead><tr><th>日期 / 联赛</th><th>主队 / 客队</th><th>比分</th><th>实际结果</th><th>模型概率</th><th>市场概率</th><th>是否选平</th><th>碰撞结果</th><th>分区</th><th>数据来源 / 限制</th></tr></thead>
            <tbody>
              <tr v-for="row in report.items || []" :key="row.matchId" class="clickable-row" @click="showDetails(row)">
                <td><div>{{ dateTime(row.matchTime || row.date) }}</div><div class="text-caption text-medium-emphasis">{{ row.league }}</div></td>
                <td><div>{{ row.home }}</div><div class="text-medium-emphasis">{{ row.guest }}</div></td>
                <td class="font-weight-bold">{{ row.homeScore }} : {{ row.awayScore }}</td>
                <td><v-chip :color="row.isDraw ? 'primary' : 'default'" variant="tonal" size="x-small">{{ row.isDraw ? '平局' : '非平局' }}</v-chip></td>
                <td class="font-weight-medium">{{ percentage(row.probability) }}<div v-if="row.rawProbability !== undefined && row.rawProbability !== null && row.rawProbability !== row.probability" class="text-caption text-medium-emphasis">原始 {{ percentage(row.rawProbability) }}</div></td>
                <td>{{ percentage(row.marketProbability) }}</td>
                <td>{{ row.probability === null ? '—' : row.selected ? '选平' : '未选' }}</td>
                <td><v-chip :color="outcomeColor(row)" variant="tonal" size="small">{{ outcomeLabel(row) }}</v-chip></td>
                <td><v-chip :color="row.split === 'test' ? 'primary' : 'default'" size="x-small" variant="outlined">{{ splitLabel(row.split) }}</v-chip></td>
                <td class="source-cell"><div class="text-caption">{{ (row.sources || []).join(' · ') || '无有效来源' }}</div><div v-if="row.warnings?.length" class="text-caption text-warning">{{ row.warnings[0] }}<span v-if="row.warnings.length > 1"> 等 {{ row.warnings.length }} 项</span></div></td>
              </tr>
              <tr v-if="!report.items?.length"><td colspan="10" class="text-center py-12 text-medium-emphasis">{{ summary.total ? '当前筛选条件下没有比赛' : '尚无历史回算数据，请点击“重建全量历史”读取已有比赛和分析数据。' }}</td></tr>
            </tbody>
          </v-table>
          <v-divider />
          <div class="d-flex flex-wrap align-center justify-end pa-3 ga-3"><v-select v-model="pageSize" :items="[30, 50, 100]" label="每页" density="compact" variant="outlined" hide-details style="max-width: 100px" @update:model-value="fetchHistory(true)" /><v-pagination v-model="page" :length="pages" :total-visible="6" density="compact" :disabled="busy" @update:model-value="fetchHistory()" /></div>
        </v-card>
        <div v-if="report.notes?.length" class="mt-4 text-caption text-medium-emphasis"><div v-for="note in report.notes" :key="note" class="mb-1">{{ note }}</div></div>
      </template>
      <v-card v-else-if="!loading" class="text-center pa-12"><v-icon size="48" color="grey">mdi-database-search</v-icon><div class="text-body-1 mt-3">尚未加载历史验证数据</div><v-btn variant="text" color="primary" class="mt-2" @click="fetchHistory()">重新加载</v-btn></v-card>
    </template>

    <template v-else>
      <v-alert type="info" variant="tonal" density="compact" class="mb-4">未来赛程使用当前模型生成概率。赛前冻结记录用于后续真实检验，与历史探索回算分开统计。</v-alert>
      <v-card class="mb-4"><v-card-text class="d-flex flex-wrap align-center ga-3"><v-text-field v-model="scheduleDate" type="date" label="赛程日期（北京时间）" density="compact" variant="outlined" hide-details class="filter-date" /><v-btn color="primary" :loading="upcomingLoading" @click="fetchUpcoming">加载赛程</v-btn><v-btn variant="text" :disabled="upcomingLoading" @click="resetUpcomingDate">今天</v-btn><v-btn variant="outlined" :loading="action === 'generate'" :disabled="!!action || upcomingLoading" prepend-icon="mdi-lock-outline" @click="freezeUpcoming">冻结未开赛预测</v-btn><v-spacer /><span v-if="upcoming" class="text-body-2 text-medium-emphasis">{{ upcoming.summary.total }} 场 · 候选 {{ upcoming.summary.candidates }} · 已冻结 {{ upcoming.summary.frozen }} · 赛程时区 {{ upcoming.scheduleTimezone }}</span></v-card-text></v-card>
      <v-card><v-table density="comfortable"><thead><tr><th>开赛时间 / 联赛</th><th>比赛</th><th>当前概率</th><th>进球模型</th><th>市场概率</th><th>筛选 / 状态</th><th>依据和限制</th></tr></thead><tbody><tr v-for="match in upcoming?.items || []" :key="match.matchId"><td>{{ dateTime(match.matchTime) }}<div class="text-caption text-medium-emphasis">{{ match.league }}</div></td><td>{{ match.home }}<div class="text-medium-emphasis">{{ match.guest }}</div></td><td class="font-weight-bold">{{ percentage(match.probability) }}</td><td>{{ percentage(match.modelProbability) }}</td><td>{{ percentage(match.marketProbability) }}</td><td><v-chip :color="match.candidate ? 'primary' : 'default'" size="small" variant="tonal">{{ match.candidate ? '候选' : match.probability === null ? '数据不足' : '未入选' }}</v-chip><div class="text-caption mt-1">{{ match.frozen ? '已冻结' : match.status }}</div></td><td class="wrap-cell text-caption"><div>{{ (match.reasons || []).join('；') }}</div><div class="text-warning">{{ (match.warnings || []).join('；') }}</div></td></tr><tr v-if="!upcoming?.items?.length"><td colspan="7" class="text-center text-medium-emphasis py-12">{{ upcomingLoading ? '正在加载赛程…' : '该日期暂无比赛' }}</td></tr></tbody></v-table></v-card>
    </template>

    <v-dialog v-model="detailsOpen" max-width="1050" scrollable>
      <v-card v-if="selectedRow">
        <v-card-title class="d-flex align-center">{{ selectedRow.home }} {{ selectedRow.homeScore }} : {{ selectedRow.awayScore }} {{ selectedRow.guest }}<v-spacer /><v-btn icon="mdi-close" variant="text" size="small" @click="detailsOpen = false" /></v-card-title>
        <v-card-subtitle>{{ selectedRow.league }} · {{ dateTime(selectedRow.matchTime) }} · {{ selectedRow.matchId }}</v-card-subtitle>
        <v-card-text>
          <v-row><v-col cols="4"><div class="text-caption">模型平局概率</div><div class="text-h5">{{ percentage(selectedRow.probability) }}</div></v-col><v-col cols="4"><div class="text-caption">市场平局概率</div><div class="text-h5">{{ percentage(selectedRow.marketProbability) }}</div></v-col><v-col cols="4"><div class="text-caption">历史碰撞</div><v-chip :color="outcomeColor(selectedRow)" class="mt-1">{{ outcomeLabel(selectedRow) }}</v-chip></v-col></v-row>
          <div class="d-flex flex-wrap ga-2 mt-4"><v-chip size="small" variant="outlined">{{ splitLabel(selectedRow.split) }}区</v-chip><v-chip v-if="selectedRow.exploratory" size="small" color="warning" variant="tonal">历史探索回算</v-chip><v-chip v-if="selectedRow.outOfTime" size="small" color="primary" variant="tonal">按时间留出</v-chip></div>
          <v-divider class="my-4" />
          <template v-if="detailFormRows.length">
            <div class="text-subtitle-2 mb-2">本场模型使用的近期战绩</div>
            <v-table density="compact" class="mb-2">
              <thead><tr><th>统计窗口</th><th>有效场数</th><th>平局场数</th><th>平局率</th><th>平滑平局率</th><th>场均进球</th><th>场均失球</th><th>总进球 ≤ 2 占比</th></tr></thead>
              <tbody><tr v-for="entry in detailFormRows" :key="entry.label"><td>{{ entry.label }}</td><td>{{ number(entry.form.sample) }}</td><td>{{ number(entry.form.draws) }}</td><td>{{ percentage(entry.form.drawRate) }}</td><td>{{ percentage(entry.form.smoothedDrawRate) }}</td><td>{{ goals(entry.form.goalsFor) }}</td><td>{{ goals(entry.form.goalsAgainst) }}</td><td>{{ percentage(entry.form.lowScoreRate) }}</td></tr></tbody>
            </v-table>
            <div class="text-caption text-medium-emphasis mb-4">只使用本场开始前已经结束的比赛；样本不足 10 场时按实际场数统计。整体窗口与对应主客场窗口分别计算，平滑值向历史基准收缩。</div>
          </template>
          <v-row v-if="selectedRow.homeExpectedGoals != null || selectedRow.awayExpectedGoals != null || selectedRow.baselineProbability != null || selectedRow.baselineSample !== undefined" class="mb-2">
            <v-col v-if="selectedRow.homeExpectedGoals != null || selectedRow.awayExpectedGoals != null" cols="12" sm="6"><v-card variant="tonal"><v-card-text><div class="text-caption">主队 / 客队预期进球</div><div class="text-h6">{{ goals(selectedRow.homeExpectedGoals) }} / {{ goals(selectedRow.awayExpectedGoals) }}</div><div class="text-caption text-medium-emphasis">由赛前攻防输入估计，用于计算同比分概率。</div></v-card-text></v-card></v-col>
            <v-col v-if="selectedRow.baselineProbability != null || selectedRow.baselineSample !== undefined" cols="12" sm="6"><v-card variant="tonal"><v-card-text><div class="text-caption">历史平局基准</div><div class="text-h6">{{ percentage(selectedRow.baselineProbability) }}<span class="text-body-2 ml-2">{{ number(selectedRow.baselineSample) }} 场</span></div><div class="text-caption text-medium-emphasis">本场时点之前的历史基准，用于收缩和概率对照；不足时的来源请看数据限制。</div></v-card-text></v-card></v-col>
          </v-row>
          <v-alert v-if="selectedRow.oddsProvenance" type="info" variant="tonal" density="compact" class="mb-4">盘口与赔率来源：{{ selectedRow.oddsProvenance }}</v-alert>
          <template v-if="selectedRow.groupProbabilities && Object.keys(selectedRow.groupProbabilities).length"><div class="text-subtitle-2 mb-2">模型分组概率与该组分区</div><v-table density="compact" class="mb-4"><thead><tr><th>模型分组</th><th>平局概率</th><th>该组历史分区</th></tr></thead><tbody><tr v-for="(probability, group) in selectedRow.groupProbabilities" :key="group"><td>{{ modelLabel(String(group)) }}</td><td>{{ percentage(probability) }}</td><td>{{ selectedRow.groupPartitions?.[group] ? splitLabel(selectedRow.groupPartitions[group]) : '—' }}</td></tr></tbody></v-table></template>
          <template v-if="selectedRow.signals?.length"><div class="text-subtitle-2 mb-2">已有分析信号</div><v-table density="compact" class="mb-4"><thead><tr><th>分析</th><th>市场</th><th>方向</th><th>数值</th></tr></thead><tbody><tr v-for="signal in selectedRow.signals" :key="signal.key"><td>{{ signal.label }}</td><td>{{ signal.market }}</td><td>{{ signal.direction }}</td><td>{{ signal.value ?? '—' }}</td></tr></tbody></v-table></template>
          <div class="text-subtitle-2 mb-2">来源</div><v-chip v-for="source in selectedRow.sources || []" :key="source" size="small" variant="outlined" class="mr-2 mb-2">{{ source }}</v-chip>
          <div class="text-subtitle-2 mt-4 mb-2">数据限制</div><v-alert v-for="warning in selectedRow.warnings || []" :key="warning" type="warning" variant="tonal" density="compact" class="mb-2">{{ warning }}</v-alert><div v-if="!selectedRow.warnings?.length" class="text-body-2 text-medium-emphasis">本条未返回额外限制，请同时参考全局数据来源说明。</div>
          <div class="text-caption text-medium-emphasis mt-4">历史探索回算的模型概率与真实赛前冻结记录应分别验证。</div>
        </v-card-text>
      </v-card>
    </v-dialog>
  </div>
</template>

<style scoped>
.filter-date { flex: 0 1 180px; min-width: 160px; }
.filter-select { flex: 0 1 210px; min-width: 170px; }
.filter-search { flex: 1 1 210px; min-width: 190px; }
.threshold-control { flex: 1 1 260px; max-width: 420px; min-width: 220px; }
.history-table :deep(th), .history-table :deep(td) { white-space: nowrap; }
.source-cell { min-width: 240px; max-width: 350px; white-space: normal !important; }
.wrap-cell { min-width: 180px; max-width: 480px; white-space: normal !important; }
.clickable-row { cursor: pointer; }
.clickable-row:hover { background: rgba(var(--v-theme-primary), .045); }
.confusion-table :deep(td) { text-align: center; }
</style>
