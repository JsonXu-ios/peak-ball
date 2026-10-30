<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { getResearchOverview, getResearchSyncStatus, syncResearchStore } from '@/api/researchStore'
import type { ResearchOverview, ResearchSyncStatus } from '@/types/researchStore'
import { formatBeijingDateTime } from '@/utils/date'
import ResearchSchemaTable from './ResearchSchemaTable.vue'
import ResearchDataInspector from './ResearchDataInspector.vue'

const overview = ref<ResearchOverview | null>(null)
const status = ref<ResearchSyncStatus | null>(null)
const loading = ref(false)
const starting = ref(false)
const error = ref('')
const statusError = ref('')
const tableSearch = ref('')
const expandedTables = ref<string[]>([])
let timer: ReturnType<typeof setTimeout> | undefined
let disposed = false
let request = 0

const layerLabels: Record<string, string> = {
  raw: '原始采集', ingest: '原始采集', source: '原始采集',
  canonical: '标准数据', entities: '标准数据', entity: '标准数据', market: '标准数据', results: '赛果标签', labels: '赛果标签',
  audit: '清洗与审计', quality: '清洗与审计',
  feature: '特征定义与快照', features: '特征定义与快照',
  prediction: '预测与结算', predictions: '预测与结算',
  experiment: '数据集与评估', evaluation: '数据集与评估', datasets: '数据集与评估', metadata: '版本管理',
}
const running = computed(() => status.value?.state === 'running')
const progress = computed(() => status.value?.total ? Math.min(100, status.value.processed / status.value.total * 100) : 0)
const cards = computed(() => {
  const s = overview.value?.summary
  return [
    { label: '标准比赛', value: s?.matches, icon: 'mdi-soccer' },
    { label: '原始采集事件', value: s?.ingestEvents, icon: 'mdi-download-network-outline' },
    { label: '盘口报价记录', value: s?.quotes, icon: 'mdi-chart-timeline-variant' },
    { label: '特征定义', value: s?.featureDefinitions, icon: 'mdi-function-variant' },
    { label: '特征快照', value: s?.featureSnapshots, icon: 'mdi-camera-outline' },
    { label: '预测记录', value: s?.predictions, icon: 'mdi-target' },
  ]
})
const groups = computed(() => {
  const grouped = new Map<string, { label: string; tables: number; rows: number }>()
  for (const table of overview.value?.tables || []) {
    const current = grouped.get(table.layer) || { label: layerLabels[table.layer] || table.layer, tables: 0, rows: 0 }
    current.tables++
    current.rows += table.rows
    grouped.set(table.layer, current)
  }
  return [...grouped.values()]
})
const tables = computed(() => {
  const query = tableSearch.value.trim().toLowerCase()
  return (overview.value?.tables || []).filter((table) => !query
    || `${table.name} ${table.label} ${table.description} ${table.columns.map((column) => `${column.name} ${column.comment}`).join(' ')}`.toLowerCase().includes(query))
})
const tableRows = computed(() => (overview.value?.tables || []).reduce((sum, table) => sum + table.rows, 0))
const syncColor = computed(() => status.value?.state === 'failed' ? 'error' : status.value?.state === 'completed' ? 'success' : 'info')
const syncLabel = computed(() => ({ idle: '等待同步', running: '正在同步', completed: '同步完成', failed: '同步失败' }[status.value?.state || 'idle']))

function number(value: number | undefined) {
  return typeof value === 'number' && Number.isFinite(value) ? value.toLocaleString('zh-CN') : '—'
}
function date(value: string | undefined) {
  return value ? formatBeijingDateTime(value, '暂无') : '暂无'
}
function message(cause: unknown) {
  const response = cause as { response?: { data?: { error?: string; message?: string }; status?: number }; message?: string }
  return response.response?.data?.error || response.response?.data?.message
    || (response.response?.status === 404 ? '当前服务尚未加载研究库接口，请在加载更新后重试。' : response.message || '无法读取研究库，请检查服务连接后重试。')
}
function schedulePoll() {
  if (timer) clearTimeout(timer)
  if (!disposed && running.value) timer = setTimeout(poll, 1500)
}
async function poll() {
  const wasRunning = running.value
  try {
    const response = await getResearchSyncStatus()
    if (disposed) return
    status.value = response.data
    statusError.value = ''
    if (wasRunning && !running.value) await loadOverview()
  } catch (cause) {
    if (disposed) return
    statusError.value = message(cause)
  }
  schedulePoll()
}
async function loadOverview() {
  const current = ++request
  loading.value = true
  error.value = ''
  try {
    const response = await getResearchOverview()
    if (disposed || current !== request) return
    if (!response.data.database || !Array.isArray(response.data.tables)) throw new Error('研究库返回的数据结构不完整，请检查服务版本。')
    overview.value = response.data
  } catch (cause) {
    if (!disposed && current === request) error.value = message(cause)
  } finally {
    if (!disposed && current === request) loading.value = false
  }
}
async function refresh() {
  await Promise.all([loadOverview(), poll()])
}
async function sync() {
  starting.value = true
  statusError.value = ''
  try {
    const response = await syncResearchStore()
    if (disposed) return
    status.value = response.data
    schedulePoll()
    if (!running.value) await loadOverview()
  } catch (cause) {
    if (!disposed) statusError.value = message(cause)
  } finally {
    if (!disposed) starting.value = false
  }
}
function expandAll() {
  expandedTables.value = tables.value.map((table) => table.name)
}

onMounted(refresh)
onBeforeUnmount(() => {
  disposed = true
  request++
  if (timer) clearTimeout(timer)
})
</script>

<template>
  <div class="research-store">
    <div class="d-flex flex-wrap align-center justify-space-between ga-3 mb-5">
      <div>
        <h1 class="text-h5 font-weight-bold"><v-icon color="primary" class="mr-2">mdi-database-search</v-icon>研究与特征库</h1>
        <div class="text-body-2 text-medium-emphasis mt-2">查看独立库的数据全貌：原始采集、清洗审计、特征快照、预测与结果评估。</div>
      </div>
      <div class="d-flex ga-2">
        <v-btn prepend-icon="mdi-refresh" variant="outlined" :loading="loading" @click="refresh">刷新概览</v-btn>
        <v-btn prepend-icon="mdi-database-sync" color="primary" :loading="starting || running" :disabled="starting || running" @click="sync">同步本地数据</v-btn>
      </div>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}<div v-if="overview" class="text-caption mt-1">下方保留上次读取结果，更新时间见库信息。</div></v-alert>
    <v-alert v-if="statusError" type="warning" variant="tonal" class="mb-4">{{ statusError }}</v-alert>
    <v-alert v-if="!error && !overview && loading" type="info" variant="tonal" class="mb-4">正在读取独立库结构与统计…</v-alert>

    <v-card v-if="status && (status.state !== 'idle' || status.message)" variant="tonal" class="mb-4 pa-4">
      <div class="d-flex flex-wrap align-center ga-3"><v-chip :color="syncColor" size="small">{{ syncLabel }}</v-chip><span>{{ status.message }}</span><span v-if="status.total" class="text-caption text-medium-emphasis">{{ number(status.processed) }} / {{ number(status.total) }}</span></div>
      <v-progress-linear v-if="running" class="mt-3" :model-value="progress" :indeterminate="!status.total" color="primary" rounded />
      <div v-if="status.error" class="text-error mt-2">{{ status.error }}</div>
      <div class="text-caption text-medium-emphasis mt-2">开始：{{ date(status.startedAt) }}<span v-if="status.finishedAt" class="ml-4">完成：{{ date(status.finishedAt) }}</span></div>
    </v-card>

    <template v-if="overview">
      <div class="d-flex flex-wrap align-center ga-3 text-caption mb-4">
        <v-chip size="small" prepend-icon="mdi-database">{{ overview.database }}</v-chip>
        <span>结构版本 {{ overview.schemaVersion }}</span><span>{{ overview.tables.length }} 张表 · {{ number(tableRows) }} 条记录</span><span>概览时间：{{ date(overview.generatedAt) }}</span>
      </div>

      <v-row dense class="mb-4">
        <v-col v-for="card in cards" :key="card.label" cols="6" md="4" xl="2">
          <v-card variant="tonal" class="pa-4 h-100"><div class="text-caption text-medium-emphasis"><v-icon size="18" class="mr-1">{{ card.icon }}</v-icon>{{ card.label }}</div><div class="text-h5 font-weight-bold mt-2">{{ number(card.value) }}</div></v-card>
        </v-col>
      </v-row>

      <v-card variant="outlined" class="pa-4 mb-5">
        <div class="text-subtitle-1 font-weight-bold mb-3">数据链路</div>
        <div class="data-flow">
          <template v-for="(step, index) in ['原始采集', '标准化与审计', '特征定义与快照', '预测封存', '赛果结算与评估']" :key="step">
            <v-icon v-if="index" color="primary" size="20" class="flow-arrow">mdi-arrow-right</v-icon><div class="flow-step"><span class="flow-number">{{ index + 1 }}</span>{{ step }}</div>
          </template>
        </div>
        <div class="text-caption text-medium-emphasis mt-3">赛前快照保留当时输入，预测记录保留当时选择；补采数据形成新版本，赛后追加真实结果用于核验。</div>
        <div class="d-flex flex-wrap ga-2 mt-3"><v-chip v-for="group in groups" :key="group.label" size="small" variant="tonal">{{ group.label }} · {{ group.tables }} 表</v-chip></div>
      </v-card>

      <v-row dense class="mb-5">
        <v-col cols="12" md="7">
          <v-card variant="outlined" class="pa-4 h-100">
            <div class="text-subtitle-1 font-weight-bold mb-3">数据质量与可用范围</div>
            <div class="quality-grid">
              <div><div class="text-caption text-medium-emphasis">赛前封存快照</div><div class="text-h6 text-success">{{ number(overview.quality.liveSnapshots) }}</div></div>
              <div><div class="text-caption text-medium-emphasis">历史重建快照</div><div class="text-h6 text-warning">{{ number(overview.quality.reconstructedSnapshots) }}</div></div>
              <div><div class="text-caption text-medium-emphasis">累计未处理审计记录</div><div class="text-h6">{{ number(overview.quality.openIssues) }}</div></div>
            </div>
            <div class="text-caption text-medium-emphasis mt-3">赛前封存说明输入在开赛前已保存，不保证源报价时间已确认；历史重建用于探索和回看，采集时间未知不能补认为赛前已知。</div>
            <div class="text-caption text-medium-emphasis mt-2">审计记录按批次累计，同一比赛可能有多条记录；逐项查看默认最新完整批次，记录数不等于异常比赛场数。</div>
            <div class="d-flex flex-wrap ga-2 mt-3"><v-chip v-for="(value, severity) in overview.quality.bySeverity" :key="severity" size="small" variant="tonal">{{ severity }}：{{ number(value) }}</v-chip></div>
          </v-card>
        </v-col>
        <v-col cols="12" md="5">
          <v-card variant="outlined" class="pa-4 h-100">
            <div class="text-subtitle-1 font-weight-bold mb-3">特征与实验版本</div>
            <div v-for="featureSet in overview.featureSets" :key="featureSet.version" class="feature-version mb-2"><span>{{ featureSet.version }}</span><v-chip size="x-small" color="primary">{{ number(featureSet.features) }} 项特征</v-chip></div>
            <div v-if="!overview.featureSets.length" class="text-body-2 text-medium-emphasis">尚未登记特征版本。</div>
            <v-divider class="my-3" /><div class="text-body-2">数据集 {{ number(overview.summary.datasets) }} · 评估记录 {{ number(overview.summary.evaluations) }}</div>
            <div class="text-caption text-medium-emphasis mt-2">模型、特征和筛选规则分别记录版本；同步数据不会自动训练或替换现行模型。</div>
          </v-card>
        </v-col>
      </v-row>

      <ResearchDataInspector :feature-sets="overview.featureSets" :revision="overview.generatedAt" />

      <div class="d-flex flex-wrap align-center justify-space-between ga-3 mb-3">
        <div><h2 class="text-h6 font-weight-bold">完整数据结构</h2><div class="text-caption text-medium-emphasis">共 {{ overview.tables.length }} 张表，展开查看全部字段、索引及关联。记录数来自当前独立库。</div></div>
        <div class="d-flex flex-wrap align-center ga-2"><v-text-field v-model="tableSearch" label="查找表、字段或说明" prepend-inner-icon="mdi-magnify" variant="outlined" density="compact" hide-details class="schema-search" /><v-btn size="small" variant="text" @click="expandAll">全部展开</v-btn><v-btn size="small" variant="text" @click="expandedTables = []">收起</v-btn></div>
      </div>
      <v-expansion-panels v-model="expandedTables" multiple variant="accordion">
        <v-expansion-panel v-for="table in tables" :key="table.name" :value="table.name">
          <v-expansion-panel-title>
            <div class="table-heading"><div class="table-title"><span class="font-weight-bold">{{ table.label || table.name }}</span><code>{{ table.name }}</code></div><div class="text-caption text-medium-emphasis">{{ layerLabels[table.layer] || table.layer }} · {{ table.columns.length }} 个字段 · {{ number(table.rows) }} 条</div></div>
          </v-expansion-panel-title>
          <v-expansion-panel-text>
            <ResearchSchemaTable :table="table" />
          </v-expansion-panel-text>
        </v-expansion-panel>
      </v-expansion-panels>
      <v-alert v-if="!tables.length" type="info" variant="tonal" class="mt-4">{{ overview.tables.length ? '没有匹配的表或字段，请调整搜索。' : '独立库尚未返回表结构，请检查初始化状态。' }}</v-alert>
    </template>
    <v-card v-else-if="!loading" variant="outlined" class="pa-8 text-center text-medium-emphasis">{{ error ? '研究库概览暂不可用。加载更新并确认独立库已初始化后，点击刷新概览。' : '尚未读取到研究库数据。' }}</v-card>
  </div>
</template>

<style scoped>
.data-flow { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; }
.flow-step { display: flex; gap: 9px; align-items: center; padding: 10px 14px; border: 1px solid rgba(var(--v-theme-on-surface), .12); border-radius: 8px; font-size: 14px; }
.flow-number { width: 24px; height: 24px; border-radius: 50%; display: inline-flex; align-items: center; justify-content: center; color: rgb(var(--v-theme-primary)); background: rgba(var(--v-theme-primary), .12); font-weight: 700; }
.quality-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 14px; }
.feature-version { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; font-size: 13px; }
.schema-search { width: 260px; max-width: 100%; }
.table-heading { display: flex; flex-wrap: wrap; flex: 1; align-items: center; justify-content: space-between; gap: 10px; padding-right: 18px; }
.table-title { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; }
code { font-size: 12px; color: rgb(var(--v-theme-primary)); }
@media (max-width: 700px) {
  .flow-arrow { display: none; }
  .quality-grid { grid-template-columns: 1fr; }
  .schema-search { width: 100%; }
}
</style>
