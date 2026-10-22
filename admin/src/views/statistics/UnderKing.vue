<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { getUnderKingStatistics } from '@/api'
import { beijingToday } from '@/utils/date'
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
  line?: string
  exp_goals?: string
  heat_goals?: string
}

/** 一个维度一行：该维度判小球的场次与命中率（不按盘口、不按热度档拆） */
interface Row {
  key: string
  title: string
  /** 后端说明本行是原样保留还是筛过；只在展开明细时显示 */
  note?: string
  matched: number
  hit: number
  miss: number
  accuracy: number
  matches: MatchDetail[]
}

interface Report {
  settled_total: number
  start_date: string
  end_date: string
  generated_at: string
  rows: Row[]
  needs_recompute?: boolean
}

const DETAIL_CAP = 300

const loading = ref(false)
const recomputing = ref(false)
const date = ref(beijingToday())
const report = ref<Report | null>(null)
const error = ref('')
const expanded = reactive<Record<string, boolean>>({})

const rows = computed(() => report.value?.rows ?? [])
const bestRow = computed(() => rows.value[0])

function toggle(key: string) {
  expanded[key] = !expanded[key]
}

function accuracyColor(accuracy: number, matched: number) {
  if (!matched) return 'default'
  if (accuracy >= 60) return 'success'
  if (accuracy >= 50) return 'primary'
  return 'warning'
}

function sourceDate() {
  return report.value?.start_date || date.value
}

function generatedAtText() {
  const raw = report.value?.generated_at
  if (!raw) return ''
  return raw.replace('T', ' ').slice(0, 19)
}

async function fetchReport(refresh = false) {
  if (!date.value) {
    error.value = '请选择日期'
    return
  }
  if (refresh) recomputing.value = true
  else loading.value = true
  error.value = ''
  try {
    const { data } = await getUnderKingStatistics({
      start_date: date.value,
      end_date: date.value,
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

function resetDate() {
  date.value = beijingToday()
  fetchReport(false)
}

onMounted(() => fetchReport(false))
</script>

<template>
  <div>
    <div class="d-flex flex-wrap align-center mb-4 ga-3">
      <div>
        <h2 class="text-h5 font-weight-bold">
          <v-icon color="info" class="mr-1">mdi-crown</v-icon>小球王
        </h2>
        <div class="text-body-2 text-medium-emphasis mt-1">
          各维度只取<strong>推荐方向为小球</strong>的场次，看总命中率。口径与明细见【完赛基础统计】。
        </div>
      </div>
      <v-spacer />
      <v-btn :loading="recomputing" color="warning" prepend-icon="mdi-calculator" @click="fetchReport(true)">重新计算所选日期</v-btn>
    </div>

    <v-card class="mb-5">
      <v-card-text class="d-flex flex-wrap align-center ga-3">
        <v-text-field v-model="date" type="date" label="日期（北京时间）" hide-details style="max-width: 230px" />
        <v-btn color="primary" variant="tonal" :loading="loading" @click="fetchReport(false)">应用日期（即时计算）</v-btn>
        <v-btn variant="text" @click="resetDate">今天</v-btn>
        <v-spacer />
        <span v-if="generatedAtText()" class="text-caption text-medium-emphasis">统计时间：{{ generatedAtText() }}</span>
      </v-card-text>
    </v-card>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-5">{{ error }}</v-alert>

    <v-card v-if="report?.needs_recompute" variant="tonal" color="warning" class="mb-5">
      <v-card-text class="text-center py-10">
        <div class="text-h6 font-weight-bold mb-2">统计结果尚未生成</div>
        <div class="text-body-2 text-medium-emphasis mb-4">
          点击「重新计算所选日期」查看当天统计，不修改全量统计快照。
        </div>
        <v-btn :loading="recomputing" color="warning" prepend-icon="mdi-calculator" @click="fetchReport(true)">重新计算所选日期</v-btn>
      </v-card-text>
    </v-card>

    <template v-if="report && !report.needs_recompute">
      <v-row class="mb-1">
        <v-col cols="12" md="3">
          <v-card color="info" variant="tonal">
            <v-card-text>
              <div class="text-body-2">总比赛（纳入完赛场次）</div>
              <div class="text-h4 font-weight-bold mt-1">{{ report.settled_total.toLocaleString() }}</div>
              <div class="text-caption mt-1">{{ sourceDate() }}</div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="12" md="3">
          <v-card variant="tonal">
            <v-card-text>
              <div class="text-body-2 text-medium-emphasis">判小球的维度</div>
              <div class="text-h4 font-weight-bold mt-1">{{ rows.length }}</div>
            </v-card-text>
          </v-card>
        </v-col>
        <v-col cols="12" md="6">
          <v-card variant="tonal">
            <v-card-text>
              <div class="text-body-2 text-medium-emphasis">当前最准</div>
              <div v-if="bestRow" class="text-subtitle-1 font-weight-medium mt-1">
                {{ bestRow.title }} —— {{ bestRow.accuracy.toFixed(2) }}%（{{ bestRow.hit }}/{{ bestRow.matched }} 场）
              </div>
              <div v-else class="text-subtitle-1 mt-1">-</div>
              <div class="text-caption text-medium-emphasis mt-1">
                各维度是同一批比赛的不同切法，场次会重复，只能横向比命中率、不能相加。先看场次再看百分比。
              </div>
            </v-card-text>
          </v-card>
        </v-col>
      </v-row>

      <v-card class="mt-6">
        <v-card-title class="pt-5 d-flex align-center flex-wrap ga-2">
          <span>各维度判小球命中率</span>
          <v-chip color="info" size="small" variant="tonal">按命中率从高到低</v-chip>
        </v-card-title>
        <v-card-text>
          <div v-if="!rows.length" class="text-medium-emphasis py-4">暂无判小球方向的维度。</div>
          <v-table v-else density="comfortable" class="stat-table">
            <thead>
              <tr>
                <th class="text-right" style="width: 56px">#</th>
                <th>维度</th>
                <th class="text-right">判小球场次</th>
                <th class="text-right">命中</th>
                <th class="text-right">未命中</th>
                <th class="text-right">命中率</th>
                <th class="text-right">明细</th>
              </tr>
            </thead>
            <tbody>
              <template v-for="(row, index) in rows" :key="row.key">
                <tr>
                  <td class="text-right font-weight-bold">{{ index + 1 }}</td>
                  <td class="font-weight-medium">{{ row.title }}</td>
                  <td class="text-right">{{ row.matched.toLocaleString() }}</td>
                  <td class="text-right">{{ row.hit.toLocaleString() }}</td>
                  <td class="text-right">{{ row.miss.toLocaleString() }}</td>
                  <td class="text-right">
                    <v-chip :color="accuracyColor(row.accuracy, row.matched)" size="small" variant="tonal">
                      {{ row.accuracy.toFixed(2) }}%
                    </v-chip>
                  </td>
                  <td class="text-right">
                    <v-btn size="small" variant="text" @click="toggle(row.key)">
                      {{ expanded[row.key] ? '收起' : '查看' }}
                    </v-btn>
                  </td>
                </tr>
                <tr v-if="expanded[row.key]">
                  <td colspan="7" class="pa-0">
                    <div class="pa-3 text-caption text-medium-emphasis">{{ row.note }}</div>
                    <div class="detail-wrap">
                      <MatchDetailTable :matches="row.matches" :total="row.matched" :cap="DETAIL_CAP" show-value />
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
