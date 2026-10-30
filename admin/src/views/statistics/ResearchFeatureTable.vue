<script setup lang="ts">
import type { ResearchFeatureDefinition } from '@/types/researchStore'

defineProps<{ items: ResearchFeatureDefinition[]; loading: boolean; error: string }>()
defineEmits<{ detail: [row: ResearchFeatureDefinition] }>()
</script>

<template>
  <v-table density="compact" class="research-feature-table"><thead><tr><th>特征 / 版本</th><th>含义与公式</th><th>类型 / 单位 / 来源</th><th>缺失策略</th><th>查看</th></tr></thead><tbody><tr v-for="row in items" :key="`${row.version}-${row.name}`"><td><code>{{ row.name }}</code><div class="text-caption text-medium-emphasis">{{ row.version }}</div></td><td class="long-text"><div>{{ row.description || '暂无说明' }}</div><div class="text-caption text-medium-emphasis mt-1">{{ row.formula || '公式详见元数据' }}</div></td><td>{{ row.type }} / {{ row.unit || '无单位' }}<div class="text-caption text-medium-emphasis">{{ row.origin }}</div></td><td class="long-text text-caption">{{ row.missingPolicy || '未声明' }}</td><td><v-btn size="small" variant="text" @click="$emit('detail', row)">详情</v-btn></td></tr><tr v-if="!items.length"><td colspan="5" class="text-center text-medium-emphasis py-6">{{ loading ? '正在读取特征…' : error ? '查询失败，请重试。' : '暂无符合条件的特征定义。' }}</td></tr></tbody></v-table>
</template>

<style scoped>
.research-feature-table :deep(th), .research-feature-table :deep(td) { white-space: nowrap; }
.research-feature-table :deep(.long-text) { min-width: 240px; max-width: 440px; white-space: normal; overflow-wrap: anywhere; }
code { font-size: 12px; color: rgb(var(--v-theme-primary)); }
</style>
