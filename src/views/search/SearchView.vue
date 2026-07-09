<template>
  <div class="min-h-screen pb-24 flex flex-col">
    <!-- Top Sticky Search Header -->
    <header class="sticky top-0 z-50 bg-background-dark/80 backdrop-blur-md border-b border-primary/10 px-4 pt-4 pb-4">
      <div class="flex items-center gap-3 max-w-md mx-auto">
        <div class="relative flex-1">
          <span class="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 text-xl">search</span>
          <input
            v-model="query"
            type="text"
            class="w-full bg-slate-800/50 border-none rounded-xl py-3 pl-10 pr-4 focus:ring-2 focus:ring-primary text-sm placeholder:text-slate-500 text-white"
            placeholder="Search teams, players, leagues..."
            @keydown.enter="handleSearch"
          />
        </div>
        <button class="bg-primary/20 text-primary p-3 rounded-xl">
          <span class="material-symbols-outlined text-xl">tune</span>
        </button>
      </div>
    </header>

    <main class="flex-1 overflow-y-auto max-w-md mx-auto w-full">
      <!-- Quick Links -->
      <div class="flex overflow-x-auto gap-3 px-4 py-6 hide-scrollbar">
        <div
          v-for="link in quickLinks"
          :key="link.label"
          class="flex flex-col items-center gap-2 flex-shrink-0 cursor-pointer"
          @click="link.action?.()"
        >
          <div
            class="w-14 h-14 rounded-full flex items-center justify-center text-white shadow-lg"
            :class="link.bgClass"
          >
            <span class="material-symbols-outlined">{{ link.icon }}</span>
          </div>
          <span class="text-[11px] font-medium" :class="link.textClass || 'text-slate-500'">{{ link.label }}</span>
        </div>
      </div>

      <!-- Search Results -->
      <template v-if="hasSearched">
        <div v-if="searchStore.loading" class="flex justify-center py-20">
          <span class="material-symbols-outlined text-primary text-4xl animate-spin">progress_activity</span>
        </div>
        <template v-else>
          <!-- Teams Results -->
          <section v-if="searchStore.result.teams.length" class="mb-8 px-4">
            <h2 class="text-lg font-bold mb-4">Teams</h2>
            <div class="space-y-3">
              <div
                v-for="team in searchStore.result.teams"
                :key="team.id"
                class="flex items-center gap-4 bg-slate-800/50 p-3 rounded-xl border border-slate-700/50"
              >
                <div class="w-12 h-12 rounded-full bg-slate-700 overflow-hidden border-2 border-primary/20">
                  <img v-if="team.teamLogo" :src="team.teamLogo" :alt="team.teamName" class="w-full h-full object-cover" />
                </div>
                <div class="flex-1">
                  <h4 class="text-sm font-bold">{{ team.teamName }}</h4>
                  <p class="text-xs text-slate-500">{{ team.league }} • Rank #{{ team.rank }}</p>
                </div>
                <span class="text-primary font-bold text-sm">{{ team.points }} pts</span>
              </div>
            </div>
          </section>

          <!-- Experts Results -->
          <section v-if="searchStore.result.experts.length" class="mb-8 px-4">
            <h2 class="text-lg font-bold mb-4">Experts</h2>
            <div class="space-y-3">
              <div
                v-for="expert in searchStore.result.experts"
                :key="expert.id"
                class="flex items-center gap-4 bg-slate-800/50 p-3 rounded-xl border border-slate-700/50"
              >
                <div class="relative">
                  <div class="w-12 h-12 rounded-full bg-slate-700 overflow-hidden">
                    <img v-if="expert.avatar" :src="expert.avatar" :alt="expert.name" class="w-full h-full object-cover" />
                  </div>
                  <div v-if="expert.verified" class="absolute -bottom-1 -right-1 bg-primary text-white rounded-full p-0.5 border-2 border-slate-800">
                    <span class="material-symbols-outlined text-[10px]">verified</span>
                  </div>
                </div>
                <div class="flex-1">
                  <h4 class="text-sm font-bold">{{ expert.name }}</h4>
                  <div class="flex items-center gap-2">
                    <span class="text-xs text-slate-500">Win Rate:</span>
                    <span class="text-xs font-bold text-green-500">{{ expert.accuracy }}%</span>
                  </div>
                </div>
                <button class="px-4 py-1.5 bg-primary/20 text-primary text-xs font-bold rounded-lg">View Tips</button>
              </div>
            </div>
          </section>

          <!-- News Results -->
          <section v-if="searchStore.result.news.length" class="mb-8 px-4">
            <h2 class="text-lg font-bold mb-4">News</h2>
            <div class="space-y-3">
              <div
                v-for="item in searchStore.result.news"
                :key="item.id"
                class="flex gap-4 bg-slate-900/40 p-3 rounded-xl border border-slate-800 cursor-pointer"
                @click="router.push('/news')"
              >
                <div class="w-16 h-16 flex-shrink-0 rounded-lg overflow-hidden bg-slate-700">
                  <img v-if="item.imageUrl" :src="item.imageUrl" :alt="item.title" class="w-full h-full object-cover" />
                </div>
                <div class="flex flex-col justify-center">
                  <h4 class="font-bold text-sm leading-snug mb-1 line-clamp-2">{{ item.title }}</h4>
                  <span class="text-xs text-slate-500">{{ item.source }}</span>
                </div>
              </div>
            </div>
          </section>

          <!-- No Results -->
          <div
            v-if="!searchStore.result.teams.length && !searchStore.result.experts.length && !searchStore.result.news.length"
            class="flex flex-col items-center justify-center py-20 text-slate-500"
          >
            <span class="material-symbols-outlined text-4xl mb-2">search_off</span>
            <p class="text-sm">No results found for "{{ query }}"</p>
          </div>
        </template>
      </template>

      <!-- Default Discovery Content (when not searching) -->
      <template v-else>
        <!-- Trending Teams -->
        <section class="mb-8">
          <div class="flex items-center justify-between px-4 mb-4">
            <h2 class="text-lg font-bold">Trending Teams</h2>
            <button class="text-primary text-sm font-medium">See All</button>
          </div>
          <div class="flex overflow-x-auto gap-4 px-4 hide-scrollbar">
            <div
              v-for="team in trendingTeams"
              :key="team.name"
              class="flex-shrink-0 w-44 bg-slate-800/50 p-4 rounded-xl border border-slate-700/50"
            >
              <div class="flex flex-col items-center text-center">
                <div class="w-16 h-16 rounded-full mb-3 bg-slate-700 overflow-hidden border-2 border-primary/20">
                  <img v-if="team.logo" :src="team.logo" :alt="team.name" class="w-full h-full object-cover" />
                </div>
                <h3 class="font-bold text-sm mb-1 truncate w-full">{{ team.name }}</h3>
                <div class="flex gap-1 mb-4">
                  <span
                    v-for="(r, i) in team.form"
                    :key="i"
                    class="w-2 h-2 rounded-full"
                    :class="formColor(r)"
                  />
                </div>
                <button
                  class="w-full py-2 text-xs font-bold rounded-lg"
                  :class="team.followed
                    ? 'border border-primary text-primary'
                    : 'bg-primary text-white'"
                >
                  {{ team.followed ? 'Following' : 'Follow' }}
                </button>
              </div>
            </div>
          </div>
        </section>

        <!-- Upcoming Derbies -->
        <section class="mb-8 px-4">
          <h2 class="text-lg font-bold mb-4">Upcoming Derbies</h2>
          <div class="relative overflow-hidden rounded-2xl bg-gradient-to-br from-primary to-blue-800 p-6 text-white shadow-xl">
            <div class="absolute top-0 right-0 p-4">
              <span class="px-2 py-1 bg-red-500 text-[10px] font-bold rounded uppercase tracking-wider">Hot Match</span>
            </div>
            <div class="flex items-center justify-between mb-6">
              <div class="text-center">
                <div class="w-16 h-16 bg-white/20 rounded-full flex items-center justify-center backdrop-blur-md mb-2">
                  <span class="text-2xl font-bold">INT</span>
                </div>
                <p class="text-xs font-bold">Inter Milan</p>
              </div>
              <div class="text-center">
                <span class="text-2xl font-black italic opacity-50">VS</span>
                <p class="text-[10px] font-medium opacity-80 mt-1">Oct 24, 20:45</p>
              </div>
              <div class="text-center">
                <div class="w-16 h-16 bg-white/20 rounded-full flex items-center justify-center backdrop-blur-md mb-2">
                  <span class="text-2xl font-bold">ACM</span>
                </div>
                <p class="text-xs font-bold">AC Milan</p>
              </div>
            </div>
            <div class="space-y-2">
              <div class="flex justify-between text-[10px] font-bold uppercase tracking-widest opacity-80">
                <span>Win Prob. 42%</span>
                <span>Draw 15%</span>
                <span>Win Prob. 43%</span>
              </div>
              <div class="h-2 w-full bg-white/20 rounded-full flex overflow-hidden">
                <div class="h-full bg-white w-[42%]" />
                <div class="h-full bg-white/40 w-[15%]" />
                <div class="h-full bg-blue-900 w-[43%]" />
              </div>
            </div>
          </div>
        </section>

        <!-- Popular Experts -->
        <section class="mb-8">
          <div class="flex items-center justify-between px-4 mb-4">
            <h2 class="text-lg font-bold">Popular Experts</h2>
            <button class="text-primary text-sm font-medium" @click="router.push('/expert')">Top Ranked</button>
          </div>
          <div class="space-y-3 px-4">
            <div
              v-for="expert in popularExperts"
              :key="expert.name"
              class="flex items-center gap-4 bg-slate-800/50 p-3 rounded-xl border border-slate-700/50"
            >
              <div class="relative">
                <div class="w-12 h-12 rounded-full bg-slate-700 overflow-hidden">
                  <img v-if="expert.avatar" :src="expert.avatar" :alt="expert.name" class="w-full h-full object-cover" />
                </div>
                <div v-if="expert.verified" class="absolute -bottom-1 -right-1 bg-primary text-white rounded-full p-0.5 border-2 border-slate-800">
                  <span class="material-symbols-outlined text-[10px]">verified</span>
                </div>
              </div>
              <div class="flex-1">
                <h4 class="text-sm font-bold">{{ expert.name }}</h4>
                <div class="flex items-center gap-2">
                  <span class="text-xs text-slate-500">Win Rate:</span>
                  <span class="text-xs font-bold text-green-500">{{ expert.accuracy }}%</span>
                </div>
              </div>
              <button class="px-4 py-1.5 bg-primary/20 text-primary text-xs font-bold rounded-lg" @click="router.push('/expert')">View Tips</button>
            </div>
          </div>
        </section>

        <!-- Recent Search History -->
        <section class="px-4 pb-12" v-if="searchStore.history.length">
          <div class="flex items-center justify-between mb-3">
            <h3 class="text-xs font-bold uppercase tracking-wider text-slate-400">Recent Searches</h3>
            <button class="text-xs font-medium text-slate-400 hover:text-primary" @click="searchStore.clearHistory()">Clear All</button>
          </div>
          <div class="flex flex-wrap gap-2">
            <div
              v-for="item in searchStore.history"
              :key="item.id"
              class="flex items-center gap-1.5 bg-slate-800 px-3 py-1.5 rounded-lg border border-slate-700 cursor-pointer"
              @click="query = item.query; handleSearch()"
            >
              <span class="text-xs font-medium">{{ item.query }}</span>
              <span class="material-symbols-outlined text-[14px] text-slate-400">close</span>
            </div>
          </div>
        </section>
      </template>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useSearchStore } from '@/store/search'

const router = useRouter()
const searchStore = useSearchStore()

const query = ref('')
const hasSearched = ref(false)

const quickLinks = [
  { label: 'Leagues', icon: 'emoji_events', bgClass: 'bg-primary shadow-primary/20', action: () => router.push('/league') },
  { label: 'Live', icon: 'sensors', bgClass: 'bg-orange-500 shadow-orange-500/20', textClass: 'text-orange-500', action: () => router.push('/') },
  { label: 'Injuries', icon: 'medical_services', bgClass: 'bg-slate-800', action: () => router.push('/news') },
  { label: 'Transfers', icon: 'currency_exchange', bgClass: 'bg-slate-800', action: () => router.push('/news') },
  { label: 'Analytics', icon: 'auto_graph', bgClass: 'bg-slate-800', action: () => router.push('/dashboard') },
]

const trendingTeams = ref([
  { name: 'Manchester City', logo: '/images/team_mancity.jpg', form: ['W', 'W', 'W', 'D', 'W'], followed: false },
  { name: 'Real Madrid', logo: '/images/team_realmadrid.jpg', form: ['W', 'D', 'W', 'W', 'L'], followed: true },
  { name: 'AC Milan', logo: '/images/team_acmilan.jpg', form: ['L', 'W', 'D', 'L', 'W'], followed: false },
])

const popularExperts = ref([
  { name: 'Marco Silva', avatar: '/images/expert_marco.jpg', accuracy: 78.4, verified: true },
  { name: 'Elena Rossi', avatar: '/images/expert_elena.jpg', accuracy: 76.1, verified: true },
])

function formColor(r: string): string {
  switch (r) {
    case 'W': return 'bg-green-500'
    case 'L': return 'bg-red-500'
    case 'D': return 'bg-yellow-500'
    default: return 'bg-slate-600'
  }
}

function handleSearch() {
  if (!query.value.trim()) return
  hasSearched.value = true
  searchStore.search(query.value)
}

onMounted(() => {
  searchStore.fetchHistory()
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
