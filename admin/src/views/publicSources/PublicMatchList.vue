<script setup lang="ts">
import { computed, ref } from 'vue'
import type { PublicMatch } from '@/api/publicSources'
import { frozenDrawPresentation } from '@/utils/drawFrozenSignal'
import { secondaryDrawPresentation } from '@/utils/drawSecondarySignal'
import { drawScorePresentation } from '@/utils/drawScoreRecommendation'
import { drawTotalsPredictionPresentation } from '@/utils/drawTotalsPrediction'
import { drawAlternativeOutcomePresentation } from '@/utils/drawAlternativeOutcome'
import { publicResultPresentation } from '@/utils/publicResults'
import DrawComparisonStages from '@/views/statistics/DrawComparisonStages.vue'
import DrawProfitSummary from '@/views/statistics/DrawProfitSummary.vue'
import PublicVoteProfit from './PublicVoteProfit.vue'

const props = defineProps<{ matches: PublicMatch[]; total: number }>()
defineEmits<{ detail: [id: string] }>()
const view = ref<'recommendations' | 'comparison' | 'profit'>('recommendations')
const names: Record<string, string> = { kickoff: '开赛时间', bonus: '竞彩奖金', support_had: '胜平负支持', support_hhad: '让球支持', euro: '欧赔', asian: '亚盘', totals: '大小球', history: '战绩' }
const rows = computed(() => props.matches.map(match => {
  const observation = match.models?.observation
  return {
    match, observation, result: publicResultPresentation(match.result),
    draw: observation ? frozenDrawPresentation(observation) : null,
    secondary: observation ? secondaryDrawPresentation(observation) : null,
    score: observation ? drawScorePresentation(observation) : null,
    totals: observation ? drawTotalsPredictionPresentation(observation) : null,
    alternative: observation ? drawAlternativeOutcomePresentation(observation) : null,
    reason: match.models?.reason || '缺少模型输入，请在分析详情查看数据缺项。',
  }
}))
function kickoff(value: string | null) {
  // Stored kickoffs already use Beijing wall time; do not reinterpret as UTC.
  return value ? value.replace('T', ' ').slice(5, 16) : '开赛时间待补'
}
function updated(value: string) {
  const at = new Date(value)
  return Number.isNaN(at.getTime()) ? '—' : at.toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })
}
function missing(match: PublicMatch) {
  return match.missing?.length ? `缺少：${match.missing.map(key => names[key] || key).join('、')}` : '当前采集项目齐全；赛前数据是否可用以模型状态为准。'
}
</script>

<template>
  <div class="public-match-list">
    <div class="list-view-bar">
      <v-tabs v-model="view" color="primary" density="compact" aria-label="比赛列表视图">
        <v-tab value="recommendations">推荐概览</v-tab>
        <v-tab value="comparison">赔率差值</v-tab>
        <v-tab value="profit">盈亏对比</v-tab>
      </v-tabs>
      <span class="list-count text-caption text-medium-emphasis">显示 {{ matches.length }} / {{ total }} 场</span>
    </div>
    <v-table class="match-table" :class="`view-${view}`">
      <colgroup><col class="match-column"><template v-if="view === 'recommendations'"><col><col><col><col></template><template v-else><col><col></template><col class="data-column"><col class="action-column"></colgroup>
      <thead><tr>
        <th scope="col">比赛 · 开赛时间（北京）</th>
        <template v-if="view === 'recommendations'"><th scope="col">入选平</th><th scope="col">二次筛选</th><th scope="col">推荐比分</th><th scope="col">大小球</th></template>
        <template v-else-if="view === 'comparison'"><th scope="col">初盘差值</th><th scope="col">即时盘差值</th></template>
        <template v-else><th scope="col">投票盈亏测算</th><th scope="col">模型模拟盈亏</th></template>
        <th scope="col">数据状态</th><th scope="col"><span class="sr-only">操作</span></th>
      </tr></thead>
      <tbody>
        <tr v-for="item in rows" :key="item.match.id" data-testid="public-match-row" :data-match-id="item.match.id">
          <td class="match-cell">
            <div class="match-meta"><strong>{{ item.match.lottery_number || item.match.official_id }}</strong><span>{{ kickoff(item.match.kickoff) }}</span></div>
            <div class="match-team"><span class="venue">主</span><strong>{{ item.match.home }}</strong></div>
            <div class="match-team"><span class="venue">客</span><strong>{{ item.match.away }}</strong></div>
            <div class="match-result" :title="item.result.reason"><strong v-if="item.result.score">{{ item.result.score }}</strong><span :class="item.result.color ? `text-${item.result.color}` : 'text-medium-emphasis'">{{ item.result.label }}</span></div>
          </td>
          <template v-if="view === 'recommendations'">
            <template v-if="item.observation && item.draw && item.secondary && item.score && item.totals">
              <td data-label="入选平">
                <v-chip size="small" :color="item.draw.color" variant="tonal" :title="item.draw.detail">{{ item.draw.label === '入选 · 待赛果' ? '入选平' : item.draw.label }}</v-chip>
                <div v-if="item.alternative?.state === 'recommended'" class="cell-note" :title="item.alternative.reason">{{ item.alternative.label }}</div>
              </td>
              <td data-label="二次筛选">
                <v-chip size="small" :color="item.secondary.decision === 'retained' && !item.result.confirmed ? 'success' : item.secondary.color" variant="tonal" :title="item.secondary.reason">{{ item.secondary.label === '二次入选 · 待赛果' ? '二次保留' : item.secondary.label }}</v-chip>
                <div v-if="typeof item.secondary.score === 'number'" class="cell-note" title="模型分数，不是平局概率">分数 {{ item.secondary.score.toFixed(3) }}</div>
                <div v-if="item.result.confirmed && item.secondary.outcomeLabel" class="cell-note" :class="item.secondary.outcomeColor ? `text-${item.secondary.outcomeColor}` : ''">{{ item.secondary.outcomeLabel }}</div>
              </td>
              <td data-label="推荐比分" :title="item.score.reason">
                <template v-if="item.score.scores.length"><div v-for="(score, index) in item.score.scores" :key="score" class="score-line"><span>{{ index === 0 ? '首选' : '次选' }}</span><strong>{{ score }}</strong></div></template>
                <span v-else class="cell-note">{{ item.score.label }}</span>
                <div v-if="item.result.confirmed && item.score.outcomeLabel" class="cell-note" :class="item.score.outcomeColor ? `text-${item.score.outcomeColor}` : ''">{{ item.score.outcomeLabel }}</div>
              </td>
              <td data-label="大小球" :title="item.totals.reason">
                <v-chip v-if="item.totals.state === 'recommended'" size="small" :color="item.totals.color" variant="tonal">{{ item.totals.label }}</v-chip>
                <span v-else class="cell-note">{{ item.totals.label }}</span>
                <div v-if="item.totals.water !== null" class="cell-note">水位 {{ item.totals.water.toFixed(2) }}</div>
                <div v-if="item.result.confirmed && item.totals.outcomeLabel" class="cell-note" :class="item.totals.outcomeColor ? `text-${item.totals.outcomeColor}` : ''">{{ item.totals.outcomeLabel }}</div>
              </td>
            </template>
            <td v-else colspan="4" class="unavailable-cell">
              <div class="unavailable-title"><v-icon size="17" color="warning">mdi-information-outline</v-icon><strong>{{ item.reason.includes('赛前') ? '缺少赛前数据，无法计算' : '模型暂不可用' }}</strong></div>
              <details class="reason-details"><summary>查看原因</summary><p>{{ item.reason }}</p></details>
            </td>
          </template>
          <template v-else-if="view === 'comparison'">
            <template v-if="item.observation"><td class="analysis-cell" data-label="初盘差值"><DrawComparisonStages :row="item.observation" stage="opening" compact /></td><td class="analysis-cell" data-label="即时盘差值"><DrawComparisonStages :row="item.observation" stage="latest" compact /></td></template>
            <td v-else colspan="2" class="unavailable-cell"><strong class="text-warning">差值暂不可用</strong><details class="reason-details"><summary>查看原因</summary><p>{{ item.reason }}</p></details></td>
          </template>
          <template v-else>
            <td class="analysis-cell" data-label="投票盈亏测算"><PublicVoteProfit :markets="item.match.analysis" /></td>
            <td class="analysis-cell" data-label="模型模拟盈亏"><DrawProfitSummary v-if="item.observation" :row="item.observation" kind="simulated" /><span v-else class="cell-note">缺少赛前输入，暂无模拟结果</span></td>
          </template>
          <td class="data-cell" data-label="数据状态">
            <v-chip size="x-small" :color="item.match.missing?.length ? 'warning' : 'success'" variant="tonal" :title="missing(item.match)">{{ item.match.missing?.length ? `资料缺 ${item.match.missing.length} 项` : '资料齐全' }}</v-chip>
            <div class="cell-note" title="最近入库时间（北京时间）">{{ updated(item.match.updated_at) }} 更新</div>
          </td>
          <td class="action-cell"><v-btn size="small" variant="text" color="primary" :aria-label="`分析详情 ${item.match.lottery_number || item.match.official_id} ${item.match.home} 对 ${item.match.away}`" @click="$emit('detail', item.match.id)">分析详情</v-btn></td>
        </tr>
        <tr v-if="!rows.length" class="empty-row"><td :colspan="view === 'recommendations' ? 7 : 5" class="empty-cell"><v-icon size="32" class="mb-3 text-medium-emphasis">mdi-soccer-field</v-icon><div>{{ total ? '没有符合当前筛选的比赛' : '此日期暂无比赛数据' }}</div><p class="cell-note">{{ total ? '切换“全部比赛”查看完整列表。' : '请在采集任务中选择日期并开始采集。' }}</p></td></tr>
      </tbody>
    </v-table>
    <p class="list-footnote text-caption text-medium-emphasis">推荐理由、完整参数和原始数据可在“分析详情”中查看。资料齐全不代表已具备赛前快照。</p>
  </div>
</template>

<style scoped>
.public-match-list { container-type: inline-size; container-name: match-list; }
.list-view-bar { display: flex; align-items: center; justify-content: space-between; gap: 12px; border-top: 1px solid rgba(var(--v-border-color), var(--v-border-opacity)); padding: 0 16px; }
.list-view-bar :deep(.v-tabs) { min-width: 0; }
.list-count { flex-shrink: 0; }
.match-table :deep(table) { width: 100%; table-layout: fixed; }
.match-column { width: 25%; }
.data-column { width: 15%; }
.action-column { width: 100px; }
.match-table :deep(th) { height: 46px; background: rgba(var(--v-theme-on-surface), .035); font-size: 12px; }
.match-table :deep(.v-table__wrapper > table > tbody > tr > td) { vertical-align: middle; padding: 10px 14px; overflow-wrap: anywhere; }
.match-table :deep(tbody > tr:hover) { background: rgba(var(--v-theme-on-surface), .025); }
.match-meta { display: flex; flex-wrap: wrap; gap: 4px 12px; margin-bottom: 9px; font-size: 12px; color: rgba(var(--v-theme-on-surface), .6); }
.match-meta strong { color: rgb(var(--v-theme-on-surface)); }
.match-team { display: flex; align-items: baseline; gap: 8px; line-height: 1.65; font-size: 14px; }
.match-result { display: flex; flex-wrap: wrap; gap: 4px 10px; align-items: baseline; margin-top: 7px; font-size: 11px; }
.match-result strong { font-size: 14px; font-variant-numeric: tabular-nums; }
.venue { flex-shrink: 0; font-size: 11px; color: rgba(var(--v-theme-on-surface), .5); }
.cell-note { margin-top: 5px; font-size: 12px; line-height: 1.5; color: rgba(var(--v-theme-on-surface), .6); }
.score-line { display: flex; align-items: center; gap: 12px; font-size: 14px; line-height: 1.7; }
.score-line span { font-size: 11px; color: rgba(var(--v-theme-on-surface), .6); }
.score-line strong { font-variant-numeric: tabular-nums; }
.unavailable-title { display: flex; align-items: center; gap: 7px; font-size: 13px; color: rgb(var(--v-theme-warning)); }
.reason-details { margin-top: 7px; font-size: 12px; color: rgba(var(--v-theme-on-surface), .6); }
.reason-details summary { width: fit-content; cursor: pointer; }
.reason-details p { max-width: 560px; line-height: 1.7; margin-top: 7px; }
.action-cell { text-align: right; padding-left: 0 !important; }
.list-footnote { padding: 12px 20px; border-top: 1px solid rgba(var(--v-border-color), var(--v-border-opacity)); }
.empty-cell { text-align: center; padding: 42px 20px !important; }
.analysis-cell :deep(.vote-profit), .analysis-cell :deep(.draw-profit-summary), .analysis-cell :deep(.comparison-stages) { min-width: 0; max-width: 100%; }
.view-comparison .match-column, .view-profit .match-column { width: 22%; }
.view-comparison .data-column, .view-profit .data-column { width: 13%; }
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
@container match-list (max-width: 1050px) {
  .match-table :deep(table), .match-table :deep(tbody) { display: block; }
  .match-table :deep(colgroup), .match-table :deep(thead) { display: none; }
  .match-table :deep(tbody > tr) { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); padding: 16px; gap: 14px 16px; border-bottom: 1px solid rgba(var(--v-border-color), var(--v-border-opacity)); }
  .match-table :deep(.v-table__wrapper > table > tbody > tr > td) { display: block; min-width: 0; min-height: 0; height: auto !important; padding: 0; border: 0; }
  .match-table :deep(td[data-label]::before) { content: attr(data-label); display: block; margin-bottom: 7px; font-size: 11px; color: rgba(var(--v-theme-on-surface), .55); }
  .match-cell, .unavailable-cell, .empty-cell { grid-column: 1 / -1; }
  .match-team { display: inline-flex; margin-right: 20px; }
  .data-cell { grid-column: 1 / 4; }
  .action-cell { align-self: end; }
  .analysis-cell { grid-column: span 2; }
  .list-footnote { border-top: 0; }
}
@container match-list (max-width: 600px) {
  .list-view-bar { display: block; padding: 0 4px; }
  .list-count { display: block; padding: 8px 12px; }
  .match-table :deep(tbody > tr) { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .match-team { display: flex; margin-right: 0; }
  .data-cell { grid-column: 1; }
  .analysis-cell { grid-column: 1 / -1; }
  .analysis-cell :deep(.profit-row) { grid-template-columns: 25px minmax(110px, 1fr) minmax(65px, 1fr); gap: 4px; }
}
</style>
