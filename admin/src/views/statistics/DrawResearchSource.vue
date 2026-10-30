<script setup lang="ts">
import { computed } from 'vue'
import type { DrawObservation } from '@/types/drawHistory'
import { researchDecisionPresentation } from '@/utils/drawResearchTakeover'
import { formatBeijingDateTime } from '@/utils/date'

const props = defineProps<{ row: Pick<DrawObservation, 'researchDecision'> }>()
const presentation = computed(() => researchDecisionPresentation(props.row.researchDecision))
const note = computed(() => [presentation.value.detail,
  props.row.researchDecision?.asOf ? `输入截止：${formatBeijingDateTime(props.row.researchDecision.asOf, '时间待核对')}` : '',
  props.row.researchDecision?.version,
].filter(Boolean).join('\n'))
</script>

<template>
  <div class="mt-1 research-source" :data-research-source="presentation.state" :title="note">
    <v-chip size="x-small" variant="text" :color="presentation.color">{{ presentation.label }}</v-chip>
  </div>
</template>
