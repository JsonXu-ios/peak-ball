<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { publicSourceOverview, publicSourceDetail, publicSourceCollect, publicSourceUpdateResults, publicSourceResultRaw, publicSourceMap, publicSourceCompanyMap, publicSourceRaw, type PublicMatch, type PublicJob, type PublicDetail, type PublicQuote, type PublicAcquisition } from '@/api/publicSources'
import DrawFrozenSignal from '@/views/statistics/DrawFrozenSignal.vue'
import DrawAlternativeOutcome from '@/views/statistics/DrawAlternativeOutcome.vue'
import DrawSecondarySignal from '@/views/statistics/DrawSecondarySignal.vue'
import DrawScoreRecommendation from '@/views/statistics/DrawScoreRecommendation.vue'
import DrawTotalsPrediction from '@/views/statistics/DrawTotalsPrediction.vue'
import DrawComparisonStages from '@/views/statistics/DrawComparisonStages.vue'
import DrawProfitSummary from '@/views/statistics/DrawProfitSummary.vue'
import PublicMatchList from './PublicMatchList.vue'
import PublicModelParameters from './PublicModelParameters.vue'
import PublicResultSummary from './PublicResultSummary.vue'
import { publicResultPresentation } from '@/utils/publicResults'
import { drawProfitAmount, drawProfitClass } from '@/utils/drawProfit'

const route = useRoute()
const isTasks = computed(() => route.path.endsWith('/tasks'))
const date = ref(new Date().toLocaleDateString('en-CA', { timeZone: 'Asia/Shanghai' }))
const limit = ref(10)
const matches = ref<PublicMatch[]>([])
const jobs = ref<PublicJob[]>([])
const loading = ref(false)
const starting = ref(false)
const updatingResults = ref(false)
const resultJobId = ref<number | null>(null)
const error = ref('')
const notice = ref('')
const network = ref<{ proxyCount: number; rotationConfigured: boolean; intervalSeconds?: number; jitterSeconds?: number; forwardedForRandomized?: boolean; error?: string }>()
const detail = ref<PublicDetail | null>(null)
const detailOpen = ref(false)
const detailLoading = ref(false)
const mapping = ref('')
const companyLabel = ref('')
const companyName = ref('')
const saving = ref(false)
const selection = ref('all')
const selectedMatches = computed(() => matches.value.filter(match => {
  const row = match.models?.observation
  if (selection.value === 'draw') return row?.frozenDrawSignal?.state === 'selected'
  if (selection.value === 'secondary') return row?.secondaryDecision?.decision === 'retained'
  if (selection.value === 'not_selected') return row?.frozenDrawSignal?.state === 'not_selected'
  if (selection.value === 'unavailable') return !row || row.frozenDrawSignal?.state === 'unavailable'
  return true
}))
const modelCounts = computed(() => ({
  draw: matches.value.filter(x => x.models?.observation?.frozenDrawSignal?.state === 'selected').length,
  secondary: matches.value.filter(x => x.models?.observation?.secondaryDecision?.decision === 'retained').length,
  unavailable: matches.value.filter(x => !x.models?.observation || x.models.observation.frozenDrawSignal?.state === 'unavailable').length,
  calculated: matches.value.filter(x => x.models?.observation?.frozenDrawSignal?.score !== undefined).length,
  incomplete: matches.value.filter(x => x.missing?.length).length,
}))
const quoteTab = ref<'euro' | 'asian' | 'totals'>('euro')
const rawOpen = ref(false)
const rawText = ref('')
let timer: ReturnType<typeof setInterval> | undefined
const running = computed(() => jobs.value.some(j => ['queued', 'running'].includes(j.status)))
const activeJob = computed(() => jobs.value.find(j => ['queued', 'running'].includes(j.status)))
const resultsRunning = computed(() => activeJob.value?.config.kind === 'results')
const resultsPending = computed(() => updatingResults.value || resultsRunning.value || resultJobId.value !== null)
const busy = computed(() => running.value || starting.value || resultsPending.value)
const detailResult = computed(() => publicResultPresentation(detail.value?.result))
const quotes = computed<PublicQuote[]>(() => detail.value?.parts[quoteTab.value] || [])
const names: Record<string, string> = { kickoff: '开赛时间', match_head: '比赛信息', bonus: '竞彩奖金', support_had: '胜平负支持', support_hhad: '让球支持', euro: '欧赔', asian: '亚盘', totals: '大小球', history: '战绩' }
const statuses: Record<string, string> = { queued: '等待执行', running: '采集中', completed: '完成', partial: '数据有缺项', failed: '失败', interrupted: '已中断' }
const sourceFields: Record<string, string> = { sourceLabel: '公司名称', sourceCompanyId: '公司ID', companyId: '公司ID', opening: '初盘', latest: '即时盘', probabilities: '概率', kelly: '即时凯利', returnRatio: '返还率', openingKelly: '初始凯利', openingKellyTime: '初始凯利时间', quoteHistory: '赔率变化记录', matchMapping: '比赛映射' }
function acquisitionLabel(value?: PublicAcquisition) {
  return value?.mode === 'api' ? '接口' : value?.mode === 'api_with_page_fields' ? '接口＋页面补字段' : value?.mode === 'page' ? '页面补充' : '历史记录'
}
function acquisitionColor(value?: PublicAcquisition) {
  return value?.mode === 'api' ? 'success' : value?.mode === 'api_with_page_fields' ? 'info' : 'grey'
}
function fieldNames(fields?: string[]) {
  return fields?.map(field => {
    const separator = field.lastIndexOf(':')
    return separator < 0 ? sourceFields[field] || field : field.slice(0, separator) + '：' + (sourceFields[field.slice(separator + 1)] || field.slice(separator + 1))
  }).join('、') || '该项数据'
}
function endpoint(url: string) {
  try { const parsed = new URL(url); return parsed.hostname + parsed.pathname } catch { return url }
}
function requestParameters(source: NonNullable<PublicAcquisition['interfaces']>[number]) {
  const values: string[] = []
  try { new URL(source.url).searchParams.forEach((value, key) => values.push(`${key}=${value}`)) } catch { /* Keep saved form fields available for historical URLs. */ }
  for (const [key, value] of Object.entries(source.form || {})) values.push(`${key}=${value}`)
  return values.join(' · ')
}
function transportLabel(value: string) { return value === 'http' ? 'HTTP直连' : value === 'browser' || value === 'browser_response' ? '浏览器会话接口响应' : value }
function message(e: unknown) { return (e as { response?: { data?: { error?: string } } })?.response?.data?.error || (e instanceof Error ? e.message : '操作失败') }
function display(value: unknown) { return value === null || value === undefined || value === '' ? '—' : String(value) }
function percent(value: number | null) { return value === null ? '—' : `${Number(value.toFixed(2))}%` }
function time(value: string | null | undefined) { return value ? new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false }) : '—' }
async function load(silent = false) {
  if (!silent) loading.value = true
  try {
    const { data } = await publicSourceOverview(date.value)
    matches.value = data.matches; jobs.value = data.jobs; network.value = data.network
    let resultFinished = false
    if (resultJobId.value !== null) {
      const job = jobs.value.find(item => item.id === resultJobId.value)
      if (job && !['queued', 'running'].includes(job.status)) {
        notice.value = ''; error.value = ''
        if (job.status === 'failed' || job.status === 'interrupted') error.value = job.message || '赛果更新未完成，请查看任务记录。'
        else notice.value = job.message || '赛果更新完成。'
        resultJobId.value = null
        resultFinished = true
      }
    }
    if ((silent || resultFinished) && detailOpen.value && detail.value) await openDetail(detail.value.match.id, true)
  } catch (e) { error.value = message(e) } finally { loading.value = false }
}
async function openDetail(id: string, silent = false) {
  detailOpen.value = true
  if (!silent) { detailLoading.value = true; detail.value = null }
  try { const { data } = await publicSourceDetail(id); detail.value = data; mapping.value = data.match.zgzcw_id || '' }
  catch (e) { error.value = message(e) } finally { detailLoading.value = false }
}
async function collect(single = false) {
  if (busy.value) return
  starting.value = true; error.value = ''; notice.value = ''
  try {
    const match = single ? detail.value?.match : undefined
    await publicSourceCollect({ date: match?.business_date || date.value, limit: Number(limit.value), ...(match ? { officialId: match.official_id, ...(mapping.value ? { zgzcwId: mapping.value } : {}) } : {}) })
    notice.value = '独立采集任务已开始，可在任务页面查看进度。'
    await load()
  } catch (e) { error.value = message(e) } finally { starting.value = false }
}
async function updateResults(single = false) {
  if (busy.value) return
  updatingResults.value = true; error.value = ''; notice.value = ''
  try {
    const match = single ? detail.value?.match : undefined
    if (single && !match) return
    const { data } = await publicSourceUpdateResults({ date: match?.business_date || date.value, ...(match ? { officialId: match.official_id } : {}) })
    resultJobId.value = data.id
    notice.value = '赛果更新任务已开始，完成后自动刷新比分与命中核对。'
    await load()
  } catch (e) {
    error.value = (e as { response?: { status?: number } })?.response?.status === 404 ? '赛果接口尚未加载，请自行重启 go_admin 后再试。' : message(e)
  } finally { updatingResults.value = false }
}
async function saveMapping() {
  if (!detail.value) return
  saving.value = true
  try { await publicSourceMap(detail.value.match.id, mapping.value); notice.value = '比赛映射已保存'; await load(); await openDetail(detail.value.match.id) }
  catch (e) { error.value = message(e) } finally { saving.value = false }
}
async function saveCompany() {
  saving.value = true
  try { await publicSourceCompanyMap(companyLabel.value, companyName.value); notice.value = '公司映射已保存'; if (detail.value) await openDetail(detail.value.match.id) }
  catch (e) { error.value = message(e) } finally { saving.value = false }
}
async function showRaw(snapshotId: number) {
  if (!detail.value) return
  try { const { data } = await publicSourceRaw(detail.value.match.id, snapshotId); rawText.value = data.payload; rawOpen.value = true }
  catch (e) { error.value = message(e) }
}
async function showResultRaw() {
  if (!detail.value?.result?.snapshotId) return
  try {
    const { data } = await publicSourceResultRaw(detail.value.match.id, detail.value.result.snapshotId)
    rawText.value = data.payload; rawOpen.value = true
  } catch (e) { error.value = message(e) }
}
onMounted(() => { load(); timer = setInterval(() => { if ((running.value || resultJobId.value !== null) && !loading.value) load(true) }, 5000) })
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>

<template>
  <div class="pa-4 public-sources">
    <div class="d-flex align-center flex-wrap ga-3 mb-4">
      <div><h2 class="text-h5">{{ isTasks ? '备选源采集任务' : '备选源比赛分析' }}</h2><div class="text-body-2 text-medium-emphasis">体彩 · 足彩网 · 独立数据空间</div></div>
      <v-spacer />
      <v-text-field v-model="date" label="竞彩日期" type="date" hide-details density="compact" style="max-width:190px" @change="load()" />
      <v-btn color="primary" variant="tonal" :disabled="busy || !date || !matches.length || loading" :loading="resultsPending" @click="updateResults()">更新当日赛果</v-btn>
      <v-btn variant="tonal" :loading="loading" @click="load()">刷新</v-btn>
    </div>
    <v-alert v-if="error" type="error" closable class="mb-3" @click:close="error = ''">{{ error }}</v-alert>
    <v-alert v-if="notice" type="success" closable class="mb-3" @click:close="notice = ''">{{ notice }}</v-alert>
    <v-alert v-if="activeJob" type="info" variant="tonal" density="compact" class="mb-3">{{ activeJob.config.date }} · {{ activeJob.config.kind === 'results' ? '赛果更新' : '数据采集' }}任务 #{{ activeJob.id }} 执行中，已处理 {{ activeJob.processed }} 场。{{ activeJob.message }}</v-alert>
    <v-alert v-if="isTasks" type="info" variant="tonal" density="compact" class="mb-4">体彩提供投票支持比例及配对赔率，用于盈亏、冷热测算；比赛信息、奖金、盘口和战绩来自足彩网。此菜单使用独立数据库，原 VIPC 数据与菜单保留。</v-alert>

    <template v-if="isTasks">
      <v-alert v-if="network" :type="network.error ? 'warning' : 'info'" variant="tonal" density="compact" class="mb-4">
        {{ network.error || '采集方式：数据接口优先，随机 X-Forwarded-For。' }}
        <span v-if="network.intervalSeconds">页面和数据请求间隔 {{ network.intervalSeconds }}—{{ network.intervalSeconds + (network.jitterSeconds || 0) }} 秒。</span>
        <span v-if="network.proxyCount">另配置 {{ network.proxyCount }} 个可选代理入口。</span>
        <div>已核查接口未返回的字段使用页面补充，详情列出字段与原因。已有接口失败会记录错误，不改用另一网站的数据补缺。</div>
      </v-alert>
      <v-card class="mb-4" title="按日期采集">
        <v-card-text>
          <div class="d-flex ga-4 align-center flex-wrap">
            <v-text-field v-model.number="limit" label="单次比赛上限" type="number" min="1" max="50" hide-details density="compact" style="max-width:180px" />
            <v-btn color="primary" :disabled="busy || !date || limit < 1 || limit > 50" :loading="starting" @click="collect()">开始采集</v-btn>
          </div>
          <p class="mt-3 text-body-2 text-medium-emphasis">体彩仅采集胜平负、让球胜平负支持比例；其余请求由足彩网提供。足彩网战绩及部分未返回字段采用页面补充，来源随快照保存，旧分工数据不混入新结果。</p>
        </v-card-text>
      </v-card>
      <v-card title="最近任务">
        <v-table density="compact"><thead><tr><th>任务</th><th>日期</th><th>状态</th><th>处理场数</th><th>开始时间</th><th>结果</th></tr></thead>
          <tbody><tr v-for="job in jobs" :key="job.id"><td>#{{ job.id }}<div class="text-caption">{{ job.config.kind === 'results' ? '更新赛果' : '数据采集' }}</div></td><td>{{ job.config.date }}</td><td><v-chip size="small" :color="job.status === 'completed' ? 'success' : job.status === 'failed' ? 'error' : 'warning'">{{ job.status === 'running' && job.config.kind === 'results' ? '更新中' : statuses[job.status] || job.status }}</v-chip></td><td>{{ job.processed }}</td><td>{{ time(job.created_at) }}</td><td>
            <div v-if="job.message">{{ job.message }}</div>
            <details v-if="job.errors.length"><summary>{{ job.errors.length }} 条来源提示（点击查看）</summary><div v-for="(item, i) in job.errors" :key="i" class="py-1">{{ item.stage }}：{{ item.message }}</div></details>
            <span v-else-if="job.status === 'completed'">采集完成</span>
          </td></tr><tr v-if="!jobs.length"><td colspan="6" class="text-center pa-6">暂无独立采集任务</td></tr></tbody>
        </v-table>
      </v-card>
    </template>
    <v-card v-else :title="`比赛列表 · ${matches.length} 场`">
      <v-card-text class="list-toolbar d-flex flex-wrap ga-3 align-center">
        <v-chip color="primary" variant="tonal">入选平 {{ modelCounts.draw }}</v-chip>
        <v-chip color="success" variant="tonal">二次保留 {{ modelCounts.secondary }}</v-chip>
        <v-chip color="info" variant="tonal">已计算 {{ modelCounts.calculated }} / {{ matches.length }}</v-chip>
        <v-chip v-if="modelCounts.incomplete" color="warning" variant="tonal">资料缺项 {{ modelCounts.incomplete }}</v-chip>
        <v-chip v-if="modelCounts.unavailable" color="warning" variant="tonal">无法计算 {{ modelCounts.unavailable }}</v-chip>
        <v-spacer />
        <v-select v-model="selection" :items="[{ title: '全部比赛', value: 'all' }, { title: '入选平', value: 'draw' }, { title: '二次保留', value: 'secondary' }, { title: '未入选', value: 'not_selected' }, { title: '无法计算', value: 'unavailable' }]" label="模型筛选" density="compact" hide-details style="max-width:200px" />
      </v-card-text>
      <PublicResultSummary :matches="matches" />
      <PublicMatchList :matches="selectedMatches" :total="matches.length" @detail="openDetail" />
    </v-card>

    <v-dialog v-model="detailOpen" max-width="1400" scrollable>
      <v-card>
        <v-card-title class="d-flex align-center">{{ detail ? `${detail.match.home} VS ${detail.match.away}` : '读取比赛' }}<v-spacer /><v-btn icon="mdi-close" variant="text" @click="detailOpen = false" /></v-card-title>
        <v-card-text>
          <v-progress-linear v-if="detailLoading" indeterminate />
          <template v-if="detail">
            <div class="d-flex ga-3 align-center flex-wrap mb-4"><v-chip>{{ detail.match.lottery_number }}</v-chip><span>开赛 {{ display(detail.match.kickoff) }}</span><v-chip :color="detail.cutoff.stopped ? 'grey' : 'primary'">{{ detail.cutoff.stopped === null ? '截止时间待补齐' : detail.cutoff.stopped ? '已过本地截止时间' : '赛前' }}</v-chip><v-spacer /><v-btn color="primary" variant="tonal" size="small" :disabled="busy" :loading="resultsPending" @click="updateResults(true)">更新本场赛果</v-btn><v-btn variant="tonal" size="small" :disabled="busy" :loading="starting" @click="collect(true)">补采本场</v-btn></div>
            <v-card variant="outlined" class="mb-4" title="比赛赛果">
              <v-card-text>
                <div class="d-flex align-center flex-wrap ga-3"><strong v-if="detailResult.score" class="text-h5">{{ detailResult.score }}</strong><v-chip size="small" :color="detailResult.color">{{ detailResult.label }}</v-chip></div>
                <div class="text-caption mt-2">{{ detailResult.reason }}</div>
                <div v-if="detail.result?.lastAttemptError" class="text-caption text-warning mt-2">最近更新未取得新结果：{{ detail.result.lastAttemptError }}</div>
                <div class="d-flex align-center flex-wrap ga-2 mt-2 text-caption"><span v-if="detail.result?.receivedAt">取得时间（北京）：{{ time(detail.result.receivedAt) }}</span><v-btn v-if="detail.result?.snapshotId" size="small" variant="text" @click="showResultRaw">查看赛果原文</v-btn></div>
                <div class="text-caption text-medium-emphasis mt-2">仅确认常规时间（含补时，不含加时和点球）的比分用于命中核对；赛前预测按原记录保留。</div>
              </v-card-text>
            </v-card>
            <v-alert v-if="detail.missing.length" type="warning" variant="tonal" density="compact" class="mb-4">尚未取得：{{ detail.missing.map(x => names[x] || x).join('、') }}</v-alert>
            <v-card variant="outlined" class="mb-4" title="选平与推荐 · 原模型规则">
              <v-alert v-if="!detail.models?.observation" type="warning" variant="tonal" density="compact">{{ detail.models?.reason || '模型等待计算' }}</v-alert>
              <template v-else>
                <v-table><thead><tr><th>入选平</th><th>次选胜负</th><th>二次筛选</th><th>两比分推荐</th><th>大小球推荐</th></tr></thead><tbody><tr>
                  <td><DrawFrozenSignal :row="detail.models.observation" /></td><td><DrawAlternativeOutcome :row="detail.models.observation" /></td><td><DrawSecondarySignal :row="detail.models.observation" /></td><td><DrawScoreRecommendation :row="detail.models.observation" /></td><td><DrawTotalsPrediction :row="detail.models.observation" /></td>
                </tr></tbody></v-table>
                <v-card-text class="text-caption text-medium-emphasis">
                  使用独立库中最后一份有效赛前快照，复用原固定模型及筛选规则。赛果独立更新，仅用于核对已有预测，不回写赛前输入。
                  <div>有效近期战绩：主队 {{ detail.models.observation.homeRecent.sample }} 场、客队 {{ detail.models.observation.awayRecent.sample }} 场；交锋 {{ detail.models.observation.headToHead?.sample || 0 }} 场。</div>
                  <div v-for="warning in detail.models.warnings" :key="warning">{{ warning }}</div>
                  <details class="mt-2"><summary>模型版本与输入快照</summary><div>{{ detail.models.version }} / {{ detail.models.observation.frozenDrawSignal?.modelVersion }}</div><pre>{{ JSON.stringify(detail.models.snapshotIds, null, 2) }}</pre></details>
                </v-card-text>
              </template>
            </v-card>
            <PublicModelParameters v-if="detail.models" :models="detail.models" />
            <div class="analysis-grid">
              <v-card v-for="(market, key) in detail.analysis" :key="key" variant="outlined" :title="key === 'spf' ? '胜平负 · 投票盈亏测算' : '让球胜平负 · 投票盈亏测算'">
                <v-card-text class="py-2 text-caption">
                  <div v-if="key === 'rqspf'">本份投票数据的让球数：{{ display(market.goal) }}</div>
                  <div>赔率、支持率、概率为采集原值；盈亏和冷热差为本地测算。</div>
                </v-card-text>
                <v-table density="compact"><thead><tr><th>选项</th><th>配对赔率</th><th>投票支持率</th><th>来源概率</th><th>假设盈亏金额</th><th>测算盈亏率</th><th>测算冷热差</th></tr></thead><tbody><tr v-for="row in market.rows" :key="row.outcome"><td>{{ row.outcome }}</td><td>{{ display(row.odds) }}</td><td>{{ percent(row.support) }}</td><td>{{ percent(row.probability) }}</td><td :class="drawProfitClass(row.profitAmount)">{{ drawProfitAmount(row.profitAmount, true) }}</td><td :class="drawProfitClass(row.profitRate)">{{ percent(row.profitRate) }}</td><td>{{ row.heatDifference === null ? '—' : `${row.heatDifference > 0 ? '+' : ''}${row.heatDifference.toFixed(2)} 个百分点` }}</td></tr></tbody></v-table>
                <v-card-text class="text-caption">
                  <div>盈亏率（%）＝100−投票支持率（%）×配对赔率。冷热差＝投票支持率−来源概率；来源未返回概率时留空。</div>
                  <div>假设测算基数：{{ drawProfitAmount(market.totalStake) }}，非真实成交金额。</div>
                  <div class="d-flex align-center flex-wrap ga-2 mt-2">
                    <span>数据采集：{{ time(market.receivedAt) }}</span>
                    <v-btn v-if="market.inputSource" size="small" variant="text" @click="showRaw(market.inputSource.snapshotId)">核对原始数据</v-btn>
                  </div>
                  <div v-if="market.inputSource" class="source-evidence">{{ market.inputSource.mode === 'api' ? '接口' : '来源' }}：<a :href="market.inputSource.url" target="_blank" rel="noopener noreferrer">{{ endpoint(market.inputSource.url) }}</a></div>
                  <div v-if="market.latestQuote" class="mt-1">奖金表最新：{{ market.latestQuote.latest.join(' / ') }}（{{ market.latestQuote.updatedAt }}），单列参考，不参与本份投票盈亏计算。</div>
                </v-card-text>
              </v-card>
            </div>
            <section v-if="detail.models?.observation" class="my-4">
              <h3 class="text-subtitle-1 mb-3">初盘／即时盘差值 · 威廉－竞彩、威廉－平均欧赔</h3>
              <DrawComparisonStages :row="detail.models.observation" />
              <v-card variant="outlined" title="模型模拟盈亏"><v-card-text><div class="text-caption text-medium-emphasis mb-2">使用平均赔率与模型估算的散户分布，属于独立模拟结果。上方投票盈亏使用采集到的支持率和配对赔率。</div><DrawProfitSummary :row="detail.models.observation" kind="simulated" /></v-card-text></v-card>
            </section>
            <v-card v-if="detail.parts.bonus" variant="outlined" class="my-4" title="竞彩奖金">
              <v-card-text>
                <div>胜平负：{{ detail.parts.bonus.spf?.latest.join(' / ') || '—' }}　让球 {{ detail.parts.bonus.rqspf?.goal || '—' }}：{{ detail.parts.bonus.rqspf?.latest.join(' / ') || '—' }}</div>
                <div class="mt-2">平局比分：<span v-for="(value, score) in detail.parts.bonus.drawScores" :key="score" class="mr-4">{{ score }}＝{{ display(value) }}</span></div>
                <div class="mt-2">总进球：<span v-for="(value, i) in detail.parts.bonus.totalGoals" :key="i" class="mr-4">{{ i === 7 ? '7+' : i }}＝{{ display(value) }}</span></div>
                <details class="mt-3"><summary>奖金变化记录</summary><div v-for="(rows, name) in detail.parts.bonus.history" :key="name" class="my-2"><strong>{{ name }} · {{ rows.length }} 次</strong><pre class="quote-history">{{ JSON.stringify(rows, null, 2) }}</pre></div></details>
              </v-card-text>
            </v-card>
            <v-card variant="outlined" class="my-4">
              <v-tabs v-model="quoteTab"><v-tab value="euro">欧赔</v-tab><v-tab value="asian">亚盘</v-tab><v-tab value="totals">大小球</v-tab></v-tabs>
              <v-table density="compact"><thead><tr><th>公司</th><th>初始（赔率／盘口）</th><th>即时（赔率／盘口）</th><th>最早记录凯利</th><th>即时凯利</th><th>返还率</th></tr></thead><tbody>
                <tr v-for="(row, i) in quotes" :key="`${row.sourceLabel}-${i}`">
                  <td>{{ row.company || row.sourceLabel }}</td><td>{{ row.opening.join(' / ') || '—' }}</td><td>{{ row.latest.join(' / ') || '—' }}</td>
                  <td>{{ row.openingKelly?.join(' / ') || '—' }}<div v-if="row.openingKellyPrices" class="text-caption">对应赔率 {{ row.openingKellyPrices.join(' / ') }}<br>{{ row.openingKellyTime }}</div><div v-if="row.openingKellyNote" class="text-caption text-warning">{{ row.openingKellyNote }}</div></td>
                  <td>{{ row.kelly?.join(' / ') || '—' }}<div v-for="warning in row.fieldWarnings || []" :key="warning" class="text-caption text-warning">{{ warning }}</div></td><td>{{ row.returnRatio || '—' }}</td>
                </tr><tr v-if="!quotes.length"><td colspan="6" class="text-center pa-5">尚未取得此类报价</td></tr></tbody></v-table>
            </v-card>
            <v-expansion-panels class="my-4">
              <v-expansion-panel v-if="detail.parts.history" title="交锋与近期战绩"><v-expansion-panel-text>
                <pre v-if="detail.parts.history.text" class="history-text">{{ detail.parts.history.text }}</pre>
                <template v-else><section v-for="(rows, group) in detail.parts.history.tables" :key="group" class="mb-4">
                  <h4 class="text-subtitle-2 mb-2">{{ ['主队近期', '客队近期', '历史交锋'][group] || '战绩记录' }}</h4>
                  <v-table density="compact"><thead><tr><th>日期</th><th>赛事</th><th>主队</th><th>比分</th><th>客队</th><th>半场</th></tr></thead><tbody><tr v-for="(row, index) in rows" :key="index"><td>{{ row[2] }}</td><td>{{ row[0] }}</td><td>{{ row[3] }}</td><td>{{ row[4] }}</td><td>{{ row[5] }}</td><td>{{ display(row[6]) }}</td></tr><tr v-if="!rows.length"><td colspan="6" class="text-medium-emphasis">来源未提供此类战绩记录</td></tr></tbody></v-table>
                </section></template>
              </v-expansion-panel-text></v-expansion-panel>
              <v-expansion-panel title="比赛与公司映射"><v-expansion-panel-text>
                <p class="mb-3">体彩 ID：{{ detail.match.official_id }}。比赛使用独立标识，不与 VIPC 主键混用。</p>
                <div class="d-flex ga-3 align-center"><v-text-field v-model="mapping" label="足彩网比赛ID" hide-details density="compact" /><v-btn :loading="saving" @click="saveMapping">保存比赛映射</v-btn></div>
                <div class="d-flex ga-3 align-center mt-4"><v-text-field v-model="companyLabel" label="来源标签，如 36*" hide-details density="compact" /><v-text-field v-model="companyName" label="对应公司，如 Bet365" hide-details density="compact" /><v-btn :loading="saving" @click="saveCompany">保存公司映射</v-btn></div>
              </v-expansion-panel-text></v-expansion-panel>
              <v-expansion-panel title="数据来源与原始快照"><v-expansion-panel-text>
                <v-table density="compact" class="acquisition-table"><thead><tr><th>数据</th><th>采集时间（北京）</th><th>采集方式与来源</th><th /></tr></thead><tbody>
                  <tr v-for="(item, kind) in detail.provenance" :key="kind">
                    <td>{{ names[kind] || kind }}</td><td>{{ time(item.received_at) }}</td>
                    <td><div class="d-flex align-center ga-2"><v-chip size="x-small" :color="acquisitionColor(item.acquisition)">{{ acquisitionLabel(item.acquisition) }}</v-chip><a :href="item.url" target="_blank" rel="noopener noreferrer">{{ item.source }}</a></div>
                      <details v-if="item.acquisition?.mode === 'api' || item.acquisition?.mode === 'api_with_page_fields' || item.acquisition?.mode === 'page'" class="mt-2">
                        <summary>查看接口与补充字段</summary>
                        <div v-for="(source, index) in item.acquisition.interfaces || []" :key="`api-${index}`" class="source-evidence mt-2">
                          <div><strong>{{ source.method }}</strong> <a :href="source.url" target="_blank" rel="noopener noreferrer">{{ endpoint(source.url) }}</a></div>
                          <div class="text-caption">{{ transportLabel(source.transport) }}<span v-if="source.fields?.length"> · {{ fieldNames(source.fields) }}</span></div>
                          <div v-if="requestParameters(source)" class="text-caption">请求参数：{{ requestParameters(source) }}</div>
                          <div v-if="source.receivedAt" class="text-caption">取得时间：{{ time(source.receivedAt) }}</div>
                        </div>
                        <div v-for="(source, index) in item.acquisition.pageFields || []" :key="`page-${index}`" class="source-evidence mt-2">
                          <div>页面补充：{{ fieldNames(source.fields) }}</div><div class="text-caption">原因：{{ source.reason }}</div>
                          <a :href="source.url" target="_blank" rel="noopener noreferrer" class="text-caption">{{ endpoint(source.url) }}</a>
                        </div>
                        <div v-for="(warning, index) in item.acquisition.fieldWarnings || []" :key="'warning-' + index" class="text-caption text-warning mt-2">{{ warning.company }}：{{ warning.message }}（接口 {{ warning.apiPrices.join(' / ') }}；页面 {{ warning.pagePrices.join(' / ') }}）</div>
                      </details>
                      <div v-else class="text-caption text-medium-emphasis mt-1">历史记录未保存采集方式，不推断为接口或页面。</div>
                    </td><td><v-btn size="small" variant="text" @click="showRaw(item.id)">查看原文</v-btn></td>
                  </tr>
                </tbody></v-table>
              </v-expansion-panel-text></v-expansion-panel>
            </v-expansion-panels>
          </template>
        </v-card-text>
      </v-card>
    </v-dialog>
    <v-dialog v-model="rawOpen" max-width="1100" scrollable><v-card title="原始快照"><v-card-text><pre class="raw-text">{{ rawText }}</pre></v-card-text><v-card-actions><v-spacer /><v-btn @click="rawOpen = false">关闭</v-btn></v-card-actions></v-card></v-dialog>
  </div>
</template>

<style scoped>
.analysis-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 520px), 1fr)); gap: 16px; }
.history-text, .raw-text, .quote-history { white-space: pre-wrap; overflow-wrap: anywhere; font-size: 12px; line-height: 1.7; max-height: 500px; overflow: auto; }
.public-sources :deep(th) { white-space: nowrap; }
.acquisition-table :deep(td) { vertical-align: top; padding-top: 10px; padding-bottom: 10px; }
.source-evidence { max-width: 620px; overflow-wrap: anywhere; }
</style>
