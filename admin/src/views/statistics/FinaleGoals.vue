<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getFinaleGoalPredictions } from '@/api'

/** 命中：true=中、false=没中、null=不适用（走盘/盘口缺失），不计入命中率 */
type Hit = boolean | null

interface GoalPrediction {
  match_id: string
  date: string
  match_time: string
  league: string
  home: string
  guest: string
  home_logo?: string
  guest_logo?: string
  /** split_under=反向·热度判小球；split_over=反向·热度判大球 */
  combo: string
  combo_label: string
  /** 恒为 over（两种组合都买大球） */
  pick: string
  /** 如「判大球 3.60」 */
  exp_goals: string
  /** 如「判小球 3.50」 */
  heat_goals: string
  /** 赛前那一刻的大小球即时盘口 */
  ou_line: string
  /** archive=赛前存档（真前瞻）；recompute=事后用当前盘口回算（仅参考） */
  source?: 'archive' | 'recompute'
  settled?: boolean
  home_score?: number
  guest_score?: number
  /** over / under，走盘为空 */
  result?: string
  snapshot_at?: string
  hit_pick?: Hit
}

interface AccuracyColumn {
  key: string
  label: string
  range: string
  matched: number
  hit: number
  miss: number
  accuracy: number
}

interface Report {
  generated_at: string
  horizon_days: number
  upcoming_total: number
  mode: 'upcoming' | 'history'
  start: string
  end: string
  predictions: GoalPrediction[]
  /** 赛前存档的命中率（真前瞻实测） */
  accuracy: { settled_total: number; columns: AccuracyColumn[] }
  /** 事后回算的命中率，单独一套，绝不与 accuracy 合并 */
  recompute_accuracy: { settled_total: number; columns: AccuracyColumn[] }
  warning?: string
}

const RANGES = [
  { value: 'upcoming', label: '未来待赛' },
  { value: 'yesterday', label: '昨天' },
  { value: '3d', label: '近3天' },
  { value: '7d', label: '近7天' },
  { value: '30d', label: '近30天' },
  { value: 'all', label: '全部' },
  { value: 'custom', label: '自定义' },
] as const

type RangeValue = (typeof RANGES)[number]['value']

const loading = ref(false)
const report = ref<Report | null>(null)
const error = ref('')
const range = ref<RangeValue>('upcoming')
const customStart = ref('')
const customEnd = ref('')

function ymd(offsetDays: number): string {
  const date = new Date()
  date.setDate(date.getDate() + offsetDays)
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

function rangeParams(value: RangeValue) {
  switch (value) {
    case 'upcoming':
      return undefined
    case 'yesterday':
      return { mode: 'history', start: ymd(-1), end: ymd(-1) }
    case '3d':
      return { mode: 'history', start: ymd(-2), end: ymd(0) }
    case '7d':
      return { mode: 'history', start: ymd(-6), end: ymd(0) }
    case '30d':
      return { mode: 'history', start: ymd(-29), end: ymd(0) }
    case 'custom':
      return { mode: 'history', start: customStart.value, end: customEnd.value }
    default:
      return { mode: 'history' }
  }
}

const isHistory = computed(() => report.value?.mode === 'history')
const rows = computed(() => report.value?.predictions ?? [])
const underHeatRows = computed(() => rows.value.filter((row) => row.combo === 'split_under'))
const overHeatRows = computed(() => rows.value.filter((row) => row.combo === 'split_over'))
const settledRows = computed(() => rows.value.filter((row) => row.settled))
const archiveRows = computed(() => rows.value.filter((row) => row.source === 'archive'))
const recomputeRows = computed(() => rows.value.filter((row) => row.source === 'recompute'))
const accuracyColumns = computed(() => report.value?.accuracy?.columns ?? [])
const recomputeColumns = computed(() => report.value?.recompute_accuracy?.columns ?? [])
const recomputeTotal = computed(() => report.value?.recompute_accuracy?.settled_total ?? 0)
/**
 * 日期分组行的 colspan。固定 10 列：时间/联赛/主队/vs/客队/预测/组合/期望球数/
 * 球数热度/盘口；历史模式再多「来源」「比分」「结果」三列。数错会让分组行的
 * 背景色在中途断掉，加减列时务必同步改这里。
 */
const columnCount = computed(() => 10 + (isHistory.value ? 3 : 0))

const groupedByDate = computed(() => {
  const groups: Array<{ date: string; rows: GoalPrediction[] }> = []
  for (const row of rows.value) {
    const last = groups[groups.length - 1]
    if (last && last.date === row.date) last.rows.push(row)
    else groups.push({ date: row.date, rows: [row] })
  }
  return groups
})

function logoSrc(path?: string): string {
  return path || ''
}

/** 大小球提示按大/小着色：判大球=橙、判小球=蓝 */
function ouClass(text: string) {
  if (text.includes('判大球') || text === 'over') return 'text-warning'
  if (text.includes('判小球') || text === 'under') return 'text-info'
  return ''
}

function resultText(row: GoalPrediction) {
  if (!row.settled) return ''
  if (row.result === 'over') return '大球'
  if (row.result === 'under') return '小球'
  return '走盘'
}

/** ✓ 中、✗ 没中；走盘与未结算不渲染标记——不显示不等于没中 */
function hitMark(row: GoalPrediction): string {
  if (!row.settled || (row.hit_pick !== true && row.hit_pick !== false)) return ''
  return row.hit_pick ? '✓' : '✗'
}

function accuracyColor(accuracy: number, matched: number) {
  if (!matched) return 'default'
  if (accuracy >= 60) return 'success'
  if (accuracy >= 50) return 'warning'
  return 'error'
}

async function fetchReport() {
  loading.value = true
  error.value = ''
  try {
    const response = await getFinaleGoalPredictions(rangeParams(range.value))
    report.value = response.data as Report
  } catch (requestError) {
    const err = requestError as { response?: { data?: { error?: string } }; message?: string }
    error.value = err.response?.data?.error || err.message || '加载预测失败'
  } finally {
    loading.value = false
  }
}

function selectRange(value: RangeValue) {
  range.value = value
  fetchReport()
}

onMounted(() => fetchReport())
</script>

<template>
  <div>
    <div class="d-flex flex-wrap align-center mb-4 ga-3">
      <div>
        <div class="text-body-2 text-medium-emphasis">
          未来 {{ report?.horizon_days ?? 14 }} 天待赛比赛中，<b>期望球数与球数热度判到相反侧</b>的场次，<b>两种组合都买大球</b>，赛前自动存档、完赛后自动结算。
          期望球数=0.3×历史场均+0.7×近期场均，球数热度方向=<b>等权</b>综合均值（历史与近期各半）对盘口的方向——两者喂的是同一对样本、只是权重不同，这正是它们能判到相反侧的原因。
          <b>两队没有交锋记录的比赛整场剔除</b>，不参与预测。
          <b>反向·热度判小球</b>只取盘口 <b>3.75 以下</b>（3.75 本身剔除）；<b>反向·热度判大球</b>只剔除 <b>2.25 这一档</b>，其余盘口不限。
          走盘（总进球正好等于盘口）不计入命中率分母。
        </div>
      </div>
      <v-spacer />
      <v-btn :loading="loading" color="primary" prepend-icon="mdi-refresh" @click="fetchReport">刷新</v-btn>
    </div>

    <div class="d-flex flex-wrap align-center ga-3 mb-4">
      <v-btn-toggle
        :model-value="range"
        density="comfortable"
        color="primary"
        variant="outlined"
        mandatory
        class="flex-wrap"
        @update:model-value="selectRange"
      >
        <v-btn v-for="item in RANGES" :key="item.value" :value="item.value">{{ item.label }}</v-btn>
      </v-btn-toggle>

      <template v-if="range === 'custom'">
        <v-text-field
          v-model="customStart"
          type="date"
          label="起"
          density="compact"
          variant="outlined"
          hide-details
          style="max-width: 170px"
        />
        <span class="text-medium-emphasis">~</span>
        <v-text-field
          v-model="customEnd"
          type="date"
          label="止"
          density="compact"
          variant="outlined"
          hide-details
          style="max-width: 170px"
        />
        <v-btn :loading="loading" color="primary" variant="tonal" @click="fetchReport">查询</v-btn>
        <span class="text-caption text-medium-emphasis">起止填同一天=看某一天</span>
      </template>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-5">{{ error }}</v-alert>
    <v-alert v-if="report?.warning" type="warning" variant="tonal" class="mb-5">
      存档/结算未完成：{{ report.warning }}（预测列表本身不受影响）
    </v-alert>

    <template v-if="report">
      <v-row class="mb-1">
        <v-col cols="12" md="3">
          <v-card color="primary" variant="tonal">
            <v-card-text>
              <div class="text-body-2">{{ isHistory ? '区间内入选场次' : `待赛场次（${report.horizon_days}天内）` }}</div>
              <div class="text-h4 font-weight-bold mt-1">
                {{ (isHistory ? rows.length : report.upcoming_total).toLocaleString() }}
              </div>
              <div v-if="isHistory" class="text-caption text-medium-emphasis">
                已结算 {{ settledRows.length }} 场 · 存档 {{ archiveRows.length }} / 回算 {{ recomputeRows.length }}
              </div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="12" md="3">
          <v-card color="info" variant="tonal">
            <v-card-text>
              <div class="text-body-2">反向·热度判小球</div>
              <div class="text-h4 font-weight-bold mt-1">{{ underHeatRows.length }}</div>
              <div class="text-caption text-medium-emphasis">盘口 &lt; 3.75</div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="12" md="3">
          <v-card color="warning" variant="tonal">
            <v-card-text>
              <div class="text-body-2">反向·热度判大球</div>
              <div class="text-h4 font-weight-bold mt-1">{{ overHeatRows.length }}</div>
              <div class="text-caption text-medium-emphasis">仅剔除 2.25</div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="12" md="3">
          <v-card variant="tonal">
            <v-card-text>
              <div class="text-body-2 text-medium-emphasis">生成时间</div>
              <div class="text-subtitle-1 font-weight-medium mt-1">{{ report.generated_at }}</div>
              <div class="text-caption text-medium-emphasis">每次打开实时计算并结算</div>
            </v-card-text>
          </v-card>
        </v-col>
      </v-row>

      <v-card class="mb-5">
        <v-card-title class="pt-5 d-flex align-center flex-wrap ga-2">
          <span>买大球命中率</span>
          <v-chip color="primary" size="small" variant="tonal">
            已结算 {{ report.accuracy.settled_total.toLocaleString() }} 场
          </v-chip>
          <v-chip size="small" variant="text">
            {{ isHistory ? (report.start || report.end ? `${report.start || '最早'} ~ ${report.end || '最新'}` : '全部') : '全部存档（未来待赛不参与结算）' }}
          </v-chip>
        </v-card-title>
        <v-card-subtitle class="pb-2 text-wrap">
          <b>存档</b>=赛前写入、事后不可改，这才是真正的前瞻实测；<b>回算</b>=上线前的比赛没有赛前快照，用库里<b>当前</b>的盘口事后重算出来的，盘口早已被赛果消化过，只能当参考，两套数字分开统计、绝不合并。走盘不计入分母。
        </v-card-subtitle>
        <v-card-text>
          <div v-if="!report.accuracy.settled_total" class="text-medium-emphasis py-3">
            还没有已结算的<b>赛前存档</b>。存档从本功能上线后开始累积——等这批待赛比赛打完、结果拉回来，再打开本页就会自动结算。
          </div>
          <v-table v-else density="compact">
            <thead>
              <tr>
                <th>组合</th>
                <th>盘口区间</th>
                <th class="text-right">结算场次</th>
                <th class="text-right">命中</th>
                <th class="text-right">未中</th>
                <th class="text-right">命中率</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="column in accuracyColumns" :key="column.key">
                <td class="font-weight-medium">{{ column.label }}</td>
                <td class="text-caption text-medium-emphasis">{{ column.range }}</td>
                <td class="text-right">{{ column.matched.toLocaleString() }}</td>
                <td class="text-right">{{ column.hit }}</td>
                <td class="text-right">{{ column.miss }}</td>
                <td class="text-right">
                  <v-chip v-if="column.matched" :color="accuracyColor(column.accuracy, column.matched)" size="small" variant="tonal">
                    {{ column.accuracy.toFixed(2) }}%
                  </v-chip>
                  <span v-else class="text-medium-emphasis">-</span>
                </td>
              </tr>
            </tbody>
          </v-table>

          <template v-if="recomputeTotal">
            <div class="d-flex align-center ga-2 mt-6 mb-1">
              <span class="font-weight-bold">回算（事后重算，仅参考）</span>
              <v-chip color="warning" size="small" variant="tonal">{{ recomputeTotal.toLocaleString() }} 场</v-chip>
            </div>
            <div class="text-caption text-medium-emphasis mb-2">
              这批比赛没有赛前存档，信号是拿库里当前的盘口现算的。别把它当成实测成绩——真正能验证策略的只有上面那张表。
            </div>
            <v-table density="compact">
              <thead>
                <tr>
                  <th>组合</th>
                  <th>盘口区间</th>
                  <th class="text-right">结算场次</th>
                  <th class="text-right">命中</th>
                  <th class="text-right">未中</th>
                  <th class="text-right">命中率</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="column in recomputeColumns" :key="column.key">
                  <td class="font-weight-medium">{{ column.label }}</td>
                  <td class="text-caption text-medium-emphasis">{{ column.range }}</td>
                  <td class="text-right">{{ column.matched.toLocaleString() }}</td>
                  <td class="text-right">{{ column.hit }}</td>
                  <td class="text-right">{{ column.miss }}</td>
                  <td class="text-right">
                    <v-chip v-if="column.matched" :color="accuracyColor(column.accuracy, column.matched)" size="small" variant="tonal">
                      {{ column.accuracy.toFixed(2) }}%
                    </v-chip>
                    <span v-else class="text-medium-emphasis">-</span>
                  </td>
                </tr>
              </tbody>
            </v-table>
          </template>
        </v-card-text>
      </v-card>

      <v-card>
        <v-card-text class="pa-0">
          <div v-if="!rows.length" class="text-medium-emphasis pa-6">
            {{ isHistory ? '该区间没有入选的比赛。' : '未来窗口内没有符合条件的比赛。' }}
          </div>
          <v-table v-else density="comfortable" class="finale-table">
            <thead>
              <tr>
                <th>时间</th>
                <th v-if="isHistory">来源</th>
                <th>联赛</th>
                <th>主队</th>
                <th class="text-center">vs</th>
                <th>客队</th>
                <th v-if="isHistory" class="text-center">比分</th>
                <th class="text-center">预测</th>
                <th>组合</th>
                <th>期望球数</th>
                <th>球数热度</th>
                <th class="text-right">盘口</th>
                <th v-if="isHistory">结果</th>
              </tr>
            </thead>
            <tbody>
              <template v-for="group in groupedByDate" :key="group.date">
                <tr class="date-row">
                  <td :colspan="columnCount" class="font-weight-bold">
                    {{ group.date }}
                    <span class="text-caption text-medium-emphasis ml-2">{{ group.rows.length }} 场</span>
                  </td>
                </tr>
                <tr v-for="p in group.rows" :key="p.match_id">
                  <td class="text-no-wrap">{{ p.match_time || p.date }}</td>
                  <td v-if="isHistory" class="text-no-wrap">
                    <v-chip :color="p.source === 'archive' ? 'primary' : 'warning'" size="x-small" variant="tonal">
                      {{ p.source === 'archive' ? '存档' : '回算' }}
                    </v-chip>
                  </td>
                  <td class="text-no-wrap league-cell">{{ p.league || '-' }}</td>
                  <td class="font-weight-medium">
                    <span class="team-cell">
                      <span class="team-logo">
                        <img v-if="logoSrc(p.home_logo)" :src="logoSrc(p.home_logo)" alt="" loading="lazy" @error="($event.target as HTMLImageElement).style.display = 'none'" />
                      </span>
                      {{ p.home }}
                    </span>
                  </td>
                  <td class="text-center text-medium-emphasis">vs</td>
                  <td class="font-weight-medium">
                    <span class="team-cell">
                      <span class="team-logo">
                        <img v-if="logoSrc(p.guest_logo)" :src="logoSrc(p.guest_logo)" alt="" loading="lazy" @error="($event.target as HTMLImageElement).style.display = 'none'" />
                      </span>
                      {{ p.guest }}
                    </span>
                  </td>
                  <td v-if="isHistory" class="text-center text-no-wrap font-weight-bold">
                    {{ p.settled ? `${p.home_score} - ${p.guest_score}` : '-' }}
                  </td>
                  <td class="text-center text-no-wrap">
                    <v-chip color="warning" size="small" variant="flat">买大球</v-chip>
                  </td>
                  <td class="text-no-wrap">
                    <v-chip size="x-small" variant="tonal" :color="p.combo === 'split_under' ? 'info' : 'warning'">
                      {{ p.combo_label }}
                    </v-chip>
                  </td>
                  <td class="text-no-wrap" :class="ouClass(p.exp_goals)">{{ p.exp_goals || '-' }}</td>
                  <td class="text-no-wrap" :class="ouClass(p.heat_goals)">{{ p.heat_goals || '-' }}</td>
                  <td class="text-right text-no-wrap">{{ p.ou_line || '-' }}</td>
                  <td v-if="isHistory" class="text-no-wrap">
                    <span v-if="p.settled" :class="ouClass(p.result || '')">{{ resultText(p) }}</span>
                    <span v-else class="text-medium-emphasis">未结算</span>
                    <span v-if="hitMark(p)" class="ml-1 font-weight-bold" :class="p.hit_pick ? 'text-success' : 'text-error'">
                      {{ hitMark(p) }}
                    </span>
                  </td>
                </tr>
              </template>
            </tbody>
          </v-table>
        </v-card-text>
      </v-card>
    </template>
  </div>
</template>

<style scoped>
.finale-table th {
  white-space: nowrap;
}
.date-row td {
  background: rgba(var(--v-theme-primary), 0.06);
}
.team-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.team-logo {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  flex-shrink: 0;
  border-radius: 50%;
  background: rgba(var(--v-border-color), 0.12);
  overflow: hidden;
}
.team-logo img {
  width: 100%;
  height: 100%;
  object-fit: contain;
}
.league-cell {
  max-width: 140px;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
