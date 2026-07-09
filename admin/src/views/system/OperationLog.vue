<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { getOperationLogs } from '@/api'

interface OperationLog {
  id: number
  username: string
  method: string
  path: string
  ip: string
  status_code: number
  latency: number
  created_at: string
}

const logs = ref<OperationLog[]>([])
const total = ref(0)
const loading = ref(false)
const page = ref(1)
const pageSize = ref(20)
const keyword = ref('')
const method = ref('')

async function fetchLogs() {
  loading.value = true
  try {
    const { data } = await getOperationLogs({
      page: page.value,
      page_size: pageSize.value,
      keyword: keyword.value,
      method: method.value,
    })
    logs.value = data.list || []
    total.value = data.total || 0
  } finally {
    loading.value = false
  }
}

onMounted(fetchLogs)

function getMethodColor(m: string) {
  const map: Record<string, string> = { GET: 'success', POST: 'primary', PUT: 'warning', DELETE: 'error', PATCH: 'info' }
  return map[m] || 'grey'
}

function getStatusColor(code: number) {
  if (code < 300) return 'success'
  if (code < 400) return 'info'
  if (code < 500) return 'warning'
  return 'error'
}
</script>

<template>
  <div>
    <h2 class="text-h5 font-weight-bold mb-4">操作日志</h2>

    <v-card class="mb-4">
      <v-card-text>
        <v-row>
          <v-col cols="12" md="4">
            <v-text-field
              v-model="keyword"
              label="搜索"
              prepend-inner-icon="mdi-magnify"
              placeholder="用户名/路径"
              clearable
              hide-details
              @keyup.enter="fetchLogs"
            />
          </v-col>
          <v-col cols="12" md="3">
            <v-select
              v-model="method"
              :items="['', 'GET', 'POST', 'PUT', 'DELETE', 'PATCH']"
              label="请求方法"
              clearable
              hide-details
              @update:model-value="fetchLogs"
            />
          </v-col>
        </v-row>
      </v-card-text>
    </v-card>

    <v-card>
      <v-data-table-server
        :headers="[
          { title: 'ID', key: 'id', width: 60 },
          { title: '用户', key: 'username' },
          { title: '方法', key: 'method', width: 90 },
          { title: '路径', key: 'path' },
          { title: 'IP', key: 'ip' },
          { title: '状态码', key: 'status_code', width: 90 },
          { title: '耗时(ms)', key: 'latency', width: 100 },
          { title: '时间', key: 'created_at' },
        ]"
        :items="logs"
        :items-length="total"
        :loading="loading"
        :items-per-page="pageSize"
        :page="page"
        @update:page="page = $event; fetchLogs()"
        @update:items-per-page="pageSize = $event; fetchLogs()"
      >
        <template #item.method="{ item }">
          <v-chip :color="getMethodColor(item.method)" size="small" variant="tonal">
            {{ item.method }}
          </v-chip>
        </template>

        <template #item.status_code="{ item }">
          <v-chip :color="getStatusColor(item.status_code)" size="small" variant="tonal">
            {{ item.status_code }}
          </v-chip>
        </template>

        <template #item.created_at="{ item }">
          {{ new Date(item.created_at).toLocaleString() }}
        </template>
      </v-data-table-server>
    </v-card>
  </div>
</template>
