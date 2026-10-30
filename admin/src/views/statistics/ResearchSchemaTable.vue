<script setup lang="ts">
import type { ResearchTable } from '@/types/researchStore'

defineProps<{ table: ResearchTable }>()
</script>

<template>
  <div class="research-schema-table" :data-schema-table="table.name">
    <p class="text-body-2 mb-3">{{ table.description }}</p>
    <v-table density="compact" class="schema-fields"><thead><tr><th>字段</th><th>类型</th><th>允许空</th><th>键</th><th>说明</th></tr></thead><tbody><tr v-for="column in table.columns" :key="column.name"><td><code>{{ column.name }}</code></td><td>{{ column.type }}</td><td>{{ column.nullable ? '是' : '否' }}</td><td><v-chip v-if="column.primaryKey" size="x-small" color="primary">主键</v-chip><span v-else>—</span></td><td class="column-comment">{{ column.comment || '—' }}</td></tr></tbody></v-table>
    <div class="schema-details mt-4">
      <div><div class="text-subtitle-2 mb-2">索引与唯一约束</div><div v-for="index in table.indexes" :key="index.name" class="text-caption mb-1"><strong>{{ index.name }}</strong><span v-if="index.unique" class="text-primary ml-1">唯一</span><code class="ml-2">{{ index.columns.join(', ') }}</code></div><div v-if="!table.indexes.length" class="text-caption text-medium-emphasis">无额外索引。</div></div>
      <div><div class="text-subtitle-2 mb-2">数据关联</div><div v-for="relation in table.foreignKeys" :key="`${relation.column}-${relation.referencedTable}`" class="text-caption mb-1"><code>{{ relation.column }}</code><span class="mx-2">→</span><code>{{ relation.referencedTable }}.{{ relation.referencedColumn }}</code></div><div v-if="!table.foreignKeys.length" class="text-caption text-medium-emphasis">该表未声明外键关联。</div></div>
    </div>
  </div>
</template>

<style scoped>
.schema-fields :deep(th), .schema-fields :deep(td) { white-space: nowrap; }
.schema-fields :deep(.column-comment) { white-space: normal; min-width: 200px; }
.schema-details { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 20px; overflow-wrap: anywhere; }
code { font-size: 12px; color: rgb(var(--v-theme-primary)); }
@media (max-width: 700px) {
  .schema-details { grid-template-columns: 1fr; }
}
</style>
