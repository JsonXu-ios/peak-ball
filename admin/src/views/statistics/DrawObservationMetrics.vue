<script setup lang="ts">
import { computed } from 'vue'
import type { DrawObservationMetrics } from '@/types/drawHistory'
import { DRAW_PROFIT_SUPPORT_VERSION } from '@/utils/drawProfitSupport'
import { SECONDARY_DRAW_VERSION } from '@/utils/drawSecondarySignal'

const props = defineProps<{ metrics?: DrawObservationMetrics | null; date: string; queried: boolean }>()
const currentVersion = computed(() => props.metrics?.secondaryVersion === SECONDARY_DRAW_VERSION)
const groups = computed(() => props.metrics ? [
  { key: 'original', label: '原固定模型', value: props.metrics.original },
  { key: 'secondary', label: '二次保留', value: props.metrics.secondary },
] : [])
const profitSupport = computed(() => props.metrics?.profitSupport?.version === DRAW_PROFIT_SUPPORT_VERSION ? props.metrics.profitSupport : null)
const profitGroups = computed(() => profitSupport.value ? [
  { key: 'comparable', label: '二次可比基准', value: profitSupport.value.secondaryComparable },
  { key: 'supported', label: '其中：盈亏分歧增强', value: profitSupport.value.secondarySupported },
  { key: 'not-supported', label: '其中：有共同方向', value: profitSupport.value.secondaryNotSupported },
] : [])

function count(value: number | undefined) {
  return typeof value === 'number' && Number.isFinite(value) ? value.toLocaleString('zh-CN') : '—'
}

function percentage(value: number | null | undefined) {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 && value <= 1
    ? `${(value * 100).toFixed(2)}%` : '—'
}
</script>

<template>
  <div class="observation-metrics mt-3" :data-metrics-status="metrics?.status || 'missing'">
    <div class="text-subtitle-2">当期命中率与平局覆盖率 <span class="text-caption text-medium-emphasis">{{ date }}</span></div>
    <template v-if="metrics && currentVersion">
      <div class="text-caption text-medium-emphasis mt-1">
        统计所选日期、赛事范围、联赛和搜索条件内的全部比赛，不受标签页、次数条件及分页影响。模型仅适用竞彩。
      </div>
      <div class="d-flex flex-wrap ga-3 text-caption mt-2">
        <span>范围内 {{ count(metrics.scopeMatches) }} 场</span>
        <span>其中竞彩 {{ count(metrics.eligibleMatches) }} 场</span>
        <span>竞彩确认完赛 {{ count(metrics.eligibleSettled) }} 场</span>
        <span>竞彩实际平局 {{ count(metrics.eligibleDraws) }} 场</span>
        <span v-if="metrics.outOfScope">非竞彩 {{ count(metrics.outOfScope) }} 场不参与模型统计</span>
      </div>
      <v-alert v-if="metrics.status === 'incomplete'" type="warning" variant="tonal" density="compact" class="mt-3 mb-2">
        当前统计不完整：{{ count(metrics.modelUnavailable) }} 场竞彩缺少可用模型结果<span v-if="metrics.secondaryUnavailable">，另有 {{ count(metrics.secondaryUnavailable) }} 场原入选比赛缺少有效二次筛选结果</span>。缺失比赛未当作未入选，竞彩实际平局仍计入覆盖率分母；下列指标仅反映已有模型结果。
      </v-alert>
      <v-table density="compact" class="metrics-table mt-2">
        <thead><tr><th>观察规则</th><th>入选</th><th>确认完赛</th><th>待赛果</th><th>命中平局</th><th>命中率</th><th>平局覆盖率</th></tr></thead>
        <tbody>
          <tr v-for="group in groups" :key="group.key" :data-metric-group="group.key">
            <td>{{ group.label }}</td>
            <td>{{ count(group.value.selected) }}</td>
            <td>{{ count(group.value.settled) }}</td>
            <td>{{ count(group.value.pending) }}</td>
            <td>{{ count(group.value.hits) }}</td>
            <td :title="`${group.value.hits} / ${group.value.settled} 场确认完赛入选比赛`">{{ percentage(metrics.modelAvailable > 0 ? group.value.precision : null) }}</td>
            <td :title="`${group.value.hits} / ${metrics.eligibleDraws} 场竞彩实际平局`">{{ percentage(metrics.modelAvailable > 0 ? group.value.coverage : null) }}</td>
          </tr>
        </tbody>
      </v-table>
      <div class="d-flex flex-wrap ga-3 text-caption mt-2">
        <span>二次剔除 {{ count(metrics.rejected.selected) }} 场</span>
        <span>确认剔除正确 {{ count(metrics.rejected.misses) }} 场</span>
        <span class="text-warning">误剔平局 {{ count(metrics.rejected.hits) }} 场</span>
        <span>剔除后待赛果 {{ count(metrics.rejected.pending) }} 场</span>
      </div>
      <div class="text-caption text-medium-emphasis mt-2">
        命中率＝命中平局 ÷ 入选且确认常规时间完赛的比赛；平局覆盖率＝命中平局 ÷ 范围内全部竞彩实际平局。未完赛不计命中率，分母为0时显示“—”。
      </div>
      <div class="profit-support-metrics mt-4" :data-profit-support-metrics-state="profitSupport?.status || 'missing'">
        <div class="text-subtitle-2">盈亏分歧增强观察</div>
        <template v-if="profitSupport">
          <div class="text-caption text-medium-emphasis mt-1">竞彩源站盈亏率与非让球模拟ROI各取最低方向，并列保留；两组没有交集时加增强标签。以下仅对二次保留比赛分组，不改变原候选池。</div>
          <div v-if="profitSupport.status === 'incomplete'" class="text-caption text-warning mt-2" role="status">二次候选有 {{ count(profitSupport.secondaryUnavailable.selected) }} 场缺少完整盈亏数据，待判；未当作无分歧。</div>
          <v-table density="compact" class="metrics-table mt-2">
            <thead><tr><th>观察分组</th><th>场次</th><th>确认完赛</th><th>待赛果</th><th>命中平局</th><th>分组命中率</th><th>平局覆盖率</th></tr></thead>
            <tbody>
              <tr v-for="group in profitGroups" :key="group.key" :data-profit-support-metric-group="group.key">
                <td>{{ group.label }}</td>
                <td>{{ count(group.value.selected) }}</td>
                <td>{{ count(group.value.settled) }}</td>
                <td>{{ count(group.value.pending) }}</td>
                <td>{{ count(group.value.hits) }}</td>
                <td :title="`${group.value.hits} / ${group.value.settled} 场分组确认完赛比赛`">{{ percentage(profitSupport.secondaryComparable.selected > 0 ? group.value.precision : null) }}</td>
                <td :title="`${group.value.hits} / ${metrics.eligibleDraws} 场竞彩实际平局`">{{ percentage(profitSupport.secondaryComparable.selected > 0 ? group.value.coverage : null) }}</td>
              </tr>
            </tbody>
          </v-table>
          <div class="text-caption text-medium-emphasis mt-2">分歧组与有共同方向组相加为可比基准；加上待判组才是全部二次候选。增强组命中率只代表该分组，不代表整个候选池；最低盈亏是各自假设下的数值最低项，不代表已知庄家意图。</div>
        </template>
        <div v-else class="text-caption text-warning mt-2" role="status">增强分组统计待后端更新，当前接口尚未提供此版本的完整窗口指标。</div>
      </div>
    </template>
    <div v-else-if="queried" class="text-caption text-warning mt-2" role="status">统计待后端更新，当前接口尚未提供完整窗口指标。</div>
    <div v-else class="text-caption text-medium-emphasis mt-2">查询后显示所选窗口的完整统计。</div>
  </div>
</template>

<style scoped>
.metrics-table :deep(th), .metrics-table :deep(td) { white-space: nowrap; }
</style>
