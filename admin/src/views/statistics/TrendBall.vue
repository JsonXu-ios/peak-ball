<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { getTrendStatistics } from '@/api'
import { beijingToday, formatBeijingDateTime } from '@/utils/date'
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
  home_against: TrendTeam
  guest_against: TrendTeam
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

function todayText() {
  return beijingToday()
}

const mode = ref<'upcoming' | 'history'>('upcoming')
const n = ref(5)
const date = ref(todayText())
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

// 该队每场的总球数（进+丢），进球丢球都发生在这支队的比赛里，合并看整体大小球走势
function seriesTotal(team: TrendTeam) {
  return team.list.map((r) => (halfTime.value ? r.half_scored + r.half_conceded : r.scored + r.conceded))
}

// ---------- 球数推荐（以 2 球为分界） ----------
// 规则：主队近 N 场总球数均值、客队近 N 场总球数均值取较小值；再与两队历史交锋总球数均值取较小值；
// 最终值 ≥ 2 推荐「2球以上」，否则「2球以下」。只用全场球数，与半场开关无关。
const GOAL_LINE = 2

interface GoalPick {
  home: number
  guest: number
  against: number
  value: number
  label: '2球以上' | '2球以下' | '样本不足'
  hit: boolean | null
}

function avgTotal(team: TrendTeam) {
  if (!team.list.length) return Number.NaN
  return team.list.reduce((sum, r) => sum + r.scored + r.conceded, 0) / team.list.length
}

function goalPick(m: TrendMatch): GoalPick {
  const home = avgTotal(m.home)
  const guest = avgTotal(m.guest)
  const against = avgTotal(m.home_against)
  if (!Number.isFinite(home) || !Number.isFinite(guest)) {
    return { home, guest, against, value: Number.NaN, label: '样本不足', hit: null }
  }
  let value = Math.min(home, guest)
  if (Number.isFinite(against)) value = Math.min(value, against)
  const label = value >= GOAL_LINE ? '2球以上' : '2球以下'
  const hit = m.settled ? (m.total_goals >= GOAL_LINE) === (label === '2球以上') : null
  return { home, guest, against, value, label, hit }
}

const pickSummary = computed(() => {
  const over = { count: 0, hit: 0 }
  const under = { count: 0, hit: 0 }
  let insufficient = 0
  rows.value.forEach((m) => {
    const pick = goalPick(m)
    if (pick.label === '样本不足') {
      insufficient++
      return
    }
    const bucket = pick.label === '2球以上' ? over : under
    bucket.count++
    if (pick.hit) bucket.hit++
  })
  return { over, under, insufficient }
})

function pickColor(label: GoalPick['label']) {
  if (label === '2球以上') return 'warning'
  if (label === '2球以下') return 'info'
  return 'grey'
}

function fmt1(value: number) {
  return Number.isFinite(value) ? value.toFixed(1) : '-'
}

function rate(hit: number, count: number) {
  return count ? `${((hit * 100) / count).toFixed(1)}%` : '-'
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
  if (!date.value) {
    error.value = '请选择日期'
    return
  }
  loading.value = true
  error.value = ''
  try {
    const params = { mode: mode.value, n: n.value, start_date: date.value, end_date: date.value }
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

function resetDate() {
  date.value = todayText()
  fetchReport()
}

watch(mode, () => {
  date.value = todayText()
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
          两队各自最近 N 场「每场总球数（进+丢）」的走势，纵轴默认 0~4、超出自动扩展。优先使用采集到的近期战绩，缺失时用本库已完赛比赛按球队自拼；只取开赛前的比赛。两队没有历史交锋的比赛不展示。
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
        <v-text-field v-model="date" type="date" label="日期（北京时间）" density="compact" hide-details variant="outlined" style="max-width: 230px" />
        <v-btn size="small" variant="tonal" @click="fetchReport()">查询</v-btn>
        <v-btn size="small" variant="text" @click="resetDate">今天</v-btn>
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
                日期 {{ report.start_date || '-' }}；本库可用于自拼的球队 {{ report.settled_pool.toLocaleString() }} 支。
                倾向 = 两队近 {{ report.n }} 场「场均进球+丢球」的均值；命中按全场总球数是否 &gt; 2.5 结算。
              </div>
              <div v-if="generatedAtText()" class="text-caption text-medium-emphasis mt-1">生成时间：{{ generatedAtText() }}</div>
            </v-card-text>
          </v-card>
        </v-col>
      </v-row>

      <v-card class="mb-4" variant="tonal" color="warning">
        <v-card-text class="d-flex flex-wrap align-center ga-4">
          <div class="font-weight-bold">球数推荐（2 球分界）</div>
          <div>
            推荐 2球以上 <span class="font-weight-bold">{{ pickSummary.over.count }}</span> 场
            <span v-if="report.mode === 'history' && pickSummary.over.count">，命中 {{ pickSummary.over.hit }}（{{ rate(pickSummary.over.hit, pickSummary.over.count) }}）</span>
          </div>
          <div>
            推荐 2球以下 <span class="font-weight-bold">{{ pickSummary.under.count }}</span> 场
            <span v-if="report.mode === 'history' && pickSummary.under.count">，命中 {{ pickSummary.under.hit }}（{{ rate(pickSummary.under.hit, pickSummary.under.count) }}）</span>
          </div>
          <div v-if="report.mode === 'history' && pickSummary.over.count + pickSummary.under.count">
            合计命中 {{ pickSummary.over.hit + pickSummary.under.hit }} / {{ pickSummary.over.count + pickSummary.under.count }}
            （{{ rate(pickSummary.over.hit + pickSummary.under.hit, pickSummary.over.count + pickSummary.under.count) }}）
          </div>
          <div v-if="pickSummary.insufficient" class="text-medium-emphasis">样本不足 {{ pickSummary.insufficient }} 场</div>
          <div class="text-caption text-medium-emphasis">
            规则：主队近 N 场总球数均值、客队近 N 场总球数均值取较小值，再与两队历史交锋总球数均值取较小值；≥2 推荐 2球以上，否则 2球以下。命中按实际总球数是否 ≥2 判定。
          </div>
        </v-card-text>
      </v-card>

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
                <th class="text-center">球数推荐</th>
                <th v-if="report.mode === 'history'" class="text-center">赛果</th>
                <th class="text-right">明细</th>
              </tr>
            </thead>
            <tbody>
              <template v-for="m in rows" :key="m.match_id">
                <tr :class="{ 'row-dim': !m.sufficient }">
                  <td class="text-no-wrap">
                    <div>{{ formatBeijingDateTime(m.match_time || m.date) }}</div>
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
                    <div class="trend-pair py-1">
                      <div class="d-flex align-center ga-1">
                        <span class="trend-pair-tag text-warning">总</span>
                        <TrendSparkline :values="seriesTotal(m.home)" kind="total" :want="report.n" :width="report.n > 5 ? 320 : 220" :height="100" />
                      </div>
                    </div>
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
                    <div class="trend-pair py-1">
                      <div class="d-flex align-center ga-1">
                        <span class="trend-pair-tag text-warning">总</span>
                        <TrendSparkline :values="seriesTotal(m.guest)" kind="total" :want="report.n" :width="report.n > 5 ? 320 : 220" :height="100" />
                      </div>
                    </div>
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
                  <td class="text-center text-no-wrap">
                    <template v-for="pick in [goalPick(m)]" :key="m.match_id + '-pick'">
                      <v-chip :color="pickColor(pick.label)" size="small" variant="flat" class="font-weight-bold">{{ pick.label }}</v-chip>
                      <v-chip v-if="pick.hit !== null" :color="pick.hit ? 'success' : 'error'" size="x-small" variant="tonal" class="ml-1">
                        {{ pick.hit ? '命中' : '未中' }}
                      </v-chip>
                      <div class="text-caption text-medium-emphasis mt-1">
                        主 {{ fmt1(pick.home) }} · 客 {{ fmt1(pick.guest) }} · 交锋 {{ fmt1(pick.against) }} → 取小 {{ fmt1(pick.value) }}
                      </div>
                    </template>
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
                  <td :colspan="report.mode === 'history' ? 12 : 11" class="pa-0">
                    <div class="detail-wrap pa-4">
                      <div class="text-subtitle-2 font-weight-bold mb-2">历史交锋 · 两队视角的进球 / 丢球走势</div>
                      <v-row>
                        <v-col v-for="team in [m.home_against, m.guest_against]" :key="team.team_id" cols="12" md="6">
                          <div class="d-flex align-center flex-wrap ga-2 mb-2">
                            <span class="font-weight-bold">{{ team.name }}</span>
                            <v-chip size="x-small" variant="tonal">交锋 {{ team.sample }} 场</v-chip>
                            <v-chip size="x-small" variant="tonal" color="success">{{ team.summary.wins }}胜 {{ team.summary.draws }}平 {{ team.summary.losses }}负</v-chip>
                            <v-chip size="x-small" variant="tonal">零封 {{ team.summary.clean_sheets }}</v-chip>
                            <v-chip size="x-small" variant="tonal">未进球 {{ team.summary.failed_to_score }}</v-chip>
                            <v-chip size="x-small" variant="tonal" color="warning">大2.5 {{ team.summary.over25 }}</v-chip>
                            <v-chip size="x-small" variant="tonal" :color="trendColor(team.summary.defense_trend, 'defense')">防守{{ team.summary.defense_trend }}</v-chip>
                          </div>
                          <div class="d-flex align-center ga-1 mb-2">
                            <span class="trend-pair-tag text-warning">总</span>
                            <TrendSparkline :values="seriesTotal(team)" kind="total" :want="team.sample || 2" :width="500" :height="130" />
                          </div>
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
                                <td class="text-no-wrap">{{ formatBeijingDateTime(r.match_time || r.date) }}</td>
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
                          <div v-else class="text-caption text-medium-emphasis">两队在开赛前没有历史交锋记录。</div>
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
.trend-pair {
  display: inline-flex;
  flex-direction: column;
  gap: 2px;
}
.trend-pair-tag {
  font-size: 11px;
  font-weight: 600;
  line-height: 1;
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
