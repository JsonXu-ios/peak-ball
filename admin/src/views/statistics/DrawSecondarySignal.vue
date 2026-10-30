<script setup lang="ts">
import { computed } from 'vue'
import type { DrawObservation } from '@/types/drawHistory'
import { secondaryDrawPresentation } from '@/utils/drawSecondarySignal'

const props = defineProps<{ row: DrawObservation }>()
const presentation = computed(() => secondaryDrawPresentation(props.row))
const hasScore = computed(() => typeof presentation.value.score === 'number')
const scoreText = computed(() => presentation.value.score?.toFixed(4) || '')
const note = computed(() => [
  '原模型入选后，模型分数不少于0.225，竞彩初盘和即时盘报价完整，赛前交锋至少5场，才二次保留。',
  '分数不是平局概率。比赛结果只用于赛后核对。',
  presentation.value.reason,
].join('\n'))
</script>

<template>
  <div
    class="secondary-draw-signal"
    :title="note"
    :data-secondary-state="presentation.decision"
    :data-secondary-version="presentation.version"
  >
    <v-chip size="small" :color="presentation.color" :variant="presentation.color ? 'tonal' : 'text'">
      {{ presentation.label }}
    </v-chip>
    <div v-if="hasScore" class="text-caption text-medium-emphasis mt-1">
      模型分数 {{ scoreText }} / 门槛 {{ presentation.threshold.toFixed(3) }}
    </div>
    <div class="text-caption text-medium-emphasis mt-1 signal-reason">{{ presentation.reason }}</div>
    <div v-if="presentation.outcomeLabel" class="text-caption mt-1" :class="presentation.outcomeColor ? `text-${presentation.outcomeColor}` : 'text-medium-emphasis'">
      {{ presentation.outcomeLabel }}
    </div>
  </div>
</template>

<style scoped>
.secondary-draw-signal { min-width: 195px; }
.signal-reason { max-width: 240px; white-space: normal; }
</style>
