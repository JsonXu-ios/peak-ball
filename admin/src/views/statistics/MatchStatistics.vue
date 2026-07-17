<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
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
  roi?: number
  roiSample?: number
  flag?: 'red' | 'black' | ''
}

interface RadarAxis {
  axis: string
  sample: number
  accuracy: number
  score: number
}

interface PickProfile {
  radar: RadarAxis[]
  asianBuckets: HeatBucket[]
  goalBuckets: HeatBucket[]
}

interface Signal {
  key: string
  title: string
  definition: string
  matched: number
  hit: number
  miss: number
  accuracy: number
  matches?: MatchDetail[]
  buckets?: HeatBucket[]
  /** 反噬指数扩展（仅我的大小球等二元玩法） */
  z?: number
  shrunkMissRate?: number
  fadeEv?: number
  fadeTriggered?: boolean
  /** 真实赔率回报（我的选择类信号） */
  roi?: number
  roiSample?: number
}

interface Report {
  settled_total: number
  start_date: string
  end_date: string
  generated_at: string
  signals: Signal[]
  pick_profile?: PickProfile
  needs_recompute?: boolean
}

const DETAIL_CAP = 300

const loading = ref(false)
const recomputing = ref(false)
const startDate = ref('')
const endDate = ref('')
const report = ref<Report | null>(null)
const error = ref('')
// which detail tables are open, keyed by signal/bucket key
const expanded = reactive<Record<string, boolean>>({})

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

// ---- 六边形雷达坐标 ----
const RADAR_CX = 110
const RADAR_CY = 108
const RADAR_R = 78

function radarPoint(index: number, total: number, ratio: number): { x: number; y: number } {
  const angle = (Math.PI * 2 * index) / total - Math.PI / 2
  return {
    x: RADAR_CX + Math.cos(angle) * RADAR_R * ratio,
    y: RADAR_CY + Math.sin(angle) * RADAR_R * ratio,
  }
}

function radarPolygon(axes: RadarAxis[], ratio: number): string {
  return axes.map((_, index) => {
    const point = radarPoint(index, axes.length, ratio)
    return `${point.x.toFixed(1)},${point.y.toFixed(1)}`
  }).join(' ')
}

function radarScorePolygon(axes: RadarAxis[]): string {
  return axes.map((axis, index) => {
    const point = radarPoint(index, axes.length, Math.max(0.04, axis.score / 100))
    return `${point.x.toFixed(1)},${point.y.toFixed(1)}`
  }).join(' ')
}

function radarLabelPos(index: number, total: number): { x: number; y: number } {
  return radarPoint(index, total, 1.22)
}

function flagColor(flag?: string) {
  if (flag === 'red') return 'error'
  if (flag === 'black') return 'secondary'
  return undefined
}

function flagLabel(flag?: string) {
  if (flag === 'red') return '红区'
  if (flag === 'black') return '黑区'
  return ''
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
          基于全部完赛比赛，逐个信号统计符合条件的场次、命中率，并可下钻查看具体比赛。所有计算均在后端完成。
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

      <!-- 我的画像：六边形 + 盘口红黑分布 -->
      <v-card v-if="report.pick_profile" class="mb-5">
        <v-card-title class="pt-5">我的画像（基于已录选择）</v-card-title>
        <v-card-subtitle class="text-wrap">六边形按“命中率相对基准”归一化(0-100)；样本&lt;3的轴记0分。红区=命中≥65%且样本≥5，黑区=命中≤35%且样本≥5。</v-card-subtitle>
        <v-card-text>
          <v-row>
            <v-col cols="12" md="5" class="d-flex justify-center align-center">
              <svg viewBox="0 0 220 216" style="max-width: 320px; width: 100%">
                <polygon
                  v-for="ring in [1, 0.75, 0.5, 0.25]"
                  :key="`ring-${ring}`"
                  :points="radarPolygon(report.pick_profile.radar, ring)"
                  fill="none"
                  stroke="currentColor"
                  stroke-opacity="0.15"
                />
                <line
                  v-for="(axis, index) in report.pick_profile.radar"
                  :key="`spoke-${axis.axis}`"
                  :x1="RADAR_CX"
                  :y1="RADAR_CY"
                  :x2="radarPoint(index, report.pick_profile.radar.length, 1).x"
                  :y2="radarPoint(index, report.pick_profile.radar.length, 1).y"
                  stroke="currentColor"
                  stroke-opacity="0.15"
                />
                <polygon
                  :points="radarScorePolygon(report.pick_profile.radar)"
                  fill="rgb(33,150,243)"
                  fill-opacity="0.35"
                  stroke="rgb(33,150,243)"
                  stroke-width="2"
                />
                <text
                  v-for="(axis, index) in report.pick_profile.radar"
                  :key="`label-${axis.axis}`"
                  :x="radarLabelPos(index, report.pick_profile.radar.length).x"
                  :y="radarLabelPos(index, report.pick_profile.radar.length).y"
                  text-anchor="middle"
                  dominant-baseline="middle"
                  fill="currentColor"
                  font-size="10"
                  font-weight="700"
                >
                  {{ axis.axis }} {{ Math.round(axis.score) }}
                </text>
              </svg>
            </v-col>
            <v-col cols="12" md="7">
              <v-table density="compact" class="stat-table mb-3">
                <thead>
                  <tr><th>维度</th><th class="text-right">样本</th><th class="text-right">命中率/ROI</th><th class="text-right">得分</th></tr>
                </thead>
                <tbody>
                  <tr v-for="axis in report.pick_profile.radar" :key="axis.axis">
                    <td class="font-weight-medium">{{ axis.axis }}</td>
                    <td class="text-right">{{ axis.sample }}</td>
                    <td class="text-right">{{ axis.accuracy.toFixed(1) }}%</td>
                    <td class="text-right">{{ Math.round(axis.score) }}</td>
                  </tr>
                </tbody>
              </v-table>
            </v-col>
          </v-row>

          <template v-for="group in [
            { title: '亚盘盘型分布（胜平负+让球选择）', rows: report.pick_profile.asianBuckets },
            { title: '大小球盘口分布（大小球选择）', rows: report.pick_profile.goalBuckets },
          ]" :key="group.title">
            <div v-if="group.rows.length" class="mb-2 mt-2 text-subtitle-2 font-weight-bold">{{ group.title }}</div>
            <v-table v-if="group.rows.length" density="compact" class="stat-table">
              <thead>
                <tr><th>盘型</th><th class="text-right">场次</th><th class="text-right">命中</th><th class="text-right">命中率</th><th class="text-right">ROI</th><th class="text-right">标记</th><th class="text-right">明细</th></tr>
              </thead>
              <tbody>
                <template v-for="bucket in group.rows" :key="bucket.key">
                  <tr>
                    <td class="font-weight-medium">{{ bucket.title }}</td>
                    <td class="text-right">{{ bucket.matched }}</td>
                    <td class="text-right">{{ bucket.hit }}</td>
                    <td class="text-right">
                      <v-chip :color="accuracyColor(bucket.accuracy, bucket.matched)" size="x-small" variant="tonal">{{ bucket.accuracy.toFixed(1) }}%</v-chip>
                    </td>
                    <td class="text-right">{{ typeof bucket.roi === 'number' ? bucket.roi.toFixed(1) + '%' : '-' }}</td>
                    <td class="text-right">
                      <v-chip v-if="flagLabel(bucket.flag)" :color="flagColor(bucket.flag)" size="x-small" variant="flat">{{ flagLabel(bucket.flag) }}</v-chip>
                      <span v-else>-</span>
                    </td>
                    <td class="text-right">
                      <v-btn size="x-small" variant="text" @click="toggle(bucket.key)">{{ expanded[bucket.key] ? '收起' : '查看' }}</v-btn>
                    </td>
                  </tr>
                  <tr v-if="expanded[bucket.key]">
                    <td colspan="7" class="pa-0">
                      <div class="detail-wrap">
                        <MatchDetailTable :matches="cappedMatches(bucket.matches)" :total="bucket.matched" :cap="DETAIL_CAP" show-value />
                      </div>
                    </td>
                  </tr>
                </template>
              </tbody>
            </v-table>
          </template>
        </v-card-text>
      </v-card>

      <v-card v-for="signal in report.signals" :key="signal.key" class="mb-5">
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
          <template v-if="typeof signal.z === 'number'">
            <v-chip size="small" variant="tonal" :color="signal.z <= -1.64 ? 'error' : 'default'">z={{ signal.z.toFixed(2) }}</v-chip>
            <v-chip size="small" variant="tonal">收缩错误率 {{ signal.shrunkMissRate?.toFixed(1) }}%</v-chip>
            <v-chip size="small" variant="tonal" :color="(signal.fadeEv ?? 0) > 6 ? 'success' : 'default'">反买EV {{ signal.fadeEv?.toFixed(1) }}%</v-chip>
            <v-chip v-if="signal.fadeTriggered" color="error" size="small" variant="flat">反噬触发：反买你的大小球方向</v-chip>
          </template>
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
                  <th class="text-right">ROI</th>
                  <th class="text-right">标记</th>
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
                    <td class="text-right">{{ typeof bucket.roi === 'number' ? bucket.roi.toFixed(1) + '%' : '-' }}</td>
                    <td class="text-right">
                      <v-chip v-if="flagLabel(bucket.flag)" :color="flagColor(bucket.flag)" size="x-small" variant="flat">{{ flagLabel(bucket.flag) }}</v-chip>
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
                    <td colspan="7" class="pa-0">
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

    <v-card v-else-if="!loading && !report?.needs_recompute" variant="tonal">
      <v-card-text class="text-medium-emphasis">暂无统计结果。</v-card-text>
    </v-card>
  </div>
</template>

<style scoped>
.stat-table th {
  white-space: nowrap;
}
.detail-wrap {
  max-height: 460px;
  overflow: auto;
  background: rgba(var(--v-theme-surface-light), 0.4);
}
</style>
