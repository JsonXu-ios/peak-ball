<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { getLastDanceStatistics } from '@/api'
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
  /** 结算盘口；组3 为「初盘→即时盘」的具体盘口变化 */
  line?: string
}

interface Bucket {
  key: string
  title: string
  matched: number
  hit: number
  miss: number
  accuracy: number
  matches: MatchDetail[]
  roi?: number
  roiSample?: number
}

interface Signal {
  key: string
  title: string
  definition: string
  /** 按比赛去重的覆盖场次（同一场可进多个分组，matched 是分组条目合计） */
  covered?: number
  matched: number
  hit: number
  miss: number
  accuracy: number
  buckets: Bucket[]
}

interface Report {
  settled_total: number
  generated_at: string
  signals: Signal[]
  needs_recompute?: boolean
}

const DETAIL_CAP = 300

const loading = ref(false)
const recomputing = ref(false)
const report = ref<Report | null>(null)
const error = ref('')
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

function hasRoi(signal: Signal) {
  return signal.buckets.some((bucket) => typeof bucket.roi === 'number')
}

function cappedMatches(matches: MatchDetail[]) {
  return matches.slice(0, DETAIL_CAP)
}

async function fetchReport(refresh = false) {
  if (refresh) recomputing.value = true
  else loading.value = true
  error.value = ''
  try {
    const { data } = await getLastDanceStatistics(refresh ? { refresh: 1 } : undefined)
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
        <h2 class="text-h5 font-weight-bold">最后一舞</h2>
        <div class="text-body-2 text-medium-emphasis mt-1">
          组合信号验证：按指定规则把既有信号叠加统计（专业信号×盈亏提示、主推≥65%同向、亚盘背离、球数期望方向），并精选警示/盘口偏离中命中率≥60%的信号，每组可下钻比赛明细以人工确认算法。
        </div>
      </div>
      <v-spacer />
      <v-btn :loading="recomputing" color="warning" prepend-icon="mdi-calculator" @click="fetchReport(true)">重新统计</v-btn>
    </div>

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
              <div class="text-body-2 text-medium-emphasis">说明</div>
              <div class="text-caption text-medium-emphasis mt-1">
                每个分组显示「符合条件的场次 / 命中 / 命中率」；命中率按已完赛结果结算。样本数小的分组请结合场次判断。
              </div>
              <div v-if="generatedAtText()" class="text-caption text-medium-emphasis mt-1">统计时间：{{ generatedAtText() }}</div>
            </v-card-text>
          </v-card>
        </v-col>
      </v-row>

      <v-card v-for="signal in report.signals" :key="signal.key" class="mb-5">
        <v-card-title class="pt-5 d-flex align-center flex-wrap ga-2">
          <span>{{ signal.title }}</span>
          <v-chip color="primary" size="small" variant="tonal">覆盖 {{ (signal.covered ?? signal.matched).toLocaleString() }} 场</v-chip>
          <v-chip size="small" variant="text">分组合计 {{ signal.matched.toLocaleString() }} 条（同场可入多组，命中率看各分组）</v-chip>
        </v-card-title>
        <v-card-subtitle class="pb-2 text-wrap">{{ signal.definition }}</v-card-subtitle>

        <v-card-text>
          <div v-if="!signal.buckets.length" class="text-medium-emphasis py-4">暂无符合条件的比赛。</div>
          <v-table v-else density="comfortable" class="stat-table">
            <thead>
              <tr>
                <th>分组</th>
                <th class="text-right">符合场次</th>
                <th class="text-right">命中</th>
                <th class="text-right">命中率</th>
                <th v-if="hasRoi(signal)" class="text-right">ROI</th>
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
                  <td v-if="hasRoi(signal)" class="text-right">
                    <v-chip v-if="typeof bucket.roi === 'number'" size="small" variant="tonal" :color="bucket.roi >= 100 ? 'success' : 'warning'">
                      {{ bucket.roi.toFixed(1) }}%（{{ bucket.roiSample }}注）
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
                  <td :colspan="hasRoi(signal) ? 6 : 5" class="pa-0">
                    <div class="detail-wrap">
                      <MatchDetailTable :matches="cappedMatches(bucket.matches)" :total="bucket.matched" :cap="DETAIL_CAP" show-value />
                    </div>
                  </td>
                </tr>
              </template>
            </tbody>
          </v-table>
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
