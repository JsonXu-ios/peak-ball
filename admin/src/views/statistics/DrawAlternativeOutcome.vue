<script setup lang="ts">
import { computed } from 'vue'
import type { DrawObservation } from '@/types/drawHistory'
import { drawAlternativeOutcomePresentation } from '@/utils/drawAlternativeOutcome'

const props = defineProps<{ row: DrawObservation }>()
const presentation = computed(() => drawAlternativeOutcomePresentation(props.row))
const note = computed(() => [
  '原模型入选平局后，排除平，取即时平均欧赔中主客赔率较低的一方。',
  '这是市场胜负备选，不是决策树自身的第二名；不要求二次保留，赛果不参与选择。',
  presentation.value.reason,
].join('\n'))
const odds = (value: number | null) => value === null ? '暂无' : value.toFixed(2)
</script>

<template>
  <div class="draw-alternative-outcome" :title="note" :data-alternative-version="presentation.version" :data-alternative-state="presentation.state" :data-alternative-direction="presentation.direction || ''">
    <v-chip v-if="presentation.state === 'recommended'" size="small" :color="presentation.color" variant="tonal">{{ presentation.label }}</v-chip>
    <div v-else class="text-caption text-medium-emphasis">{{ presentation.label }}</div>
    <div v-if="presentation.state === 'recommended'" class="text-caption text-medium-emphasis mt-1">即时均赔：主 {{ odds(presentation.homeOdds) }} / 客 {{ odds(presentation.awayOdds) }}</div>
    <div class="text-caption text-medium-emphasis mt-1 alternative-reason">{{ presentation.reason }}</div>
    <div v-if="presentation.outcomeLabel" class="text-caption mt-1" :class="presentation.outcomeColor ? `text-${presentation.outcomeColor}` : 'text-medium-emphasis'">{{ presentation.outcomeLabel }}</div>
  </div>
</template>

<style scoped>
.draw-alternative-outcome { min-width: 185px; }
.alternative-reason { max-width: 240px; white-space: normal; }
</style>
