<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    /** 按时间升序的进球序列 */
    scored: number[]
    /** 按时间升序的丢球序列 */
    conceded: number[]
    /** 期望场次，不足时右侧留空 */
    want?: number
    width?: number
    height?: number
    /** 是否在点上标数字 */
    labels?: boolean
  }>(),
  { want: 5, width: 140, height: 44, labels: true },
)

const PAD_X = 8
const PAD_TOP = 9
const PAD_BOTTOM = 6

const slots = computed(() => Math.max(props.want, props.scored.length, 2))
const maxValue = computed(() => Math.max(3, ...props.scored, ...props.conceded))

function x(index: number) {
  const span = props.width - PAD_X * 2
  return PAD_X + (span * index) / (slots.value - 1)
}

function y(value: number) {
  const span = props.height - PAD_TOP - PAD_BOTTOM
  return PAD_TOP + span - (span * value) / maxValue.value
}

function path(values: number[]) {
  return values.map((v, i) => `${i === 0 ? 'M' : 'L'}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join(' ')
}

const scoredPath = computed(() => path(props.scored))
const concededPath = computed(() => path(props.conceded))
</script>

<template>
  <svg :width="width" :height="height" :viewBox="`0 0 ${width} ${height}`" class="trend-sparkline">
    <line
      v-for="i in slots"
      :key="`g${i}`"
      :x1="x(i - 1)"
      :x2="x(i - 1)"
      :y1="PAD_TOP"
      :y2="height - PAD_BOTTOM"
      class="grid"
    />
    <path v-if="conceded.length" :d="concededPath" class="line conceded" />
    <path v-if="scored.length" :d="scoredPath" class="line scored" />
    <template v-for="(v, i) in conceded" :key="`c${i}`">
      <circle :cx="x(i)" :cy="y(v)" r="2.2" class="dot conceded" />
    </template>
    <template v-for="(v, i) in scored" :key="`s${i}`">
      <circle :cx="x(i)" :cy="y(v)" r="2.4" class="dot scored" />
      <text v-if="labels" :x="x(i)" :y="y(v) - 4" class="label scored" text-anchor="middle">{{ v }}</text>
    </template>
    <template v-if="labels">
      <text
        v-for="(v, i) in conceded"
        :key="`cl${i}`"
        :x="x(i)"
        :y="y(v) + 9"
        class="label conceded"
        text-anchor="middle"
      >
        {{ v }}
      </text>
    </template>
  </svg>
</template>

<style scoped>
.trend-sparkline {
  display: block;
  overflow: visible;
}
.grid {
  stroke: rgba(var(--v-theme-on-surface), 0.08);
  stroke-width: 1;
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
.label {
  font-size: 8px;
  font-weight: 600;
}
.label.scored {
  fill: rgb(var(--v-theme-error));
}
.label.conceded {
  fill: rgb(var(--v-theme-info));
}
</style>
