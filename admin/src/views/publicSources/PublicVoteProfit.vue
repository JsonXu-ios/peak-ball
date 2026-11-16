<script setup lang="ts">
import type { PublicVoteMarket } from '@/api/publicSources'
import { drawProfitAmount, drawProfitClass, drawProfitRate } from '@/utils/drawProfit'
defineProps<{ markets?: Record<string, PublicVoteMarket> }>()

function value(input: number | null, suffix = '') {
  return input === null || !Number.isFinite(input) ? '—' : `${input}${suffix}`
}
function handicap(input: PublicVoteMarket['goal']) {
  return input === null || input === undefined || input === '' ? '让球数未知' : String(input)
}
</script>

<template>
  <div class="vote-profit text-caption" data-profit-kind="public-vote">
    <template v-if="markets && Object.keys(markets).length">
      <div class="text-medium-emphasis">庄家视角：正为盈利，负为亏损</div>
      <section v-for="(market, key) in markets" :key="key" class="mt-2">
        <div class="font-weight-bold">{{ key === 'spf' ? '胜平负' : `让球胜平负（${handicap(market.goal)}）` }} · 本地测算</div>
        <div class="profit-row text-medium-emphasis"><span>赛果</span><span>盈利金额</span><span>盈亏率</span></div>
        <div v-for="row in market.rows" :key="row.outcome" class="profit-row">
          <span>{{ row.outcome }}</span>
          <span>
            <span :class="drawProfitClass(row.profitAmount)">{{ drawProfitAmount(row.profitAmount, true) }}</span>
            <small class="d-block text-medium-emphasis">支持 {{ value(row.support, '%') }} × 赔率 {{ value(row.odds) }}</small>
          </span>
          <span :class="drawProfitClass(row.profitRate)">{{ drawProfitRate(row.profitRate) }}</span>
        </div>
        <div class="text-medium-emphasis">假设基数 {{ drawProfitAmount(market.totalStake) }}</div>
      </section>
      <div class="text-medium-emphasis mt-1">盈亏率＝100−支持率（%）×配对赔率。<br />支持率、赔率取同一份采集数据，盈亏为本地测算。</div>
    </template>
    <div v-else class="text-medium-emphasis">尚未取得支持比例，无法测算盈亏。</div>
  </div>
</template>

<style scoped>
.vote-profit { min-width: 285px; max-width: 360px; white-space: normal; line-height: 1.7; }
.profit-row { display: grid; grid-template-columns: 28px minmax(140px, 1fr) minmax(72px, 1fr); gap: 6px; }
.profit-row > span { white-space: nowrap; }
</style>
