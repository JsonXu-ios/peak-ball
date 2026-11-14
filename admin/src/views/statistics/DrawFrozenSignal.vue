<script setup lang="ts">
import { computed } from 'vue'
import type { DrawObservation } from '@/types/drawHistory'
import { frozenDrawPresentation, hasCurrentJcDrawSelectionMetadata } from '@/utils/drawFrozenSignal'

const props = defineProps<{ row: DrawObservation }>()
const presentation = computed(() => frozenDrawPresentation(props.row))
const selectionCurrent = computed(() => !props.row.jingcaiId?.trim() || hasCurrentJcDrawSelectionMetadata(props.row.frozenDrawSignal))
const note = computed(() => [
  '竞彩原入选：原模型分数达标且至少1场有效赛前交锋；以分析服务返回状态为准，赛果只用于核对。',
  props.row.frozenDrawSignal?.modelVersion,
  presentation.value.detail,
].filter(Boolean).join('\n'))
</script>

<template>
  <div class="frozen-draw-signal" :title="note" :data-model-state="row.frozenDrawSignal?.state || 'missing'">
    <v-chip size="small" :color="presentation.color" :variant="presentation.color ? 'tonal' : 'text'">{{ presentation.label }}</v-chip>
    <div v-if="row.frozenDrawSignal?.state === 'selected' && selectionCurrent" class="text-caption text-medium-emphasis mt-1">{{ row.jingcaiId?.trim() ? '竞彩原入选' : '固定模型入选' }}</div>
    <div v-else-if="row.frozenDrawSignal?.state === 'unavailable' || !selectionCurrent || (row.frozenDrawSignal?.state === 'not_selected' && presentation.detail)" class="text-caption text-medium-emphasis mt-1 signal-reason">{{ presentation.detail }}</div>
  </div>
</template>

<style scoped>
.frozen-draw-signal { min-width: 135px; }
.signal-reason { max-width: 220px; white-space: normal; }
</style>
