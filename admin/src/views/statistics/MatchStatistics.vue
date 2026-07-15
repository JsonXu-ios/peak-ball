<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { getMatchStatistics } from '@/api'
import MatchDetailTable from './MatchDetailTable.vue'

interface MatchDetail {
  match_id: string
  date: string
  home: string
  guest: string
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
  tier: number
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
  matched: number
  hit: number
  miss: number
  accuracy: number
  matches?: MatchDetail[]
  buckets?: HeatBucket[]
}

interface Report {
  settled_total: number
  start_date: string
  end_date: string
  generated_at: string
  signals: Signal[]
}

const DETAIL_CAP = 300

const loading = ref(false)
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

async function fetchReport() {
  loading.value = true
  error.value = ''
  try {
    const { data } = await getMatchStatistics({
      start_date: startDate.value || undefined,
      end_date: endDate.value || undefined,
    })
    report.value = data as Report
    Object.keys(expanded).forEach((key) => delete expanded[key])
  } catch (requestError) {
    error.value = requestError instanceof Error ? requestError.message : '加载统计失败'
  } finally {
    loading.value = false
  }
}

function resetRange() {
  startDate.value = ''
  endDate.value = ''
  fetchReport()
}

onMounted(fetchReport)
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
      <v-btn :loading="loading" color="primary" prepend-icon="mdi-refresh" @click="fetchReport">重新统计</v-btn>
    </div>

    <v-card class="mb-5">
      <v-card-text class="d-flex flex-wrap align-center ga-3">
        <v-text-field v-model="startDate" type="date" label="开始日期" hide-details style="max-width: 210px" />
        <v-text-field v-model="endDate" type="date" label="结束日期" hide-details style="max-width: 210px" />
        <v-btn color="primary" variant="tonal" :loading="loading" @click="fetchReport">应用日期</v-btn>
        <v-btn variant="text" @click="resetRange">查看全部</v-btn>
      </v-card-text>
    </v-card>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-5">{{ error }}</v-alert>

    <template v-if="report">
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

      <v-card v-for="signal in report.signals" :key="signal.key" class="mb-5">
        <v-card-title class="pt-5 d-flex align-center flex-wrap ga-2">
          <span>{{ signal.title }}</span>
          <v-chip color="primary" size="small" variant="tonal">符合 {{ signal.matched.toLocaleString() }} 场</v-chip>
          <v-chip :color="accuracyColor(signal.accuracy, signal.matched)" size="small" variant="tonal">
            命中率 {{ signal.matched ? signal.accuracy.toFixed(2) + '%' : '-' }}
          </v-chip>
          <v-chip size="small" variant="text">命中 {{ signal.hit }} / 未命中 {{ signal.miss }}</v-chip>
        </v-card-title>
        <v-card-subtitle class="pb-2 text-wrap">{{ signal.definition }}</v-card-subtitle>

        <v-card-text>
          <!-- Heat signals: bucket table, each bucket drills into its matches -->
          <template v-if="signal.buckets">
            <v-table density="comfortable" class="stat-table">
              <thead>
                <tr>
                  <th>热度档位</th>
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

    <v-card v-else-if="!loading" variant="tonal">
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
