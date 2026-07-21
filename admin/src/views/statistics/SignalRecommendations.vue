<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { getSignalRecommendations } from '@/api'

interface ConditionRow {
  key: string
  title: string
  market: 'spf' | 'asian' | 'dxq' | 'score'
  sample: number
  hit: number
  accuracy: number
  roi?: number
  roiSample?: number
  mode: 'follow' | 'inverse'
}

interface FiredSignal {
  key: string
  title: string
  mode: 'follow' | 'inverse'
  pick: string
  /** 后端提炼的最终答案，每条信号一个明确结论 */
  answer?: string
  extra?: string
  accuracy: number
  sample: number
}

interface RecommendationRow {
  matchId: string
  date: string
  state: string
  matchTime?: string
  league?: string
  home: string
  guest: string
  homeLogo?: string
  guestLogo?: string
  markets: {
    spf: FiredSignal[]
    asian: FiredSignal[]
    dxq: FiredSignal[]
    score: FiredSignal[]
  }
}

interface Report {
  needs_recompute: boolean
  stats_generated_at?: string
  settled_total?: number
  upcoming_total?: number
  days?: number
  min_sample?: number
  conditions: ConditionRow[]
  recommendations: RecommendationRow[]
}

const MARKETS: Array<{ key: 'spf' | 'asian' | 'dxq' | 'score'; label: string; color: string }> = [
  { key: 'spf', label: '胜平负', color: 'indigo' },
  { key: 'asian', label: '让球/亚盘', color: 'orange-darken-2' },
  { key: 'dxq', label: '大小球', color: 'teal' },
  { key: 'score', label: '比分', color: 'purple' },
]

interface FlatSignal extends FiredSignal {
  marketLabel: string
  marketColor: string
}

/** 只展开有信号的方向，按 胜平负→让球→大小球→比分 顺序 */
function flatSignals(row: RecommendationRow): FlatSignal[] {
  const result: FlatSignal[] = []
  for (const market of MARKETS) {
    for (const signal of row.markets[market.key] ?? []) {
      result.push({ ...signal, marketLabel: market.label, marketColor: market.color })
    }
  }
  return result
}

const WEEKDAYS = ['周日', '周一', '周二', '周三', '周四', '周五', '周六']

function dateHeader(date: string): string {
  const parsed = new Date(`${date}T00:00:00`)
  if (Number.isNaN(parsed.getTime())) return date
  return `${date} ${WEEKDAYS[parsed.getDay()]}`
}

function groupedRecommendations(rows: RecommendationRow[]): Array<{ date: string; rows: RecommendationRow[] }> {
  const groups: Array<{ date: string; rows: RecommendationRow[] }> = []
  for (const row of rows) {
    const last = groups[groups.length - 1]
    if (last && last.date === row.date) last.rows.push(row)
    else groups.push({ date: row.date, rows: [row] })
  }
  return groups
}

function timeOnly(row: RecommendationRow): string {
  const raw = row.matchTime || ''
  return raw.length >= 16 ? raw.slice(11, 16) : raw
}

const loading = ref(false)
const recomputing = ref(false)
const error = ref('')
const days = ref(3)
const report = ref<Report | null>(null)

async function fetchReport(refresh = false) {
  if (refresh) recomputing.value = true
  else loading.value = true
  error.value = ''
  try {
    const { data } = await getSignalRecommendations({ days: days.value, ...(refresh ? { refresh: 1 } : {}) })
    report.value = data as Report
  } catch (requestError) {
    const err = requestError as { response?: { data?: { error?: string } }; message?: string }
    error.value = err.response?.data?.error || err.message || '加载失败'
  } finally {
    loading.value = false
    recomputing.value = false
  }
}

function marketLabel(market: string) {
  return MARKETS.find((entry) => entry.key === market)?.label ?? market
}

function modeColor(mode: string) {
  return mode === 'follow' ? 'success' : 'error'
}

onMounted(() => fetchReport(false))
</script>

<template>
  <div>
    <div class="d-flex flex-wrap align-center mb-4 ga-3">
      <div>
        <h2 class="text-h5 font-weight-bold">高价值信号推荐</h2>
        <div class="text-body-2 text-medium-emphasis mt-1">
          全部维度实盘结算后，仅命中率≥70%（跟）或≤30%（自动反向）且样本≥{{ report?.min_sample ?? 8 }}的信号上岗；待赛比赛按四个购买方向给出推荐。
        </div>
      </div>
      <v-spacer />
      <v-select
        v-model="days"
        :items="[1, 2, 3, 5, 7, 14]"
        label="未来天数"
        density="compact"
        hide-details
        style="max-width: 110px"
        @update:model-value="fetchReport(false)"
      />
      <v-btn :loading="recomputing" color="warning" prepend-icon="mdi-calculator" @click="fetchReport(true)">重新计算</v-btn>
      <v-btn :loading="loading" color="primary" variant="tonal" prepend-icon="mdi-refresh" @click="fetchReport(false)">刷新列表</v-btn>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-5">{{ error }}</v-alert>

    <v-card v-if="report?.needs_recompute" variant="tonal" color="warning">
      <v-card-text class="text-center py-10">
        <div class="text-h6 font-weight-bold mb-2">信号库尚未结算</div>
        <div class="text-body-2 text-medium-emphasis mb-4">点击「重新计算」对全部完赛比赛结算一次（约几秒），之后打开页面只做轻量匹配，不会卡。</div>
        <v-btn :loading="recomputing" color="warning" prepend-icon="mdi-calculator" @click="fetchReport(true)">重新计算</v-btn>
      </v-card-text>
    </v-card>

    <template v-else-if="report">
      <v-row class="mb-1">
        <v-col cols="12" md="4">
          <v-card color="primary" variant="tonal">
            <v-card-text>
              <div class="text-body-2">未来 {{ report.days }} 天推荐</div>
              <div class="text-h4 font-weight-bold mt-1">{{ report.recommendations.length }}</div>
              <div class="text-caption mt-1">待赛 {{ report.upcoming_total }} 场 · 结算基数 {{ report.settled_total?.toLocaleString() }} 场完赛</div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="12" md="8">
          <v-card variant="tonal">
            <v-card-text>
              <div class="d-flex align-center justify-space-between">
                <div class="text-body-2 text-medium-emphasis">上岗信号（{{ report.conditions.length }} 条）</div>
                <div class="text-caption text-medium-emphasis">信号库结算时间：{{ report.stats_generated_at }}</div>
              </div>
              <v-table density="compact" class="mt-1">
                <thead>
                  <tr><th>信号</th><th>方向</th><th class="text-right">命中率(样本)</th><th class="text-right">ROI</th><th class="text-right">模式</th></tr>
                </thead>
                <tbody>
                  <tr v-for="condition in report.conditions" :key="condition.key">
                    <td class="font-weight-medium">{{ condition.title }}</td>
                    <td>{{ marketLabel(condition.market) }}</td>
                    <td class="text-right">{{ condition.accuracy.toFixed(1) }}% ({{ condition.sample }})</td>
                    <td class="text-right">{{ typeof condition.roi === 'number' ? condition.roi.toFixed(1) + '%' : '-' }}</td>
                    <td class="text-right">
                      <v-chip :color="modeColor(condition.mode)" size="x-small" variant="flat">{{ condition.mode === 'follow' ? '跟' : '反向' }}</v-chip>
                    </td>
                  </tr>
                </tbody>
              </v-table>
            </v-card-text>
          </v-card>
        </v-col>
      </v-row>

      <v-card>
        <v-card-title class="pt-5 d-flex align-center ga-2">
          待赛推荐
          <v-chip color="primary" size="small" variant="tonal">{{ report.recommendations.length }} 场</v-chip>
          <v-spacer />
          <span class="d-flex align-center ga-2 text-caption text-medium-emphasis">
            方向：
            <v-chip v-for="market in MARKETS" :key="market.key" :color="market.color" size="x-small" variant="flat">{{ market.label }}</v-chip>
          </span>
        </v-card-title>
        <v-card-text class="pa-0">
          <div v-if="!report.recommendations.length" class="text-medium-emphasis py-8 text-center">
            未来 {{ report.days }} 天暂无命中上岗信号的比赛。
          </div>
          <template v-for="group in groupedRecommendations(report.recommendations)" :key="group.date">
            <div class="date-header px-4 py-2">
              {{ dateHeader(group.date) }}
              <span class="text-medium-emphasis font-weight-regular">· {{ group.rows.length }} 场</span>
            </div>
            <div v-for="row in group.rows" :key="row.matchId" class="rec-row d-flex px-4 py-3">
              <!-- 左：比赛信息 -->
              <div class="rec-match shrink-0">
                <div class="d-flex align-center ga-2 mb-1">
                  <span class="rec-time font-weight-bold">{{ timeOnly(row) || row.date }}</span>
                  <v-chip v-if="row.league" size="x-small" variant="tonal" class="league-chip">{{ row.league }}</v-chip>
                </div>
                <div class="rec-team d-flex align-center ga-2">
                  <span class="team-logo">
                    <img v-if="row.homeLogo" :src="row.homeLogo" alt="" loading="lazy" @error="($event.target as HTMLImageElement).style.display = 'none'" />
                  </span>
                  <span class="font-weight-bold">{{ row.home }}</span>
                </div>
                <div class="rec-team d-flex align-center ga-2">
                  <span class="team-logo">
                    <img v-if="row.guestLogo" :src="row.guestLogo" alt="" loading="lazy" @error="($event.target as HTMLImageElement).style.display = 'none'" />
                  </span>
                  <span class="font-weight-bold">{{ row.guest }}</span>
                </div>
              </div>
              <v-divider vertical class="mx-4" />
              <!-- 右：推荐（只列有信号的方向） -->
              <div class="rec-signals flex-grow-1 d-flex flex-column justify-center ga-2">
                <div v-for="signal in flatSignals(row)" :key="signal.key" class="d-flex align-center flex-wrap ga-2">
                  <v-chip :color="signal.marketColor" size="small" variant="flat" class="market-chip shrink-0">{{ signal.marketLabel }}</v-chip>
                  <span class="rec-pick font-weight-bold">{{ signal.pick }}</span>
                  <v-chip :color="modeColor(signal.mode)" size="x-small" variant="tonal" class="shrink-0">
                    {{ signal.mode === 'follow' ? '跟' : '反向' }} · 历史{{ signal.accuracy.toFixed(0) }}% / {{ signal.sample }}场
                  </v-chip>
                  <span class="text-caption text-disabled">{{ signal.title }}<template v-if="signal.extra"> · {{ signal.extra }}</template></span>
                  <v-chip
                    v-if="signal.answer"
                    :color="modeColor(signal.mode)"
                    size="small"
                    variant="flat"
                    class="rec-answer ml-auto shrink-0 font-weight-bold"
                  >
                    答案：{{ signal.answer }}
                  </v-chip>
                </div>
              </div>
            </div>
          </template>
        </v-card-text>
      </v-card>
    </template>
  </div>
</template>

<style scoped>
.date-header {
  font-weight: 700;
  font-size: 0.85rem;
  background: rgba(var(--v-theme-primary), 0.06);
  border-top: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-bottom: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
}
.rec-row {
  border-bottom: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  transition: background 0.15s;
}
.rec-row:hover {
  background: rgba(var(--v-theme-primary), 0.04);
}
.rec-match {
  width: 220px;
}
.rec-time {
  font-variant-numeric: tabular-nums;
}
.rec-team {
  line-height: 1.7;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.league-chip {
  max-width: 140px;
}
.market-chip {
  min-width: 72px;
  justify-content: center;
}
.rec-pick {
  font-size: 0.95rem;
}
.rec-answer {
  font-size: 0.9rem;
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
</style>
