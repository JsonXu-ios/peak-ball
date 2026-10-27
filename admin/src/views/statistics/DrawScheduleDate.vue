<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { zhHans } from 'vuetify/locale'
import { getDrawObservationDates } from '@/api/drawHistory'
import { BEIJING_TIME_ZONE, beijingToday } from '@/utils/date'
import { parseScheduleCalendarDate, scheduleCalendarDate } from '@/utils/drawCalendar'

defineOptions({ inheritAttrs: false })
const props = defineProps<{
  modelValue: string
  mode: 'today' | 'history'
  scope: 'all' | 'sporttery'
  revision?: number
}>()
const emit = defineEmits<{ 'update:modelValue': [value: string]; change: [] }>()
const open = ref(false)
const initial = parseScheduleCalendarDate(props.modelValue) || parseScheduleCalendarDate(beijingToday())!
const calendarYear = ref(initial.getFullYear())
const calendarMonth = ref(initial.getMonth())
const selectedDate = computed(() => parseScheduleCalendarDate(props.modelValue))
const month = computed(() => `${calendarYear.value}-${String(calendarMonth.value + 1).padStart(2, '0')}`)
const counts = ref<Record<string, number>>({})
const loading = ref(false)
const error = ref('')
const markerLabel = computed(() => props.mode === 'today' ? '有比赛' : '有已完赛平局')
let request = 0

function setOpen(value: boolean) {
  if (value) {
    const selected = parseScheduleCalendarDate(props.modelValue || beijingToday())
    if (selected) {
      calendarYear.value = selected.getFullYear()
      calendarMonth.value = selected.getMonth()
    }
  }
  open.value = value
}

function choose(value: unknown) {
  const date = scheduleCalendarDate(value)
  if (!date) return
  open.value = false
  if (date !== props.modelValue) {
    emit('update:modelValue', date)
    emit('change')
  }
}

function dayTitle(date: string) {
  const value = counts.value[date]
  if (value) return `${date} · ${value} 场${props.mode === 'today' ? '比赛' : '已完赛平局'}`
  return `${date}${loading.value ? ' · 正在读取比赛标记' : error.value ? ' · 比赛标记读取失败' : ` · 暂无${props.mode === 'today' ? '比赛' : '已完赛平局'}`}`
}

async function loadDates() {
  const pending = ++request
  counts.value = {}
  error.value = ''
  if (!open.value) { loading.value = false; return }
  const submitted = { month: month.value, mode: props.mode, scope: props.scope }
  loading.value = true
  try {
    const { data } = await getDrawObservationDates(submitted)
    if (pending !== request) return
    if (data.month !== submitted.month || data.mode !== submitted.mode || data.scope !== submitted.scope || data.scheduleTimezone !== BEIJING_TIME_ZONE || !Array.isArray(data.dates)) {
      throw new Error('日期标记接口尚未更新，比赛查询仍可使用')
    }
    const next: Record<string, number> = {}
    for (const item of data.dates) {
      if (!parseScheduleCalendarDate(item.date) || !item.date.startsWith(`${submitted.month}-`) || !Number.isInteger(item.count) || item.count < 1) {
        throw new Error('日期标记数据不符合所选月份')
      }
      next[item.date] = item.count
    }
    counts.value = next
  } catch (err) {
    if (pending !== request) return
    const e = err as { response?: { data?: { error?: string; message?: string } }; message?: string }
    error.value = e.response?.data?.error || e.response?.data?.message || e.message || '比赛日期标记读取失败'
  } finally {
    if (pending === request) loading.value = false
  }
}

watch([open, month, () => props.mode, () => props.scope, () => props.revision], () => { void loadDates() })
onBeforeUnmount(() => { request++ })
</script>

<template>
  <v-menu :model-value="open" :close-on-content-click="false" @update:model-value="setOpen">
    <template #activator="{ props: activatorProps }">
      <v-text-field v-bind="{ ...$attrs, ...activatorProps }" class="schedule-date-field" :model-value="modelValue" label="赛程日期（北京时间）" prepend-inner-icon="mdi-calendar-month" density="compact" variant="outlined" readonly hide-details aria-label="选择赛程日期，日历圆点表示有比赛" />
    </template>
    <v-card class="schedule-calendar" max-width="350">
      <v-locale-provider locale="zhHans" :messages="{ zhHans }">
        <v-date-picker :model-value="selectedDate" v-model:year="calendarYear" v-model:month="calendarMonth" color="primary" hide-header :first-day-of-week="1" @update:model-value="choose">
          <template #day="{ item, props: dayProps }">
            <v-btn v-bind="dayProps" :title="dayTitle(item.isoDate)" :aria-label="dayTitle(item.isoDate)" :data-match-count="counts[item.isoDate] || 0" :data-schedule-date="item.isoDate">
              {{ item.localized }}<span v-if="counts[item.isoDate]" class="schedule-marker" aria-hidden="true" />
            </v-btn>
          </template>
        </v-date-picker>
      </v-locale-provider>
      <v-progress-linear v-if="loading" indeterminate color="primary" />
      <div class="px-4 pt-1 pb-3 text-caption text-medium-emphasis" aria-live="polite">
        <div class="d-flex align-center ga-2"><span class="schedule-marker legend-marker" />圆点：{{ scope === 'sporttery' ? '竞彩范围' : '全部赛事' }}{{ markerLabel }}</div>
        <div class="mt-1">按北京时间赛程日统计；不受联赛、球队和次数条件影响。</div>
        <div v-if="loading" class="mt-1">正在读取本月标记…</div>
        <div v-if="error" class="text-warning mt-1">{{ error }}<v-btn size="x-small" variant="text" @click="loadDates">重试标记</v-btn></div>
      </div>
    </v-card>
  </v-menu>
</template>

<style scoped>
.schedule-date-field { flex: 0 1 180px; min-width: 160px; }
.schedule-marker { position: absolute; bottom: 2px; left: calc(50% - 3px); width: 6px; height: 6px; border-radius: 50%; background: rgb(var(--v-theme-primary)); outline: 1px solid rgb(var(--v-theme-surface)); }
.legend-marker { position: static; display: inline-block; flex: 0 0 6px; }
.schedule-calendar :deep(.v-date-picker-month__day--selected .schedule-marker) { background: currentColor; }
</style>
