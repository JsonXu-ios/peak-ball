<script setup lang="ts">
import { computed } from 'vue'
import type { ResearchEvaluation } from '@/types/researchStore'
import { isResearchScoreRankMetrics, researchEvaluationBasis, researchRate, researchScoreMethod } from '@/utils/researchEvaluation'
import { formatBeijingDateTime } from '@/utils/date'

const props = defineProps<{ evaluation: ResearchEvaluation }>()
const basis = computed(() => researchEvaluationBasis(props.evaluation))
const scoreMethod = computed(() => researchScoreMethod(props.evaluation))
const scoreMetricsKnown = computed(() => isResearchScoreRankMetrics(props.evaluation))
const names = { original: '原模型', secondary: '二次保留', enhanced: '二次增强' } as const
const groups = computed(() => (['original', 'secondary', 'enhanced'] as const).flatMap((key) => {
  const group = props.evaluation.metrics.groups[key]
  return group ? [{ key, label: names[key], ...group }] : []
}))
</script>

<template>
  <v-card variant="outlined" class="pa-4 research-evaluation" :data-evaluation-id="evaluation.id" :data-evaluation-scope="evaluation.scope">
    <div class="d-flex flex-wrap align-center ga-2 mb-2"><v-chip :color="basis.color" size="small">{{ basis.label }}</v-chip><span class="text-caption text-medium-emphasis">{{ formatBeijingDateTime(evaluation.createdAt, '暂无') }}</span></div>
    <div class="text-caption text-medium-emphasis mb-3">{{ basis.note }}</div>
    <div class="text-caption mb-3" :class="scoreMethod.kind === 'current-dynamic' ? 'text-primary' : 'text-medium-emphasis'" :data-score-method="scoreMethod.kind"><strong>{{ scoreMethod.label }}</strong> · {{ scoreMethod.note }}</div>
    <v-table density="compact"><thead><tr><th>组别</th><th>已完赛样本 / 平局</th><th>入选 / 命中平局</th><th>选平命中率</th><th>平局覆盖率</th><th>比分候选 / 首选命中</th><th>首选命中率</th><th>比分候选 / 前二命中</th><th>前二命中率</th></tr></thead><tbody><tr v-for="group in groups" :key="group.key"><td>{{ group.label }}</td><td>{{ group.eligible }} / {{ group.draws }}</td><td>{{ group.selected }} / {{ group.hits }}</td><td>{{ researchRate(group.precision) }}</td><td>{{ researchRate(group.recall) }}</td><td>{{ scoreMetricsKnown ? group.scoreCandidates : '暂无' }} / {{ scoreMetricsKnown ? group.firstScoreHits ?? '暂无' : '暂无' }}</td><td>{{ researchRate(scoreMetricsKnown ? group.firstScoreHitRate : null) }}</td><td>{{ scoreMetricsKnown ? group.scoreCandidates : '暂无' }} / {{ scoreMetricsKnown ? group.scoreHits : '暂无' }}</td><td>{{ researchRate(scoreMetricsKnown ? group.scoreHitRate : null) }}</td></tr><tr v-if="!groups.length"><td colspan="9" class="text-medium-emphasis py-4">暂无分组评估结果。</td></tr></tbody></v-table>
    <div v-if="evaluation.metrics.pending !== undefined || evaluation.metrics.settled !== undefined" class="text-caption text-medium-emphasis mt-3">已确认完赛 {{ evaluation.metrics.settled ?? '暂无' }} 场 · 待确认或未完赛 {{ evaluation.metrics.pending ?? '暂无' }} 场；上表命中率只使用已确认的常规时间赛果。</div>
    <div class="text-caption text-medium-emphasis mt-2">首选命中率只核对第一推荐；前二命中率核对两个推荐之一，均以已完赛比分候选场次为分母。旧版本未保存首选统计时显示“暂无”。版本 {{ evaluation.version }} · {{ evaluation.scope }}</div>
  </v-card>
</template>

<style scoped>
.research-evaluation :deep(th), .research-evaluation :deep(td) { white-space: nowrap; }
</style>
