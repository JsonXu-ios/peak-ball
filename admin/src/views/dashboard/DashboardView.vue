<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { getDashboardStats } from '@/api'

interface Stats {
  total_matches: number
  today_matches: number
  total_users: number
  active_users: number
  crawler_success: number
  crawler_failed: number
}

interface LeagueStat {
  league: string
  count: number
}

interface CrawlerLog {
  id: number
  task_name: string
  status: string
  start_time: string
  duration: number
  items_count: number
}

const stats = ref<Stats>({
  total_matches: 0,
  today_matches: 0,
  total_users: 0,
  active_users: 0,
  crawler_success: 0,
  crawler_failed: 0,
})
const recentLogs = ref<CrawlerLog[]>([])
const leagueStats = ref<LeagueStat[]>([])
const loading = ref(false)

const statCards = ref([
  { title: '总比赛数', icon: 'mdi-soccer', color: 'primary', key: 'total_matches' },
  { title: '今日比赛', icon: 'mdi-calendar-today', color: 'success', key: 'today_matches' },
  { title: '管理用户', icon: 'mdi-account-multiple', color: 'info', key: 'total_users' },
  { title: '爬虫成功', icon: 'mdi-check-circle', color: 'success', key: 'crawler_success' },
  { title: '爬虫失败', icon: 'mdi-alert-circle', color: 'error', key: 'crawler_failed' },
])

onMounted(async () => {
  loading.value = true
  try {
    const { data } = await getDashboardStats()
    stats.value = data.stats
    recentLogs.value = data.recent_logs || []
    leagueStats.value = data.league_stats || []
  } catch {
    // handle silently
  } finally {
    loading.value = false
  }
})

function getStatusColor(status: string) {
  const map: Record<string, string> = {
    success: 'success',
    failed: 'error',
    running: 'warning',
  }
  return map[status] || 'grey'
}

function formatDuration(ms: number) {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}
</script>

<template>
  <div>
    <h2 class="text-h5 font-weight-bold mb-6">仪表盘</h2>

    <!-- Stats Cards -->
    <v-row>
      <v-col
        v-for="card in statCards"
        :key="card.key"
        cols="12"
        sm="6"
        md="4"
        lg
      >
        <v-card :loading="loading">
          <v-card-text class="d-flex align-center">
            <v-avatar :color="card.color" size="48" class="mr-4">
              <v-icon color="white">{{ card.icon }}</v-icon>
            </v-avatar>
            <div>
              <div class="text-caption text-medium-emphasis">{{ card.title }}</div>
              <div class="text-h5 font-weight-bold">
                {{ stats[card.key as keyof Stats] ?? 0 }}
              </div>
            </div>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>

    <v-row class="mt-4">
      <!-- League Distribution -->
      <v-col cols="12" md="6">
        <v-card>
          <v-card-title class="text-subtitle-1">联赛数据分布</v-card-title>
          <v-card-text>
            <v-list density="compact">
              <v-list-item
                v-for="ls in leagueStats"
                :key="ls.league"
              >
                <template #prepend>
                  <v-icon size="small" color="primary">mdi-trophy</v-icon>
                </template>
                <v-list-item-title>{{ ls.league }}</v-list-item-title>
                <template #append>
                  <v-chip size="small" color="primary" variant="tonal">{{ ls.count }}</v-chip>
                </template>
              </v-list-item>
              <v-list-item v-if="!leagueStats.length">
                <v-list-item-title class="text-medium-emphasis">暂无数据</v-list-item-title>
              </v-list-item>
            </v-list>
          </v-card-text>
        </v-card>
      </v-col>

      <!-- Recent Crawler Logs -->
      <v-col cols="12" md="6">
        <v-card>
          <v-card-title class="text-subtitle-1">最近爬虫日志</v-card-title>
          <v-card-text>
            <v-list density="compact">
              <v-list-item
                v-for="log in recentLogs"
                :key="log.id"
              >
                <template #prepend>
                  <v-icon :color="getStatusColor(log.status)" size="small">
                    {{ log.status === 'success' ? 'mdi-check-circle' : log.status === 'failed' ? 'mdi-alert-circle' : 'mdi-loading' }}
                  </v-icon>
                </template>
                <v-list-item-title>{{ log.task_name }}</v-list-item-title>
                <v-list-item-subtitle>
                  耗时: {{ formatDuration(log.duration) }} | 数据: {{ log.items_count }}条
                </v-list-item-subtitle>
                <template #append>
                  <v-chip :color="getStatusColor(log.status)" size="x-small" variant="tonal">
                    {{ log.status }}
                  </v-chip>
                </template>
              </v-list-item>
              <v-list-item v-if="!recentLogs.length">
                <v-list-item-title class="text-medium-emphasis">暂无日志</v-list-item-title>
              </v-list-item>
            </v-list>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>
  </div>
</template>
