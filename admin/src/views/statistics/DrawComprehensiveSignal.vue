<script setup lang="ts">
import { computed } from 'vue'
import type { DrawObservation } from '@/types/drawHistory'
import { comprehensiveAlternativePresentation, comprehensiveDrawPresentation, comprehensiveNumber as number } from '@/utils/drawComprehensive'

const props = defineProps<{ row: DrawObservation }>()
const signal = computed(() => props.row.comprehensiveSignal)
const primary = computed(() => comprehensiveDrawPresentation(props.row))
const alternative = computed(() => comprehensiveAlternativePresentation(props.row))
const paths = computed(() => [
  { key: 'draw', label: '选平依据', steps: signal.value?.drawPath || [], samples: signal.value?.drawLeafSamples },
  { key: 'alternative', label: '次选依据', steps: signal.value?.alternativePath || [], samples: signal.value?.alternativeLeafSamples },
])
const note = computed(() => [
  '全量数据新树独立对照：选平与胜负次选分别由决策树给出。',
  '评分为树叶内训练频率，未经概率校准。',
  primary.value.reason, alternative.value.reason,
].filter(Boolean).join('\n'))
</script>

<template>
  <div class="draw-comprehensive" :title="note" :data-comprehensive-state="primary.state" :data-comprehensive-version="signal?.modelVersion || ''">
    <div class="text-caption text-medium-emphasis mb-1">实验对照</div>
    <v-chip size="small" :color="primary.color || undefined" :variant="primary.color ? 'tonal' : 'text'">{{ primary.label }}</v-chip>
    <div v-if="signal && signal.score !== undefined" class="text-caption text-medium-emphasis mt-1">树评分 {{ number(signal.score) }} / 门槛 {{ number(signal.threshold) }}</div>
    <div v-if="primary.state !== 'selected' && primary.reason" class="text-caption text-medium-emphasis mt-1 comprehensive-reason">{{ primary.reason }}</div>
    <div v-if="primary.state === 'selected'" class="mt-2" :data-comprehensive-alternative="alternative.state" :data-comprehensive-direction="alternative.direction || ''">
      <v-chip v-if="alternative.state === 'recommended'" size="small" :color="alternative.color" variant="tonal">{{ alternative.label }}</v-chip>
      <div v-else class="text-caption text-medium-emphasis">次选暂无</div>
      <div v-if="alternative.state !== 'recommended' && alternative.reason" class="text-caption text-medium-emphasis mt-1 comprehensive-reason">{{ alternative.reason }}</div>
      <div v-if="alternative.outcomeLabel" class="text-caption mt-1" :class="alternative.outcomeColor ? `text-${alternative.outcomeColor}` : 'text-medium-emphasis'">{{ alternative.outcomeLabel }}</div>
    </div>

    <details v-if="signal" class="comprehensive-details text-caption mt-2" @click.stop @keydown.stop>
      <summary>查看新树依据</summary>
      <div class="text-medium-emphasis mt-2">{{ signal.modelVersion }} · {{ signal.inputVersion }}</div>
      <div class="text-medium-emphasis">有效输入 {{ number(signal.observedInputs, 0) }} / {{ number(signal.totalInputs, 0) }}</div>
      <div class="text-medium-emphasis">评分是训练叶内频率，未经概率校准。</div>
      <section v-for="path in paths" :key="path.key" class="mt-2" :data-comprehensive-path="path.key">
        <strong>{{ path.label }}</strong><span class="text-medium-emphasis ml-2">叶内训练 {{ number(path.samples, 0) }} 场</span>
        <ol v-if="path.steps.length" class="rule-path mt-1">
          <li v-for="(step, index) in path.steps" :key="`${step.feature}-${index}`" :title="step.feature">
            <span>{{ step.label || step.feature }}：</span><span v-if="step.imputed" class="text-warning">缺失，填补 {{ number(step.usedValue) }}</span><span v-else>{{ number(step.value) }}</span>
            {{ step.wentLeft ? '≤' : '>' }} {{ number(step.threshold) }}
          </li>
        </ol>
        <div v-else class="text-medium-emphasis">暂无分支明细</div>
      </section>
      <div v-if="signal.alternativeScores" class="mt-2">胜负树评分：主 {{ number(signal.alternativeScores.home) }} / 客 {{ number(signal.alternativeScores.away) }}</div>
      <section v-if="signal.moduleCoverage?.length" class="mt-2">
        <strong>各模块输入</strong>
        <div class="text-medium-emphasis">有值 / 登记；引用数为两棵树全局使用数。</div>
        <div v-for="module in signal.moduleCoverage" :key="module.key" class="module-coverage">
          <span>{{ module.label || module.key }}</span><span>{{ number(module.available, 0) }} / {{ number(module.total, 0) }} · 引用 {{ number(module.used, 0) }}</span>
        </div>
      </section>
    </details>
  </div>
</template>

<style scoped>
.draw-comprehensive { min-width: 220px; max-width: 280px; white-space: normal; }
.comprehensive-reason { max-width: 270px; white-space: normal; }
.comprehensive-details { min-width: 220px; max-width: 340px; }
.comprehensive-details summary { cursor: pointer; color: rgb(var(--v-theme-primary)); }
.rule-path { padding-left: 18px; white-space: normal; }
.rule-path li + li { margin-top: 4px; }
.module-coverage { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 3px 12px; }
</style>
