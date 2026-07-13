<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getMatchStatistics } from '@/api'

interface StatisticRow {
  key: string
  label: string
  sample: number
  correct: number
  accuracy: number
}

interface StatisticGroup {
  key: string
  title: string
  definition: string
  rows: StatisticRow[]
}

interface Report {
  settled_total: number
  start_date: string
  end_date: string
  generated_at: string
  groups: StatisticGroup[]
}

const loading = ref(false)
const startDate = ref('')
const endDate = ref('')
const report = ref<Report | null>(null)
const error = ref('')

const sourceRange = computed(() => {
  if (!report.value?.start_date && !report.value?.end_date) return '全部历史完赛比赛'
  return `${report.value.start_date || '最早'} 至 ${report.value.end_date || '最新'}`
})

async function fetchReport() {
  loading.value = true
  error.value = ''
  try {
    const { data } = await getMatchStatistics({
      start_date: startDate.value || undefined,
      end_date: endDate.value || undefined,
    })
    report.value = data as Report
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

function accuracyColor(accuracy: number) {
  if (accuracy >= 60) return 'success'
  if (accuracy >= 50) return 'primary'
  return 'warning'
}

onMounted(fetchReport)
</script>

<template>
  <div>
    <div class="d-flex flex-wrap align-center mb-4 ga-3">
      <div>
        <h2 class="text-h5 font-weight-bold">完赛比赛统计分析</h2>
        <div class="text-body-2 text-medium-emphasis mt-1">不区分比赛级别，仅使用已经完赛且数据完整的比赛。</div>
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
              <div class="text-subtitle-1 font-weight-medium mt-1">{{ sourceRange }}</div>
              <div class="text-caption text-medium-emphasis mt-1">正确率 = 命中数 ÷ 有效样本；缺少原始数据、走盘或无法形成方向的比赛会自动剔除。</div>
            </v-card-text>
          </v-card>
        </v-col>
      </v-row>

      <v-card v-for="group in report.groups" :key="group.key" class="mb-5">
        <v-card-title class="pt-5">{{ group.title }}</v-card-title>
        <v-card-subtitle class="pb-2 text-wrap">{{ group.definition }}</v-card-subtitle>
        <v-card-text>
          <v-table density="comfortable" class="statistics-table">
            <thead>
              <tr>
                <th>统计维度</th>
                <th class="text-right">有效样本</th>
                <th class="text-right">命中</th>
                <th class="text-right">正确率</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in group.rows" :key="row.key">
                <td class="font-weight-medium">{{ row.label }}</td>
                <td class="text-right">{{ row.sample.toLocaleString() }}</td>
                <td class="text-right">{{ row.correct.toLocaleString() }}</td>
                <td class="text-right">
                  <v-chip :color="accuracyColor(row.accuracy)" size="small" variant="tonal">{{ row.sample ? `${row.accuracy.toFixed(2)}%` : '-' }}</v-chip>
                </td>
              </tr>
              <tr v-if="!group.rows.length">
                <td colspan="4" class="text-center text-medium-emphasis py-6">暂无可统计数据</td>
              </tr>
            </tbody>
          </v-table>
        </v-card-text>
      </v-card>
    </template>

    <v-card v-else-if="!loading" variant="tonal">
      <v-card-text class="text-medium-emphasis">暂无统计结果。</v-card-text>
    </v-card>
  </div>
</template>

<style scoped>
.statistics-table th {
  white-space: nowrap;
}
</style>
