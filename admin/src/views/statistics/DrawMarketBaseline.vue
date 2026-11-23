<script setup lang="ts">
import { computed } from 'vue'
import type { DrawObservation } from '@/types/drawHistory'
import { drawMarketBaseline, DRAW_MARKET_BASELINE_THRESHOLD } from '@/utils/drawMarketBaseline'

const props = defineProps<{ row: DrawObservation }>()
const item = computed(() => drawMarketBaseline(props.row))
const agreementLabel = computed(() => ({ both: '与模型同选', market_only: '仅市场选', model_only: '仅模型选', neither: '' })[item.value.agreement ?? 'neither'])
const color = computed(() => item.value.state !== 'selected' ? undefined : item.value.drawHit === true ? 'success' : item.value.drawHit === false ? 'warning' : 'teal')
const note = computed(() => [
  '市场对照：即时平均欧赔按 1/赔率 归一化得到平局隐含概率。',
  `≥${(DRAW_MARKET_BASELINE_THRESHOLD * 100).toFixed(0)}% 视为市场入选；门槛只用 2026-10-15 前的竞彩数据选定，之后的赛果只做前瞻观察。`,
  '口径与原模型一致：竞彩且至少1场有效赛前交锋。即时均赔可能接近收盘价，不等于下注时可得价格。',
  item.value.reason,
].filter(Boolean).join('\n'))
</script>

<template>
  <div class="draw-market-baseline" :title="note" :data-market-version="item.version" :data-market-state="item.state">
    <v-chip v-if="item.state === 'selected'" size="small" :color="color" variant="tonal">{{ item.label }}</v-chip>
    <div v-else class="text-caption text-medium-emphasis">{{ item.label }}</div>
    <div v-if="item.impliedDraw !== null" class="text-caption text-medium-emphasis mt-1">平局隐含 {{ (item.impliedDraw * 100).toFixed(1) }}%</div>
    <div v-if="agreementLabel" class="text-caption mt-1" :class="item.agreement === 'both' ? 'text-medium-emphasis' : 'text-info'">{{ agreementLabel }}</div>
    <div v-if="item.state === 'unavailable' && item.reason" class="text-caption text-medium-emphasis mt-1 market-reason">{{ item.reason }}</div>
  </div>
</template>

<style scoped>
.draw-market-baseline { min-width: 150px; }
.market-reason { max-width: 200px; white-space: normal; }
</style>
