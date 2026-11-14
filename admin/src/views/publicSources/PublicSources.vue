<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { publicSourceOverview, publicSourceDetail, publicSourceCollect, publicSourceMap, publicSourceCompanyMap, publicSourceRaw, type PublicMatch, type PublicJob, type PublicDetail, type PublicQuote } from '@/api/publicSources'
import DrawFrozenSignal from '@/views/statistics/DrawFrozenSignal.vue'
import DrawAlternativeOutcome from '@/views/statistics/DrawAlternativeOutcome.vue'
import DrawSecondarySignal from '@/views/statistics/DrawSecondarySignal.vue'
import DrawScoreRecommendation from '@/views/statistics/DrawScoreRecommendation.vue'
import DrawTotalsPrediction from '@/views/statistics/DrawTotalsPrediction.vue'

const route = useRoute()
const isTasks = computed(() => route.path.endsWith('/tasks'))
const date = ref(new Date().toLocaleDateString('en-CA', { timeZone: 'Asia/Shanghai' }))
const limit = ref(10)
const matches = ref<PublicMatch[]>([])
const jobs = ref<PublicJob[]>([])
const loading = ref(false)
const starting = ref(false)
const error = ref('')
const notice = ref('')
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
}))
const quoteTab = ref<'euro' | 'asian' | 'totals'>('euro')
const rawOpen = ref(false)
const rawText = ref('')
let timer: ReturnType<typeof setInterval> | undefined
const running = computed(() => jobs.value.some(j => ['queued', 'running'].includes(j.status)))
const quotes = computed<PublicQuote[]>(() => detail.value?.parts[quoteTab.value] || [])
const names: Record<string, string> = { bonus: '竞彩奖金', support_had: '胜平负支持', support_hhad: '让球支持', euro: '欧赔', asian: '亚盘', totals: '大小球', history: '战绩' }
const statuses: Record<string, string> = { queued: '等待执行', running: '采集中', completed: '完成', partial: '部分完成', failed: '失败', interrupted: '已中断' }
function message(e: unknown) { return (e as { response?: { data?: { error?: string } } })?.response?.data?.error || (e instanceof Error ? e.message : '操作失败') }
function display(value: unknown) { return value === null || value === undefined || value === '' ? '—' : String(value) }
function percent(value: number | null) { return value === null ? '—' : `${Number(value.toFixed(2))}%` }
function time(value: string | null | undefined) { return value ? new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false }) : '—' }
async function load(silent = false) {
  if (!silent) loading.value = true
  try {
    const { data } = await publicSourceOverview(date.value)
    matches.value = data.matches; jobs.value = data.jobs
    if (silent && detailOpen.value && detail.value) await openDetail(detail.value.match.id, true)
  } catch (e) { error.value = message(e) } finally { loading.value = false }
}
async function openDetail(id: string, silent = false) {
  detailOpen.value = true
  if (!silent) { detailLoading.value = true; detail.value = null }
  try { const { data } = await publicSourceDetail(id); detail.value = data; mapping.value = data.match.zgzcw_id || '' }
  catch (e) { error.value = message(e) } finally { detailLoading.value = false }
}
async function collect(single = false) {
  starting.value = true; error.value = ''; notice.value = ''
  try {
    const match = single ? detail.value?.match : undefined
    await publicSourceCollect({ date: match?.business_date || date.value, limit: Number(limit.value), ...(match ? { officialId: match.official_id, ...(mapping.value ? { zgzcwId: mapping.value } : {}) } : {}) })
    notice.value = '独立采集任务已开始，可在任务页面查看进度。'
    await load()
  } catch (e) { error.value = message(e) } finally { starting.value = false }
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
onMounted(() => { load(); timer = setInterval(() => { if (running.value && !loading.value) load(true) }, 5000) })
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>

<template>
  <div class="pa-4 public-sources">
    <div class="d-flex align-center flex-wrap ga-3 mb-4">
      <div><h2 class="text-h5">{{ isTasks ? '备选源采集任务' : '备选源比赛分析' }}</h2><div class="text-body-2 text-medium-emphasis">体彩 · 足彩网 · 独立数据空间</div></div>
      <v-spacer />
      <v-text-field v-model="date" label="竞彩日期" type="date" hide-details density="compact" style="max-width:190px" @change="load()" />
      <v-btn variant="tonal" :loading="loading" @click="load()">刷新</v-btn>
    </div>
    <v-alert v-if="error" type="error" closable class="mb-3" @click:close="error = ''">{{ error }}</v-alert>
    <v-alert v-if="notice" type="success" closable class="mb-3" @click:close="notice = ''">{{ notice }}</v-alert>
    <v-alert type="info" variant="tonal" density="compact" class="mb-4">此菜单只使用独立数据库。支持比例为用户投票；盈亏、冷热为本地测算。原 VIPC 数据与菜单保留。</v-alert>

    <template v-if="isTasks">
      <v-card class="mb-4" title="按日期采集">
        <v-card-text>
          <div class="d-flex ga-4 align-center flex-wrap">
            <v-text-field v-model.number="limit" label="单次比赛上限" type="number" min="1" max="50" hide-details density="compact" style="max-width:180px" />
            <v-btn color="primary" :disabled="running || !date || limit < 1 || limit > 50" :loading="starting" @click="collect()">开始采集</v-btn>
          </div>
          <p class="mt-3 text-body-2 text-medium-emphasis">依次采集支持比例、竞彩奖金、欧赔、亚盘、大小球和战绩，并按页面分页读取。部分来源失败会记录原因，已成功取得的数据保留。</p>
        </v-card-text>
      </v-card>
      <v-card title="最近任务">
        <v-table density="compact"><thead><tr><th>任务</th><th>日期</th><th>状态</th><th>处理场数</th><th>开始时间</th><th>结果</th></tr></thead>
          <tbody><tr v-for="job in jobs" :key="job.id"><td>#{{ job.id }}</td><td>{{ job.config.date }}</td><td><v-chip size="small" :color="job.status === 'completed' ? 'success' : job.status === 'failed' ? 'error' : 'warning'">{{ statuses[job.status] || job.status }}</v-chip></td><td>{{ job.processed }}</td><td>{{ time(job.created_at) }}</td><td>
            <div v-if="job.message">{{ job.message }}</div>
            <details v-if="job.errors.length"><summary>{{ job.errors.length }} 项未完成</summary><div v-for="(item, i) in job.errors" :key="i" class="py-1">{{ item.stage }}：{{ item.message }}</div></details>
            <span v-else-if="job.status === 'completed'">采集完成</span>
          </td></tr><tr v-if="!jobs.length"><td colspan="6" class="text-center pa-6">暂无独立采集任务</td></tr></tbody>
        </v-table>
      </v-card>
    </template>
    <v-card v-else :title="`比赛列表 · ${matches.length} 场`">
      <v-card-text class="d-flex flex-wrap ga-3 align-center">
        <v-chip color="primary" variant="tonal">入选平 {{ modelCounts.draw }}</v-chip>
        <v-chip color="success" variant="tonal">二次保留 {{ modelCounts.secondary }}</v-chip>
        <v-chip v-if="modelCounts.unavailable" color="warning" variant="tonal">待计算 {{ modelCounts.unavailable }}</v-chip>
        <v-spacer />
        <v-select v-model="selection" :items="[{ title: '全部比赛', value: 'all' }, { title: '入选平', value: 'draw' }, { title: '二次保留', value: 'secondary' }, { title: '未入选', value: 'not_selected' }, { title: '待计算', value: 'unavailable' }]" label="模型筛选" density="compact" hide-details style="max-width:200px" />
      </v-card-text>
      <v-table class="model-observation-table"><thead><tr><th>编号 / 开赛时间（北京）</th><th>比赛</th><th>入选平</th><th>次选胜负</th><th>二次筛选</th><th>两比分推荐</th><th>大小球推荐</th><th>数据 / 最近入库</th><th /></tr></thead>
        <tbody><tr v-for="match in selectedMatches" :key="match.id">
          <td>{{ match.lottery_number || match.official_id }}<div class="text-caption">{{ display(match.kickoff) }}</div></td>
          <td class="font-weight-medium">{{ match.home }} <span class="text-medium-emphasis mx-1">VS</span> {{ match.away }}</td>
          <template v-if="match.models?.observation">
            <td><DrawFrozenSignal :row="match.models.observation" /></td>
            <td><DrawAlternativeOutcome :row="match.models.observation" /></td>
            <td><DrawSecondarySignal :row="match.models.observation" /></td>
            <td><DrawScoreRecommendation :row="match.models.observation" /></td>
            <td><DrawTotalsPrediction :row="match.models.observation" /></td>
          </template>
          <td v-else colspan="5" class="text-medium-emphasis">{{ match.models?.reason || '模型等待计算' }}</td>
          <td><v-chip v-for="part in match.parts" :key="part" size="x-small" class="ma-1" color="primary" variant="tonal">{{ names[part] || part }}</v-chip><div class="text-caption">{{ time(match.updated_at) }}</div></td>
          <td><v-btn size="small" variant="text" color="primary" @click="openDetail(match.id)">分析详情</v-btn></td>
        </tr>
        <tr v-if="!selectedMatches.length"><td colspan="9" class="text-center pa-8">{{ matches.length ? '没有符合当前模型筛选的比赛。' : '此日期暂无独立数据，请从采集任务开始。' }}</td></tr></tbody>
      </v-table>
    </v-card>

    <v-dialog v-model="detailOpen" max-width="1400" scrollable>
      <v-card>
        <v-card-title class="d-flex align-center">{{ detail ? `${detail.match.home} VS ${detail.match.away}` : '读取比赛' }}<v-spacer /><v-btn icon="mdi-close" variant="text" @click="detailOpen = false" /></v-card-title>
        <v-card-text>
          <v-progress-linear v-if="detailLoading" indeterminate />
          <template v-if="detail">
            <div class="d-flex ga-3 align-center flex-wrap mb-4"><v-chip>{{ detail.match.lottery_number }}</v-chip><span>开赛 {{ display(detail.match.kickoff) }}</span><v-chip :color="detail.cutoff.stopped ? 'grey' : 'primary'">{{ detail.cutoff.stopped === null ? '截止时间待补齐' : detail.cutoff.stopped ? '已过本地截止时间' : '赛前' }}</v-chip><v-spacer /><v-btn variant="tonal" size="small" :disabled="running" :loading="starting" @click="collect(true)">补采本场</v-btn></div>
            <v-alert v-if="detail.missing.length" type="warning" variant="tonal" density="compact" class="mb-4">尚未取得：{{ detail.missing.map(x => names[x] || x).join('、') }}</v-alert>
            <v-card variant="outlined" class="mb-4" title="选平与推荐 · 原模型规则">
              <v-alert v-if="!detail.models?.observation" type="warning" variant="tonal" density="compact">{{ detail.models?.reason || '模型等待计算' }}</v-alert>
              <template v-else>
                <v-table><thead><tr><th>入选平</th><th>次选胜负</th><th>二次筛选</th><th>两比分推荐</th><th>大小球推荐</th></tr></thead><tbody><tr>
                  <td><DrawFrozenSignal :row="detail.models.observation" /></td><td><DrawAlternativeOutcome :row="detail.models.observation" /></td><td><DrawSecondarySignal :row="detail.models.observation" /></td><td><DrawScoreRecommendation :row="detail.models.observation" /></td><td><DrawTotalsPrediction :row="detail.models.observation" /></td>
                </tr></tbody></v-table>
                <v-card-text class="text-caption text-medium-emphasis">
                  使用独立库中最后一份有效赛前快照，复用原固定模型及筛选规则。赛后补采不回写赛前结果；本菜单不核对最终赛果。
                  <div>有效近期战绩：主队 {{ detail.models.observation.homeRecent.sample }} 场、客队 {{ detail.models.observation.awayRecent.sample }} 场；交锋 {{ detail.models.observation.headToHead?.sample || 0 }} 场。</div>
                  <div v-for="warning in detail.models.warnings" :key="warning">{{ warning }}</div>
                  <details class="mt-2"><summary>模型版本与输入快照</summary><div>{{ detail.models.version }} / {{ detail.models.observation.frozenDrawSignal?.modelVersion }}</div><pre>{{ JSON.stringify(detail.models.snapshotIds, null, 2) }}</pre></details>
                </v-card-text>
              </template>
            </v-card>
            <div class="analysis-grid">
              <v-card v-for="(market, key) in detail.analysis" :key="key" variant="outlined" :title="key === 'spf' ? '胜平负 · 投票盈亏测算' : '让球胜平负 · 投票盈亏测算'">
                <v-table density="compact"><thead><tr><th>选项</th><th>配对赔率</th><th>支持率</th><th>概率</th><th>盈亏率</th><th>冷热差</th></tr></thead><tbody><tr v-for="row in market.rows" :key="row.outcome"><td>{{ row.outcome }}</td><td>{{ display(row.odds) }}</td><td>{{ percent(row.support) }}</td><td>{{ percent(row.probability) }}</td><td :class="row.profitRate !== null && row.profitRate < 0 ? 'text-error' : 'text-success'">{{ percent(row.profitRate) }}</td><td>{{ row.heatDifference === null ? '—' : `${row.heatDifference > 0 ? '+' : ''}${row.heatDifference.toFixed(2)} 个百分点` }}</td></tr></tbody></v-table>
                <v-card-text class="text-caption">投票页采集：{{ time(market.receivedAt) }}。盈亏率＝(1−支持率×配对赔率)×100%。冷热差＝支持率−页面概率。<div v-if="market.latestQuote">奖金表最新：{{ market.latestQuote.latest.join(' / ') }}（{{ market.latestQuote.updatedAt }}），与投票页可能不同步。</div></v-card-text>
              </v-card>
            </div>
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
              <v-table density="compact"><thead><tr><th>公司</th><th>初始（赔率／盘口）</th><th>即时（赔率／盘口）</th><th>最早记录凯利</th><th>即时凯利</th><th>返还率</th></tr></thead><tbody><tr v-for="(row, i) in quotes" :key="`${row.sourceLabel}-${i}`"><td>{{ row.company || row.sourceLabel }}</td><td>{{ row.opening.join(' / ') }}</td><td>{{ row.latest.join(' / ') }}</td><td>{{ row.openingKelly?.join(' / ') || '—' }}</td><td>{{ row.kelly.join(' / ') }}</td><td>{{ row.returnRatio }}</td></tr><tr v-if="!quotes.length"><td colspan="6" class="text-center pa-5">尚未取得此类报价</td></tr></tbody></v-table>
            </v-card>
            <v-expansion-panels class="my-4">
              <v-expansion-panel v-if="detail.parts.history" title="交锋、近期战绩与未来赛程"><v-expansion-panel-text><pre class="history-text">{{ detail.parts.history.text }}</pre></v-expansion-panel-text></v-expansion-panel>
              <v-expansion-panel title="比赛与公司映射"><v-expansion-panel-text>
                <p class="mb-3">体彩 ID：{{ detail.match.official_id }}。比赛使用独立标识，不与 VIPC 主键混用。</p>
                <div class="d-flex ga-3 align-center"><v-text-field v-model="mapping" label="足彩网比赛ID" hide-details density="compact" /><v-btn :loading="saving" @click="saveMapping">保存比赛映射</v-btn></div>
                <div class="d-flex ga-3 align-center mt-4"><v-text-field v-model="companyLabel" label="来源标签，如 36*" hide-details density="compact" /><v-text-field v-model="companyName" label="对应公司，如 Bet365" hide-details density="compact" /><v-btn :loading="saving" @click="saveCompany">保存公司映射</v-btn></div>
              </v-expansion-panel-text></v-expansion-panel>
              <v-expansion-panel title="数据来源与原始快照"><v-expansion-panel-text><v-table density="compact"><thead><tr><th>数据</th><th>采集时间（北京）</th><th>来源</th><th /></tr></thead><tbody><tr v-for="(item, kind) in detail.provenance" :key="kind"><td>{{ names[kind] || kind }}</td><td>{{ time(item.received_at) }}</td><td><a :href="item.url" target="_blank" rel="noopener noreferrer">{{ item.source }}</a></td><td><v-btn size="small" variant="text" @click="showRaw(item.id)">查看原文</v-btn></td></tr></tbody></v-table></v-expansion-panel-text></v-expansion-panel>
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
</style>
