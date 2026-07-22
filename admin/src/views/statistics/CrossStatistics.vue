<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getCrossStatistics } from '@/api'

interface TopBucket {
  bucket: string
  count: number
  pct: number
  basePct?: number
}

interface DistRow {
  key: string
  title: string
  direction: string
  directionLabel: string
  sample: number
  mode: 'follow' | 'inverse' | ''
  top: TopBucket[]
}

interface PairRow {
  keyA: string
  titleA: string
  marketA: string
  keyB: string
  titleB: string
  marketB: string
  sample: number
  accA: number
  accB: number
  accBoth: number
  globalA: number
  sampleA: number
  globalB: number
  sampleB: number
  upliftA: number
  upliftB: number
  freshA: boolean
  freshB: boolean
}

interface DerivedMarket {
  direction: string
  label: string
  signals: number
  votes: number
}

interface DerivedDist {
  source: string
  top: TopBucket[]
}

interface ComboRow {
  title: string
  withCond: string
  pick: string
  mode: 'follow' | 'inverse'
  accuracy: number
  sample: number
}

interface DerivedRow {
  matchId: string
  date: string
  state: string
  matchTime?: string
  league?: string
  home: string
  guest: string
  homeLogo?: string
  guestLogo?: string
  spf?: DerivedMarket
  asian?: DerivedMarket
  dxq?: DerivedMarket
  score?: DerivedDist
  goals?: DerivedDist
  combos?: ComboRow[]
}

interface Report {
  needs_recompute: boolean
  stats_generated_at?: string
  settled_total?: number
  upcoming_total?: number
  days?: number
  min_sample?: number
  high_cutoff?: number
  low_cutoff?: number
  pair_uplift_pp?: number
  fresh_pairs?: number
  spf_score: DistRow[]
  dxq_goals: DistRow[]
  pairs: PairRow[]
  derived: DerivedRow[]
}

const MARKET_LABELS: Record<string, string> = {
  spf: '胜平负',
  asian: '让球/亚盘',
  dxq: '大小球',
  score: '比分',
}

const loading = ref(false)
const recomputing = ref(false)
const error = ref('')
const days = ref(3)
const tab = ref('derived')
const report = ref<Report | null>(null)

async function fetchReport(refresh = false) {
  if (refresh) recomputing.value = true
  else loading.value = true
  error.value = ''
  try {
    const { data } = await getCrossStatistics({ days: days.value, ...(refresh ? { refresh: 1 } : {}) })
    report.value = data as Report
  } catch (requestError) {
    const err = requestError as { response?: { data?: { error?: string } }; message?: string }
    error.value = err.response?.data?.error || err.message || '加载失败'
  } finally {
    loading.value = false
    recomputing.value = false
  }
}

const WEEKDAYS = ['周日', '周一', '周二', '周三', '周四', '周五', '周六']

function dateHeader(date: string): string {
  const parsed = new Date(`${date}T00:00:00`)
  if (Number.isNaN(parsed.getTime())) return date
  return `${date} ${WEEKDAYS[parsed.getDay()]}`
}

function groupedDerived(rows: DerivedRow[]): Array<{ date: string; rows: DerivedRow[] }> {
  const groups: Array<{ date: string; rows: DerivedRow[] }> = []
  for (const row of rows) {
    const last = groups[groups.length - 1]
    if (last && last.date === row.date) last.rows.push(row)
    else groups.push({ date: row.date, rows: [row] })
  }
  return groups
}

function timeOnly(row: DerivedRow): string {
  const raw = row.matchTime || ''
  return raw.length >= 16 ? raw.slice(11, 16) : raw
}

function modeColor(mode: string) {
  if (mode === 'follow') return 'success'
  if (mode === 'inverse') return 'error'
  return 'grey'
}

function modeLabel(mode: string) {
  if (mode === 'follow') return '跟'
  if (mode === 'inverse') return '反向'
  return '未上岗'
}

function accColor(acc: number) {
  const high = report.value?.high_cutoff ?? 70
  const low = report.value?.low_cutoff ?? 30
  if (acc >= high) return 'success'
  if (acc <= low) return 'error'
  return undefined
}

/** 信号分布相对全库基线的放大倍数，>1.3 视为显著新维度 */
function lift(bucket: TopBucket): number | null {
  if (typeof bucket.basePct !== 'number' || bucket.basePct <= 0) return null
  return bucket.pct / bucket.basePct
}

function upliftText(value: number): string {
  return `${value > 0 ? '+' : ''}${value.toFixed(1)}pp`
}

const derivedCount = computed(() => report.value?.derived.length ?? 0)

onMounted(() => fetchReport(false))
</script>

<template>
  <div>
    <div class="d-flex flex-wrap align-center mb-4 ga-3">
      <div>
        <h2 class="text-h5 font-weight-bold">交叉信号分析</h2>
        <div class="text-body-2 text-medium-emphasis mt-1">
          把信号库两两交叉、并与比分/总进球联动结算：胜平负信号→比分分布、大小球信号→总进球分布、信号组合条件命中率；再基于上岗信号对待赛比赛做五市场推演。
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
        <div class="text-h6 font-weight-bold mb-2">交叉库尚未结算</div>
        <div class="text-body-2 text-medium-emphasis mb-4">点击「重新计算」对全部完赛比赛做一次交叉结算（约几秒），之后打开页面只做轻量匹配。</div>
        <v-btn :loading="recomputing" color="warning" prepend-icon="mdi-calculator" @click="fetchReport(true)">重新计算</v-btn>
      </v-card-text>
    </v-card>

    <template v-else-if="report">
      <v-row class="mb-1">
        <v-col cols="12" md="3">
          <v-card color="primary" variant="tonal">
            <v-card-text>
              <div class="text-body-2">五市场推演（未来 {{ report.days }} 天）</div>
              <div class="text-h4 font-weight-bold mt-1">{{ derivedCount }}</div>
              <div class="text-caption mt-1">待赛 {{ report.upcoming_total }} 场</div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="12" md="3">
          <v-card color="success" variant="tonal">
            <v-card-text>
              <div class="text-body-2">组合演变出的新维度</div>
              <div class="text-h4 font-weight-bold mt-1">{{ report.fresh_pairs ?? 0 }}</div>
              <div class="text-caption mt-1">单独未上岗、组合后命中率≥{{ report.high_cutoff }}%或≤{{ report.low_cutoff }}%</div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="12" md="3">
          <v-card variant="tonal">
            <v-card-text>
              <div class="text-body-2">信号→比分 / 信号→总进球 维度</div>
              <div class="text-h4 font-weight-bold mt-1">{{ report.spf_score.length }} / {{ report.dxq_goals.length }}</div>
              <div class="text-caption mt-1">样本≥{{ report.min_sample }} 的方向分布</div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="12" md="3">
          <v-card variant="tonal">
            <v-card-text>
              <div class="text-body-2">结算基数</div>
              <div class="text-h4 font-weight-bold mt-1">{{ report.settled_total?.toLocaleString() }}</div>
              <div class="text-caption mt-1">结算时间：{{ report.stats_generated_at }}</div>
            </v-card-text>
          </v-card>
        </v-col>
      </v-row>

      <v-card>
        <v-tabs v-model="tab" color="primary">
          <v-tab value="derived">待赛五市场推演 ({{ derivedCount }})</v-tab>
          <v-tab value="pairs">信号组合 ({{ report.pairs.length }})</v-tab>
          <v-tab value="spfScore">胜平负×比分 ({{ report.spf_score.length }})</v-tab>
          <v-tab value="dxqGoals">大小球×总进球 ({{ report.dxq_goals.length }})</v-tab>
        </v-tabs>
        <v-divider />

        <v-window v-model="tab">
          <!-- ==== 待赛五市场推演 ==== -->
          <v-window-item value="derived">
            <div v-if="!report.derived.length" class="text-medium-emphasis py-8 text-center">
              未来 {{ report.days }} 天暂无可推演的比赛（无上岗信号触发、也无历史组合命中）。
            </div>
            <template v-for="group in groupedDerived(report.derived)" :key="group.date">
              <div class="date-header px-4 py-2">
                {{ dateHeader(group.date) }}
                <span class="text-medium-emphasis font-weight-regular">· {{ group.rows.length }} 场</span>
              </div>
              <div v-for="row in group.rows" :key="row.matchId" class="rec-row d-flex px-4 py-3">
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
                <div class="flex-grow-1 d-flex flex-column justify-center ga-2">
                  <div v-if="row.spf" class="d-flex align-center flex-wrap ga-2">
                    <v-chip color="indigo" size="small" variant="flat" class="market-chip shrink-0">胜平负</v-chip>
                    <span class="rec-pick font-weight-bold">{{ row.spf.label }}</span>
                    <span class="text-caption text-disabled">{{ row.spf.signals }} 条上岗信号 · 净票 {{ row.spf.votes }}</span>
                  </div>
                  <div v-if="row.asian" class="d-flex align-center flex-wrap ga-2">
                    <v-chip color="orange-darken-2" size="small" variant="flat" class="market-chip shrink-0">让球</v-chip>
                    <span class="rec-pick font-weight-bold">{{ row.asian.label }}</span>
                    <span class="text-caption text-disabled">{{ row.asian.signals }} 条上岗信号 · 净票 {{ row.asian.votes }}</span>
                  </div>
                  <div v-if="row.dxq" class="d-flex align-center flex-wrap ga-2">
                    <v-chip color="teal" size="small" variant="flat" class="market-chip shrink-0">大小球</v-chip>
                    <span class="rec-pick font-weight-bold">{{ row.dxq.label }}</span>
                    <span class="text-caption text-disabled">{{ row.dxq.signals }} 条上岗信号 · 净票 {{ row.dxq.votes }}</span>
                  </div>
                  <div v-if="row.score" class="d-flex align-center flex-wrap ga-2">
                    <v-chip color="purple" size="small" variant="flat" class="market-chip shrink-0">比分</v-chip>
                    <v-chip v-for="item in row.score.top.slice(0, 3)" :key="item.bucket" size="small" variant="tonal" color="purple">
                      {{ item.bucket }} · {{ item.pct.toFixed(1) }}%
                    </v-chip>
                    <span class="text-caption text-disabled">{{ row.score.source }}</span>
                  </div>
                  <div v-if="row.goals" class="d-flex align-center flex-wrap ga-2">
                    <v-chip color="cyan-darken-2" size="small" variant="flat" class="market-chip shrink-0">总进球</v-chip>
                    <v-chip v-for="item in row.goals.top.slice(0, 3)" :key="item.bucket" size="small" variant="tonal" color="cyan-darken-2">
                      {{ item.bucket }} 球 · {{ item.pct.toFixed(1) }}%
                    </v-chip>
                    <span class="text-caption text-disabled">{{ row.goals.source }}</span>
                  </div>
                  <div v-for="(combo, index) in row.combos ?? []" :key="index" class="d-flex align-center flex-wrap ga-2">
                    <v-chip color="deep-purple" size="small" variant="flat" class="market-chip shrink-0">组合</v-chip>
                    <span class="rec-pick font-weight-bold">{{ combo.pick }}</span>
                    <v-chip :color="modeColor(combo.mode)" size="x-small" variant="tonal" class="shrink-0">
                      {{ combo.mode === 'follow' ? '跟' : '反向' }} · 组合命中 {{ combo.accuracy.toFixed(0) }}% / {{ combo.sample }}场
                    </v-chip>
                    <span class="text-caption text-disabled">「{{ combo.title }}」当「{{ combo.withCond }}」同时触发</span>
                  </div>
                </div>
              </div>
            </template>
          </v-window-item>

          <!-- ==== 信号组合 ==== -->
          <v-window-item value="pairs">
            <div class="px-4 pt-3 text-caption text-medium-emphasis">
              A、B 同场同时触发时各自的条件命中率（A|B、B|A）与其全库命中率对比；绿色=组合后新达到≥{{ report.high_cutoff }}%或≤{{ report.low_cutoff }}%（新维度），仅展示提升≥{{ report.pair_uplift_pp }}pp 或跨过阈值的组合。
            </div>
            <div v-if="!report.pairs.length" class="text-medium-emphasis py-8 text-center">暂无满足样本要求的组合。</div>
            <v-table v-else density="compact" class="mt-2">
              <thead>
                <tr>
                  <th>信号 A</th>
                  <th>信号 B</th>
                  <th class="text-right">同触样本</th>
                  <th class="text-right">A|B 命中</th>
                  <th class="text-right">A 全库</th>
                  <th class="text-right">B|A 命中</th>
                  <th class="text-right">B 全库</th>
                  <th class="text-right">双中率</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="pair in report.pairs" :key="pair.keyA + '|' + pair.keyB">
                  <td>
                    <div class="font-weight-medium">{{ pair.titleA }}</div>
                    <div class="text-caption text-disabled">{{ MARKET_LABELS[pair.marketA] ?? pair.marketA }}</div>
                  </td>
                  <td>
                    <div class="font-weight-medium">{{ pair.titleB }}</div>
                    <div class="text-caption text-disabled">{{ MARKET_LABELS[pair.marketB] ?? pair.marketB }}</div>
                  </td>
                  <td class="text-right">{{ pair.sample }}</td>
                  <td class="text-right">
                    <v-chip :color="accColor(pair.accA)" size="small" :variant="pair.freshA ? 'flat' : 'tonal'">
                      {{ pair.accA.toFixed(1) }}%
                      <template v-if="pair.freshA">·新</template>
                    </v-chip>
                    <div class="text-caption" :class="pair.upliftA >= 0 ? 'text-success' : 'text-error'">{{ upliftText(pair.upliftA) }}</div>
                  </td>
                  <td class="text-right text-medium-emphasis">{{ pair.globalA.toFixed(1) }}% ({{ pair.sampleA }})</td>
                  <td class="text-right">
                    <v-chip :color="accColor(pair.accB)" size="small" :variant="pair.freshB ? 'flat' : 'tonal'">
                      {{ pair.accB.toFixed(1) }}%
                      <template v-if="pair.freshB">·新</template>
                    </v-chip>
                    <div class="text-caption" :class="pair.upliftB >= 0 ? 'text-success' : 'text-error'">{{ upliftText(pair.upliftB) }}</div>
                  </td>
                  <td class="text-right text-medium-emphasis">{{ pair.globalB.toFixed(1) }}% ({{ pair.sampleB }})</td>
                  <td class="text-right">{{ pair.accBoth.toFixed(1) }}%</td>
                </tr>
              </tbody>
            </v-table>
          </v-window-item>

          <!-- ==== 胜平负×比分 ==== -->
          <v-window-item value="spfScore">
            <div class="px-4 pt-3 text-caption text-medium-emphasis">
              胜平负信号触发且指向某方向时，最终比分（按竞彩比分选项分桶）的分布；「基线」为全库该比分占比，倍数≥1.3 高亮，代表该信号显著放大了这个比分的概率。
            </div>
            <div v-if="!report.spf_score.length" class="text-medium-emphasis py-8 text-center">暂无满足样本要求的分布。</div>
            <v-table v-else density="compact" class="mt-2">
              <thead>
                <tr>
                  <th>信号</th>
                  <th>指向</th>
                  <th class="text-right">样本</th>
                  <th class="text-right">模式</th>
                  <th>比分分布 Top（占比 / 基线）</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="row in report.spf_score" :key="row.key + '|' + row.direction">
                  <td class="font-weight-medium">{{ row.title }}</td>
                  <td>{{ row.directionLabel }}</td>
                  <td class="text-right">{{ row.sample }}</td>
                  <td class="text-right">
                    <v-chip :color="modeColor(row.mode)" size="x-small" :variant="row.mode ? 'flat' : 'tonal'">{{ modeLabel(row.mode) }}</v-chip>
                  </td>
                  <td class="py-2">
                    <span v-for="item in row.top" :key="item.bucket" class="mr-2">
                      <v-chip size="small" :color="(lift(item) ?? 0) >= 1.3 ? 'success' : undefined" :variant="(lift(item) ?? 0) >= 1.3 ? 'flat' : 'tonal'">
                        {{ item.bucket }} · {{ item.pct.toFixed(1) }}%
                        <template v-if="typeof item.basePct === 'number'"> / {{ item.basePct.toFixed(1) }}%</template>
                      </v-chip>
                    </span>
                  </td>
                </tr>
              </tbody>
            </v-table>
          </v-window-item>

          <!-- ==== 大小球×总进球 ==== -->
          <v-window-item value="dxqGoals">
            <div class="px-4 pt-3 text-caption text-medium-emphasis">
              大小球信号触发且判大/判小时，总进球数（竞彩总进球选项 0~7+）的分布；「基线」为全库该球数占比，倍数≥1.3 高亮。
            </div>
            <div v-if="!report.dxq_goals.length" class="text-medium-emphasis py-8 text-center">暂无满足样本要求的分布。</div>
            <v-table v-else density="compact" class="mt-2">
              <thead>
                <tr>
                  <th>信号</th>
                  <th>指向</th>
                  <th class="text-right">样本</th>
                  <th class="text-right">模式</th>
                  <th>总进球分布 Top（占比 / 基线）</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="row in report.dxq_goals" :key="row.key + '|' + row.direction">
                  <td class="font-weight-medium">{{ row.title }}</td>
                  <td>{{ row.directionLabel }}</td>
                  <td class="text-right">{{ row.sample }}</td>
                  <td class="text-right">
                    <v-chip :color="modeColor(row.mode)" size="x-small" :variant="row.mode ? 'flat' : 'tonal'">{{ modeLabel(row.mode) }}</v-chip>
                  </td>
                  <td class="py-2">
                    <span v-for="item in row.top" :key="item.bucket" class="mr-2">
                      <v-chip size="small" :color="(lift(item) ?? 0) >= 1.3 ? 'success' : undefined" :variant="(lift(item) ?? 0) >= 1.3 ? 'flat' : 'tonal'">
                        {{ item.bucket }} 球 · {{ item.pct.toFixed(1) }}%
                        <template v-if="typeof item.basePct === 'number'"> / {{ item.basePct.toFixed(1) }}%</template>
                      </v-chip>
                    </span>
                  </td>
                </tr>
              </tbody>
            </v-table>
          </v-window-item>
        </v-window>
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
