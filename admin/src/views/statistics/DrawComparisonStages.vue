<script setup lang="ts">
import { computed } from 'vue'
import type { DrawComparisonMetric, DrawObservation } from '@/types/drawHistory'
import { drawComparisonStages } from '@/utils/drawFrontend'

const props = defineProps<{ row: DrawObservation; compact?: boolean; stage?: 'opening' | 'latest' }>()
const stages = computed(() => drawComparisonStages(props.row).filter((stage) => !props.stage || stage.key === props.stage))
const outcomes = ['胜', '平', '负']
const result = (metric: DrawComparisonMetric) => metric.available && metric.result?.length ? metric.result.join(' / ') : '暂无'
const numeric = (value: number | null | undefined) => typeof value === 'number' && Number.isFinite(value)
function text(value: number | null | undefined, kelly = false) {
  if (!numeric(value)) return '暂无'
  const rounded = Number(value!.toFixed(kelly ? 3 : 2))
  return `${!kelly && rounded > 0 ? '+' : ''}${rounded.toFixed(kelly ? 3 : 2)}`
}
const color = (value: number | null | undefined) => !numeric(value) || Number(value!.toFixed(2)) === 0 ? 'text-medium-emphasis' : value! > 0 ? 'text-error' : 'text-success'
const quote = (value: number | null | undefined) => numeric(value) && value! > 0 ? value!.toFixed(2) : '暂无'
const metricValue = (metric: DrawComparisonMetric, index: number) => metric.available ? metric.values?.[index] : null
const kellyClass = (metric: DrawComparisonMetric, label: string) => metric.available && metric.result.includes(label) ? 'text-primary font-weight-bold' : 'text-medium-emphasis'
</script>

<template>
  <div class="comparison-stages" :class="[compact ? 'comparison-compact' : 'comparison-details', { 'comparison-single': !!props.stage }]">
    <template v-for="stage in stages" :key="stage.key">
      <div v-if="compact" class="comparison-stage" :data-stage="stage.key">
        <div v-if="!props.stage" class="font-weight-bold" :title="stage.note">{{ stage.label }}对比</div>
        <div :title="stage.kelly.reason || stage.kelly.basis">凯利：{{ result(stage.kelly) }}</div>
        <div class="kelly-values" :title="stage.kelly.reason || stage.kelly.basis">凯利值：<span v-for="(label, index) in outcomes" :key="label" class="mr-2" :class="kellyClass(stage.kelly, label)">{{ label }} {{ text(metricValue(stage.kelly, index), true) }}</span></div>
        <div :title="stage.ticai.reason || stage.ticai.basis">体彩：{{ result(stage.ticai) }}</div>
        <div class="ticai-values" :title="stage.ticai.reason || stage.ticai.basis">体彩差值：<span v-for="(label, index) in outcomes" :key="label" class="mr-2" :class="color(metricValue(stage.ticai, index))">{{ label }} {{ text(metricValue(stage.ticai, index)) }}</span></div>
        <div class="average-difference-summary" :title="stage.williamAverage.reason || stage.williamAverage.basis">威廉－平均欧赔：<span v-for="(label, index) in outcomes" :key="label" class="mr-2" :class="color(metricValue(stage.williamAverage, index))">{{ label }} {{ text(metricValue(stage.williamAverage, index)) }}</span></div>
      </div>
      <v-card v-else variant="outlined" class="mb-4 comparison-stage" :data-stage="stage.key">
        <v-card-title class="text-subtitle-2">{{ stage.label }}对比</v-card-title>
        <v-table density="compact"><thead><tr><th>项目</th><th v-for="label in outcomes" :key="label">{{ label }}</th><th>判断</th></tr></thead><tbody>
          <tr><td>威廉赔率</td><td v-for="(_, i) in outcomes" :key="i">{{ quote(stage.william?.[i]) }}</td><td>—</td></tr>
          <tr><td>竞彩赔率</td><td v-for="(_, i) in outcomes" :key="i">{{ quote(stage.sporttery?.[i]) }}</td><td>—</td></tr>
          <tr><td>平均欧赔</td><td v-for="(_, i) in outcomes" :key="i">{{ quote(stage.average?.[i]) }}</td><td>—</td></tr>
          <tr><td>凯利值</td><td v-for="(label, i) in outcomes" :key="label" :class="kellyClass(stage.kelly, label)">{{ text(metricValue(stage.kelly, i), true) }}</td><td>{{ result(stage.kelly) }}</td></tr>
          <tr><td>体彩差值（威廉－竞彩）</td><td v-for="(_, i) in outcomes" :key="i" :class="color(metricValue(stage.ticai, i))">{{ text(metricValue(stage.ticai, i)) }}</td><td>{{ result(stage.ticai) }}</td></tr>
          <tr><td>威廉－平均欧赔</td><td v-for="(_, i) in outcomes" :key="i" :class="color(metricValue(stage.williamAverage, i))">{{ text(metricValue(stage.williamAverage, i)) }}</td><td>—</td></tr>
        </tbody></v-table>
        <div class="pa-3 text-caption text-medium-emphasis">
          <div>{{ stage.note }}</div>
          <div>凯利：{{ stage.kelly.available ? stage.kelly.basis : stage.kelly.reason }}</div>
          <div>体彩：{{ stage.ticai.available ? stage.ticai.basis : stage.ticai.reason }}</div>
          <div v-if="!stage.williamAverage.available">威廉－平均欧赔：{{ stage.williamAverage.reason }}</div>
          <div><span class="text-error">差值正数：威廉更高</span>；<span class="text-success">负数：威廉更低</span>；凯利取最小值，体彩取绝对差最小值。</div>
        </div>
      </v-card>
    </template>
  </div>
</template>

<style scoped>
.comparison-stages { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; align-items: start; }
.comparison-stage { min-width: 0; }
.comparison-single { grid-template-columns: minmax(0, 1fr); gap: 0; }
.kelly-values > span, .ticai-values > span, .average-difference-summary > span { display: inline-block; white-space: nowrap; }
</style>
