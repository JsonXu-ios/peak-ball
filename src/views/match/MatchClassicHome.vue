<template>
  <div class="min-h-screen pb-24">
    <header class="sticky top-0 z-50 bg-background-dark/80 backdrop-blur-md border-b border-slate-800">
      <div class="flex items-center justify-between p-4 pb-2">
        <div class="flex items-center gap-3">
          <div class="size-10 shrink-0 rounded-full bg-primary/20 border border-primary/30 overflow-hidden flex items-center justify-center">
            <span class="material-symbols-outlined text-primary">person</span>
          </div>
          <div>
            <p class="text-[10px] uppercase tracking-wider text-slate-400 font-bold">Good Evening</p>
            <h1 class="text-lg font-bold leading-tight">Alex Morgan</h1>
          </div>
        </div>
        <div class="flex gap-2">
          <button class="p-2 rounded-full hover:bg-slate-800 text-slate-300" @click="router.push('/search')">
            <span class="material-symbols-outlined">search</span>
          </button>
          <button class="p-2 rounded-full hover:bg-slate-800 text-slate-300 relative" @click="router.push('/notifications')">
            <span class="material-symbols-outlined">notifications</span>
            <span class="absolute top-2 right-2 size-2 bg-red-500 rounded-full border-2 border-background-dark"></span>
          </button>
        </div>
      </div>
    </header>

    <main>
      <div class="px-4 py-3">
        <div class="flex items-center gap-2 overflow-x-auto no-scrollbar pb-2">
          <button
            v-for="(filter, idx) in filters"
            :key="filter"
            class="px-4 py-2 rounded-full text-sm font-semibold whitespace-nowrap transition-colors"
            :class="activeFilter === idx
              ? 'bg-primary text-white'
              : 'bg-slate-800 text-slate-400 border border-transparent hover:border-primary/50'"
            @click="activeFilter = idx"
          >
            {{ filter }}
          </button>
        </div>
      </div>

      <section class="mt-2">
        <div class="px-4 flex items-center justify-between mb-3">
          <h2 class="text-xl font-bold tracking-tight">Hot Predictions</h2>
          <button class="text-primary text-sm font-semibold" @click="activeFilter = 1">View All</button>
        </div>
        <div class="flex overflow-x-auto no-scrollbar px-4 gap-4 pb-4">
          <div
            v-for="match in hotMatches"
            :key="match.matchId"
            class="flex-none w-[280px] bg-[#1a2235] rounded-xl overflow-hidden border border-slate-800 cursor-pointer"
            :class="{ 'neon-glow': match.status === 1 }"
            @click="goToMatch(match.matchId)"
          >
            <div class="relative h-32 w-full bg-slate-800">
              <div class="absolute inset-0 bg-gradient-to-t from-[#1a2235] to-transparent"></div>
              <div
                class="absolute top-3 left-3 px-2 py-1 text-[10px] font-bold text-white rounded uppercase"
                :class="match.status === 1 ? 'bg-red-600' : 'bg-primary'"
              >
                {{ match.status === 1 ? 'LIVE' : 'Upcoming' }}
              </div>
              <div class="absolute bottom-3 left-3 flex items-center gap-2">
                <span class="text-white font-bold text-lg">
                  {{ getShortName(match.home) }} vs {{ getShortName(match.guest) }}
                </span>
              </div>
            </div>
            <div class="p-4">
              <div class="flex justify-between items-center mb-3">
                <span class="text-xs text-slate-400">{{ match.league }}</span>
                <span class="text-xs font-bold text-primary">Expert Tips</span>
              </div>
              <div class="grid grid-cols-3 gap-2">
                <button class="bg-primary/10 border border-primary/30 rounded-lg py-2 flex flex-col items-center">
                  <span class="text-[10px] text-slate-400 uppercase font-bold">Home</span>
                  <span class="text-sm font-bold text-primary">--</span>
                </button>
                <button class="bg-slate-800 rounded-lg py-2 flex flex-col items-center">
                  <span class="text-[10px] text-slate-400 uppercase font-bold">Draw</span>
                  <span class="text-sm font-bold">--</span>
                </button>
                <button class="bg-slate-800 rounded-lg py-2 flex flex-col items-center">
                  <span class="text-[10px] text-slate-400 uppercase font-bold">Away</span>
                  <span class="text-sm font-bold">--</span>
                </button>
              </div>
            </div>
          </div>

          <div v-if="hotMatches.length === 0" class="flex-none w-[280px] bg-[#1a2235] rounded-xl border border-slate-800 p-8 flex items-center justify-center">
            <p class="text-slate-500 text-sm">暂无热门比赛</p>
          </div>
        </div>
      </section>

      <section class="mt-4">
        <div class="px-4 mb-4">
          <h2 class="text-xl font-bold tracking-tight">Leagues</h2>
        </div>
        <div class="flex overflow-x-auto no-scrollbar px-4 gap-6 pb-2 border-b border-slate-800">
          <button
            v-for="league in leagueList"
            :key="league"
            class="pb-3 border-b-2 font-bold text-sm whitespace-nowrap uppercase"
            :class="activeLeague === league
              ? 'border-primary text-primary'
              : 'border-transparent text-slate-400'"
            @click="activeLeague = league"
          >
            {{ league }}
          </button>
        </div>
      </section>

      <section class="px-4 py-6 space-y-4">
        <div v-if="loading" class="flex justify-center py-12">
          <span class="material-symbols-outlined text-4xl text-primary animate-spin">progress_activity</span>
        </div>

        <div
          v-for="match in filteredMatches"
          :key="match.matchId"
          class="bg-[#1a2235] p-4 rounded-xl border border-slate-800 cursor-pointer hover:border-primary/30 transition-colors"
          @click="goToMatch(match.matchId)"
        >
          <div class="flex items-center justify-between mb-4">
            <div class="flex flex-col gap-1">
              <div class="flex items-center gap-3">
                <div class="size-6 bg-slate-800 rounded-full overflow-hidden">
                  <img v-if="match.homeLogo" :src="logoUrl(match.homeLogo)" alt="" class="w-full h-full object-cover" />
                </div>
                <span class="font-semibold text-sm">{{ match.home }}</span>
                <span v-if="match.status >= 2" class="ml-auto text-primary font-bold">{{ match.homeScore }}</span>
              </div>
              <div class="flex items-center gap-3">
                <div class="size-6 bg-slate-800 rounded-full overflow-hidden">
                  <img v-if="match.guestLogo" :src="logoUrl(match.guestLogo)" alt="" class="w-full h-full object-cover" />
                </div>
                <span class="font-semibold text-sm">{{ match.guest }}</span>
                <span v-if="match.status >= 2" class="ml-auto text-slate-500 font-bold">{{ match.guestScore }}</span>
              </div>
            </div>
            <div class="text-right">
              <p v-if="match.displayState === '完场'" class="text-xs font-bold text-slate-400">FT</p>
              <p v-else-if="match.status === 1" class="text-[10px] text-red-500 font-bold animate-pulse">LIVE</p>
              <p v-else class="text-xs font-bold">{{ formatMatchTime(match.matchTime) }}</p>
              <p class="text-xs text-slate-400">{{ match.league }}</p>
            </div>
          </div>
          <div class="flex items-center justify-between pt-4 border-t border-slate-800">
            <div class="flex items-center gap-2">
              <span class="material-symbols-outlined text-sm text-primary">groups</span>
              <span class="text-xs text-slate-500">Expert Tips</span>
            </div>
            <button class="text-primary text-xs font-bold flex items-center gap-1">
              Full Analysis <span class="material-symbols-outlined text-sm">chevron_right</span>
            </button>
          </div>
        </div>

        <div v-if="!loading && filteredMatches.length === 0" class="text-center py-12 text-slate-500">
          <span class="material-symbols-outlined text-4xl mb-2 block">sports_soccer</span>
          <p>暂无比赛数据</p>
        </div>
      </section>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { resolveAssetUrl } from '@/api/request'
import { useMatchStore } from '@/store/match'

const router = useRouter()
const matchStore = useMatchStore()

const filters = ['All Matches', 'Hot Predictions', 'Expert Tips', 'My Bets']
const activeFilter = ref(0)
const activeLeague = ref('ALL')

const loading = computed(() => matchStore.loading)
const matches = computed(() => matchStore.matches)

const leagueList = computed(() => {
  const set = new Set(matches.value.map((match) => match.league))
  return ['ALL', ...Array.from(set)]
})

const filteredMatches = computed(() => {
  if (activeLeague.value === 'ALL') return matches.value
  return matches.value.filter((match) => match.league === activeLeague.value)
})

const hotMatches = computed(() => matches.value.slice(0, 3))

function goToMatch(matchId: string) {
  router.push(`/match/${matchId}`)
}

function getShortName(name: string): string {
  return name.length > 5 ? name.slice(0, 3).toUpperCase() : name.toUpperCase()
}

function logoUrl(logo: string): string {
  return resolveAssetUrl(logo)
}

function formatMatchTime(time: string): string {
  if (!time) return ''
  const date = new Date(time)
  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
}

onMounted(() => {
  matchStore.fetchMatches()
})
</script>