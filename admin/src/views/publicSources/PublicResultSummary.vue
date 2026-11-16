<script setup lang="ts">
import { computed } from 'vue'
import type { PublicMatch } from '@/api/publicSources'
import { publicResultMetrics } from '@/utils/publicResults'
const props = defineProps<{ matches: PublicMatch[] }>()
const metrics = computed(() => publicResultMetrics(props.matches))
const cards = computed(() => [
  { name: '入选平命中', ...metrics.value.draw },
  { name: '二次保留命中', ...metrics.value.secondary },
  { name: '前二比分命中', ...metrics.value.scores },
])
</script>

<template>
  <div class="result-summary">
    <div class="result-summary-title"><strong>赛果核对</strong><span>已确认 {{ metrics.confirmed }} / {{ matches.length }} 场<span v-if="metrics.pending"> · 待确认 {{ metrics.pending }} 场</span></span></div>
    <div v-if="metrics.confirmed" class="result-metrics">
      <div v-for="card in cards" :key="card.name"><span>{{ card.name }}</span><strong>{{ card.total ? `${card.hits} / ${card.total} · ${(card.hits / card.total * 100).toFixed(1)}%` : '暂无可核对预测' }}</strong></div>
      <div class="totals-results"><span>大小球结算</span><strong>全赢 {{ metrics.totals.full_win }} · 赢半 {{ metrics.totals.half_win }} · 走水 {{ metrics.totals.push }} · 输半 {{ metrics.totals.half_loss }} · 全输 {{ metrics.totals.full_loss }}</strong></div>
    </div>
    <p>{{ metrics.confirmed ? '统计当前日期全部比赛，仅纳入已有赛前预测且确认常规时间赛果的场次；缺赛前数据、待核实、取消或延期不进入命中率分母。' : '更新赛果后核对赛前预测。未确认的比分不按 0:0 处理，也不计为未命中。' }}</p>
  </div>
</template>

<style scoped>
.result-summary { padding: 14px 20px; border-top: 1px solid rgba(var(--v-border-color), var(--v-border-opacity)); }
.result-summary-title { display: flex; align-items: baseline; flex-wrap: wrap; gap: 8px 16px; font-size: 13px; }
.result-summary-title span, .result-summary p, .result-metrics span { color: rgba(var(--v-theme-on-surface), .6); font-size: 12px; }
.result-metrics { display: flex; flex-wrap: wrap; gap: 12px 32px; margin-top: 12px; }
.result-metrics > div { display: flex; flex-direction: column; gap: 4px; }
.result-metrics strong { font-size: 13px; font-variant-numeric: tabular-nums; }
.result-summary p { margin-top: 10px; line-height: 1.6; }
</style>
