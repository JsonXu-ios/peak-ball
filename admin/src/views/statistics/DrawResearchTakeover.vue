<script setup lang="ts">
import { computed } from 'vue'
import type { DrawResearchTakeover } from '@/types/drawHistory'
import { researchTakeoverPresentation } from '@/utils/drawResearchTakeover'

const props = defineProps<{ status?: DrawResearchTakeover | null }>()
const presentation = computed(() => researchTakeoverPresentation(props.status))
</script>

<template>
  <v-alert :type="presentation.color" variant="tonal" density="compact" class="mb-4" :data-research-takeover="presentation.state">
    <strong>{{ presentation.label }}</strong><span class="ml-2">{{ presentation.detail }}</span>
    <div class="text-caption mt-1">接管来源只说明本页预测从哪里读取，原模型及二次筛选仍按原版本核对；比分推荐单独展示。</div>
  </v-alert>
</template>
