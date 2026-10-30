<script setup lang="ts">
import { computed } from 'vue'
import type { DrawObservation } from '@/types/drawHistory'
import { classifyDrawProfitSupport, type DrawProfitDirection, type DrawProfitRates } from '@/utils/drawProfitSupport'
import { classifySecondaryDraw } from '@/utils/drawSecondarySignal'

const props = defineProps<{ row: DrawObservation }>()
const signal = computed(() => classifyDrawProfitSupport(props.row))
const retained = computed(() => classifySecondaryDraw(props.row).decision === 'retained')
const label = computed(() => signal.value.state === 'supported' ? retained.value ? '二次增强' : '盈亏分歧'
  : signal.value.state === 'not_supported' ? '无此增强' : signal.value.state === 'out_of_scope' ? '非竞彩' : '待判')
const labels: Record<DrawProfitDirection, string> = { home: '胜', draw: '平', away: '负' }
const minimumText = (values: DrawProfitDirection[], rates: DrawProfitRates) => values.length ? values.map(value => {
  const rate = rates[['home', 'draw', 'away'].indexOf(value)]
  return `${labels[value]} ${typeof rate === 'number' ? `${rate > 0 ? '+' : ''}${rate.toFixed(2)}%` : '暂无'}`
}).join(' / ') : '暂无'
const ratesText = (values: DrawProfitRates) => values.map((value, index) => `${['胜', '平', '负'][index]} ${value === null ? '暂无' : `${value > 0 ? '+' : ''}${value.toFixed(2)}%`}`).join(' / ')
const note = computed(() => [
  `盈亏方向分歧 ${signal.value.version}`,
  '分别取竞彩源站盈亏率与非让球模拟ROI的最低方向，并列全部保留；两组没有交集时标记。',
  `竞彩：${ratesText(signal.value.officialRates)}`,
  `模拟：${ratesText(signal.value.simulatedRates)}`,
  signal.value.reason,
  '仅为原候选池内的观察标签，不改变原模型分数、二次门槛或候选名单。',
].join('\n'))
</script>

<template>
  <div class="profit-support-signal" :title="note" :data-profit-support-state="signal.state" :data-profit-support-version="signal.version">
    <v-chip size="small" :color="signal.state === 'supported' ? 'info' : undefined" :variant="signal.state === 'supported' ? 'tonal' : 'text'">{{ label }}</v-chip>
    <template v-if="signal.state !== 'out_of_scope'">
      <div class="text-caption text-medium-emphasis mt-1">竞彩最低：{{ minimumText(signal.officialMinimum, signal.officialRates) }}</div>
      <div class="text-caption text-medium-emphasis">模拟最低：{{ minimumText(signal.simulatedMinimum, signal.simulatedRates) }}</div>
      <div v-if="signal.state === 'supported'" class="text-caption mt-1" :class="retained ? 'text-info' : 'text-medium-emphasis'">{{ retained ? '无共同方向，增强观察' : '特征匹配，未二次保留' }}</div>
      <div v-else class="text-caption text-medium-emphasis mt-1 signal-reason">{{ signal.reason }}</div>
    </template>
  </div>
</template>

<style scoped>
.profit-support-signal { min-width: 195px; max-width: 245px; white-space: normal; }
.signal-reason { white-space: normal; }
</style>
