<script setup lang="ts">
import { computed } from 'vue'
import type { DrawFrontendLoadState, DrawObservation } from '@/types/drawHistory'
import { DRAW_FEATURE_FORMULA, DRAW_FEATURE_NOTE, drawFeatureSignal } from '@/utils/drawFeature'
import type { DrawFeatureComparison } from '@/utils/drawFeature'

const props = defineProps<{ row: DrawObservation; analysisState?: DrawFrontendLoadState }>()
defineEmits<{ retryAnalysis: [] }>()
const signal = computed(() => drawFeatureSignal(props.row))
const outcomes = ['胜', '平', '负']
const phases = [{ key: 'opening' as const, label: '初盘' }, { key: 'latest' as const, label: '即时' }]
const signed = (value: number) => {
  const rounded = Number(value.toFixed(2))
  return `${rounded > 0 ? '+' : ''}${Object.is(rounded, -0) ? '0.00' : rounded.toFixed(2)}`
}
const differenceClass = (value: number) => Number(value.toFixed(2)) > 0 ? 'text-error' : Number(value.toFixed(2)) < 0 ? 'text-success' : 'text-medium-emphasis'
const selection = (item: DrawFeatureComparison) => item.topTwo.map((index) => outcomes[index]).join('/')
function label(item: DrawFeatureComparison) {
  if (item.kind === 'missing') return '暂无'
  if (item.kind === 'none') return '无匹配'
  if (item.kind === 'largest') return item.differences!.every((value) => value === 0) ? '平项并列最大，差值均为0' : `平项绝对值${item.largestTied ? '并列' : ''}最大`
  return item.kind === 'negative' ? '负数含平' : '正数含平'
}
function calculation(item: DrawFeatureComparison) {
  if (!item.differences) return `缺少该阶段完整有效的威廉或${item.source === 'sporttery' ? '竞彩' : '平均欧赔'}三项报价`
  return `${item.source === 'sporttery' ? '体彩：威廉－竞彩' : '均赔：威廉－平均欧赔'}；${outcomes.map((outcome, index) => `${outcome} ${item.william![index]}－${item.comparison![index]}＝${signed(item.differences![index]!)}`).join('；')}`
}
</script>

<template>
  <div class="draw-feature text-caption" :title="`${DRAW_FEATURE_FORMULA}。${DRAW_FEATURE_NOTE}`">
    <div v-for="phase in phases" :key="phase.key" class="feature-phase" :data-draw-feature-phase="phase.key">
      <div class="font-weight-bold">{{ phase.label }}</div>
      <div v-for="item in signal[phase.key]" :key="item.source" class="feature-comparison" :data-feature-source="item.source" :data-feature-kind="item.kind" :title="calculation(item)">
        <span :class="item.kind === 'none' || item.kind === 'missing' ? 'text-medium-emphasis' : 'text-primary'">{{ item.label }}：{{ label(item) }}</span>
        <template v-if="item.kind !== 'none' && item.kind !== 'missing'">
          <span>（平 <span :class="differenceClass(item.drawDifference!)">{{ signed(item.drawDifference!) }}</span><template v-if="item.kind !== 'largest'">；两项 {{ selection(item) }}<span v-if="item.topTwoTied">（并列）</span></template>）</span>
        </template>
        <div v-if="item.differences" class="feature-values">
          <span v-for="(outcome, index) in outcomes" :key="outcome" :class="[differenceClass(item.differences[index]!), { 'font-weight-bold': index === 1 && item.kind !== 'none' }]">{{ outcome }} {{ signed(item.differences[index]!) }}</span>
        </div>
      </div>
    </div>
    <div v-if="analysisState?.loading" class="text-medium-emphasis">正在读取分析数据…</div>
    <div v-else-if="analysisState?.error" class="text-error">分析读取失败：{{ analysisState.error }}</div>
    <v-btn v-if="!row.frontendAnalysis && !analysisState?.loading" size="x-small" variant="text" color="primary" @click.stop="$emit('retryAnalysis')" @keydown.enter.stop @keydown.space.stop>{{ analysisState?.error ? '重试分析' : '读取分析' }}</v-btn>
  </div>
</template>

<style scoped>
.draw-feature { min-width: 320px; max-width: 400px; white-space: normal; line-height: 1.7; }
.feature-phase + .feature-phase { margin-top: 6px; }
.feature-values > span { display: inline-block; white-space: nowrap; margin-right: 10px; }
</style>
