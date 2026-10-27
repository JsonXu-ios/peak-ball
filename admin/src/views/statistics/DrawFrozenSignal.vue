<script setup lang="ts">
import { computed } from 'vue'
import type { DrawObservation } from '@/types/drawHistory'
import { frozenDrawPresentation } from '@/utils/drawFrozenSignal'

const props = defineProps<{ row: DrawObservation }>()
const presentation = computed(() => frozenDrawPresentation(props.row))
const note = computed(() => [
  '竞彩选平固定模型 v1；先按输入决定是否入选，再核对完赛结果。',
  props.row.frozenDrawSignal?.modelVersion,
  presentation.value.detail,
].filter(Boolean).join('\n'))
</script>

<template>
  <div class="frozen-draw-signal" :title="note" :data-model-state="row.frozenDrawSignal?.state || 'missing'">
    <v-chip size="small" :color="presentation.color" :variant="presentation.color ? 'tonal' : 'text'">{{ presentation.label }}</v-chip>
    <div v-if="row.frozenDrawSignal?.state === 'selected'" class="text-caption text-medium-emphasis mt-1">固定模型入选</div>
    <div v-else-if="row.frozenDrawSignal?.state === 'unavailable'" class="text-caption text-medium-emphasis mt-1 signal-reason">{{ presentation.detail }}</div>
  </div>
</template>

<style scoped>
.frozen-draw-signal { min-width: 135px; }
.signal-reason { max-width: 220px; white-space: normal; }
</style>
