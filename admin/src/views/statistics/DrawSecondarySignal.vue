<script setup lang="ts">
import { computed } from 'vue'
import type { DrawObservation } from '@/types/drawHistory'
import { secondaryDrawPresentation } from '@/utils/drawSecondarySignal'

const props = defineProps<{ row: DrawObservation }>()
const presentation = computed(() => secondaryDrawPresentation(props.row))
const hasScore = computed(() => typeof presentation.value.score === 'number')
const scoreText = computed(() => presentation.value.score?.toFixed(4) || '')
const note = computed(() => [
  `二次筛选 ${presentation.value.version}`,
  '按当前输入快照，先由原固定模型入选，再按模型分数筛选；分数不是平局概率。',
  '分数 < 0.225 剔除；分数 ≥ 0.225 保留。比赛结果只用于赛后核对。',
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
    <div v-if="hasScore" class="text-caption text-medium-emphasis">
      {{ presentation.decision === 'rejected' ? '低于门槛，剔除低分候选' : '达到门槛，保留候选' }}
    </div>
    <div v-if="presentation.outcomeLabel" class="text-caption mt-1" :class="presentation.outcomeColor ? `text-${presentation.outcomeColor}` : 'text-medium-emphasis'">
      {{ presentation.outcomeLabel }}
    </div>
    <div v-if="presentation.decision === 'unavailable'" class="text-caption text-medium-emphasis mt-1 signal-reason">
      {{ presentation.reason }}
    </div>
  </div>
</template>

<style scoped>
.secondary-draw-signal { min-width: 195px; }
.signal-reason { max-width: 240px; white-space: normal; }
</style>
