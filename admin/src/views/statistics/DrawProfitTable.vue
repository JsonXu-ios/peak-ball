<script setup lang="ts">
import type { BookmakerMarket, BookmakerOutcome } from '@/types/drawHistory'

defineProps<{ market: BookmakerMarket; totalStake?: number; local?: boolean }>()

function fixed(value: number, digits = 2) {
  return value.toFixed(digits).replace(/\.0+$/, '').replace(/(\.\d*[1-9])0+$/, '$1')
}

function money(value: number | undefined) {
  if (!Number.isFinite(value)) return '暂无'
  const number = Number(value)
  return Math.abs(number) >= 10000 ? `${fixed(number / 10000, 1)}万` : `${Math.round(number).toLocaleString('zh-CN')}元`
}

function signedMoney(value: number, available: boolean) {
  if (!available) return '赔率不足'
  if (!Number.isFinite(value)) return '暂无'
  return `${value > 0 ? '+' : value < 0 ? '-' : ''}${money(Math.abs(value))}`
}

function percent(value: number | undefined, signed = false) {
  if (!Number.isFinite(value)) return '暂无'
  return `${signed && Number(value) > 0 ? '+' : ''}${fixed(Number(value))}%`
}

function roi(value: number | undefined, available = true) {
  if (!available) return '赔率不足'
  if (!Number.isFinite(value)) return '暂无'
  return `${Number(value) > 0 ? '+' : ''}${Number(value).toFixed(2)}%`
}

function outcome(row: BookmakerOutcome) {
  return { home: '胜', draw: '平', away: '负' }[row.outcome] || row.outcomeLabel
}

function valueClass(value: number | undefined, available = true) {
  if (!available || !Number.isFinite(value) || Math.abs(Number(value)) < 0.01) return 'text-medium-emphasis'
  return Number(value) > 0 ? 'text-error' : 'text-success'
}

function odds(row: BookmakerOutcome) {
  return Number.isFinite(row.odds) && row.odds > 0 ? fixed(row.odds) : '暂无'
}
</script>

<template>
  <v-card variant="outlined" class="profit-card">
    <v-card-title class="d-flex flex-wrap align-center ga-2 text-subtitle-2">
      {{ local ? '本地庄家盈亏' : `${market.name}交易盈亏` }}
      <v-chip v-if="market.simulated" color="warning" size="x-small" variant="tonal">模拟</v-chip>
      <span v-if="market.goal" class="text-caption text-medium-emphasis">让球 {{ market.goal }}</span>
      <v-spacer />
      <span class="text-caption text-medium-emphasis">测算总投注额 {{ money(totalStake) }}</span>
    </v-card-title>
    <v-table density="compact">
      <thead><tr><th>赛果</th><th>{{ local ? '平均欧赔' : '指数' }}</th><th>{{ local ? '散户心理' : '彩民支持率' }}</th><th>交易额</th><th>庄家赔付</th><th>本地庄家盈亏</th><th>{{ local ? '本地 ROI' : market.simulated ? '模拟庄家盈亏率' : '官方庄家盈亏率' }}</th><th v-if="!local">冷热指数</th></tr></thead>
      <tbody>
        <tr v-for="row in market.bookmakerByOutcome || []" :key="row.outcome">
          <td class="font-weight-bold">{{ outcome(row) }}</td>
          <td>{{ odds(row) }}</td>
          <td :class="row.retailShare > 65 ? 'text-error font-weight-bold' : ''">{{ percent(row.retailShare) }}</td>
          <td>{{ money(row.betStake) }}</td>
          <td>{{ row.available ? money(row.payout) : '赔率不足' }}</td>
          <td class="font-weight-bold" :class="valueClass(row.bookmakerProfit, row.available)">{{ signedMoney(row.bookmakerProfit, row.available) }}</td>
          <td v-if="local" :class="valueClass(row.bookmakerRoi, row.available)">{{ roi(row.bookmakerRoi, row.available) }}</td>
          <td v-else :class="valueClass(row.officialProfitRate)">{{ roi(row.officialProfitRate) }}</td>
          <td v-if="!local" :class="valueClass(row.hotColdIndex)">{{ Number.isFinite(row.hotColdIndex) ? fixed(Number(row.hotColdIndex)) : '暂无' }}</td>
        </tr>
        <tr v-if="!market.bookmakerByOutcome?.length"><td :colspan="local ? 7 : 8" class="text-center py-6 text-medium-emphasis">暂无盈亏明细</td></tr>
      </tbody>
    </v-table>
    <div v-if="market.simulationNote" class="text-caption pa-3" :class="market.simulated ? 'text-warning' : 'text-medium-emphasis'">{{ market.simulationNote }}</div>
    <div v-if="local" class="text-caption text-medium-emphasis pa-3">沿用前台本地测算：平均欧赔与加权散户心理，不使用竞彩投注数据。</div>
  </v-card>
</template>

<style scoped>
.profit-card :deep(th), .profit-card :deep(td) { white-space: nowrap; }
</style>
