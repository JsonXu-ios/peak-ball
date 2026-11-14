<script setup lang="ts">
import { computed } from 'vue'
import type { DrawObservation } from '@/types/drawHistory'
import { NON_SPORTTERY_OBSERVATION_VERSION, nonSportteryDrawPresentation, nonSportteryAlternativePresentation } from '@/utils/drawNonSporttery'

const props = defineProps<{ row: DrawObservation }>()
const primary = computed(() => nonSportteryDrawPresentation(props.row))
const alternative = computed(() => nonSportteryAlternativePresentation(props.row))
const note = computed(() => [
  '非竞彩独立观察：沿用现有固定选平模型及门槛，赛果只用于完赛核对。',
  '入选后取真实即时平均欧赔中主客赔率较低的一方作为次选。',
  primary.value.detail,
  alternative.value.reason,
].filter(Boolean).join('\n'))
const odds = (value: number | null) => value === null ? '暂无' : value.toFixed(2)
</script>

<template>
  <div class="draw-nonsporttery" :data-nonsporttery-version="NON_SPORTTERY_OBSERVATION_VERSION" :data-nonsporttery-state="primary.state" :title="note">
    <span v-if="primary.state === 'out_of_scope'" class="text-medium-emphasis">—</span>
    <template v-else>
      <v-chip size="small" :color="primary.color || undefined" :variant="primary.color ? 'tonal' : 'text'">{{ primary.label }}</v-chip>
      <div v-if="primary.state === 'selected'" class="text-caption text-medium-emphasis mt-1">非竞彩独立入选</div>
      <div v-else-if="primary.detail" class="text-caption text-medium-emphasis mt-1 nonsporttery-reason">{{ primary.detail }}</div>
      <div v-if="primary.state === 'selected'" class="mt-2" :data-nonsporttery-alternative="alternative.state" :data-nonsporttery-direction="alternative.direction || ''">
        <v-chip v-if="alternative.state === 'recommended'" size="small" :color="alternative.color" variant="tonal">{{ alternative.label }}</v-chip>
        <div v-else class="text-caption text-medium-emphasis">{{ alternative.label }}</div>
        <div v-if="alternative.state === 'recommended'" class="text-caption text-medium-emphasis mt-1">即时均赔：主 {{ odds(alternative.homeOdds) }} / 客 {{ odds(alternative.awayOdds) }}</div>
        <div v-else class="text-caption text-medium-emphasis mt-1 nonsporttery-reason">{{ alternative.reason }}</div>
        <div v-if="alternative.outcomeLabel" class="text-caption mt-1" :class="alternative.outcomeColor ? `text-${alternative.outcomeColor}` : 'text-medium-emphasis'">{{ alternative.outcomeLabel }}</div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.draw-nonsporttery { min-width: 195px; }
.nonsporttery-reason { max-width: 240px; white-space: normal; }
</style>
