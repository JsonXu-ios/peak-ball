<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { generateAnalysisRuleSnapshot, getAnalysisRuleSnapshotData, getAnalysisRuleSnapshotInfo, getCrawlerLogDetail, syncCrawlerData } from '@/api'

interface RuleSnapshotRule {
  value: string
  sample: number
  bothCorrect: number
  rate: number
}

interface RuleSnapshotRow {
  label: string
  sample: number
  rules: RuleSnapshotRule[]
}

const loading = ref(false)
const result = ref<Record<string, unknown> | null>(null)
const errorMsg = ref('')
const activeLog = ref<Record<string, unknown> | null>(null)
const ruleSnapshotInfo = ref<Record<string, unknown> | null>(null)
const ruleGenerating = ref(false)
const ruleGenerateResult = ref<Record<string, unknown> | null>(null)
const ruleGenerateError = ref('')
const ruleSnapshotData = ref<Record<string, unknown> | null>(null)
const ruleSnapshotRows = computed(() => {
  const rows = ruleSnapshotData.value?.commonRows
  return Array.isArray(rows) ? rows as RuleSnapshotRow[] : []
})
let pollTimer: number | undefined

const form = reactive({
  type: 'all',
  date: '',
  match_id: '',
  async: true,
  force: false,
})

const activeDetails = computed(() => parseDetails(String(activeLog.value?.details || '')))
const activeProgress = computed(() => {
  const total = Number(activeLog.value?.items_count || activeDetails.value.items_count || 0)
  if (!total) return 0
  const done = Number(activeLog.value?.success_count || 0) + Number(activeLog.value?.failed_count || 0) + Number(activeDetails.value.skipped_count || 0)
  return Math.min(100, Math.round((done / total) * 100))
})

const taskTypes = [
  { title: '比赛列表', value: 'match_list' },
  { title: '历史战绩', value: 'history' },
  { title: '联赛排名/杯赛积分榜', value: 'rank' },
  { title: '欧赔数据', value: 'odds_euro' },
  { title: '盘口数据', value: 'odds_pankou' },
  { title: '阶段赔率盘口刷新', value: 'odds_refresh' },
  { title: '全量同步 (包含所有)', value: 'all' },
]

const detailTypes = ['history', 'rank', 'odds_euro', 'odds_pankou', 'odds_refresh']
const syncGuides: Record<string, string> = {
  match_list: '拉比赛基础信息、比分状态和队徽。日期留空时走 today/next，会包含今日和明日；填日期时只拉那一天。',
  history: '填比赛ID时只拉单场历史；不填比赛ID时按日期批量拉当天比赛历史。',
  rank: '填比赛ID时只拉单场排名/积分榜；不填比赛ID时按日期批量拉当天比赛排名/积分榜。',
  odds_euro: '填比赛ID时只拉单场欧赔；不填比赛ID时按日期批量拉当天比赛欧赔。',
  odds_pankou: '填比赛ID时只拉单场盘口；不填比赛ID时按日期批量拉当天比赛盘口。',
  odds_refresh: '阶段性更新赔率专用：强制刷新欧赔、亚盘和大小球，不重拉历史和排名。',
  all: '启动后推荐先跑：比赛列表 -> 历史 -> 排名/积分榜 -> 欧赔 -> 盘口。日期留空时包含今日和明日，填日期时只跑那一天。',
}
const showDateInput = computed(() => form.type === 'match_list' || form.type === 'all' || detailTypes.includes(form.type))
const showMatchInput = computed(() => detailTypes.includes(form.type))
const currentSyncGuide = computed(() => syncGuides[form.type] || '')
const workflowSteps = [
  '启动项目后：先跑“全量同步”，让比赛列表、历史、排名、欧赔、盘口按顺序入库。',
  '临近开赛或赛中：跑“阶段赔率盘口刷新”，只更新欧赔、亚盘和大小球。',
  '比分或赛程变化：跑“比赛列表”，刷新基础信息、比分状态和队徽。',
  '补某一天：填日期后跑“全量同步”，只处理该日期。',
]

function useStartupPreset() {
  Object.assign(form, { type: 'all', date: '', match_id: '', async: true, force: false })
}

function useMatchListPreset() {
  Object.assign(form, { type: 'match_list', date: '', match_id: '', async: true, force: false })
}

function useOddsRefreshPreset() {
  Object.assign(form, { type: 'odds_refresh', date: '', match_id: '', async: true, force: true })
}

async function handleSync() {
  loading.value = true
  result.value = null
  errorMsg.value = ''

  try {
    const payload: Record<string, unknown> = {
      type: form.type,
      async: form.async,
      force: form.force,
    }
    if (form.date) payload.date = form.date
    if (form.match_id) payload.match_id = form.match_id

    const { data } = await syncCrawlerData(payload as { type: string; date?: string; match_id?: string; async?: boolean; force?: boolean })
    result.value = data
    const logId = Number((data.result as Record<string, unknown> | undefined)?.log_id)
    if (logId) startPollingLog(logId)
  } catch (err: unknown) {
    const error = err as { response?: { data?: { error?: string } } }
    errorMsg.value = error.response?.data?.error || '同步失败'
  } finally {
    loading.value = false
  }
}

async function startPollingLog(logId: number) {
  if (pollTimer) window.clearInterval(pollTimer)
  await fetchLog(logId)
  pollTimer = window.setInterval(async () => {
    await fetchLog(logId)
    if (activeLog.value?.status !== 'running' && pollTimer) {
      window.clearInterval(pollTimer)
      pollTimer = undefined
    }
  }, 3000)
}

async function fetchLog(logId: number) {
  const { data } = await getCrawlerLogDetail(logId)
  activeLog.value = data
}

async function fetchRuleSnapshotInfo() {
  try {
    const [infoResponse, dataResponse] = await Promise.all([
      getAnalysisRuleSnapshotInfo(),
      getAnalysisRuleSnapshotData(),
    ])
    ruleSnapshotInfo.value = infoResponse.data
    ruleSnapshotData.value = dataResponse.data
  } catch {
    ruleSnapshotInfo.value = null
    ruleSnapshotData.value = null
  }
}

async function handleGenerateRuleSnapshot() {
  ruleGenerating.value = true
  ruleGenerateResult.value = null
  ruleGenerateError.value = ''
  try {
    const { data } = await generateAnalysisRuleSnapshot()
    ruleGenerateResult.value = data
    await fetchRuleSnapshotInfo()
  } catch (err: unknown) {
    const error = err as { response?: { data?: { error?: string } } }
    ruleGenerateError.value = error.response?.data?.error || '生成规则快照失败'
  } finally {
    ruleGenerating.value = false
  }
}

function percentText(value: unknown) {
  const numeric = Number(value)
  if (!Number.isFinite(numeric)) return '-'
  return `${Math.round(numeric * 100)}%`
}

function parseDetails(details: string) {
  if (!details) return {} as Record<string, unknown>
  try {
    return JSON.parse(details) as Record<string, unknown>
  } catch {
    return {} as Record<string, unknown>
  }
}

onMounted(fetchRuleSnapshotInfo)

onBeforeUnmount(() => {
  if (pollTimer) window.clearInterval(pollTimer)
})
</script>

<template>
  <div>
    <h2 class="text-h5 font-weight-bold mb-4">数据同步</h2>

    <v-row>
      <v-col cols="12" md="6">
        <v-card>
          <v-card-title>
            <v-icon class="mr-2">mdi-sync</v-icon>
            手动触发数据同步
          </v-card-title>
          <v-card-text>
            <v-alert type="info" variant="tonal" density="compact" class="mb-4">
              手动数据同步是临时立即执行一次爬虫，不会保存成任务，也不会自动定时运行。它适合补爬某一天、临时重抓某一场，或验证接口是否正常。
            </v-alert>

            <div class="d-flex flex-wrap ga-2 mb-4">
              <v-btn variant="tonal" color="primary" prepend-icon="mdi-playlist-check" @click="useStartupPreset">启动后全量</v-btn>
              <v-btn variant="tonal" color="success" prepend-icon="mdi-calendar-sync" @click="useMatchListPreset">刷新赛程比分</v-btn>
              <v-btn variant="tonal" color="warning" prepend-icon="mdi-chart-line-variant" @click="useOddsRefreshPreset">刷新赔率盘口</v-btn>
            </div>

            <v-select
              v-model="form.type"
              :items="taskTypes"
              label="同步类型"
              class="mb-3"
            />

            <v-alert type="info" variant="tonal" density="compact" class="mb-3">
              {{ currentSyncGuide }}
            </v-alert>

            <v-text-field
              v-if="showDateInput"
              v-model="form.date"
              type="date"
              label="日期 (可选)"
              prepend-inner-icon="mdi-calendar"
              clearable
              hint="留空时 match_list/all/odds_refresh 覆盖今日+明日；选择日期时只处理那一天"
              persistent-hint
              class="mb-3"
            />

            <v-text-field
              v-if="showMatchInput"
              v-model="form.match_id"
              label="比赛ID (可选)"
              placeholder="例如 498150656"
              hint="填了只同步这一场；不填则按上面的日期批量同步"
              persistent-hint
              class="mb-3"
            />

            <v-switch
              v-model="form.async"
              label="异步执行"
              hint="开启后按钮会立即返回，实际进度看下方运行监控或爬虫日志"
              persistent-hint
              color="primary"
              class="mb-4"
            />

            <v-switch
              v-model="form.force"
              label="强制重爬已有明细"
              hint="默认会跳过已有历史/排名/欧赔/盘口；阶段赔率盘口刷新会自动按强制更新处理"
              persistent-hint
              color="warning"
              class="mb-4"
            />

            <v-btn
              color="primary"
              size="large"
              block
              :loading="loading"
              prepend-icon="mdi-rocket-launch"
              @click="handleSync"
            >
              {{ form.async ? '异步同步' : '同步执行' }}
            </v-btn>
          </v-card-text>
        </v-card>
      </v-col>

      <v-col cols="12" md="6">
        <v-card>
          <v-card-title>
            <v-icon class="mr-2">mdi-information</v-icon>
            说明
          </v-card-title>
          <v-card-text>
            <v-list density="compact">
              <v-list-item v-for="step in workflowSteps" :key="step">
                <template #prepend>
                  <v-icon color="primary" size="small">mdi-check-circle-outline</v-icon>
                </template>
                <v-list-item-title>{{ step }}</v-list-item-title>
              </v-list-item>

              <v-divider class="my-3" />

              <v-list-item v-if="ruleSnapshotInfo">
                <template #prepend>
                  <v-icon color="purple" size="small">mdi-file-chart-outline</v-icon>
                </template>
                <v-list-item-title>历史规律规则池</v-list-item-title>
                <v-list-item-subtitle class="text-wrap">
                  {{ ruleSnapshotInfo.absolute_path || ruleSnapshotInfo.path }}
                </v-list-item-subtitle>
                <v-list-item-subtitle>
                  状态：{{ ruleSnapshotInfo.exists ? '文件存在' : '文件不存在，将使用空快照/实时计算兜底' }}
                </v-list-item-subtitle>
              </v-list-item>

              <div class="px-4 pb-2">
                <v-btn
                  color="purple"
                  variant="tonal"
                  prepend-icon="mdi-database-sync"
                  :loading="ruleGenerating"
                  block
                  @click="handleGenerateRuleSnapshot"
                >
                  生成历史规律规则池
                </v-btn>
                <v-alert v-if="ruleGenerateResult" type="success" variant="tonal" density="compact" class="mt-3">
                  已生成：完场样本 {{ ruleGenerateResult.total || 0 }}，路径 {{ ruleGenerateResult.path || ruleSnapshotInfo?.absolute_path || '-' }}
                </v-alert>
                <v-alert v-if="ruleGenerateError" type="error" variant="tonal" density="compact" class="mt-3">
                  {{ ruleGenerateError }}
                </v-alert>
              </div>

              <v-list-item v-if="ruleSnapshotData">
                <template #prepend>
                  <v-icon color="purple" size="small">mdi-table-eye</v-icon>
                </template>
                <v-list-item-title>规则池数据</v-list-item-title>
                <v-list-item-subtitle>
                  样本 {{ ruleSnapshotData.total || 0 }}，更新时间 {{ ruleSnapshotData.updatedAt || '-' }}
                </v-list-item-subtitle>
              </v-list-item>

              <div v-if="ruleSnapshotRows.length" class="px-4 pb-2">
                <v-expansion-panels variant="accordion" density="compact">
                  <v-expansion-panel v-for="row in ruleSnapshotRows" :key="row.label">
                    <v-expansion-panel-title>
                      {{ row.label }} · 双中样本 {{ row.sample }} · 规则 {{ row.rules?.length || 0 }} 条
                    </v-expansion-panel-title>
                    <v-expansion-panel-text>
                      <v-table density="compact">
                        <thead>
                          <tr>
                            <th class="text-left">规则</th>
                            <th class="text-right">双中</th>
                            <th class="text-right">样本</th>
                            <th class="text-right">命中率</th>
                          </tr>
                        </thead>
                        <tbody>
                          <tr v-for="rule in row.rules" :key="`${row.label}-${rule.value}`">
                            <td>{{ rule.value }}</td>
                            <td class="text-right">{{ rule.bothCorrect }}</td>
                            <td class="text-right">{{ rule.sample }}</td>
                            <td class="text-right font-weight-bold">{{ percentText(rule.rate) }}</td>
                          </tr>
                        </tbody>
                      </v-table>
                    </v-expansion-panel-text>
                  </v-expansion-panel>
                </v-expansion-panels>
              </div>
              <v-alert v-else-if="ruleSnapshotData" type="info" variant="tonal" density="compact" class="mx-4 mb-2">
                当前规则池还没有规则数据，点击“生成历史规律规则池”后会在这里显示。
              </v-alert>

              <v-divider class="my-3" />

              <v-list-item>
                <template #prepend>
                  <v-icon color="primary" size="small">mdi-soccer</v-icon>
                </template>
                <v-list-item-title>比赛列表</v-list-item-title>
                <v-list-item-subtitle>从 vipc.cn 拉取指定日期的比赛列表</v-list-item-subtitle>
              </v-list-item>
              <v-list-item>
                <template #prepend>
                  <v-icon color="info" size="small">mdi-chart-timeline</v-icon>
                </template>
                <v-list-item-title>历史战绩</v-list-item-title>
                <v-list-item-subtitle>获取指定比赛的双方历史交锋记录</v-list-item-subtitle>
              </v-list-item>
              <v-list-item>
                <template #prepend>
                  <v-icon color="secondary" size="small">mdi-format-list-numbered</v-icon>
                </template>
                <v-list-item-title>联赛排名/杯赛积分榜</v-list-item-title>
                <v-list-item-subtitle>调用 rank 接口，联赛返回排名，杯赛返回积分榜</v-list-item-subtitle>
              </v-list-item>
              <v-list-item>
                <template #prepend>
                  <v-icon color="success" size="small">mdi-chart-line</v-icon>
                </template>
                <v-list-item-title>欧赔数据</v-list-item-title>
                <v-list-item-subtitle>获取各博彩公司的欧赔数据</v-list-item-subtitle>
              </v-list-item>
              <v-list-item>
                <template #prepend>
                  <v-icon color="warning" size="small">mdi-chart-bar</v-icon>
                </template>
                <v-list-item-title>盘口数据</v-list-item-title>
                <v-list-item-subtitle>获取亚盘和大小球数据</v-list-item-subtitle>
              </v-list-item>
              <v-list-item>
                <template #prepend>
                  <v-icon color="warning" size="small">mdi-chart-line-variant</v-icon>
                </template>
                <v-list-item-title>阶段赔率盘口刷新</v-list-item-title>
                <v-list-item-subtitle>强制刷新欧赔、亚盘和大小球，适合临近开赛和赛中反复执行</v-list-item-subtitle>
              </v-list-item>
              <v-list-item>
                <template #prepend>
                  <v-icon color="error" size="small">mdi-sync-circle</v-icon>
                </template>
                <v-list-item-title>全量同步</v-list-item-title>
                <v-list-item-subtitle>拉取比赛列表后逐个获取详细数据（耗时较长）</v-list-item-subtitle>
              </v-list-item>
            </v-list>

            <v-divider class="my-3" />

            <v-alert type="info" variant="tonal" density="compact">
              <strong>注意:</strong> 全量同步每个请求间有 1.5 秒延迟（反爬策略），可能需要较长时间。建议使用异步模式。
            </v-alert>
          </v-card-text>
        </v-card>

        <!-- Result Card -->
        <v-card v-if="result" class="mt-4">
          <v-card-title>
            <v-icon color="success" class="mr-2">mdi-check-circle</v-icon>
            执行结果
          </v-card-title>
          <v-card-text>
            <pre class="text-body-2">{{ JSON.stringify(result, null, 2) }}</pre>
          </v-card-text>
        </v-card>

        <v-card v-if="activeLog" class="mt-4">
          <v-card-title>
            <v-icon color="primary" class="mr-2">mdi-radar</v-icon>
            运行监控 #{{ activeLog.id }}
          </v-card-title>
          <v-card-text>
            <div class="d-flex align-center justify-space-between mb-2">
              <span>状态：{{ activeLog.status }}</span>
              <span>{{ activeLog.success_count || 0 }} 成功 / {{ activeLog.failed_count || 0 }} 失败 / {{ activeDetails.skipped_count || 0 }} 跳过</span>
            </div>
            <v-progress-linear :model-value="activeProgress" height="10" rounded color="primary" />
            <div class="text-caption mt-3">当前：{{ activeDetails.current || '-' }}</div>
            <v-list v-if="Array.isArray(activeDetails.notes)" density="compact" class="mt-2">
              <v-list-item v-for="note in activeDetails.notes" :key="String(note)" :title="String(note)" />
            </v-list>
          </v-card-text>
        </v-card>

        <v-alert v-if="errorMsg" type="error" variant="tonal" class="mt-4" closable>
          {{ errorMsg }}
        </v-alert>
      </v-col>
    </v-row>
  </div>
</template>
