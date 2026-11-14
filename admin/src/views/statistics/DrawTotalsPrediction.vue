<script setup lang="ts">
import { computed } from 'vue'
import type { DrawObservation } from '@/types/drawHistory'
import { drawTotalsPredictionPresentation } from '@/utils/drawTotalsPrediction'

const props = defineProps<{ row: DrawObservation }>()
const presentation = computed(() => drawTotalsPredictionPresentation(props.row))
const note = computed(() => [
  '独立大小球推荐，按所示盘口和对应水位核对常规时间赛果。',
  presentation.value.parts.length === 2 ? `${presentation.value.line} 分盘：${presentation.value.parts.join(' / ')}` : '',
  presentation.value.reason,
].filter(Boolean).join('\n'))
</script>

<template>
  <div class="draw-totals-prediction" :title="note" :data-totals-version="presentation.version" :data-totals-state="presentation.state">
    <v-chip v-if="presentation.state === 'recommended'" size="small" :color="presentation.color" variant="tonal">{{ presentation.label }}</v-chip>
    <div v-else class="text-caption text-medium-emphasis">{{ presentation.label }}</div>
    <div v-if="presentation.state === 'recommended'" class="text-caption text-medium-emphasis mt-1">水位 {{ presentation.water?.toFixed(2) }}</div>
    <div v-else class="totals-reason text-caption text-medium-emphasis mt-1">{{ presentation.reason }}</div>
    <div v-if="presentation.outcomeLabel" class="text-caption mt-1" :class="presentation.outcomeColor ? `text-${presentation.outcomeColor}` : 'text-medium-emphasis'">{{ presentation.outcomeLabel }}</div>
    <div v-if="presentation.jointLabel" class="text-caption text-success mt-1">{{ presentation.jointLabel }}</div>
  </div>
</template>

<style scoped>
.draw-totals-prediction { min-width: 150px; }
.totals-reason { max-width: 210px; white-space: normal; }
</style>
