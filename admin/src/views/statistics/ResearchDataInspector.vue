<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { getResearchAuditIssues, getResearchEvaluations, getResearchFeatures } from '@/api/researchStore'
import type { ResearchAuditIssue, ResearchAuditPage, ResearchEvaluation, ResearchFeatureDefinition, ResearchPage } from '@/types/researchStore'
import ResearchEvaluationTable from './ResearchEvaluationTable.vue'
import ResearchFeatureTable from './ResearchFeatureTable.vue'
import ResearchAuditTable from './ResearchAuditTable.vue'

const props = defineProps<{ featureSets: Array<{ version: string; features: number }>; revision: string }>()
const panels = ref<string[]>([])
const featureVersion = ref('')
const featureSearch = ref('')
const featurePage = ref(1)
const features = ref<ResearchPage<ResearchFeatureDefinition> | null>(null)
const featureLoading = ref(false)
const featureError = ref('')
const severity = ref('')
const auditPage = ref(1)
const audits = ref<ResearchAuditPage | null>(null)
const auditLoading = ref(false)
const auditError = ref('')
const evaluations = ref<ResearchEvaluation[] | null>(null)
const evaluationLoading = ref(false)
const evaluationError = ref('')
const detail = ref<{ title: string; value: unknown } | null>(null)
const pageSize = 25
let disposed = false
let featureRequest = 0
let auditRequest = 0
let evaluationRequest = 0
const versionOptions = computed(() => [{ title: '全部特征版本', value: '' }, ...props.featureSets.map((set) => ({ title: `${set.version}（${set.features}项）`, value: set.version }))])
const severityOptions = [{ title: '全部等级', value: '' }, { title: '错误', value: 'error' }, { title: '警告', value: 'warning' }, { title: '提示', value: 'info' }]
const featurePages = computed(() => Math.max(1, Math.ceil((features.value?.total || 0) / pageSize)))
const auditPages = computed(() => Math.max(1, Math.ceil((audits.value?.total || 0) / pageSize)))
const detailText = computed(() => detail.value ? JSON.stringify(detail.value.value, null, 2) : '')

function errorText(cause: unknown) {
  const response = cause as { response?: { data?: { error?: string; message?: string }; status?: number }; message?: string }
  return response.response?.data?.error || response.response?.data?.message
    || (response.response?.status === 404 ? '当前服务尚未加载此查询接口，请在加载更新后重试。' : response.message || '读取失败，请重试。')
}
async function loadFeatures(reset = false) {
  if (reset) featurePage.value = 1
  const current = ++featureRequest
  featureLoading.value = true
  featureError.value = ''
  try {
    const response = await getResearchFeatures({ version: featureVersion.value || undefined, search: featureSearch.value.trim() || undefined, page: featurePage.value, page_size: pageSize })
    if (disposed || current !== featureRequest) return
    features.value = response.data
  } catch (cause) { if (!disposed && current === featureRequest) featureError.value = errorText(cause) }
  finally { if (!disposed && current === featureRequest) featureLoading.value = false }
}
async function loadAudits(reset = false) {
  if (reset) auditPage.value = 1
  const current = ++auditRequest
  auditLoading.value = true
  auditError.value = ''
  try {
    const response = await getResearchAuditIssues({ severity: severity.value || undefined, page: auditPage.value, page_size: pageSize })
    if (disposed || current !== auditRequest) return
    audits.value = response.data
  } catch (cause) { if (!disposed && current === auditRequest) auditError.value = errorText(cause) }
  finally { if (!disposed && current === auditRequest) auditLoading.value = false }
}
async function loadEvaluations() {
  const current = ++evaluationRequest
  evaluationLoading.value = true
  evaluationError.value = ''
  try {
    const response = await getResearchEvaluations()
    if (disposed || current !== evaluationRequest) return
    evaluations.value = response.data.items
  } catch (cause) { if (!disposed && current === evaluationRequest) evaluationError.value = errorText(cause) }
  finally { if (!disposed && current === evaluationRequest) evaluationLoading.value = false }
}
function openFeature(row: ResearchFeatureDefinition) { detail.value = { title: `${row.name} · ${row.version}`, value: row } }
function openAudit(row: ResearchAuditIssue) { detail.value = { title: `审计问题 #${row.id} · ${row.code}`, value: row } }

// Expanded panels fetch independently; the structure overview does not wait.
watch(panels, (values) => {
  if (values.includes('features') && !features.value && !featureLoading.value) void loadFeatures()
  if (values.includes('audits') && !audits.value && !auditLoading.value) void loadAudits()
  if (values.includes('evaluations') && !evaluations.value && !evaluationLoading.value) void loadEvaluations()
})
watch(() => props.revision, (value, old) => {
  if (value === old) return
  featureRequest++
  auditRequest++
  evaluationRequest++
  featureLoading.value = false
  auditLoading.value = false
  evaluationLoading.value = false
  features.value = null
  audits.value = null
  evaluations.value = null
  if (panels.value.includes('features')) void loadFeatures(true)
  if (panels.value.includes('audits')) void loadAudits(true)
  if (panels.value.includes('evaluations')) void loadEvaluations()
})
onBeforeUnmount(() => { disposed = true; featureRequest++; auditRequest++; evaluationRequest++ })
</script>

<template>
  <section class="research-data-inspector mb-5">
    <div class="mb-3"><h2 class="text-h6 font-weight-bold">特征、清洗与分析记录</h2><div class="text-caption text-medium-emphasis">按需展开查询，每页25条；各区域独立加载，不影响结构概览。</div></div>
    <v-expansion-panels v-model="panels" multiple variant="accordion">
      <v-expansion-panel value="features">
        <v-expansion-panel-title><div><span class="font-weight-bold">特征字典</span><span class="text-caption text-medium-emphasis ml-3">公式、来源、单位、缺失策略与版本</span></div></v-expansion-panel-title>
        <v-expansion-panel-text>
          <div class="d-flex flex-wrap ga-3 align-center mb-3"><v-select v-model="featureVersion" :items="versionOptions" label="特征版本" density="compact" variant="outlined" hide-details class="dictionary-version" :disabled="featureLoading" @update:model-value="loadFeatures(true)" /><v-text-field v-model="featureSearch" label="查找特征名或说明" prepend-inner-icon="mdi-magnify" density="compact" variant="outlined" hide-details class="dictionary-search" @keyup.enter="loadFeatures(true)" /><v-btn color="primary" :loading="featureLoading" @click="loadFeatures(true)">查询</v-btn></div>
          <v-alert v-if="featureError" type="error" variant="tonal" class="mb-3">{{ featureError }}</v-alert>
          <v-progress-linear v-if="featureLoading" indeterminate color="primary" />
          <ResearchFeatureTable :items="features?.items || []" :loading="featureLoading" :error="featureError" @detail="openFeature" />
          <div v-if="features" class="d-flex flex-wrap justify-space-between align-center mt-3"><span class="text-caption text-medium-emphasis">共 {{ features.total.toLocaleString('zh-CN') }} 项</span><v-pagination v-if="featurePages > 1" v-model="featurePage" :length="featurePages" :total-visible="5" density="compact" :disabled="featureLoading" @update:model-value="loadFeatures()" /></div>
        </v-expansion-panel-text>
      </v-expansion-panel>

      <v-expansion-panel value="audits">
        <v-expansion-panel-title><div><span class="font-weight-bold">清洗问题</span><span class="text-caption text-medium-emphasis ml-3">最新完整同步批次的异常与处理依据</span></div></v-expansion-panel-title>
        <v-expansion-panel-text>
          <div class="d-flex flex-wrap ga-3 align-center mb-3"><v-select v-model="severity" :items="severityOptions" label="问题等级" density="compact" variant="outlined" hide-details class="severity-filter" :disabled="auditLoading" @update:model-value="loadAudits(true)" /><v-btn variant="outlined" prepend-icon="mdi-refresh" :loading="auditLoading" @click="loadAudits(true)">刷新问题</v-btn><span v-if="audits" class="text-caption text-medium-emphasis">批次 {{ audits.runId }} · 本条件共 {{ audits.total.toLocaleString('zh-CN') }} 条</span></div>
          <div class="text-caption text-medium-emphasis mb-3">此处仅查询最新完整同步批次，与概览的累计问题总数不同。展开“详情”可查看比赛、字段和原始审计依据。</div>
          <v-alert v-if="auditError" type="error" variant="tonal" class="mb-3">{{ auditError }}</v-alert><v-progress-linear v-if="auditLoading" indeterminate color="primary" />
          <ResearchAuditTable :items="audits?.items || []" :loading="auditLoading" :error="auditError" @detail="openAudit" />
          <v-pagination v-if="auditPages > 1" v-model="auditPage" :length="auditPages" :total-visible="5" density="compact" :disabled="auditLoading" class="mt-3" @update:model-value="loadAudits()" />
        </v-expansion-panel-text>
      </v-expansion-panel>

      <v-expansion-panel value="evaluations">
        <v-expansion-panel-title><div><span class="font-weight-bold">评估记录</span><span class="text-caption text-medium-emphasis ml-3">每种口径的最新版本结果，区分历史与赛前验证</span></div></v-expansion-panel-title>
        <v-expansion-panel-text>
          <div class="d-flex justify-end mb-3"><v-btn variant="outlined" prepend-icon="mdi-refresh" :loading="evaluationLoading" @click="loadEvaluations">刷新评估</v-btn></div>
          <v-alert v-if="evaluationError" type="error" variant="tonal" class="mb-3">{{ evaluationError }}</v-alert><v-progress-linear v-if="evaluationLoading" indeterminate color="primary" class="mb-3" />
          <ResearchEvaluationTable v-for="evaluation in evaluations || []" :key="evaluation.id" :evaluation="evaluation" class="mb-3" />
          <div v-if="!evaluations?.length" class="text-center text-medium-emphasis py-6">{{ evaluationLoading ? '正在读取评估记录…' : evaluationError ? '查询失败，请重试。' : '尚无评估记录，请先完成同步；没有完赛样本时命中率显示暂无。' }}</div>
        </v-expansion-panel-text>
      </v-expansion-panel>
    </v-expansion-panels>
    <v-dialog :model-value="!!detail" max-width="900" @update:model-value="(value) => { if (!value) detail = null }"><v-card v-if="detail"><v-card-title class="detail-title">{{ detail.title }}</v-card-title><v-card-text><pre class="detail-json">{{ detailText }}</pre></v-card-text><v-card-actions><v-spacer /><v-btn @click="detail = null">关闭</v-btn></v-card-actions></v-card></v-dialog>
  </section>
</template>

<style scoped>
.dictionary-version { flex: 0 1 330px; min-width: 220px; }
.dictionary-search { flex: 1 1 260px; min-width: 190px; }
.severity-filter { max-width: 200px; }
.detail-title { white-space: normal; overflow-wrap: anywhere; }
.detail-json { font-size: 12px; line-height: 1.65; white-space: pre-wrap; overflow-wrap: anywhere; }
</style>
