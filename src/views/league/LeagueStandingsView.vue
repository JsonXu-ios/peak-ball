<template>
  <div class="min-h-screen pb-24">
    <!-- Header / League Selector -->
    <header class="sticky top-0 z-50 bg-background-dark/80 backdrop-blur-md border-b border-primary/10 px-4 py-3">
      <div class="flex items-center justify-between max-w-md mx-auto">
        <button class="p-2 -ml-2" @click="router.back()">
          <span class="material-symbols-outlined text-2xl">chevron_left</span>
        </button>
        <button
          class="flex items-center gap-2 bg-primary/10 px-4 py-1.5 rounded-full border border-primary/20"
          @click="showLeagueSelector = !showLeagueSelector"
        >
          <span class="font-semibold text-sm">{{ selectedLeague.name }}</span>
          <span class="material-symbols-outlined text-sm text-primary">expand_more</span>
        </button>
        <button class="p-2 -mr-2" @click="router.push('/search')">
          <span class="material-symbols-outlined text-2xl">search</span>
        </button>
      </div>
    </header>

    <!-- League Selector Dropdown -->
    <div v-if="showLeagueSelector" class="px-4 max-w-md mx-auto">
      <div class="bg-slate-800 rounded-xl p-2 mt-2 border border-slate-700 shadow-xl">
        <button
          v-for="league in leagues"
          :key="league.id"
          class="w-full text-left px-4 py-2 rounded-lg text-sm hover:bg-primary/10 transition-colors"
          :class="selectedLeague.id === league.id ? 'text-primary font-bold' : 'text-slate-300'"
          @click="selectLeague(league)"
        >
          {{ league.name }}
        </button>
      </div>
    </div>

    <!-- Main Navigation Tabs -->
    <nav class="px-4 mt-4 max-w-md mx-auto">
      <div class="flex bg-primary/10 p-1 rounded-xl">
        <button
          v-for="tab in mainTabs"
          :key="tab"
          class="flex-1 py-2 text-sm font-medium rounded-lg transition-all"
          :class="activeMainTab === tab ? 'bg-primary text-white shadow-sm' : 'text-slate-400'"
          @click="activeMainTab = tab"
        >
          {{ tab }}
        </button>
      </div>
    </nav>

    <!-- Sub-Filters -->
    <div class="px-4 mt-6 flex gap-2 overflow-x-auto hide-scrollbar max-w-md mx-auto">
      <button
        v-for="filter in subFilters"
        :key="filter"
        class="px-4 py-1.5 rounded-full text-xs font-semibold whitespace-nowrap transition-all"
        :class="activeFilter === filter ? 'bg-primary text-white' : 'bg-slate-800 text-slate-400'"
        @click="activeFilter = filter"
      >
        {{ filter }}
      </button>
    </div>

    <main class="mt-6 px-4 pb-6 max-w-md mx-auto">
      <!-- Loading -->
      <div v-if="leagueStore.loading" class="flex justify-center py-20">
        <span class="material-symbols-outlined text-primary text-4xl animate-spin">progress_activity</span>
      </div>

      <template v-else>
        <!-- Table Tab -->
        <template v-if="activeMainTab === 'Table'">
          <!-- Standings Table -->
          <div class="overflow-x-auto hide-scrollbar rounded-xl border border-primary/5 bg-slate-900/50 shadow-sm">
            <table class="w-full text-left border-collapse">
              <thead>
                <tr class="bg-primary/5 text-[10px] uppercase tracking-wider text-slate-400">
                  <th class="py-3 px-3 sticky left-0 bg-slate-900/90 backdrop-blur w-12 text-center z-10">#</th>
                  <th class="py-3 px-2 sticky left-12 bg-slate-900/90 backdrop-blur min-w-[140px] z-10">Team</th>
                  <th class="py-3 px-2 text-center">GP</th>
                  <th class="py-3 px-2 text-center">W</th>
                  <th class="py-3 px-2 text-center">D</th>
                  <th class="py-3 px-2 text-center">L</th>
                  <th class="py-3 px-2 text-center">GD</th>
                  <th class="py-3 px-4 text-center font-bold text-primary">Pts</th>
                  <th class="py-3 px-4 text-center min-w-[120px]">Form</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-primary/5">
                <template v-for="(row, idx) in leagueStore.standings" :key="row.id">
                  <!-- Zone separator -->
                  <tr v-if="idx > 0 && row.zone !== leagueStore.standings[idx - 1].zone" class="bg-slate-800/20">
                    <td class="py-2 px-4 text-[10px] text-slate-400 uppercase font-bold tracking-widest text-center" colspan="9">
                      {{ zoneLabel(row.zone) }}
                    </td>
                  </tr>
                  <tr class="text-sm">
                    <td class="py-4 px-3 sticky left-0 bg-slate-900 text-center font-medium z-10"
                      :class="zoneBorderClass(row.zone)">
                      {{ row.rank }}
                    </td>
                    <td class="py-4 px-2 sticky left-12 bg-slate-900 z-10">
                      <div class="flex items-center gap-2">
                        <img
                          v-if="row.teamLogo"
                          :alt="row.teamName"
                          :src="row.teamLogo"
                          class="w-6 h-6 rounded-sm"
                        />
                        <span class="font-semibold truncate">{{ row.teamName }}</span>
                      </div>
                    </td>
                    <td class="py-4 px-2 text-center text-slate-500">{{ row.played }}</td>
                    <td class="py-4 px-2 text-center">{{ row.won }}</td>
                    <td class="py-4 px-2 text-center">{{ row.drawn }}</td>
                    <td class="py-4 px-2 text-center">{{ row.lost }}</td>
                    <td class="py-4 px-2 text-center">{{ row.goalDiff > 0 ? '+' + row.goalDiff : row.goalDiff }}</td>
                    <td class="py-4 px-4 text-center font-bold text-primary">{{ row.points }}</td>
                    <td class="py-4 px-4 text-center">
                      <div class="flex justify-center gap-1">
                        <div
                          v-for="(r, i) in parseForm(row.form)"
                          :key="i"
                          class="w-2 h-2 rounded-full"
                          :class="formColor(r)"
                        />
                      </div>
                    </td>
                  </tr>
                </template>
              </tbody>
            </table>
          </div>

          <!-- Top Scorers Section -->
          <section class="mt-10">
            <div class="flex items-center justify-between mb-4">
              <h3 class="text-lg font-bold">Top Scorers</h3>
              <div class="flex gap-2">
                <button
                  class="px-3 py-1 text-[10px] font-bold rounded-md uppercase"
                  :class="scorerTab === 'goals' ? 'bg-primary text-white' : 'bg-primary/10 text-primary'"
                  @click="scorerTab = 'goals'"
                >Goals</button>
                <button
                  class="px-3 py-1 text-[10px] font-bold rounded-md uppercase"
                  :class="scorerTab === 'assists' ? 'bg-primary text-white' : 'bg-primary/10 text-primary'"
                  @click="scorerTab = 'assists'"
                >Assists</button>
              </div>
            </div>
            <div class="space-y-3">
              <div
                v-for="player in leagueStore.topScorers"
                :key="player.id"
                class="bg-slate-900/50 p-3 rounded-xl border border-primary/5 flex items-center justify-between shadow-sm"
              >
                <div class="flex items-center gap-3">
                  <div class="relative">
                    <div class="w-12 h-12 rounded-full bg-slate-700 overflow-hidden">
                      <img
                        v-if="player.avatar"
                        :alt="player.playerName"
                        :src="player.avatar"
                        class="w-full h-full object-cover"
                      />
                    </div>
                    <span
                      class="absolute -top-1 -left-1 w-5 h-5 text-[10px] text-white rounded-full flex items-center justify-center font-bold border-2 border-slate-900"
                      :class="rankBadgeColor(player.rank)"
                    >{{ player.rank }}</span>
                  </div>
                  <div>
                    <p class="font-bold text-sm">{{ player.playerName }}</p>
                    <p class="text-xs text-slate-500">{{ player.teamName }}</p>
                  </div>
                </div>
                <div class="text-right">
                  <p class="text-xl font-bold text-primary">{{ scorerTab === 'goals' ? player.goals : player.assists }}</p>
                  <p class="text-[10px] uppercase text-slate-400 font-medium">{{ scorerTab === 'goals' ? 'Goals' : 'Assists' }}</p>
                </div>
              </div>
            </div>
          </section>

          <!-- Zone Legend -->
          <section class="mt-10 p-4 bg-primary/5 rounded-xl border border-primary/10">
            <h4 class="text-xs font-bold uppercase text-slate-500 mb-3 tracking-widest">Promotion / Relegation</h4>
            <div class="space-y-2">
              <div class="flex items-center gap-2 text-xs">
                <div class="w-3 h-3 rounded-sm bg-primary" />
                <span class="text-slate-500">Champions League Group Stage</span>
              </div>
              <div class="flex items-center gap-2 text-xs">
                <div class="w-3 h-3 rounded-sm bg-primary/40" />
                <span class="text-slate-500">Europa League Group Stage</span>
              </div>
              <div class="flex items-center gap-2 text-xs">
                <div class="w-3 h-3 rounded-sm bg-green-500/50" />
                <span class="text-slate-500">Conference League Qualification</span>
              </div>
              <div class="flex items-center gap-2 text-xs">
                <div class="w-3 h-3 rounded-sm bg-red-500" />
                <span class="text-slate-500">Relegation</span>
              </div>
            </div>
          </section>
        </template>

        <!-- Matches Tab (placeholder) -->
        <template v-if="activeMainTab === 'Matches'">
          <div class="flex flex-col items-center justify-center py-20 text-slate-500">
            <span class="material-symbols-outlined text-4xl mb-2">sports_soccer</span>
            <p class="text-sm">Match schedule coming soon</p>
          </div>
        </template>

        <!-- Stats Tab (placeholder) -->
        <template v-if="activeMainTab === 'Stats'">
          <div class="flex flex-col items-center justify-center py-20 text-slate-500">
            <span class="material-symbols-outlined text-4xl mb-2">bar_chart</span>
            <p class="text-sm">Stats coming soon</p>
          </div>
        </template>
      </template>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useLeagueStore } from '@/store/league'

const router = useRouter()
const leagueStore = useLeagueStore()

interface LeagueOption {
  id: number
  name: string
}

const leagues: LeagueOption[] = [
  { id: 36, name: 'Premier League' },
  { id: 34, name: 'La Liga' },
  { id: 8, name: 'Serie A' },
  { id: 9, name: 'Bundesliga' },
  { id: 11, name: 'Ligue 1' },
]

const selectedLeague = ref<LeagueOption>(leagues[0])
const showLeagueSelector = ref(false)
const mainTabs = ['Table', 'Matches', 'Stats']
const activeMainTab = ref('Table')
const subFilters = ['Overall', 'Home', 'Away', 'Last 5 Games']
const activeFilter = ref('Overall')
const scorerTab = ref<'goals' | 'assists'>('goals')

function selectLeague(league: LeagueOption) {
  selectedLeague.value = league
  showLeagueSelector.value = false
  loadData()
}

function loadData() {
  leagueStore.fetchStandings(selectedLeague.value.id)
  leagueStore.fetchTopScorers(selectedLeague.value.id)
}

function parseForm(form: string): string[] {
  if (!form) return []
  return form.split('')
}

function formColor(result: string): string {
  switch (result) {
    case 'W': return 'bg-green-500'
    case 'L': return 'bg-red-500'
    case 'D': return 'bg-slate-400'
    default: return 'bg-slate-600'
  }
}

function zoneBorderClass(zone: string): string {
  switch (zone) {
    case 'champions_league': return 'border-l-4 border-primary'
    case 'europa_league': return 'border-l-4 border-primary/60'
    case 'conference_league': return 'border-l-4 border-green-500/50'
    case 'relegation': return 'border-l-4 border-red-500'
    default: return 'border-l-4 border-transparent'
  }
}

function zoneLabel(zone: string): string {
  switch (zone) {
    case 'champions_league': return 'Champions League Spot'
    case 'europa_league': return 'Europa League Spot'
    case 'conference_league': return 'Conference League Spot'
    case 'relegation': return 'Relegation Zone'
    default: return ''
  }
}

function rankBadgeColor(rank: number): string {
  if (rank === 1) return 'bg-primary'
  if (rank === 2) return 'bg-slate-400'
  if (rank === 3) return 'bg-orange-700'
  return 'bg-slate-600'
}

watch(() => selectedLeague.value, () => loadData())

onMounted(() => {
  loadData()
})
</script>

<style scoped>
.hide-scrollbar::-webkit-scrollbar {
  display: none;
}
.hide-scrollbar {
  -ms-overflow-style: none;
  scrollbar-width: none;
}
</style>
