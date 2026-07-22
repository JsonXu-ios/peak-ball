<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { getMatchStatistics } from '@/api'
import MatchDetailTable from './MatchDetailTable.vue'

interface MatchDetail {
  match_id: string
  date: string
  match_time?: string
  league?: string
  home: string
  guest: string
  home_logo?: string
  guest_logo?: string
  home_score: number
  guest_score: number
  state: string
  pick: string
  result: string
  hit: boolean
  value: number
}

interface HeatBucket {
  key: string
  title: string
  tier?: number
  matched: number
  hit: number
  miss: number
  accuracy: number
  matches: MatchDetail[]
}

interface Signal {
  key: string
  title: string
  definition: string
  /** 命中赛果分类：spf/asian/goals/dxq/score/mixed */
  market?: string
  matched: number
  hit: number
  miss: number
  accuracy: number
  /** 按 赛前方向×盘口线 拆分：每行是该方向在该盘口线下的场次与命中率 */
  directions?: Array<{ pick: string; line?: string; matched: number; hit: number; miss: number; accuracy: number }>
  matches?: MatchDetail[]
  buckets?: HeatBucket[]
  /** 真实赔率回报（13a/13b） */
  roi?: number
  roiSample?: number
}

interface Report {
  settled_total: number
  start_date: string
  end_date: string
  generated_at: string
  signals: Signal[]
  needs_recompute?: boolean
}

const DETAIL_CAP = 300

// 维度分组（展示顺序 + 颜色）。target 说明该组信号的命中率结算于哪个赛果。
const MARKETS: Array<{ key: string; label: string; color: string; target: string }> = [
  { key: 'spf', label: '胜平负', color: 'indigo', target: '胜平负（主胜/平/客胜）' },
  { key: 'asian', label: '让球/亚盘', color: 'orange-darken-2', target: '让球赢盘（主/客盖帽）' },
  { key: 'goals', label: '球数期望', color: 'cyan-darken-2', target: '大小球（对总进球判大/小）' },
  { key: 'dxq', label: '大小球盘口', color: 'teal', target: '大小球（对总进球判大/小）' },
  { key: 'score', label: '比分', color: 'purple', target: '比分（任一命中）' },
  { key: 'mixed', label: '综合/多赛果', color: 'blue-grey', target: '多赛果（按分组明细各自结算）' },
]

// 已下线的信号：旧快照里可能还带着，前端直接过滤掉。
const REMOVED_SIGNALS = new Set([
  'pick_spf', 'pick_rqspf', 'pick_dxq', 'pick_score', 'direct_over_signals',
  'pick_overview', 'cross_spf_base', 'cross_spf_comfort', 'cross_dxq_qiu', 'cross_dxq_composite',
  'chase_signals',
])

// 快照缓存里的旧数据可能没有 market 字段：按信号 key 兜底归类，保证分组不塌成一堆。
function marketOf(signal: Signal): string {
  if (signal.market) return signal.market
  const map: Record<string, string> = {
    asian_heat: 'asian', line_discrepancy: 'asian',
    goals_heat: 'dxq', goals_discrepancy: 'dxq', base_qiu: 'dxq',
    history_goals: 'goals', recent_goals: 'goals', goals_composite: 'goals',
    pro_signal: 'spf', trade_comfort: 'spf', sim_trade_comfort: 'spf',
    history_handicap: 'spf', recent_handicap: 'spf', asian_composite: 'spf',
    base_spf: 'spf',
  }
  return map[signal.key] || 'mixed'
}

interface SignalGroup {
  key: string
  label: string
  color: string
  target: string
  signals: Signal[]
}

function groupedSignals(signals: Signal[]): SignalGroup[] {
  const byMarket = new Map<string, Signal[]>()
  for (const signal of signals) {
    if (REMOVED_SIGNALS.has(signal.key)) continue
    const key = marketOf(signal)
    if (!byMarket.has(key)) byMarket.set(key, [])
    byMarket.get(key)!.push(signal)
  }
  const groups: SignalGroup[] = []
  for (const meta of MARKETS) {
    const rows = byMarket.get(meta.key)
    if (!rows || !rows.length) continue
    groups.push({ key: meta.key, label: meta.label, color: meta.color, target: meta.target, signals: rows })
  }
  return groups
}

const loading = ref(false)
const recomputing = ref(false)
const startDate = ref('')
const endDate = ref('')
const report = ref<Report | null>(null)
const error = ref('')
// which detail tables are open, keyed by signal/bucket key
const expanded = reactive<Record<string, boolean>>({})

const signalGroups = computed(() => (report.value ? groupedSignals(report.value.signals) : []))

function toggle(key: string) {
  expanded[key] = !expanded[key]
}

function accuracyColor(accuracy: number, matched: number) {
  if (!matched) return 'default'
  if (accuracy >= 60) return 'success'
  if (accuracy >= 50) return 'primary'
  return 'warning'
}

function sourceRange() {
  if (!report.value?.start_date && !report.value?.end_date) return '全部历史完赛比赛'
  return `${report.value?.start_date || '最早'} 至 ${report.value?.end_date || '最新'}`
}

function cappedMatches(matches: MatchDetail[]) {
  return matches.slice(0, DETAIL_CAP)
}

async function fetchReport(refresh = false) {
  if (refresh) recomputing.value = true
  else loading.value = true
  error.value = ''
  try {
    const { data } = await getMatchStatistics({
      start_date: startDate.value || undefined,
      end_date: endDate.value || undefined,
      ...(refresh && !startDate.value && !endDate.value ? { refresh: 1 } : {}),
    })
    report.value = data as Report
    Object.keys(expanded).forEach((key) => delete expanded[key])
  } catch (requestError) {
    const err = requestError as { response?: { data?: { error?: string } }; message?: string }
    error.value = err.response?.data?.error || err.message || '加载统计失败'
  } finally {
    loading.value = false
    recomputing.value = false
  }
}

function resetRange() {
  startDate.value = ''
  endDate.value = ''
  fetchReport(false)
}

function generatedAtText() {
  const raw = report.value?.generated_at
  if (!raw) return ''
  return raw.replace('T', ' ').slice(0, 19)
}

onMounted(() => fetchReport(false))
</script>

<template>
  <div>
    <div class="d-flex flex-wrap align-center mb-4 ga-3">
      <div>
        <h2 class="text-h5 font-weight-bold">完赛比赛信号统计</h2>
        <div class="text-body-2 text-medium-emphasis mt-1">
          基于全部完赛比赛，按维度（胜平负 / 让球 / 球数 / 大小球 / 比分）分组，逐个信号统计符合条件的场次与命中率，并可下钻查看具体比赛。
        </div>
      </div>
      <v-spacer />
      <v-btn :loading="recomputing" color="warning" prepend-icon="mdi-calculator" @click="fetchReport(true)">重新统计</v-btn>
    </div>

    <v-card class="mb-5">
      <v-card-text class="d-flex flex-wrap align-center ga-3">
        <v-text-field v-model="startDate" type="date" label="开始日期" hide-details style="max-width: 210px" />
        <v-text-field v-model="endDate" type="date" label="结束日期" hide-details style="max-width: 210px" />
        <v-btn color="primary" variant="tonal" :loading="loading" @click="fetchReport(false)">应用日期（即时计算）</v-btn>
        <v-btn variant="text" @click="resetRange">查看全部（读缓存）</v-btn>
        <v-spacer />
        <span v-if="generatedAtText()" class="text-caption text-medium-emphasis">统计时间：{{ generatedAtText() }}</span>
      </v-card-text>
    </v-card>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-5">{{ error }}</v-alert>

    <v-card v-if="report?.needs_recompute" variant="tonal" color="warning" class="mb-5">
      <v-card-text class="text-center py-10">
        <div class="text-h6 font-weight-bold mb-2">统计结果尚未生成</div>
        <div class="text-body-2 text-medium-emphasis mb-4">点击「重新统计」计算一次并存入数据库，之后每次打开直接读取，不再重算。</div>
        <v-btn :loading="recomputing" color="warning" prepend-icon="mdi-calculator" @click="fetchReport(true)">重新统计</v-btn>
      </v-card-text>
    </v-card>

    <template v-if="report && !report.needs_recompute">
      <v-row class="mb-1">
        <v-col cols="12" md="4">
          <v-card color="primary" variant="tonal">
            <v-card-text>
              <div class="text-body-2">纳入完赛场次</div>
              <div class="text-h4 font-weight-bold mt-1">{{ report.settled_total.toLocaleString() }}</div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="12" md="8">
          <v-card variant="tonal">
            <v-card-text>
              <div class="text-body-2 text-medium-emphasis">统计范围</div>
              <div class="text-subtitle-1 font-weight-medium mt-1">{{ sourceRange() }}</div>
              <div class="text-caption text-medium-emphasis mt-1">
                每个信号显示“符合条件的场次 / 命中 / 命中率”。命中率按已完赛结果判断该信号方向猜没猜对；仅有盘口/赔率数据的比赛才会进入相应信号。
              </div>
            </v-card-text>
          </v-card>
        </v-col>
      </v-row>

      <template v-for="group in signalGroups" :key="`sec-${group.key}`">
        <div class="market-header d-flex align-center flex-wrap ga-2 mt-8 mb-3">
          <v-chip :color="group.color" size="small" variant="flat">{{ group.label }}</v-chip>
          <span class="text-subtitle-1 font-weight-bold">命中赛果：{{ group.target }}</span>
          <span class="text-caption text-medium-emphasis">{{ group.signals.length }} 个信号</span>
        </div>

        <v-card v-for="signal in group.signals" :key="signal.key" class="mb-5">
          <v-card-title class="pt-5 d-flex align-center flex-wrap ga-2">
            <span>{{ signal.title }}</span>
            <v-chip color="primary" size="small" variant="tonal">符合 {{ signal.matched.toLocaleString() }} 场</v-chip>
            <v-chip :color="accuracyColor(signal.accuracy, signal.matched)" size="small" variant="tonal">
              命中率 {{ signal.matched ? signal.accuracy.toFixed(2) + '%' : '-' }}
            </v-chip>
            <v-chip size="small" variant="text">命中 {{ signal.hit }} / 未命中 {{ signal.miss }}</v-chip>
            <v-chip v-if="typeof signal.roi === 'number'" size="small" variant="tonal" :color="signal.roi >= 100 ? 'success' : 'warning'">
              ROI {{ signal.roi.toFixed(1) }}%（{{ signal.roiSample }}注有赔率）
            </v-chip>
          </v-card-title>
          <v-card-subtitle class="pb-2 text-wrap">{{ signal.definition }}</v-card-subtitle>

          <v-card-text>
            <!-- Heat signals: bucket table, each bucket drills into its matches -->
            <template v-if="signal.buckets">
              <v-table density="comfortable" class="stat-table">
                <thead>
                  <tr>
                    <th>分组</th>
                    <th class="text-right">符合场次</th>
                    <th class="text-right">命中</th>
                    <th class="text-right">命中率</th>
                    <th class="text-right">明细</th>
                  </tr>
                </thead>
                <tbody>
                  <template v-for="bucket in signal.buckets" :key="bucket.key">
                    <tr>
                      <td class="font-weight-medium">{{ bucket.title }}</td>
                      <td class="text-right">{{ bucket.matched.toLocaleString() }}</td>
                      <td class="text-right">{{ bucket.hit }}</td>
                      <td class="text-right">
                        <v-chip v-if="bucket.matched" :color="accuracyColor(bucket.accuracy, bucket.matched)" size="small" variant="tonal">
                          {{ bucket.accuracy.toFixed(2) }}%
                        </v-chip>
                        <span v-else>-</span>
                      </td>
                      <td class="text-right">
                        <v-btn v-if="bucket.matched" size="small" variant="text" @click="toggle(bucket.key)">
                          {{ expanded[bucket.key] ? '收起' : '查看' }}
                        </v-btn>
                        <span v-else>-</span>
                      </td>
                    </tr>
                    <tr v-if="expanded[bucket.key]">
                      <td colspan="5" class="pa-0">
                        <div class="detail-wrap">
                          <MatchDetailTable :matches="cappedMatches(bucket.matches)" :total="bucket.matched" :cap="DETAIL_CAP" show-value />
                        </div>
                      </td>
                    </tr>
                  </template>
                </tbody>
              </v-table>
            </template>

            <!-- Normal signals: single drill-down of matched matches -->
            <template v-else>
              <div v-if="!signal.matched" class="text-medium-emphasis py-4">暂无符合条件的比赛。</div>
              <template v-else>
                <!-- 按 赛前方向×盘口线 拆分：盘口线是该行统计的结算依据 -->
                <v-table v-if="signal.directions?.length" density="compact" class="stat-table direction-table mb-3">
                  <thead>
                    <tr>
                      <th>赛前方向</th>
                      <th v-if="signal.directions.some((d) => d.line)" class="text-right">盘口</th>
                      <th class="text-right">场次</th>
                      <th class="text-right">命中</th>
                      <th class="text-right">未命中</th>
                      <th class="text-right">命中率</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="direction in signal.directions" :key="`${direction.pick}|${direction.line || ''}`">
                      <td class="font-weight-medium">{{ direction.pick }}</td>
                      <td v-if="signal.directions.some((d) => d.line)" class="text-right">{{ direction.line || '-' }}</td>
                      <td class="text-right">{{ direction.matched.toLocaleString() }}</td>
                      <td class="text-right">{{ direction.hit }}</td>
                      <td class="text-right">{{ direction.miss }}</td>
                      <td class="text-right">
                        <v-chip :color="accuracyColor(direction.accuracy, direction.matched)" size="small" variant="tonal">
                          {{ direction.accuracy.toFixed(2) }}%
                        </v-chip>
                      </td>
                    </tr>
                  </tbody>
                </v-table>
                <v-btn size="small" variant="tonal" class="mb-3" @click="toggle(signal.key)">
                  {{ expanded[signal.key] ? '收起明细' : `查看明细（${signal.matched} 场）` }}
                </v-btn>
                <div v-if="expanded[signal.key]" class="detail-wrap">
                  <MatchDetailTable :matches="cappedMatches(signal.matches || [])" :total="signal.matched" :cap="DETAIL_CAP" show-value />
                </div>
              </template>
            </template>
          </v-card-text>
        </v-card>
      </template>
    </template>

    <v-card v-else-if="!loading && !report?.needs_recompute" variant="tonal">
      <v-card-text class="text-medium-emphasis">暂无统计结果。</v-card-text>
    </v-card>
  </div>
</template>

<style scoped>
.stat-table th {
  white-space: nowrap;
}
.market-header {
  padding: 6px 12px;
  border-left: 4px solid rgba(var(--v-theme-primary), 0.6);
  background: rgba(var(--v-theme-primary), 0.04);
  border-radius: 4px;
}
.direction-table {
  max-width: 620px;
}
.detail-wrap {
  max-height: 460px;
  overflow: auto;
  background: rgba(var(--v-theme-surface-light), 0.4);
}
</style>
