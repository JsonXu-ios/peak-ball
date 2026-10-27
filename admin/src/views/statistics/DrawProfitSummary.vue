<script setup lang="ts">
import { computed } from 'vue'
import type { DrawFrontendLoadState, DrawObservation } from '@/types/drawHistory'
import { drawProfitAmount, drawProfitClass, drawProfitMarkets, drawProfitRate, type DrawProfitKind } from '@/utils/drawProfit'

const props = defineProps<{ row: DrawObservation; kind: DrawProfitKind; analysisState?: DrawFrontendLoadState }>()
defineEmits<{ retryAnalysis: [] }>()
const markets = computed(() => drawProfitMarkets(props.row, props.kind))
const emptyText = computed(() => {
  if (props.analysisState?.loading) return '正在读取前台分析…'
  if (props.analysisState?.error) return props.analysisState.error
  if (props.row.frontendAnalysis === undefined) return '尚未读取前台分析数据'
  return props.kind === 'sporttery' ? '暂无竞彩庄家盈亏数据' : '暂无模拟庄家盈亏数据'
})
</script>

<template>
  <div class="draw-profit-summary text-caption" :data-profit-kind="kind">
    <template v-if="markets.length">
      <div class="text-medium-emphasis">庄家视角：正为盈利，负为亏损</div>
      <section v-for="market in markets" :key="market.key" class="profit-market" :data-profit-market="market.key" :title="market.note">
        <div class="font-weight-bold">{{ market.name }}<span v-if="market.simulated" class="text-warning ml-1">模拟</span><span v-if="market.goal" class="text-medium-emphasis ml-1">让球 {{ market.goal }}</span></div>
        <div class="profit-row text-medium-emphasis"><span>赛果</span><span>本地测算（元）</span><span>{{ market.simulated ? '模拟 ROI' : '源站盈亏率' }}</span></div>
        <div v-for="outcome in market.outcomes" :key="outcome.outcome" class="profit-row" :data-profit-outcome="outcome.outcome">
          <span class="font-weight-bold">{{ outcome.label }}</span>
          <span :class="drawProfitClass(outcome.profit)" :title="outcome.profit === null ? outcome.unavailableReason : '沿用前台本地测算的庄家盈亏金额'">{{ outcome.profit === null ? outcome.unavailableReason : drawProfitAmount(outcome.profit, true) }}</span>
          <span :class="drawProfitClass(outcome.rate)">{{ drawProfitRate(outcome.rate) }}</span>
        </div>
        <div class="text-medium-emphasis">测算基数：{{ drawProfitAmount(market.totalStake) }}</div>
        <div v-if="!market.simulated" class="text-medium-emphasis">盈亏率取源站，金额为前台测算</div>
      </section>
    </template>
    <div v-else :class="analysisState?.error ? 'text-error' : 'text-medium-emphasis'">{{ emptyText }}<v-btn v-if="row.frontendAnalysis === undefined && !analysisState?.loading" size="x-small" variant="text" color="primary" @click.stop="$emit('retryAnalysis')" @keydown.enter.stop @keydown.space.stop>{{ analysisState?.error ? '重试' : '读取分析' }}</v-btn></div>
  </div>
</template>

<style scoped>
.draw-profit-summary { min-width: 290px; max-width: 360px; white-space: normal; line-height: 1.7; }
.profit-market { margin-top: 4px; }
.profit-market + .profit-market { border-top: 1px solid rgba(var(--v-theme-on-surface), .12); padding-top: 5px; margin-top: 7px; }
.profit-row { display: grid; grid-template-columns: 28px minmax(120px, 1fr) minmax(92px, 1fr); gap: 6px; }
.profit-row > span { white-space: nowrap; }
</style>
