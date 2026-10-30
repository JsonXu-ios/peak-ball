<script setup lang="ts">
import { computed } from 'vue'
import type { DrawObservation } from '@/types/drawHistory'
import { drawScorePresentation } from '@/utils/drawScoreRecommendation'

const props = defineProps<{ row: DrawObservation }>()
const presentation = computed(() => drawScorePresentation(props.row))
const note = computed(() => `${presentation.value.reason}\n首选、次选按本场数据排序，比分推荐不改变原模型及二次筛选。`)
</script>

<template>
  <div class="draw-score-recommendation" :title="note" :data-score-state="presentation.state" :data-score-version="presentation.version">
    <div v-if="presentation.scores.length" class="d-flex ga-2">
      <v-chip v-for="(score, index) in presentation.scores" :key="score" size="small" color="primary" variant="tonal">{{ index === 0 ? '首选' : '次选' }} {{ score }}</v-chip>
    </div>
    <div class="text-caption text-medium-emphasis" :class="{ 'mt-1': presentation.scores.length }">{{ presentation.label }}</div>
    <div v-if="presentation.reason" class="text-caption text-medium-emphasis score-reason">{{ presentation.reason }}</div>
    <div v-if="presentation.outcomeLabel" class="text-caption mt-1" :class="presentation.outcomeColor ? `text-${presentation.outcomeColor}` : 'text-medium-emphasis'">{{ presentation.outcomeLabel }}</div>
  </div>
</template>

<style scoped>
.draw-score-recommendation { min-width: 145px; }
.score-reason { max-width: 260px; white-space: normal; }
</style>
