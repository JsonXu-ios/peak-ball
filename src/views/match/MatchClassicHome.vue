<template>
  <div class="min-h-screen bg-[#0b1020] pb-24 text-white">
    <header class="sticky top-0 z-40 border-b border-slate-800 bg-[#0b1020]/95 backdrop-blur">
      <div class="mx-auto max-w-5xl px-4 py-3">
        <div class="flex items-center justify-between gap-3">
          <div class="min-w-0">
            <p class="text-[11px] font-black text-primary">今日足球 · {{ dateLabel }}</p>
            <h1 class="truncate text-xl font-black">比赛首页</h1>
          </div>
          <div class="flex items-center gap-1">
            <button class="flex size-10 items-center justify-center rounded-full text-slate-300 hover:bg-slate-800" title="搜索" @click="router.push('/search')">
              <span class="material-symbols-outlined">search</span>
            </button>
            <button class="relative flex size-10 items-center justify-center rounded-full text-slate-300 hover:bg-slate-800" title="通知" @click="router.push('/notifications')">
              <span class="material-symbols-outlined">notifications</span>
              <span class="absolute right-2 top-2 size-2 rounded-full border-2 border-[#0b1020] bg-red-500"></span>
            </button>
          </div>
        </div>

        <div class="mt-3 grid grid-cols-[auto_1fr_auto_auto] items-center gap-2">
          <button class="flex size-10 items-center justify-center rounded-md border border-slate-700 bg-slate-900/80 disabled:opacity-50" :disabled="loading" title="前一天" @click="shiftDate(-1)">
            <span class="material-symbols-outlined">chevron_left</span>
          </button>
          <input v-model="selectedDate" type="date" class="h-10 min-w-0 rounded-md border border-slate-700 bg-slate-900/80 px-3 text-sm font-bold text-white [color-scheme:dark]" :disabled="loading" @change="loadDate" />
          <button class="h-10 rounded-md border border-slate-700 bg-slate-900/80 px-3 text-xs font-black disabled:opacity-50" :disabled="loading || selectedDate === todayString" @click="setDate(todayString)">今天</button>
          <button class="flex size-10 items-center justify-center rounded-md border border-slate-700 bg-slate-900/80 disabled:opacity-50" :disabled="loading" title="后一天" @click="shiftDate(1)">
            <span class="material-symbols-outlined">chevron_right</span>
          </button>
        </div>
      </div>
    </header>

    <main class="mx-auto max-w-5xl space-y-5 px-3 py-4">
      <section class="grid grid-cols-4 gap-2">
        <div v-for="stat in summaryStats" :key="stat.label" class="rounded-lg border border-slate-800 bg-slate-900/80 px-2 py-3 text-center">
          <p class="text-[10px] font-black text-slate-500">{{ stat.label }}</p>
          <p class="mt-1 text-lg font-black" :class="stat.tone">{{ stat.value }}</p>
        </div>
      </section>

      <section v-if="featuredMatches.length" class="overflow-hidden">
        <div class="mb-3 flex items-center justify-between px-1">
          <div>
            <p class="text-[11px] font-black text-primary">重点赛程</p>
            <h2 class="text-lg font-black">焦点比赛</h2>
          </div>
          <button class="text-xs font-black text-slate-400" @click="statusFilter = 'all'">查看全部</button>
        </div>
        <div class="flex gap-3 overflow-x-auto pb-2">
          <article v-for="match in featuredMatches" :key="match.matchId" class="w-[280px] shrink-0 cursor-pointer overflow-hidden rounded-xl border border-slate-800 bg-[#151d2f]" @click="goToMatch(match.matchId)">
            <div class="flex items-center justify-between border-b border-slate-800 bg-slate-900/70 px-3 py-2">
              <span class="truncate text-xs font-black text-slate-400">{{ match.league }}</span>
              <span class="rounded px-2 py-1 text-[10px] font-black" :class="statusClass(match)">{{ statusText(match) }}</span>
            </div>
            <div class="grid grid-cols-[1fr_auto_1fr] items-center gap-2 px-3 py-5 text-center">
              <div class="min-w-0">
                <TeamLogo :src="match.homeLogo" :name="match.home" />
                <p class="mt-2 truncate text-sm font-black">{{ match.home }}</p>
              </div>
              <div>
                <p v-if="isStarted(match)" class="text-2xl font-black">{{ match.homeScore }} : {{ match.guestScore }}</p>
                <p v-else class="text-lg font-black">{{ formatMatchTime(match.matchTime) }}</p>
                <p class="mt-1 text-[10px] font-bold text-slate-500">{{ match.displayState || '未开始' }}</p>
              </div>
              <div class="min-w-0">
                <TeamLogo :src="match.guestLogo" :name="match.guest" />
                <p class="mt-2 truncate text-sm font-black">{{ match.guest }}</p>
              </div>
            </div>
          </article>
        </div>
      </section>

      <section>
        <div class="mb-3 flex items-end justify-between gap-3 px-1">
          <div>
            <p class="text-[11px] font-black text-primary">完整赛程</p>
            <h2 class="text-lg font-black">比赛列表</h2>
          </div>
          <select v-model="activeLeague" class="h-9 max-w-36 rounded-md border border-slate-700 bg-slate-900 px-2 text-xs font-black text-white" aria-label="选择首页联赛" @change="visibleLimit = pageSize">
            <option value="ALL">全部联赛</option>
            <option v-for="league in leagueList" :key="league" :value="league">{{ league }}</option>
          </select>
        </div>

        <div class="mb-3 grid grid-cols-4 gap-1 rounded-lg border border-slate-800 bg-slate-950/60 p-1">
          <button v-for="filter in statusFilters" :key="filter.value" class="h-9 rounded-md px-1 text-xs font-black transition" :class="statusFilter === filter.value ? 'bg-primary text-white' : 'text-slate-400 hover:bg-slate-900'" @click="selectStatus(filter.value)">
            {{ filter.label }}
          </button>
        </div>

        <div v-if="loading" class="flex justify-center py-16">
          <span class="material-symbols-outlined animate-spin text-4xl text-primary">progress_activity</span>
        </div>
        <div v-else-if="matchStore.error" class="rounded-lg border border-red-900/60 bg-red-950/30 px-4 py-8 text-center text-sm font-bold text-red-300">
          {{ matchStore.error }}
          <button class="ml-2 text-white underline" @click="loadDate">重试</button>
        </div>
        <div v-else class="space-y-2">
          <article v-for="match in visibleMatches" :key="match.matchId" class="cursor-pointer rounded-lg border border-slate-800 bg-[#151d2f] p-3 transition hover:border-primary/50" @click="goToMatch(match.matchId)">
            <div class="mb-3 flex items-center justify-between gap-2 text-xs">
              <span class="truncate font-black text-slate-400">{{ match.league }}</span>
              <span class="shrink-0 font-black" :class="statusTextClass(match)">{{ statusText(match) }}</span>
            </div>
            <div class="grid grid-cols-[1fr_auto_1fr] items-center gap-3">
              <div class="flex min-w-0 items-center gap-2">
                <TeamLogo :src="match.homeLogo" :name="match.home" small />
                <div class="min-w-0">
                  <p class="truncate text-sm font-black">{{ match.home }}</p>
                  <p class="text-[10px] font-bold text-slate-500">{{ rankText(match.homeRank) }}</p>
                </div>
              </div>
              <div class="min-w-16 text-center">
                <p v-if="isStarted(match)" class="text-lg font-black">{{ match.homeScore }} : {{ match.guestScore }}</p>
                <p v-else class="text-sm font-black">{{ formatMatchTime(match.matchTime) }}</p>
                <p class="text-[10px] font-bold text-slate-600">VS</p>
              </div>
              <div class="flex min-w-0 flex-row-reverse items-center gap-2 text-right">
                <TeamLogo :src="match.guestLogo" :name="match.guest" small />
                <div class="min-w-0">
                  <p class="truncate text-sm font-black">{{ match.guest }}</p>
                  <p class="text-[10px] font-bold text-slate-500">{{ rankText(match.guestRank) }}</p>
                </div>
              </div>
            </div>
          </article>

          <div v-if="!filteredMatches.length" class="rounded-lg border border-slate-800 bg-slate-900/50 py-14 text-center text-slate-500">
            <span class="material-symbols-outlined mb-2 block text-4xl">sports_soccer</span>
            <p class="text-sm font-bold">当前筛选下暂无比赛</p>
          </div>
          <button v-if="visibleMatches.length < filteredMatches.length" class="h-11 w-full rounded-lg border border-slate-700 bg-slate-900 text-sm font-black text-slate-300" @click="visibleLimit += pageSize">
            加载更多（剩余 {{ filteredMatches.length - visibleMatches.length }} 场）
          </button>
        </div>
      </section>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { resolveAssetUrl } from '@/api/request'
import { useMatchStore } from '@/store/match'
import type { Match } from '@/types/match'

type StatusFilter = 'all' | 'live' | 'upcoming' | 'finished'

const router = useRouter()
const matchStore = useMatchStore()
const todayString = localDateString(new Date())
const selectedDate = ref(matchStore.selectedDate || todayString)
const activeLeague = ref('ALL')
const statusFilter = ref<StatusFilter>('all')
const pageSize = 80
const visibleLimit = ref(pageSize)

const TeamLogo = defineComponent({
  props: { src: { type: String, default: '' }, name: { type: String, required: true }, small: { type: Boolean, default: false } },
  setup(props) {
    return () => h('div', { class: [props.small ? 'size-9' : 'mx-auto size-12', 'shrink-0 overflow-hidden rounded-full border border-slate-700 bg-slate-800 flex items-center justify-center'] }, [
      props.src
        ? h('img', { src: resolveAssetUrl(props.src), alt: props.name, class: 'size-full object-cover' })
        : h('span', { class: 'material-symbols-outlined text-slate-500' }, 'shield'),
    ])
  },
})

const loading = computed(() => matchStore.loading)
const matches = computed(() => matchStore.matches)
const dateLabel = computed(() => selectedDate.value === todayString ? '今天' : selectedDate.value)
const liveCount = computed(() => matches.value.filter(isLive).length)
const finishedCount = computed(() => matches.value.filter(isFinished).length)
const upcomingCount = computed(() => matches.value.filter((match) => !isLive(match) && !isFinished(match)).length)
const summaryStats = computed(() => [
  { label: '全部', value: matches.value.length, tone: 'text-white' },
  { label: '进行中', value: liveCount.value, tone: 'text-red-400' },
  { label: '未开始', value: upcomingCount.value, tone: 'text-emerald-400' },
  { label: '已结束', value: finishedCount.value, tone: 'text-slate-300' },
])
const statusFilters: Array<{ value: StatusFilter; label: string }> = [
  { value: 'all', label: '全部' },
  { value: 'live', label: '进行中' },
  { value: 'upcoming', label: '未开始' },
  { value: 'finished', label: '已结束' },
]
const leagueList = computed(() => Array.from(new Set(matches.value.map((match) => match.league).filter(Boolean))).sort((left, right) => left.localeCompare(right, 'zh-CN')))
const filteredMatches = computed(() => matches.value.filter((match) => {
  if (activeLeague.value !== 'ALL' && match.league !== activeLeague.value) return false
  if (statusFilter.value === 'live') return isLive(match)
  if (statusFilter.value === 'upcoming') return !isLive(match) && !isFinished(match)
  if (statusFilter.value === 'finished') return isFinished(match)
  return true
}))
const visibleMatches = computed(() => filteredMatches.value.slice(0, visibleLimit.value))
const featuredMatches = computed(() => {
  const live = matches.value.filter(isLive)
  const upcoming = matches.value.filter((match) => !isLive(match) && !isFinished(match))
  return [...live, ...upcoming].slice(0, 6)
})

watch(matches, () => {
  if (activeLeague.value !== 'ALL' && !leagueList.value.includes(activeLeague.value)) activeLeague.value = 'ALL'
})

function selectStatus(value: StatusFilter) {
  statusFilter.value = value
  visibleLimit.value = pageSize
}

function setDate(value: string) {
  selectedDate.value = value
  loadDate()
}

function shiftDate(days: number) {
  const date = new Date(`${selectedDate.value}T12:00:00`)
  date.setDate(date.getDate() + days)
  setDate(localDateString(date))
}

function loadDate() {
  matchStore.selectedDate = selectedDate.value
  activeLeague.value = 'ALL'
  statusFilter.value = 'all'
  visibleLimit.value = pageSize
  void matchStore.fetchMatches(selectedDate.value)
}

function goToMatch(matchId: string) {
  router.push(`/match/${matchId}`)
}

function isLive(match: Match) {
  return ['上半场', '中场', '下半场', '进行中'].includes(match.displayState)
}

function isFinished(match: Match) {
  return ['完场', '取消', '推迟', '延期'].includes(match.displayState)
}

function isStarted(match: Match) {
  return isLive(match) || isFinished(match)
}

function statusText(match: Match) {
  if (isLive(match)) return match.displayState || '进行中'
  if (isFinished(match)) return match.displayState || '已结束'
  return '未开始'
}

function statusClass(match: Match) {
  if (isLive(match)) return 'bg-red-500/20 text-red-300'
  if (isFinished(match)) return 'bg-slate-700 text-slate-300'
  return 'bg-primary/15 text-primary'
}

function statusTextClass(match: Match) {
  if (isLive(match)) return 'text-red-400'
  if (isFinished(match)) return 'text-slate-500'
  return 'text-primary'
}

function formatMatchTime(value: string) {
  if (!value) return '--:--'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '--:--'
  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
}

function rankText(value: string) {
  return value ? `排名 ${value}` : '暂无排名'
}

function localDateString(date: Date) {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

onMounted(loadDate)
</script>
