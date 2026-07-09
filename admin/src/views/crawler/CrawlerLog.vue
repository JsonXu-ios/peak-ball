<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { getCrawlerLogDetail, getCrawlerLogs } from '@/api'

interface CrawlerLog {
  id: number
  task_id: number
  task_name: string
  status: string
  start_time: string
  end_time: string | null
  duration: number
  items_count: number
  success_count: number
  failed_count: number
  error_msg: string
  details: string
}

interface CrawlerDetails {
  run_key?: string
  type?: string
  date?: string
  match_id?: string
  current?: string
  items_count?: number
  success_count?: number
  failed_count?: number
  skipped_count?: number
  notes?: string[]
}

const logs = ref<CrawlerLog[]>([])
const total = ref(0)
const loading = ref(false)
const page = ref(1)
const pageSize = ref(20)
const status = ref('')
const taskName = ref('')

const detailDialog = ref(false)
const selectedLog = ref<CrawlerLog | null>(null)
let refreshTimer: number | undefined

const selectedDetails = computed(() => parseDetails(selectedLog.value?.details))

async function fetchLogs() {
  loading.value = true
  try {
    const { data } = await getCrawlerLogs({
      page: page.value,
      page_size: pageSize.value,
      status: status.value,
      task_name: taskName.value,
    })
    logs.value = data.list || []
    total.value = data.total || 0
  } finally {
    loading.value = false
  }
}

onMounted(fetchLogs)

onMounted(() => {
  refreshTimer = window.setInterval(() => {
    if (logs.value.some((log) => log.status === 'running')) fetchLogs()
  }, 5000)
})

onBeforeUnmount(() => {
  if (refreshTimer) window.clearInterval(refreshTimer)
})

function getStatusColor(s: string) {
  const map: Record<string, string> = { success: 'success', failed: 'error', running: 'warning' }
  return map[s] || 'grey'
}

function formatDuration(ms: number) {
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
  return `${(ms / 60000).toFixed(1)}m`
}

async function showDetail(log: CrawlerLog) {
  const { data } = await getCrawlerLogDetail(log.id)
  selectedLog.value = data
  detailDialog.value = true
}

function parseDetails(details?: string): CrawlerDetails {
  if (!details) return {}
  try {
    return JSON.parse(details) as CrawlerDetails
  } catch {
    return {}
  }
}

function progressPercent(log: CrawlerLog) {
  const details = parseDetails(log.details)
  const total = log.items_count || details.items_count || 0
  if (!total) return 0
  const done = log.success_count + log.failed_count + (details.skipped_count || 0)
  return Math.min(100, Math.round((done / total) * 100))
}
</script>

<template>
  <div>
    <h2 class="text-h5 font-weight-bold mb-4">爬虫日志</h2>

    <!-- Filters -->
    <v-card class="mb-4">
      <v-card-text>
        <v-row>
          <v-col cols="12" md="3">
            <v-text-field
              v-model="taskName"
              label="任务名称"
              clearable
              hide-details
              @keyup.enter="fetchLogs"
            />
          </v-col>
          <v-col cols="12" md="3">
            <v-select
              v-model="status"
              :items="['', 'running', 'success', 'failed']"
              label="状态"
              clearable
              hide-details
              @update:model-value="fetchLogs"
            />
          </v-col>
          <v-col cols="12" md="2">
            <v-btn color="primary" @click="fetchLogs">搜索</v-btn>
          </v-col>
        </v-row>
      </v-card-text>
    </v-card>

    <v-card>
      <v-data-table-server
        :headers="[
          { title: 'ID', key: 'id', width: 60 },
          { title: '任务名称', key: 'task_name' },
          { title: '状态', key: 'status', width: 100 },
          { title: '开始时间', key: 'start_time' },
          { title: '耗时', key: 'duration', width: 100 },
          { title: '数据量', key: 'items_count', width: 80 },
          { title: '成功', key: 'success_count', width: 80 },
          { title: '失败', key: 'failed_count', width: 80 },
          { title: '操作', key: 'actions', sortable: false, width: 80 },
        ]"
        :items="logs"
        :items-length="total"
        :loading="loading"
        :items-per-page="pageSize"
        :page="page"
        @update:page="page = $event; fetchLogs()"
        @update:items-per-page="pageSize = $event; fetchLogs()"
      >
        <template #item.status="{ item }">
          <v-chip :color="getStatusColor(item.status)" size="small" variant="tonal">
            {{ item.status }}
          </v-chip>
        </template>

        <template #item.start_time="{ item }">
          {{ new Date(item.start_time).toLocaleString() }}
        </template>

        <template #item.duration="{ item }">
          {{ formatDuration(item.duration) }}
        </template>

        <template #item.items_count="{ item }">
          <div class="min-width-120">
            <div class="text-caption">{{ item.success_count + item.failed_count + (parseDetails(item.details).skipped_count || 0) }}/{{ item.items_count || '-' }}</div>
            <v-progress-linear
              v-if="item.status === 'running' || item.items_count"
              :model-value="progressPercent(item)"
              height="6"
              rounded
              color="primary"
            />
          </div>
        </template>

        <template #item.actions="{ item }">
          <v-btn size="small" variant="text" color="primary" @click="showDetail(item)">详情</v-btn>
        </template>
      </v-data-table-server>
    </v-card>

    <!-- Detail Dialog -->
    <v-dialog v-model="detailDialog" max-width="760">
      <v-card v-if="selectedLog">
        <v-card-title>日志详情 #{{ selectedLog.id }}</v-card-title>
        <v-card-text>
          <v-row dense>
            <v-col cols="6"><strong>任务:</strong> {{ selectedLog.task_name }}</v-col>
            <v-col cols="6"><strong>状态:</strong>
              <v-chip :color="getStatusColor(selectedLog.status)" size="small">{{ selectedLog.status }}</v-chip>
            </v-col>
            <v-col cols="6"><strong>开始:</strong> {{ new Date(selectedLog.start_time).toLocaleString() }}</v-col>
            <v-col cols="6"><strong>结束:</strong> {{ selectedLog.end_time ? new Date(selectedLog.end_time).toLocaleString() : '-' }}</v-col>
            <v-col cols="4"><strong>耗时:</strong> {{ formatDuration(selectedLog.duration) }}</v-col>
            <v-col cols="4"><strong>成功:</strong> {{ selectedLog.success_count }}</v-col>
            <v-col cols="4"><strong>失败:</strong> {{ selectedLog.failed_count }}</v-col>
            <v-col cols="4"><strong>跳过:</strong> {{ selectedDetails.skipped_count || 0 }}</v-col>
            <v-col cols="8"><strong>当前:</strong> {{ selectedDetails.current || '-' }}</v-col>
            <v-col cols="12">
              <v-progress-linear :model-value="progressPercent(selectedLog)" height="10" rounded color="primary" />
            </v-col>
          </v-row>
          <v-card variant="tonal" class="mt-4">
            <v-card-title class="text-subtitle-2">执行细节</v-card-title>
            <v-card-text>
              <div class="text-body-2 mb-2">Run Key: {{ selectedDetails.run_key || '-' }}</div>
              <v-list v-if="selectedDetails.notes?.length" density="compact">
                <v-list-item v-for="note in selectedDetails.notes" :key="note" :title="note" />
              </v-list>
              <pre class="text-caption mt-2">{{ JSON.stringify(selectedDetails, null, 2) }}</pre>
            </v-card-text>
          </v-card>
          <v-alert v-if="selectedLog.error_msg" type="error" variant="tonal" class="mt-4">
            {{ selectedLog.error_msg }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="detailDialog = false">关闭</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>
