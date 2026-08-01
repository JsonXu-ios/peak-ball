<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getFinalePredictions } from '@/api'

/** 逐列命中：true=中、false=没中、null=不适用（信号为空/走盘），不计入命中率 */
type Hit = boolean | null

interface Prediction {
  match_id: string
  date: string
  match_time: string
  league: string
  home: string
  guest: string
  home_logo?: string
  guest_logo?: string
  /** home / away（只有主推≥70% 的场次会下发） */
  pick: string
  /** 主推提示：方向+概率文本，如「客胜 72.3%」，无数据为 '' */
  sig_base: string
  /** 买小球推荐·超出±0.75（口径同基础统计 #33）：'买小球' 或 ''。仅展示 */
  goal_pick: string
  /** 买小球推荐·±0.75 带内（口径同基础统计 #32 常规段）。与上一列互斥 */
  goal_pick_mid: string
  /** 亚盘/大小球即时盘口，无数据为 '' */
  ah_line: string
  ou_line: string
  trade_pick: string
  sim_pick: string
  /** archive=赛前存档（真前瞻）；recompute=事后用当前赔率回算（仅参考） */
  source?: 'archive' | 'recompute'
  /** 以下仅历史行有值 */
  settled?: boolean
  home_score?: number
  guest_score?: number
  result?: string
  snapshot_at?: string
  hits?: Record<string, Hit>
}

interface AccuracyColumn {
  key: string
  label: string
  matched: number
  hit: number
  miss: number
  accuracy: number
}

interface Accuracy {
  settled_total: number
  columns: AccuracyColumn[]
}

interface Report {
  generated_at: string
  horizon_days: number
  upcoming_total: number
  mode: 'upcoming' | 'history'
  start: string
  end: string
  predictions: Prediction[]
  /** 赛前存档的命中率（真前瞻实测） */
  accuracy: Accuracy
  /** 事后回算的命中率，单独一套，绝不与 accuracy 合并 */
  recompute_accuracy: Accuracy
  warning?: string
}

/** 区间快捷键。upcoming=未来待赛（默认）；其余都查存档 */
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
      // 起止都留空就是全部；只填一头也允许（另一头不限）。
      return { mode: 'history', start: customStart.value, end: customEnd.value }
    default:
      return { mode: 'history' }
  }
}

const isHistory = computed(() => report.value?.mode === 'history')
const rows = computed(() => report.value?.predictions ?? [])
const homePicks = computed(() => rows.value.filter((row) => row.pick === 'home'))
const awayPicks = computed(() => rows.value.filter((row) => row.pick === 'away'))
const settledRows = computed(() => rows.value.filter((row) => row.settled))
const archiveRows = computed(() => rows.value.filter((row) => row.source === 'archive'))
const recomputeRows = computed(() => rows.value.filter((row) => row.source === 'recompute'))
const accuracyColumns = computed(() => report.value?.accuracy?.columns ?? [])
const recomputeColumns = computed(() => report.value?.recompute_accuracy?.columns ?? [])
const recomputeTotal = computed(() => report.value?.recompute_accuracy?.settled_total ?? 0)

/**
 * 有已完赛的场次就显示比分/赛果两列。未来待赛模式下今天的比赛也全留在列表里，
 * 踢完的那些同样要能看到结果，所以这两列不能只挂在历史模式上。
 */
const showResultColumns = computed(() => isHistory.value || rows.value.some((row) => row.settled))
/**
 * 表格总列数。固定 12 列：时间/联赛/主队/VS/客队/预测/主推/大小球超出带/大小球带内/
 * 亚盘/大小球/盈亏提示；历史模式多「来源」列，有完赛场次时再多「比分」「赛果」两列。
 * 数错会让日期分组行的背景色在中途断掉，加减列时务必同步改这里。
 */
const columnCount = computed(() => 12 + (isHistory.value ? 1 : 0) + (showResultColumns.value ? 2 : 0))

/** 按日期分组（接口已按开赛时间升序返回） */
const groupedByDate = computed(() => {
  const groups: Array<{ date: string; rows: Prediction[] }> = []
  for (const row of rows.value) {
    const last = groups[groups.length - 1]
    if (last && last.date === row.date) last.rows.push(row)
    else groups.push({ date: row.date, rows: [row] })
  }
  return groups
})

function pickColor(pick: string) {
  return pick === 'home' ? 'error' : 'success'
}

function pickLabel(pick: string) {
  return pick === 'home' ? '买主胜' : '买客胜'
}

function logoSrc(path?: string): string {
  return path || ''
}

/** 按方向着色：偏主队=红、偏客队=绿（含赢盘方向），其余默认色 */
function sigClass(text: string) {
  if (text.startsWith('主胜') || text.startsWith('主队')) return 'text-error'
  if (text.startsWith('客胜') || text.startsWith('客队')) return 'text-success'
  return ''
}

/** 取某列的命中结果；未结算或不适用返回 null */
function hitOf(row: Prediction, key: string): Hit {
  if (!row.settled) return null
  const value = row.hits?.[key]
  return value === true || value === false ? value : null
}

/**
 * 命中标记：✓ 中、✗ 没中。不适用（走盘、信号为空）与未结算一律返回空串、
 * 不渲染任何标记——口径和命中率分母一致，不显示不等于没中。
 */
function hitMark(row: Prediction, key: string): string {
  const hit = hitOf(row, key)
  if (hit === null) return ''
  return hit ? '✓' : '✗'
}

function hitMarkClass(row: Prediction, key: string): string {
  return hitOf(row, key) ? 'text-success' : 'text-error'
}

function resultLabel(result?: string) {
  if (result === 'home') return '主胜'
  if (result === 'away') return '客胜'
  if (result === 'draw') return '平局'
  return ''
}

/**
 * 赛果着色：预测错的那些单独用紫色实心，扫一眼就能挑出来。
 * 本页红/绿已经被「偏主队/偏客队」占死，错判必须换个色，否则会跟方向混在一起。
 */
function resultChip(row: Prediction) {
  return hitOf(row, 'pick') === false
    ? { color: 'purple', variant: 'flat' as const }
    : { color: 'success', variant: 'tonal' as const }
}

/**
 * 大小球推荐着色：打出的用青色实心挑出来，没打出的压成灰色；还没结算的按方向上色
 *（判大球=橙、判小球=蓝，与明细表、H5 的大小球配色一致）。
 * 这一列不进逐列命中率表（本页对它只展示不结算），颜色只是让历史里一眼看出准不准。
 */
function goalPickChip(row: Prediction, key: 'goal_pick' | 'goal_pick_mid') {
  const hit = hitOf(row, key)
  if (hit === true) return { color: 'teal', variant: 'flat' as const }
  if (hit === false) return { color: 'grey', variant: 'tonal' as const }
  return { color: row[key] === '买大球' ? 'warning' : 'info', variant: 'flat' as const }
}

function accuracyColor(accuracy: number, matched: number) {
  if (!matched) return 'default'
  if (accuracy >= 60) return 'success'
  if (accuracy >= 50) return 'primary'
  return 'warning'
}

async function fetchReport() {
  loading.value = true
  error.value = ''
  try {
    const { data } = await getFinalePredictions(rangeParams(range.value))
    report.value = data as Report
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
    <h2 class="text-h5 font-weight-bold mb-4">终章 · 比赛预测与前瞻实测</h2>

    <div class="d-flex flex-wrap align-center mb-4 ga-3">
      <div>
        <div class="text-body-2 text-medium-emphasis mt-1">
          今天起 {{ report?.horizon_days ?? 14 }} 天内命中「前端主推≥70%」信号的场次，赛前自动存档；比赛完赛后（拉回结果即可）自动逐列结算命中。
          <b>今天的比赛全部保留</b>，已开赛/已完赛的照样列出并显示比分与对错，不会踢一场少一场（存档仍严格按开赛时间冻结，前瞻实测不受影响）。
          <span class="text-error font-weight-bold">红色=偏主队</span>、<span class="text-success font-weight-bold">绿色=偏客队</span>；主推概率、亚盘/大小球盘口与交易/模拟盈亏推荐仅随行展示，不参与预测。两列<b>买小球</b>用同一个差值 =<b> 大小球盘口 − 球数综合均值</b>（历史场均与近期场均等权），按差值落在哪一段分开显示、<b>互斥</b>，一场比赛最多填一列，<b>只推小球——判到大球的场次两列都留空</b>：<b>超出±0.75</b> 列走【完赛基础统计】#33 的反向判断（差值 ≤−0.75，即均值高出盘口 0.75 球以上）；<b>±0.75内</b> 列走 #32 的常规判断（差值为正，即均值低于盘口）。本页只展示、不计入命中率。历史模式下：<b>赛果</b>列预测错的用<span class="text-purple font-weight-bold">紫色</span>标出；大小球推荐<b>打出</b>的用<span class="text-teal font-weight-bold">青色</span>、没打出的压成灰色，未结算的按方向上色（<span class="text-warning font-weight-bold">橙=买大</span>、<span class="text-info font-weight-bold">蓝=买小</span>）。
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
              <div class="text-body-2">{{ isHistory ? '区间内存档场次' : `窗口内比赛（今天起${report.horizon_days}天）` }}</div>
              <div class="text-h4 font-weight-bold mt-1">
                {{ (isHistory ? rows.length : report.upcoming_total).toLocaleString() }}
              </div>
              <div v-if="isHistory" class="text-caption text-medium-emphasis">
                已结算 {{ settledRows.length }} 场 · 存档 {{ archiveRows.length }} / 回算 {{ recomputeRows.length }}
              </div>
              <div v-else class="text-caption text-medium-emphasis">
                其中有预测 {{ rows.length }} 场（已完赛 {{ settledRows.length }}）
              </div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="12" md="3">
          <v-card color="error" variant="tonal">
            <v-card-text>
              <div class="text-body-2">买主胜</div>
              <div class="text-h4 font-weight-bold mt-1">{{ homePicks.length }}</div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="12" md="3">
          <v-card color="success" variant="tonal">
            <v-card-text>
              <div class="text-body-2">买客胜</div>
              <div class="text-h4 font-weight-bold mt-1">{{ awayPicks.length }}</div>
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
          <span>逐列命中率</span>
          <v-chip color="primary" size="small" variant="tonal">
            已结算 {{ report.accuracy.settled_total.toLocaleString() }} 场
          </v-chip>
          <v-chip size="small" variant="text">
            {{ isHistory ? (report.start || report.end ? `${report.start || '最早'} ~ ${report.end || '最新'}` : '全部') : '全部存档（未来待赛不参与结算）' }}
          </v-chip>
        </v-card-title>
        <v-card-subtitle class="pb-2 text-wrap">
          <b>存档</b>=赛前写入、事后不可改，这才是真正的前瞻实测；<b>回算</b>=上线前的比赛没有赛前快照，用库里<b>当前</b>的赔率盘口事后重算出来的，赔率早已被赛果消化过，只能当参考，两套数字分开统计、绝不合并。空信号（页面显示 -）与走盘不计入分母。「预测」列与「主推」列口径相同（预测就是主推≥70% 推出来的），所以只列一行。
        </v-card-subtitle>
        <v-card-text>
          <div v-if="!report.accuracy.settled_total" class="text-medium-emphasis py-3">
            还没有已结算的<b>赛前存档</b>。存档从本功能上线后开始累积——等这批待赛比赛打完、结果拉回来，再打开本页就会自动结算。
          </div>
          <v-table v-else density="compact">
            <thead>
              <tr>
                <th>信号列</th>
                <th class="text-right">结算场次</th>
                <th class="text-right">命中</th>
                <th class="text-right">未中</th>
                <th class="text-right">命中率</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="column in accuracyColumns" :key="column.key">
                <td class="font-weight-medium">{{ column.label }}</td>
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
              这批比赛没有赛前存档，信号是拿库里当前的赔率盘口现算的。别把它当成实测成绩——真正能验证策略的只有上面那张表。
            </div>
            <v-table density="compact">
              <thead>
                <tr>
                  <th>信号列</th>
                  <th class="text-right">结算场次</th>
                  <th class="text-right">命中</th>
                  <th class="text-right">未中</th>
                  <th class="text-right">命中率</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="column in recomputeColumns" :key="column.key">
                  <td class="font-weight-medium">{{ column.label }}</td>
                  <td class="text-right">{{ column.matched.toLocaleString() }}</td>
                  <td class="text-right">{{ column.hit }}</td>
                  <td class="text-right">{{ column.miss }}</td>
                  <td class="text-right">
                    <v-chip v-if="column.matched" size="small" variant="tonal" color="warning">
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
        <v-card-text>
          <div v-if="!rows.length" class="text-medium-emphasis text-center py-10">
            {{ isHistory ? '该区间内没有主推≥70% 的比赛（既没有赛前存档，回算也没算出符合条件的场次）。' : '当前没有符合信号的待赛比赛。' }}
          </div>
          <v-table v-else density="comfortable" class="finale-table">
            <thead>
              <tr>
                <th>时间</th>
                <th v-if="isHistory">来源</th>
                <th>联赛</th>
                <th>主队</th>
                <th class="text-center">VS</th>
                <th>客队</th>
                <th v-if="showResultColumns" class="text-center">比分</th>
                <th class="text-center">预测</th>
                <th v-if="showResultColumns" class="text-center">赛果</th>
                <th>主推</th>
                <th>买小球·超出±0.75</th>
                <th>买小球·±0.75内</th>
                <th class="text-right">亚盘</th>
                <th class="text-right">大小球</th>
                <th>盈亏提示</th>
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
                <tr
                  v-for="p in group.rows"
                  :key="p.match_id"
                  :class="p.pick === 'home' ? 'row-home' : p.pick === 'away' ? 'row-away' : ''"
                >
                  <td class="text-no-wrap">{{ p.match_time || p.date }}</td>
                  <td v-if="isHistory" class="text-no-wrap">
                    <v-chip
                      :color="p.source === 'archive' ? 'primary' : 'warning'"
                      size="x-small"
                      variant="tonal"
                      :title="p.source === 'archive' ? `赛前存档，快照于 ${p.snapshot_at}` : '无赛前存档，用当前赔率事后回算，仅供参考'"
                    >
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
                  <td v-if="showResultColumns" class="text-center text-no-wrap font-weight-bold">
                    <span v-if="p.settled">{{ p.home_score }} - {{ p.guest_score }}</span>
                    <span v-else class="text-medium-emphasis font-weight-regular">未开赛</span>
                  </td>
                  <td class="text-center">
                    <v-chip v-if="p.pick" :color="pickColor(p.pick)" variant="flat" size="large" class="font-weight-bold px-5">
                      {{ pickLabel(p.pick) }}
                    </v-chip>
                    <span v-else class="text-medium-emphasis">-</span>
                  </td>
                  <td v-if="showResultColumns" class="text-center text-no-wrap">
                    <v-chip
                      v-if="p.settled && resultLabel(p.result)"
                      v-bind="resultChip(p)"
                      size="small"
                      class="font-weight-bold"
                      :title="hitOf(p, 'pick') === false ? '预测错了' : ''"
                    >
                      {{ resultLabel(p.result) }}
                    </v-chip>
                    <span v-else class="text-medium-emphasis">-</span>
                  </td>
                  <td class="text-no-wrap">
                    <span v-if="p.sig_base" :class="sigClass(p.sig_base)">{{ p.sig_base }}</span>
                    <span v-else class="text-medium-emphasis">-</span>
                  </td>
                  <td class="text-no-wrap">
                    <v-chip v-if="p.goal_pick" v-bind="goalPickChip(p, 'goal_pick')" size="small">
                      {{ p.goal_pick }}
                    </v-chip>
                    <span v-else class="text-medium-emphasis">-</span>
                  </td>
                  <td class="text-no-wrap">
                    <v-chip v-if="p.goal_pick_mid" v-bind="goalPickChip(p, 'goal_pick_mid')" size="small">
                      {{ p.goal_pick_mid }}
                    </v-chip>
                    <span v-else class="text-medium-emphasis">-</span>
                  </td>
                  <td class="text-right text-no-wrap">
                    <span v-if="p.ah_line">{{ p.ah_line }}</span>
                    <span v-else class="text-medium-emphasis">-</span>
                  </td>
                  <td class="text-right text-no-wrap">
                    <span v-if="p.ou_line">{{ p.ou_line }}</span>
                    <span v-else class="text-medium-emphasis">-</span>
                  </td>
                  <td class="text-no-wrap text-medium-emphasis">
                    <template v-if="p.trade_pick">
                      <span>交易:{{ p.trade_pick }}</span>
                      <span v-if="hitMark(p, 'trade')" class="mx-1 font-weight-bold" :class="hitMarkClass(p, 'trade')">{{ hitMark(p, 'trade') }}</span>
                      <span v-else class="mr-2"></span>
                    </template>
                    <template v-if="p.sim_pick">
                      <span>模拟:{{ p.sim_pick }}</span>
                      <span v-if="hitMark(p, 'sim')" class="ml-1 font-weight-bold" :class="hitMarkClass(p, 'sim')">{{ hitMark(p, 'sim') }}</span>
                    </template>
                    <span v-if="!p.trade_pick && !p.sim_pick">-</span>
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
.date-row {
  background: rgba(var(--v-theme-primary), 0.08);
}
.row-home {
  background: rgba(var(--v-theme-error), 0.06);
}
.row-away {
  background: rgba(var(--v-theme-success), 0.06);
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
  width: 20px;
  height: 20px;
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
  max-width: 130px;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
