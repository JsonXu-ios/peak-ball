<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { getTrendStatistics } from '@/api'
import TrendSparkline from './TrendSparkline.vue'

interface TrendRecord {
  match_id: string
  date: string
  match_time: string
  league: string
  opponent: string
  is_home: boolean
  score: string
  scored: number
  conceded: number
  half_scored: number
  half_conceded: number
  result: string
}

interface TrendSummary {
  avg_scored: number
  avg_conceded: number
  avg_total: number
  clean_sheets: number
  failed_to_score: number
  over25: number
  wins: number
  draws: number
  losses: number
  attack_trend: string
  defense_trend: string
}

interface TrendTeam {
  team_id: number
  name: string
  logo: string
  sample: number
  want: number
  source: 'history' | 'self' | 'none'
  list: TrendRecord[]
  summary: TrendSummary
}

interface TrendMatch {
  match_id: string
  date: string
  match_time: string
  league: string
  state: string
  settled: boolean
  home: TrendTeam
  guest: TrendTeam
  combined_avg: number
  tag: string
  sufficient: boolean
  home_score: number
  guest_score: number
  total_goals: number
  actual_over: boolean
  hit: boolean | null
}

interface Report {
  mode: 'upcoming' | 'history'
  n: number
  start_date: string
  end_date: string
  settled_pool: number
  summary: {
    total: number
    sufficient: number
    neutral: number
    over: { count: number; hit: number; accuracy: number }
    under: { count: number; hit: number; accuracy: number }
  }
  matches: TrendMatch[]
  generated_at: string
}

const loading = ref(false)
const error = ref('')
const report = ref<Report | null>(null)
const expanded = reactive<Record<string, boolean>>({})

const mode = ref<'upcoming' | 'history'>('upcoming')
const n = ref(5)
const startDate = ref('')
const endDate = ref('')
const halfTime = ref(false)
const onlySufficient = ref(false)
const league = ref('')
const sortBy = ref<'time' | 'combined_desc' | 'combined_asc'>('time')

const nOptions = [
  { title: '近 5 场', value: 5 },
  { title: '近 10 场', value: 10 },
]

const sortOptions = [
  { title: '按开赛时间', value: 'time' },
  { title: '合计场均球数 高→低', value: 'combined_desc' },
  { title: '合计场均球数 低→高', value: 'combined_asc' },
]

const leagues = computed(() => {
  const set = new Set<string>()
  report.value?.matches.forEach((m) => m.league && set.add(m.league))
  return Array.from(set).sort()
})

const rows = computed(() => {
  if (!report.value) return []
  let list = report.value.matches.slice()
  if (onlySufficient.value) list = list.filter((m) => m.sufficient)
  if (league.value) list = list.filter((m) => m.league === league.value)
  if (sortBy.value === 'combined_desc') list.sort((a, b) => b.combined_avg - a.combined_avg)
  if (sortBy.value === 'combined_asc') list.sort((a, b) => a.combined_avg - b.combined_avg)
  return list
})

function series(team: TrendTeam, key: 'scored' | 'conceded') {
  return team.list.map((r) => (halfTime.value ? r[`half_${key}`] : r[key]))
}

function toggle(id: string) {
  expanded[id] = !expanded[id]
}

function tagColor(tag: string) {
  if (tag === '大球倾向') return 'warning'
  if (tag === '小球倾向') return 'info'
  if (tag === '中性') return 'default'
  return 'grey'
}

function trendIcon(text: string) {
  if (text === '上升' || text === '转差') return 'mdi-arrow-top-right'
  if (text === '下降' || text === '转好') return 'mdi-arrow-bottom-right'
  if (text === '持平') return 'mdi-arrow-right'
  return 'mdi-minus'
}

function trendColor(text: string, kind: 'attack' | 'defense') {
  if (text === '样本不足' || text === '持平') return 'grey'
  const good = kind === 'attack' ? text === '上升' : text === '转好'
  return good ? 'success' : 'error'
}

function resultColor(result: string) {
  if (result === '胜') return 'success'
  if (result === '平') return 'grey'
  return 'error'
}

function sourceText(source: TrendTeam['source']) {
  if (source === 'history') return '采集近况'
  if (source === 'self') return '本库自算'
  return '无数据'
}

function sampleText(team: TrendTeam) {
  return `${team.sample}/${team.want}`
}

function generatedAtText() {
  const raw = report.value?.generated_at
  return raw ? raw.replace('T', ' ').slice(0, 19) : ''
}

async function fetchReport() {
  loading.value = true
  error.value = ''
  try {
    const params: { mode: string; n: number; start_date?: string; end_date?: string } = { mode: mode.value, n: n.value }
    if (startDate.value) params.start_date = startDate.value
    if (endDate.value) params.end_date = endDate.value
    const { data } = await getTrendStatistics(params)
    report.value = data as Report
    Object.keys(expanded).forEach((key) => delete expanded[key])
  } catch (requestError) {
    const err = requestError as { response?: { data?: { error?: string } }; message?: string }
    error.value = err.response?.data?.error || err.message || '加载趋势球失败'
  } finally {
    loading.value = false
  }
}

watch(mode, () => {
  startDate.value = ''
  endDate.value = ''
  fetchReport()
})
watch(n, () => fetchReport())

onMounted(() => fetchReport())
</script>

<template>
  <div>
    <div class="d-flex flex-wrap align-center mb-4 ga-3">
      <div>
        <h2 class="text-h5 font-weight-bold">趋势球</h2>
        <div class="text-body-2 text-medium-emphasis mt-1">
          两队各自最近 N 场的进球 / 丢球走势。优先使用采集到的近期战绩，缺失时用本库已完赛比赛按球队自拼；只取开赛前的比赛。红线为进球，蓝线为丢球。
        </div>
      </div>
      <v-spacer />
      <v-btn :loading="loading" color="primary" prepend-icon="mdi-refresh" @click="fetchReport()">刷新</v-btn>
    </div>

    <v-card class="mb-4">
      <v-card-text class="d-flex flex-wrap align-center ga-3">
        <v-btn-toggle v-model="mode" mandatory density="comfortable" color="primary" variant="outlined" divided>
          <v-btn value="upcoming" size="small">未开赛</v-btn>
          <v-btn value="history" size="small">完赛回测</v-btn>
        </v-btn-toggle>
        <v-select v-model="n" :items="nOptions" density="compact" hide-details variant="outlined" style="max-width: 130px" />
        <v-text-field v-model="startDate" type="date" label="开始日期" density="compact" hide-details variant="outlined" style="max-width: 170px" />
        <v-text-field v-model="endDate" type="date" label="结束日期" density="compact" hide-details variant="outlined" style="max-width: 170px" />
        <v-btn size="small" variant="tonal" @click="fetchReport()">查询</v-btn>
        <v-divider vertical class="mx-1" />
        <v-select
          v-model="league"
          :items="leagues"
          label="联赛"
          density="compact"
          hide-details
          clearable
          variant="outlined"
          style="max-width: 170px"
        />
        <v-select v-model="sortBy" :items="sortOptions" density="compact" hide-details variant="outlined" style="max-width: 210px" />
        <v-switch v-model="halfTime" label="半场" density="compact" hide-details color="primary" />
        <v-switch v-model="onlySufficient" label="只看样本充足" density="compact" hide-details color="primary" />
      </v-card-text>
    </v-card>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-5">{{ error }}</v-alert>

    <template v-if="report">
      <v-row class="mb-1">
        <v-col cols="6" md="2">
          <v-card color="primary" variant="tonal">
            <v-card-text>
              <div class="text-body-2">比赛数</div>
              <div class="text-h5 font-weight-bold mt-1">{{ report.summary.total }}</div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="6" md="2">
          <v-card variant="tonal">
            <v-card-text>
              <div class="text-body-2">两队样本充足</div>
              <div class="text-h5 font-weight-bold mt-1">
                {{ report.summary.sufficient }}
                <span class="text-caption text-medium-emphasis">/ {{ report.summary.total }}</span>
              </div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="6" md="2">
          <v-card color="warning" variant="tonal">
            <v-card-text>
              <div class="text-body-2">大球倾向（≥{{ 3 }}）</div>
              <div class="text-h5 font-weight-bold mt-1">
                {{ report.summary.over.count }}
                <span v-if="report.mode === 'history' && report.summary.over.count" class="text-caption">
                  命中 {{ report.summary.over.hit }}（{{ report.summary.over.accuracy.toFixed(1) }}%）
                </span>
              </div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="6" md="2">
          <v-card color="info" variant="tonal">
            <v-card-text>
              <div class="text-body-2">小球倾向（≤{{ 2 }}）</div>
              <div class="text-h5 font-weight-bold mt-1">
                {{ report.summary.under.count }}
                <span v-if="report.mode === 'history' && report.summary.under.count" class="text-caption">
                  命中 {{ report.summary.under.hit }}（{{ report.summary.under.accuracy.toFixed(1) }}%）
                </span>
              </div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="12" md="4">
          <v-card variant="tonal">
            <v-card-text>
              <div class="text-body-2 text-medium-emphasis">说明</div>
              <div class="text-caption text-medium-emphasis mt-1">
                范围 {{ report.start_date || '-' }} ~ {{ report.end_date || '不限' }}；本库可用于自拼的球队 {{ report.settled_pool.toLocaleString() }} 支。
                倾向 = 两队近 {{ report.n }} 场「场均进球+丢球」的均值；命中按全场总球数是否 &gt; 2.5 结算。
              </div>
              <div v-if="generatedAtText()" class="text-caption text-medium-emphasis mt-1">生成时间：{{ generatedAtText() }}</div>
            </v-card-text>
          </v-card>
        </v-col>
      </v-row>

      <v-card>
        <v-card-text class="pa-0">
          <div v-if="!rows.length" class="text-medium-emphasis pa-6">没有符合条件的比赛。</div>
          <v-table v-else density="comfortable" class="trend-table">
            <thead>
              <tr>
                <th>时间 / 联赛</th>
                <th>主队</th>
                <th class="text-center">近 {{ report.n }} 场走势</th>
                <th class="text-right">场均进/丢</th>
                <th>客队</th>
                <th class="text-center">近 {{ report.n }} 场走势</th>
                <th class="text-right">场均进/丢</th>
                <th class="text-right">合计场均</th>
                <th class="text-center">倾向</th>
                <th v-if="report.mode === 'history'" class="text-center">赛果</th>
                <th class="text-right">明细</th>
              </tr>
            </thead>
            <tbody>
              <template v-for="m in rows" :key="m.match_id">
                <tr :class="{ 'row-dim': !m.sufficient }">
                  <td class="text-no-wrap">
                    <div>{{ m.match_time || m.date }}</div>
                    <div class="text-caption text-medium-emphasis">{{ m.league }}</div>
                  </td>
                  <td class="text-no-wrap">
                    <div class="d-flex align-center ga-2">
                      <span class="team-logo"><img v-if="m.home.logo" :src="m.home.logo" alt="" loading="lazy" @error="($event.target as HTMLImageElement).style.display = 'none'" /></span>
                      <div>
                        <div class="font-weight-medium">{{ m.home.name }}</div>
                        <div class="text-caption text-medium-emphasis">
                          {{ sampleText(m.home) }} · {{ sourceText(m.home.source) }}
                        </div>
                      </div>
                    </div>
                  </td>
                  <td class="text-center">
                    <TrendSparkline :scored="series(m.home, 'scored')" :conceded="series(m.home, 'conceded')" :want="report.n" :width="report.n > 5 ? 220 : 140" />
                  </td>
                  <td class="text-right text-no-wrap">
                    <span class="text-error font-weight-medium">{{ m.home.summary.avg_scored.toFixed(1) }}</span>
                    /
                    <span class="text-info font-weight-medium">{{ m.home.summary.avg_conceded.toFixed(1) }}</span>
                    <div class="text-caption">
                      <v-icon size="14" :color="trendColor(m.home.summary.attack_trend, 'attack')" :icon="trendIcon(m.home.summary.attack_trend)" />
                      <span class="text-medium-emphasis">攻{{ m.home.summary.attack_trend }}</span>
                    </div>
                  </td>
                  <td class="text-no-wrap">
                    <div class="d-flex align-center ga-2">
                      <span class="team-logo"><img v-if="m.guest.logo" :src="m.guest.logo" alt="" loading="lazy" @error="($event.target as HTMLImageElement).style.display = 'none'" /></span>
                      <div>
                        <div class="font-weight-medium">{{ m.guest.name }}</div>
                        <div class="text-caption text-medium-emphasis">
                          {{ sampleText(m.guest) }} · {{ sourceText(m.guest.source) }}
                        </div>
                      </div>
                    </div>
                  </td>
                  <td class="text-center">
                    <TrendSparkline :scored="series(m.guest, 'scored')" :conceded="series(m.guest, 'conceded')" :want="report.n" :width="report.n > 5 ? 220 : 140" />
                  </td>
                  <td class="text-right text-no-wrap">
                    <span class="text-error font-weight-medium">{{ m.guest.summary.avg_scored.toFixed(1) }}</span>
                    /
                    <span class="text-info font-weight-medium">{{ m.guest.summary.avg_conceded.toFixed(1) }}</span>
                    <div class="text-caption">
                      <v-icon size="14" :color="trendColor(m.guest.summary.attack_trend, 'attack')" :icon="trendIcon(m.guest.summary.attack_trend)" />
                      <span class="text-medium-emphasis">攻{{ m.guest.summary.attack_trend }}</span>
                    </div>
                  </td>
                  <td class="text-right font-weight-bold">{{ m.combined_avg ? m.combined_avg.toFixed(2) : '-' }}</td>
                  <td class="text-center">
                    <v-chip :color="tagColor(m.tag)" size="small" variant="tonal">{{ m.tag }}</v-chip>
                  </td>
                  <td v-if="report.mode === 'history'" class="text-center text-no-wrap">
                    <span class="font-weight-medium">{{ m.home_score }}-{{ m.guest_score }}</span>
                    <v-chip v-if="m.hit !== null" :color="m.hit ? 'success' : 'error'" size="x-small" variant="tonal" class="ml-1">
                      {{ m.hit ? '命中' : '未中' }}
                    </v-chip>
                  </td>
                  <td class="text-right">
                    <v-btn size="small" variant="text" @click="toggle(m.match_id)">{{ expanded[m.match_id] ? '收起' : '查看' }}</v-btn>
                  </td>
                </tr>
                <tr v-if="expanded[m.match_id]">
                  <td :colspan="report.mode === 'history' ? 11 : 10" class="pa-0">
                    <div class="detail-wrap pa-4">
                      <v-row>
                        <v-col v-for="team in [m.home, m.guest]" :key="team.team_id" cols="12" md="6">
                          <div class="d-flex align-center flex-wrap ga-2 mb-2">
                            <span class="font-weight-bold">{{ team.name }}</span>
                            <v-chip size="x-small" variant="tonal">{{ sourceText(team.source) }} {{ sampleText(team) }}</v-chip>
                            <v-chip size="x-small" variant="tonal" color="success">{{ team.summary.wins }}胜 {{ team.summary.draws }}平 {{ team.summary.losses }}负</v-chip>
                            <v-chip size="x-small" variant="tonal">零封 {{ team.summary.clean_sheets }}</v-chip>
                            <v-chip size="x-small" variant="tonal">未进球 {{ team.summary.failed_to_score }}</v-chip>
                            <v-chip size="x-small" variant="tonal" color="warning">大2.5 {{ team.summary.over25 }}</v-chip>
                            <v-chip size="x-small" variant="tonal" :color="trendColor(team.summary.defense_trend, 'defense')">防守{{ team.summary.defense_trend }}</v-chip>
                          </div>
                          <TrendSparkline :scored="series(team, 'scored')" :conceded="series(team, 'conceded')" :want="report.n" :width="420" :height="90" class="mb-2" />
                          <table v-if="team.list.length" class="mini-table">
                            <thead>
                              <tr>
                                <th>日期</th>
                                <th>联赛</th>
                                <th>主/客</th>
                                <th>对手</th>
                                <th class="text-center">比分</th>
                                <th class="text-center">半场</th>
                                <th class="text-right">进</th>
                                <th class="text-right">丢</th>
                                <th class="text-center">结果</th>
                              </tr>
                            </thead>
                            <tbody>
                              <tr v-for="r in team.list" :key="r.match_id + r.date">
                                <td class="text-no-wrap">{{ r.match_time || r.date }}</td>
                                <td>{{ r.league }}</td>
                                <td>{{ r.is_home ? '主' : '客' }}</td>
                                <td>{{ r.opponent }}</td>
                                <td class="text-center">{{ r.score }}</td>
                                <td class="text-center">{{ r.half_scored }}-{{ r.half_conceded }}</td>
                                <td class="text-right text-error">{{ r.scored }}</td>
                                <td class="text-right text-info">{{ r.conceded }}</td>
                                <td class="text-center">
                                  <v-chip :color="resultColor(r.result)" size="x-small" variant="flat">{{ r.result }}</v-chip>
                                </td>
                              </tr>
                            </tbody>
                          </table>
                          <div v-else class="text-caption text-medium-emphasis">该队在本库没有开赛前的完赛记录。</div>
                        </v-col>
                      </v-row>
                    </div>
                  </td>
                </tr>
              </template>
            </tbody>
          </v-table>
        </v-card-text>
      </v-card>
    </template>

    <v-card v-else-if="!loading" variant="tonal">
      <v-card-text class="text-medium-emphasis">暂无数据。</v-card-text>
    </v-card>
  </div>
</template>

<style scoped>
.trend-table th {
  white-space: nowrap;
}
.row-dim {
  opacity: 0.6;
}
.detail-wrap {
  background: rgba(var(--v-theme-surface-light), 0.4);
}
.team-logo {
  width: 22px;
  height: 22px;
  flex: 0 0 22px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}
.team-logo img {
  max-width: 22px;
  max-height: 22px;
}
.mini-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}
.mini-table th,
.mini-table td {
  padding: 4px 8px;
  border-bottom: 1px solid rgba(var(--v-theme-on-surface), 0.08);
  text-align: left;
}
.mini-table th {
  color: rgba(var(--v-theme-on-surface), 0.6);
  font-weight: 500;
}
</style>
