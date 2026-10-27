<script setup lang="ts">
import { computed } from 'vue'
import type { BookmakerOutcome, DirectionValues, DrawCompanyQuote, DrawFrontendLoadState, DrawObservation, DrawObservedEuroOdds, DrawObservedLine } from '@/types/drawHistory'
import { isDrawSimulatedResultMarket } from '@/utils/drawProfit'
import DrawProfitTable from './DrawProfitTable.vue'
import DrawComparisonStages from './DrawComparisonStages.vue'

const props = defineProps<{ row: DrawObservation; compact?: boolean; heatOnly?: boolean; comparisonStage?: 'opening' | 'latest'; analysisState?: DrawFrontendLoadState; quotesState?: DrawFrontendLoadState }>()
defineEmits<{ retryAnalysis: []; retryQuotes: [] }>()
const analysis = computed(() => props.row.frontendAnalysis)
const markets = computed(() => analysis.value?.roiSimulation?.markets || [])
const tradeMarkets = computed(() => markets.value.filter((market) => !market.simulated && !['william', 'bet365'].includes(market.key)))
const simulatedMarkets = computed(() => markets.value.filter(isDrawSimulatedResultMarket))
const sporttery = computed(() => markets.value.find((market) => market.key === 'sporttery' && !market.simulated))
const bettingRows = computed(() => sporttery.value?.bettingRatio?.length ? sporttery.value.bettingRatio : sporttery.value?.bookmakerByOutcome || [])

function value(raw: unknown): string {
  if (raw === null || raw === undefined || raw === '') return '暂无'
  if (Array.isArray(raw)) return raw.map(value).join(', ')
  if (typeof raw === 'number' && !Number.isFinite(raw)) return '暂无'
  return String(raw)
}

function list(raw: unknown[] | null | undefined) {
  return raw?.length ? raw.map(value).join(' / ') : '暂无'
}

function percent(raw: unknown) {
  const text = value(raw)
  return text === '暂无' || text.endsWith('%') ? text : `${text}%`
}

function fixed(raw: number | undefined, signed = false) {
  if (!Number.isFinite(raw)) return '暂无'
  const numeric = Number(raw)
  return `${signed && numeric > 0 ? '+' : ''}${numeric.toFixed(2).replace(/\.0+$/, '').replace(/(\.\d*[1-9])0+$/, '$1')}%`
}

function heatClass(values: unknown[] | undefined) {
  return (values || []).slice(0, 2).some((raw) => Number.parseFloat(String(raw).replace(/%$/, '')) > 65) ? 'text-error font-weight-bold' : ''
}

function euroText(euro: DrawObservedEuroOdds, phase: 'opening' | 'latest') {
  const values = euro?.[phase] || []
  return values.some((raw) => raw !== null && raw !== undefined && raw !== '') ? [0, 1, 2].map((index) => value(values[index])).join(' / ') : '暂无'
}

function unpricedEuro(euro: DrawObservedEuroOdds) {
  const values = [...(euro?.opening || []), ...(euro?.latest || [])].filter((raw) => raw !== null && raw !== undefined && raw.trim() !== '')
  return euro?.source === 'missing' && values.length > 0 && values.every((raw) => Number(raw) === 0)
}

function openingText(company: DrawCompanyQuote) {
  if (company.key === 'sporttery' && company.euroOdds.openingStatus === 'no_quotes') return '源站无报价记录'
  if (company.key === 'sporttery' && unpricedEuro(company.euroOdds)) return '未开价'
  const opening = euroText(company.euroOdds, 'opening')
  return company.key === 'sporttery' && opening === '暂无' ? '未采集初盘' : opening
}

function lineText(line: DrawObservedLine, phase: 'opening' | 'latest', labels: string[]) {
  const odds = line?.[phase === 'opening' ? 'openingOdds' : 'latestOdds'] || []
  const raw = line?.[phase]
  if ((raw === null || raw === undefined || raw === '') && !odds.some((item) => item !== null && item !== undefined && item !== '')) return '暂无'
  return `${value(raw)}（${labels.map((label, index) => `${label} ${value(odds[index])}`).join(' / ')}）`
}

function triplet(values: DirectionValues | undefined) {
  if (!values) return '暂无'
  const score = (raw: number) => Number.isFinite(raw) ? raw.toFixed(2).replace(/\.00$/, '') : '暂无'
  return `主 ${score(values.home)} / 平 ${score(values.draw)} / 客 ${score(values.away)}`
}

function odds(raw: number) {
  return Number.isFinite(raw) && raw > 0 ? raw.toFixed(2).replace(/\.0+$/, '').replace(/(\.\d*[1-9])0+$/, '$1') : '暂无'
}

function outcome(row: BookmakerOutcome) {
  return { home: '胜', draw: '平', away: '负' }[row.outcome] || row.outcomeLabel
}
</script>

<template>
  <div v-if="compact" class="frontend-summary text-caption">
    <template v-if="analysis">
      <template v-if="!comparisonStage">
        <div :class="heatClass(analysis.yapantouzhu)">亚盘热度：主 {{ percent(analysis.yapantouzhu?.[0]) }} / 客 {{ percent(analysis.yapantouzhu?.[1]) }}</div>
        <div :class="heatClass(analysis.qiushutouzhu)">大小球热度：大 {{ percent(analysis.qiushutouzhu?.[0]) }} / 小 {{ percent(analysis.qiushutouzhu?.[1]) }}</div>
      </template>
      <DrawComparisonStages v-if="!heatOnly" :row="row" :stage="comparisonStage" compact />
    </template>
    <div v-else :class="analysisState?.error ? 'text-error' : 'text-medium-emphasis'">{{ analysisState?.loading ? '正在读取前台分析…' : analysisState?.error || (row.frontendAnalysis === undefined ? '尚未读取前台分析数据' : '暂无本场前台分析数据') }}<v-btn v-if="row.frontendAnalysis === undefined && !analysisState?.loading" size="x-small" variant="text" color="primary" @click.stop="$emit('retryAnalysis')" @keydown.enter.stop @keydown.space.stop>{{ analysisState?.error ? '重试' : '读取分析' }}</v-btn></div>
  </div>
  <div v-else class="frontend-data">
    <div class="text-subtitle-1 font-weight-bold mb-1">前台数据核对</div>
    <div class="text-caption text-medium-emphasis mb-3">沿用前台现有分析值；公司原始报价与前台计算盘口分别列出。</div>

    <v-card variant="outlined" class="mb-4">
      <v-card-title class="text-subtitle-2">竞彩 / 威廉 / Bet365 公司报价</v-card-title>
      <v-table v-if="row.companyQuotes?.length" density="compact" class="company-table">
        <thead><tr><th>公司</th><th>初盘（胜 / 平 / 负）</th><th>即时盘（胜 / 平 / 负）</th><th>亚盘初盘（主 / 客水位）</th><th>亚盘即时盘</th><th>大小球初盘（大 / 小水位）</th><th>大小球即时盘</th></tr></thead>
        <tbody>
          <tr v-for="company in row.companyQuotes" :key="company.key">
            <td class="font-weight-bold">{{ company.name }}</td>
            <td>{{ openingText(company) }}</td>
            <td>{{ euroText(company.euroOdds, 'latest') }}<div v-if="unpricedEuro(company.euroOdds)" class="text-caption text-medium-emphasis">未开价（原始记录）</div></td>
            <td>{{ lineText(company.asianHandicap, 'opening', ['主', '客']) }}</td>
            <td>{{ lineText(company.asianHandicap, 'latest', ['主', '客']) }}</td>
            <td>{{ lineText(company.goalsLine, 'opening', ['大', '小']) }}</td>
            <td>{{ lineText(company.goalsLine, 'latest', ['大', '小']) }}</td>
          </tr>
        </tbody>
      </v-table>
      <div v-else class="text-body-2 pa-4" :class="quotesState?.error ? 'text-error' : 'text-medium-emphasis'">{{ quotesState?.loading ? '正在读取本场原始欧赔和盘口…' : quotesState?.error || (row.companyQuotes === undefined ? '尚未读取公司报价' : '暂无公司报价对比数据') }}<v-btn v-if="row.companyQuotes === undefined && !quotesState?.loading" size="small" variant="text" color="primary" class="ml-2" @click.stop="$emit('retryQuotes')">{{ quotesState?.error ? '重试报价' : '读取报价' }}</v-btn></div>
      <div v-if="row.companyQuotes?.length && (quotesState?.loading || quotesState?.error)" class="text-caption px-3" :class="quotesState?.error ? 'text-error' : 'text-medium-emphasis'">{{ quotesState?.loading ? '正在补充竞彩初盘…' : quotesState.error }}<v-btn v-if="quotesState?.error" size="x-small" variant="text" @click.stop="$emit('retryQuotes')">重试补充</v-btn></div>
      <div class="text-caption text-medium-emphasis pa-3">每家公司分别展示实际记录，缺失为“暂无”；即时盘为当前已获取的最新报价。</div>
    </v-card>

    <DrawComparisonStages :row="row" />

    <template v-if="analysis">
      <v-row>
        <v-col cols="12" xl="6">
          <v-card variant="outlined" class="h-100">
            <v-card-title class="text-subtitle-2">前台盘口与投注热度</v-card-title>
            <v-table density="compact"><thead><tr><th>项目</th><th>亚盘</th><th>大小球</th></tr></thead><tbody>
              <tr><td>取样来源</td><td>{{ analysis.basis?.asian || '旧接口未提供实际取样来源' }}</td><td>{{ analysis.basis?.goals || '旧接口未提供实际取样来源' }}</td></tr>
              <tr><td>初盘</td><td>{{ value(analysis.yapanpankou1) }}</td><td>{{ value(analysis.qiushupankou1) }}</td></tr>
              <tr><td>即时盘</td><td>{{ value(analysis.yapanpankou2) }}</td><td>{{ value(analysis.qiushupankou2) }}</td></tr>
              <tr><td>投注热度</td><td :class="heatClass(analysis.yapantouzhu)">主 {{ percent(analysis.yapantouzhu?.[0]) }} / 客 {{ percent(analysis.yapantouzhu?.[1]) }}</td><td :class="heatClass(analysis.qiushutouzhu)">大 {{ percent(analysis.qiushutouzhu?.[0]) }} / 小 {{ percent(analysis.qiushutouzhu?.[1]) }}</td></tr>
              <tr><td>压力方向</td><td>{{ value(analysis.yapantouzhu?.[12]) }}</td><td>{{ value(analysis.qiushutouzhu?.[6]) }}</td></tr>
            </tbody></v-table>
            <div class="text-caption text-medium-emphasis pa-3">计算盘口用于分析，公司原始报价见上表。</div>
          </v-card>
        </v-col>
        <v-col cols="12" xl="6">
          <v-card variant="outlined" class="h-100">
            <v-card-title class="text-subtitle-2">散户心理</v-card-title>
            <v-table density="compact"><tbody>
              <tr><th>散户心理 · 隐含基底</th><td>主 {{ Number.isFinite(analysis.winProbability) ? `${Math.round(analysis.winProbability)}%` : '暂无' }} / 平 {{ Number.isFinite(analysis.drawProbability) ? `${Math.round(analysis.drawProbability)}%` : '暂无' }} / 客 {{ Number.isFinite(analysis.loseProbability) ? `${Math.round(analysis.loseProbability)}%` : '暂无' }}</td></tr>
              <tr><th>散户心理 · 加权分布</th><td>{{ list(analysis.sanhuxinli?.slice(0, 3)) }}</td></tr>
              <tr><th>人气备注</th><td>{{ value(analysis.sanhuxinli?.[4]) }}</td></tr>
            </tbody></v-table>
          </v-card>
        </v-col>
      </v-row>

      <v-card v-if="sporttery" variant="outlined" class="mt-4">
        <v-card-title class="text-subtitle-2">竞彩投注比例 · 官方数据</v-card-title>
        <v-table density="compact"><thead><tr><th>赛果</th><th>指数</th><th>概率</th><th>彩民支持率</th><th>误差</th><th>心理误差</th></tr></thead><tbody>
          <tr v-for="(row, index) in bettingRows" :key="row.outcome"><td class="font-weight-bold">{{ outcome(row) }}</td><td>{{ odds(row.odds) }}</td><td>{{ fixed(row.probability) }}</td><td :class="row.retailShare > 65 ? 'text-error font-weight-bold' : ''">{{ fixed(row.retailShare) }}</td><td>{{ fixed(row.error, true) }}</td><td v-if="index === 0" :rowspan="bettingRows.length">{{ value(sporttery.psychologyErrorLabel) }}</td></tr>
          <tr v-if="!bettingRows.length"><td colspan="6" class="text-center py-6 text-medium-emphasis">暂无官方投注比例明细</td></tr>
        </tbody></v-table>
      </v-card>

      <template v-if="analysis.localMarket">
        <div class="d-flex flex-wrap ga-4 text-caption text-medium-emphasis mt-4 mb-2"><span>本地欧赔：{{ triplet(analysis.localMarket.odds) }}</span><span>本地散户心理：{{ triplet(analysis.localMarket.retailDistribution) }}</span></div>
        <DrawProfitTable :market="analysis.localMarket" :total-stake="analysis.roiSimulation?.totalStake" local />
      </template>
      <v-card v-else variant="outlined" class="mt-4"><v-card-title class="text-subtitle-2">本地庄家盈亏</v-card-title><div class="pa-4 text-body-2 text-medium-emphasis">暂无本地庄家盈亏数据</div></v-card>

      <div class="text-subtitle-2 mt-4 mb-2">交易盈亏</div>
      <div v-if="!tradeMarkets.length" class="text-body-2 text-medium-emphasis mb-4">暂无交易盈亏数据</div>
      <DrawProfitTable v-for="market in tradeMarkets" :key="market.key" :market="market" :total-stake="analysis.roiSimulation?.totalStake" class="mb-3" />

      <div class="text-subtitle-2 mt-4 mb-2">模拟交易盈亏</div>
      <div v-if="!simulatedMarkets.length" class="text-body-2 text-medium-emphasis">暂无模拟交易盈亏数据</div>
      <DrawProfitTable v-for="market in simulatedMarkets" :key="market.key" :market="market" :total-stake="analysis.roiSimulation?.totalStake" class="mb-3" />
    </template>
    <v-alert v-else :type="analysisState?.error ? 'error' : 'info'" variant="tonal" density="compact">{{ analysisState?.loading ? '正在读取所选日期的前台分析…' : analysisState?.error || (row.frontendAnalysis === undefined ? '尚未读取前台分析数据' : '暂无本场前台分析数据') }}<v-btn v-if="row.frontendAnalysis === undefined && !analysisState?.loading" size="small" variant="text" color="primary" class="ml-2" @click.stop="$emit('retryAnalysis')">{{ analysisState?.error ? '重试分析' : '读取分析' }}</v-btn></v-alert>
  </div>
</template>

<style scoped>
.frontend-summary { min-width: 230px; max-width: 360px; white-space: normal; line-height: 1.7; }
.frontend-data { white-space: normal; }
.frontend-data :deep(th), .frontend-data :deep(td) { white-space: normal; }
.company-table :deep(th), .company-table :deep(td) { white-space: nowrap; }
.company-table :deep(td) { vertical-align: top; padding-top: 12px; padding-bottom: 12px; }
</style>
