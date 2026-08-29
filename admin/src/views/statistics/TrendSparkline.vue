<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    /** 按时间升序的数值序列（进球或丢球） */
    values: number[]
    /** scored=进球（红），conceded=丢球（蓝），total=进+丢合计（橙） */
    kind?: 'scored' | 'conceded' | 'total'
    /** 期望场次，不足时右侧留空 */
    want?: number
    width?: number
    height?: number
    /** 是否在点上标数字 */
    labels?: boolean
  }>(),
  { kind: 'scored', want: 5, width: 140, height: 72, labels: true },
)

const PAD_LEFT = 22
const PAD_RIGHT = 8
const PAD_TOP = 14
const PAD_BOTTOM = 8

const slots = computed(() => Math.max(props.want, props.values.length, 2))
// 纵轴默认 0~4 共 5 个刻度，数据超出 4 时按最大值扩展
const maxTick = computed(() => Math.max(4, Math.ceil(Math.max(0, ...props.values))))
const ticks = computed(() => Array.from({ length: maxTick.value + 1 }, (_, i) => i))

function x(index: number) {
  const span = props.width - PAD_LEFT - PAD_RIGHT
  return PAD_LEFT + (span * index) / (slots.value - 1)
}

function y(value: number) {
  const span = props.height - PAD_TOP - PAD_BOTTOM
  return PAD_TOP + span - (span * value) / maxTick.value
}

const linePath = computed(() =>
  props.values.map((v, i) => `${i === 0 ? 'M' : 'L'}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join(' '),
)
</script>

<template>
  <svg :width="width" :height="height" :viewBox="`0 0 ${width} ${height}`" class="trend-sparkline">
    <template v-for="tick in ticks" :key="`t${tick}`">
      <line :x1="PAD_LEFT" :x2="width - PAD_RIGHT" :y1="y(tick)" :y2="y(tick)" class="grid" />
      <text :x="PAD_LEFT - 5" :y="y(tick) + 3" class="tick" text-anchor="end">{{ tick }}</text>
    </template>
    <line
      v-for="i in slots"
      :key="`g${i}`"
      :x1="x(i - 1)"
      :x2="x(i - 1)"
      :y1="PAD_TOP"
      :y2="height - PAD_BOTTOM"
      class="grid vertical"
    />
    <path v-if="values.length" :d="linePath" class="line" :class="kind" />
    <template v-for="(v, i) in values" :key="`d${i}`">
      <circle :cx="x(i)" :cy="y(v)" r="3.2" class="dot" :class="kind" />
      <text v-if="labels" :x="x(i)" :y="y(v) - 6" class="label" :class="kind" text-anchor="middle">{{ v }}</text>
    </template>
  </svg>
</template>

<style scoped>
.trend-sparkline {
  display: block;
  overflow: visible;
}
.grid {
  stroke: rgba(var(--v-theme-on-surface), 0.1);
  stroke-width: 1;
}
.grid.vertical {
  stroke: rgba(var(--v-theme-on-surface), 0.05);
}
.tick {
  font-size: 9px;
  fill: rgba(var(--v-theme-on-surface), 0.45);
}
.line {
  fill: none;
  stroke-width: 1.6;
  stroke-linejoin: round;
  stroke-linecap: round;
}
.line.scored,
.dot.scored {
  stroke: rgb(var(--v-theme-error));
}
.dot.scored {
  fill: rgb(var(--v-theme-error));
}
.line.conceded,
.dot.conceded {
  stroke: rgb(var(--v-theme-info));
}
.dot.conceded {
  fill: rgb(var(--v-theme-surface));
  stroke-width: 1.4;
}
.line.total,
.dot.total {
  stroke: rgb(var(--v-theme-warning));
}
.dot.total {
  fill: rgb(var(--v-theme-warning));
}
.label {
  font-size: 11px;
  font-weight: 600;
}
.label.scored {
  fill: rgb(var(--v-theme-error));
}
.label.conceded {
  fill: rgb(var(--v-theme-info));
}
.label.total {
  fill: rgb(var(--v-theme-warning));
}
</style>
