<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import {
  getCrawlerTasks,
  createCrawlerTask,
  updateCrawlerTask,
  deleteCrawlerTask,
  runCrawlerTask,
  toggleCrawlerTask,
} from '@/api'

interface CrawlerTask {
  id: number
  name: string
  type: string
  status: string
  schedule: string
  description: string
  config: string
  is_enabled: boolean
  run_count: number
  success_rate: number
  last_run_at: string | null
  next_run_at: string | null
}

const tasks = ref<CrawlerTask[]>([])
const loading = ref(false)
const runError = ref('')

const dialog = ref(false)
const dialogTitle = ref('新增任务')
const editingId = ref<number | null>(null)
const formData = reactive({
  name: '',
  type: 'match_list',
  schedule: '',
  description: '',
  config: '',
  is_enabled: true,
})

const deleteDialog = ref(false)
const deleteTaskId = ref(0)
const runningTaskId = ref<number | null>(null)
let refreshTimer: number | undefined

const taskTypes = [
  { title: '比赛列表', value: 'match_list' },
  { title: '历史战绩', value: 'history' },
  { title: '联赛排名/杯赛积分榜', value: 'rank' },
  { title: '欧赔数据', value: 'odds_euro' },
  { title: '盘口数据', value: 'odds_pankou' },
  { title: '阶段赔率盘口刷新', value: 'odds_refresh' },
  { title: '全量同步', value: 'all' },
]

const taskGuides: Record<string, string> = {
  match_list: '按日期拉取比赛基础信息、比分状态和队徽。config 不填 date 时走 today/next，包含今日和明日；填 date 时只跑那一天。',
  history: '历史战绩明细。config 填 match_id 时只跑单场；不填 match_id 时按 date 批量跑当天已入库比赛。',
  rank: '联赛排名或杯赛积分榜。config 填 match_id 时只跑单场；不填 match_id 时按 date 批量跑当天已入库比赛。',
  odds_euro: '欧赔明细。config 填 match_id 时只跑单场；不填 match_id 时按 date 批量跑当天已入库比赛。',
  odds_pankou: '盘口明细。config 填 match_id 时只跑单场；不填 match_id 时按 date 批量跑当天已入库比赛。',
  odds_refresh: '阶段性赔率更新。会强制刷新欧赔、亚盘和大小球，不重拉历史和排名；适合临近开赛或赛中定时执行。',
  all: '全量同步会先拉比赛列表，再逐场拉历史、排名、欧赔和盘口；启动项目后优先执行。config 不填 date 时包含今日和明日。',
}

const currentTaskGuide = computed(() => taskGuides[formData.type] || '')

async function fetchTasks() {
  loading.value = true
  try {
    const { data } = await getCrawlerTasks()
    tasks.value = data.list || []
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  fetchTasks()
  refreshTimer = window.setInterval(() => {
    if (tasks.value.some((task) => task.status === 'running')) fetchTasks()
  }, 5000)
})

onBeforeUnmount(() => {
  if (refreshTimer) window.clearInterval(refreshTimer)
})

function openCreate() {
  dialogTitle.value = '新增任务'
  editingId.value = null
  Object.assign(formData, { name: '', type: 'match_list', schedule: '', description: '', config: '', is_enabled: true })
  dialog.value = true
}

function openEdit(task: CrawlerTask) {
  dialogTitle.value = '编辑任务'
  editingId.value = task.id
  Object.assign(formData, {
    name: task.name,
    type: task.type,
    schedule: task.schedule,
    description: task.description,
    config: task.config || '',
    is_enabled: task.is_enabled,
  })
  dialog.value = true
}

async function handleSave() {
  if (editingId.value) {
    await updateCrawlerTask(editingId.value, { ...formData })
  } else {
    await createCrawlerTask({ ...formData })
  }
  dialog.value = false
  fetchTasks()
}

async function handleRun(id: number, async = true) {
  runningTaskId.value = id
  runError.value = ''
  try {
    await runCrawlerTask(id, async)
    await fetchTasks()
  } catch (error: unknown) {
    runError.value = getErrorMessage(error)
  } finally {
    runningTaskId.value = null
  }
}

async function handleToggle(id: number) {
  await toggleCrawlerTask(id)
  fetchTasks()
}

function openDelete(id: number) {
  deleteTaskId.value = id
  deleteDialog.value = true
}

async function handleDelete() {
  await deleteCrawlerTask(deleteTaskId.value)
  deleteDialog.value = false
  fetchTasks()
}

function getStatusColor(status: string) {
  const map: Record<string, string> = {
    pending: 'grey',
    running: 'warning',
    success: 'success',
    failed: 'error',
  }
  return map[status] || 'grey'
}

function getTypeLabel(type: string) {
  return taskTypes.find((t) => t.value === type)?.title || type
}

function getTaskGuide(type: string) {
  return taskGuides[type] || '未知任务类型，请检查任务配置。'
}

function getConfigSummary(task: CrawlerTask) {
  const config = parseTaskConfig(task.config)
  if (!config) return '空配置：按默认日期执行，单场明细任务会批量处理当天比赛。'
  const parts = [
    config.date ? `日期 ${config.date}` : '',
    config.match_id ? `比赛 ${config.match_id}` : '',
    config.force ? '强制重抓' : '',
  ].filter(Boolean)
  return parts.length ? parts.join(' · ') : '配置已填写，但没有 date、match_id 或 force 字段。'
}

function parseTaskConfig(config: string) {
  if (!config?.trim()) return null
  try {
    return JSON.parse(config) as { date?: string; match_id?: string; force?: boolean }
  } catch {
    return null
  }
}

function getErrorMessage(error: unknown) {
  if (typeof error === 'object' && error !== null && 'response' in error) {
    const response = (error as { response?: { data?: { error?: string; message?: string } } }).response
    return response?.data?.error || response?.data?.message || '任务启动失败，请查看爬虫日志。'
  }
  if (error instanceof Error) return error.message
  return '任务启动失败，请查看爬虫日志。'
}
</script>

<template>
  <div>
    <div class="d-flex align-center justify-space-between mb-4">
      <h2 class="text-h5 font-weight-bold">爬虫任务</h2>
      <v-btn color="primary" prepend-icon="mdi-plus" @click="openCreate">新增任务</v-btn>
    </div>

    <v-alert type="info" variant="tonal" class="mb-4">
      爬虫任务是保存下来的执行模板，适合重复运行或以后接定时调度；点击异步执行后会立即启动，进度和失败原因到“爬虫日志/运行监控”里看。
      不填 match_id 的历史、排名、欧赔、盘口任务会按日期批量跑当天比赛；填了 match_id 就只跑那一场。
    </v-alert>

    <v-alert v-if="runError" type="error" variant="tonal" closable class="mb-4" @click:close="runError = ''">
      {{ runError }}
    </v-alert>

    <v-row>
      <v-col
        v-for="task in tasks"
        :key="task.id"
        cols="12"
        md="6"
        lg="4"
      >
        <v-card>
          <v-card-title class="d-flex align-center">
            <v-icon class="mr-2" :color="task.is_enabled ? 'primary' : 'grey'">mdi-robot</v-icon>
            {{ task.name }}
            <v-spacer />
            <v-chip :color="getStatusColor(task.status)" size="small" variant="tonal">
              {{ task.status }}
            </v-chip>
          </v-card-title>

          <v-card-text>
            <div class="text-body-2 text-medium-emphasis mb-2">{{ task.description }}</div>
            <v-alert type="info" variant="tonal" density="compact" class="mb-3">
              {{ getTaskGuide(task.type) }}
            </v-alert>

            <v-row dense>
              <v-col cols="6">
                <div class="text-caption text-medium-emphasis">类型</div>
                <v-chip size="small" color="info" variant="tonal">{{ getTypeLabel(task.type) }}</v-chip>
              </v-col>
              <v-col cols="6">
                <div class="text-caption text-medium-emphasis">调度</div>
                <div class="text-body-2">{{ task.schedule || '手动' }}</div>
              </v-col>
              <v-col cols="6">
                <div class="text-caption text-medium-emphasis">运行次数</div>
                <div class="text-body-2">{{ task.run_count }}</div>
              </v-col>
              <v-col cols="6">
                <div class="text-caption text-medium-emphasis">成功率</div>
                <div class="text-body-2">{{ task.success_rate.toFixed(1) }}%</div>
              </v-col>
              <v-col cols="12">
                <div class="text-caption text-medium-emphasis">配置说明</div>
                <div class="text-body-2">{{ getConfigSummary(task) }}</div>
              </v-col>
            </v-row>

            <div v-if="task.last_run_at" class="text-caption text-medium-emphasis mt-2">
              上次运行: {{ new Date(task.last_run_at).toLocaleString() }}
            </div>
            <v-progress-linear
              v-if="task.status === 'running'"
              indeterminate
              color="primary"
              class="mt-3"
            />
          </v-card-text>

          <v-card-actions>
            <v-btn
              size="small"
              variant="tonal"
              :color="task.is_enabled ? 'warning' : 'success'"
              @click="handleToggle(task.id)"
            >
              {{ task.is_enabled ? '禁用' : '启用' }}
            </v-btn>
            <v-btn size="small" variant="tonal" color="primary" @click="openEdit(task)">编辑</v-btn>
            <v-spacer />
            <v-btn
              size="small"
              variant="tonal"
              color="success"
              prepend-icon="mdi-play"
              :loading="runningTaskId === task.id || task.status === 'running'"
              :disabled="task.status === 'running' || !task.is_enabled"
              @click="handleRun(task.id, true)"
            >
              异步执行
            </v-btn>
            <v-btn
              size="small"
              variant="text"
              color="error"
              icon="mdi-delete"
              @click="openDelete(task.id)"
            />
          </v-card-actions>
        </v-card>
      </v-col>
    </v-row>

    <!-- Create/Edit Dialog -->
    <v-dialog v-model="dialog" max-width="500">
      <v-card>
        <v-card-title>{{ dialogTitle }}</v-card-title>
        <v-card-text>
          <v-text-field v-model="formData.name" label="任务名称" class="mb-2" />
          <v-select v-model="formData.type" :items="taskTypes" label="任务类型" class="mb-2" />
          <v-text-field v-model="formData.schedule" label="Cron 表达式" placeholder="0 8 * * *" class="mb-2" />
          <v-textarea v-model="formData.description" label="描述" rows="2" class="mb-2" />
          <v-textarea
            v-model="formData.config"
            label="配置 JSON"
            rows="3"
            placeholder='{"match_id":"498150656","force":false}'
            hint='常用字段：date 指定日期，match_id 指定单场，force=true 表示已有数据也重抓。'
            persistent-hint
            class="mb-2"
          />
          <v-alert type="info" variant="tonal" density="compact" class="mb-2">
            {{ currentTaskGuide }}
          </v-alert>
          <v-switch v-model="formData.is_enabled" label="启用" color="primary" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="dialog = false">取消</v-btn>
          <v-btn color="primary" @click="handleSave">保存</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- Delete Dialog -->
    <v-dialog v-model="deleteDialog" max-width="400">
      <v-card>
        <v-card-title>确认删除</v-card-title>
        <v-card-text>确定要删除该爬虫任务吗？</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="deleteDialog = false">取消</v-btn>
          <v-btn color="error" @click="handleDelete">删除</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>
