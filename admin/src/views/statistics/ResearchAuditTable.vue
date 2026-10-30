<script setup lang="ts">
import type { ResearchAuditIssue } from '@/types/researchStore'
import { formatBeijingDateTime } from '@/utils/date'

defineProps<{ items: ResearchAuditIssue[]; loading: boolean; error: string }>()
defineEmits<{ detail: [row: ResearchAuditIssue] }>()
function severityColor(value: string) { return value === 'error' ? 'error' : value === 'warning' ? 'warning' : 'info' }
function severityLabel(value: string) { return ({ error: '错误', warning: '警告', info: '提示' } as Record<string, string>)[value] || value }
</script>

<template>
  <v-table density="compact" class="research-audit-table"><thead><tr><th>等级 / 编码</th><th>比赛ID / 字段</th><th>问题说明</th><th>记录时间（北京时间）</th><th>依据</th></tr></thead><tbody><tr v-for="row in items" :key="row.id"><td><v-chip :color="severityColor(row.severity)" size="x-small">{{ severityLabel(row.severity) }}</v-chip><div class="text-caption mt-1">{{ row.code }}</div></td><td><code>{{ row.matchId || '批次级' }}</code><div class="text-caption text-medium-emphasis">{{ row.field || '—' }}</div></td><td class="long-text">{{ row.message }}</td><td class="text-caption">{{ formatBeijingDateTime(row.createdAt, '暂无') }}</td><td><v-btn size="small" variant="text" @click="$emit('detail', row)">详情</v-btn></td></tr><tr v-if="!items.length"><td colspan="5" class="text-center text-medium-emphasis py-6">{{ loading ? '正在读取审计问题…' : error ? '查询失败，请重试。' : '当前批次与等级下暂无审计问题。' }}</td></tr></tbody></v-table>
</template>

<style scoped>
.research-audit-table :deep(th), .research-audit-table :deep(td) { white-space: nowrap; }
.research-audit-table :deep(.long-text) { min-width: 240px; max-width: 440px; white-space: normal; overflow-wrap: anywhere; }
code { font-size: 12px; color: rgb(var(--v-theme-primary)); }
</style>
