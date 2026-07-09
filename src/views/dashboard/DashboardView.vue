<template>
  <div class="min-h-screen pb-24">
    <!-- Top Navigation Bar -->
    <header class="sticky top-0 z-50 bg-background-dark/80 backdrop-blur-md border-b border-slate-800">
      <div class="flex items-center justify-between px-4 h-16">
        <div class="flex items-center gap-3">
          <div class="w-10 h-10 rounded-full bg-primary/20 flex items-center justify-center border border-primary/30 overflow-hidden">
            <img v-if="userStore.user?.avatar" :src="userStore.user.avatar" :alt="userStore.user.nickname" class="w-full h-full object-cover" />
            <span v-else class="material-symbols-outlined text-primary">person</span>
          </div>
          <div>
            <h1 class="text-lg font-bold leading-none">Dashboard</h1>
            <p class="text-xs text-slate-400">Welcome back, {{ userStore.user?.nickname || userStore.user?.username || 'User' }}</p>
          </div>
        </div>
        <div class="flex items-center gap-2">
          <button class="p-2 rounded-full hover:bg-slate-800 relative" @click="router.push('/notifications')">
            <span class="material-symbols-outlined">notifications</span>
            <span class="absolute top-2 right-2 w-2 h-2 bg-red-500 rounded-full border-2 border-background-dark"></span>
          </button>
          <button class="p-2 rounded-full hover:bg-slate-800" @click="router.push('/profile')">
            <span class="material-symbols-outlined">settings</span>
          </button>
        </div>
      </div>
    </header>

    <main class="pb-8">
      <!-- Quick Stats Grid -->
      <div class="grid grid-cols-3 gap-3 p-4">
        <div class="bg-[#1c2433] p-4 rounded-xl border border-slate-800 shadow-sm">
          <p class="text-[10px] uppercase tracking-wider text-slate-400 font-semibold mb-1">Profit</p>
          <p class="text-lg font-bold" :class="(displayStats?.totalProfit ?? 0) >= 0 ? 'text-emerald-500' : 'text-red-500'">
            {{ (displayStats?.totalProfit ?? 0) >= 0 ? '+' : '' }}${{ (displayStats?.totalProfit ?? 0).toLocaleString() }}
          </p>
          <div class="flex items-center gap-1 mt-1">
            <span class="material-symbols-outlined text-[14px]" :class="(displayStats?.profitChange ?? 0) >= 0 ? 'text-emerald-500' : 'text-red-500'">{{ (displayStats?.profitChange ?? 0) >= 0 ? 'trending_up' : 'trending_down' }}</span>
            <span class="text-[10px] font-medium" :class="(displayStats?.profitChange ?? 0) >= 0 ? 'text-emerald-500' : 'text-red-500'">{{ (displayStats?.profitChange ?? 0) >= 0 ? '+' : '' }}{{ displayStats?.profitChange ?? 0 }}%</span>
          </div>
        </div>
        <div class="bg-[#1c2433] p-4 rounded-xl border border-slate-800 shadow-sm">
          <p class="text-[10px] uppercase tracking-wider text-slate-400 font-semibold mb-1">Accuracy</p>
          <p class="text-lg font-bold text-primary">{{ (displayStats?.accuracy ?? 0).toFixed(1) }}%</p>
          <div class="flex items-center gap-1 mt-1">
            <span class="material-symbols-outlined text-[14px] text-primary">analytics</span>
            <span class="text-[10px] text-primary font-medium">{{ (displayStats?.accuracy ?? 0) >= 70 ? 'High' : (displayStats?.accuracy ?? 0) >= 50 ? 'Med' : 'Low' }}</span>
          </div>
        </div>
        <div class="bg-[#1c2433] p-4 rounded-xl border border-slate-800 shadow-sm">
          <p class="text-[10px] uppercase tracking-wider text-slate-400 font-semibold mb-1">Active</p>
          <p class="text-lg font-bold">{{ String(displayStats?.ongoing ?? 0).padStart(2, '0') }}</p>
          <div class="flex items-center gap-1 mt-1">
            <span class="material-symbols-outlined text-[14px] text-slate-400">pending_actions</span>
            <span class="text-[10px] text-slate-400 font-medium">Stakes</span>
          </div>
        </div>
      </div>

      <!-- Performance Chart -->
      <section class="px-4 mb-6">
        <div class="bg-[#1c2433] p-5 rounded-xl border border-slate-800 shadow-sm">
          <div class="flex justify-between items-start mb-4">
            <div>
              <h3 class="text-sm font-semibold text-slate-400 uppercase tracking-tight">Performance History</h3>
              <p class="text-2xl font-bold mt-1">Success Rate</p>
            </div>
            <div class="bg-slate-800 px-2 py-1 rounded text-[10px] font-bold text-slate-500">LAST 30 DAYS</div>
          </div>
          <div class="relative h-40 w-full mt-4">
            <svg class="overflow-visible" fill="none" height="100%" preserveAspectRatio="none" viewBox="0 0 472 150" width="100%">
              <defs>
                <linearGradient id="chartGradient" x1="0" x2="0" y1="0" y2="1">
                  <stop offset="0%" stop-color="#135bec" stop-opacity="0.3" />
                  <stop offset="100%" stop-color="#135bec" stop-opacity="0" />
                </linearGradient>
              </defs>
              <path d="M0 109C18.1538 109 18.1538 21 36.3077 21C54.4615 21 54.4615 41 72.6154 41C90.7692 41 90.7692 93 108.923 93C127.077 93 127.077 33 145.231 33C163.385 33 163.385 101 181.538 101C199.692 101 199.692 61 217.846 61C236 61 236 45 254.154 45C272.308 45 272.308 121 290.462 121C308.615 121 308.615 149 326.769 149C344.923 149 344.923 1 363.077 1C381.231 1 381.231 81 399.385 81C417.538 81 417.538 129 435.692 129C453.846 129 453.846 25 472 25V150H0V109Z" fill="url(#chartGradient)" />
              <path d="M0 109C18.1538 109 18.1538 21 36.3077 21C54.4615 21 54.4615 41 72.6154 41C90.7692 41 90.7692 93 108.923 93C127.077 93 127.077 33 145.231 33C163.385 33 163.385 101 181.538 101C199.692 101 199.692 61 217.846 61C236 61 236 45 254.154 45C272.308 45 272.308 121 290.462 121C308.615 121 308.615 149 326.769 149C344.923 149 344.923 1 363.077 1C381.231 1 381.231 81 399.385 81C417.538 81 417.538 129 435.692 129C453.846 129 453.846 25 472 25" stroke="#135bec" stroke-linecap="round" stroke-width="3" />
            </svg>
          </div>
          <div class="flex justify-between mt-4">
            <p class="text-slate-400 text-[11px] font-bold">OCT 01</p>
            <p class="text-slate-400 text-[11px] font-bold">OCT 15</p>
            <p class="text-slate-400 text-[11px] font-bold">TODAY</p>
          </div>
        </div>
      </section>

      <!-- Prediction List Tabs -->
      <section class="px-4">
        <div class="flex p-1 bg-slate-800 rounded-lg mb-4">
          <button
            class="flex-1 py-2 text-sm font-semibold rounded-md"
            :class="activeTab === 'ongoing' ? 'bg-[#1c2433] text-white shadow-sm' : 'text-slate-400'"
            @click="switchTab('ongoing')"
          >
            Ongoing
          </button>
          <button
            class="flex-1 py-2 text-sm font-semibold"
            :class="activeTab === 'settled' ? 'bg-[#1c2433] text-white shadow-sm' : 'text-slate-400'"
            @click="switchTab('settled')"
          >
            Settled
          </button>
        </div>

        <div v-if="predictionStore.loading" class="flex justify-center py-10">
          <span class="material-symbols-outlined text-4xl text-primary animate-spin">progress_activity</span>
        </div>

        <div v-else class="space-y-3">
          <div
            v-for="pred in predictionStore.predictions"
            :key="pred.id"
            class="bg-[#1c2433] rounded-xl border border-slate-800 overflow-hidden shadow-sm"
          >
            <div class="bg-slate-800/50 px-4 py-2 border-b border-slate-800 flex justify-between items-center">
              <span class="text-[10px] font-bold text-slate-500 uppercase tracking-wider">{{ pred.matchId }}</span>
              <span
                class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full"
                :class="{
                  'bg-primary/20 text-primary': pred.status === 'ongoing',
                  'bg-green-500/20 text-green-500': pred.status === 'won',
                  'bg-red-500/20 text-red-500':   pred.status === 'lost',
                  'bg-slate-500/20 text-slate-400': pred.status === 'void',
                }"
              >{{ pred.status }}</span>
            </div>
            <div class="p-4 flex justify-between items-center">
              <div>
                <p class="text-sm font-bold">Pick: {{ pred.pick }}</p>
                <p class="text-[10px] text-slate-500">Odds {{ pred.odds }} · Stake ${{ pred.stake }}</p>
              </div>
              <div class="text-right">
                <p class="text-xs font-bold" :class="pred.profit >= 0 ? 'text-green-500' : 'text-red-500'">
                  {{ pred.profit >= 0 ? '+' : '' }}${{ pred.profit }}
                </p>
              </div>
            </div>
          </div>

          <div v-if="!predictionStore.predictions.length" class="bg-[#1c2433] rounded-xl border border-slate-800 overflow-hidden shadow-sm">
            <div class="p-8 text-center">
              <span class="material-symbols-outlined text-4xl text-slate-600 mb-2 block">sports_score</span>
              <p class="text-slate-500 text-sm">前往比赛列表选择比赛进行预测</p>
              <button class="mt-4 px-6 py-2 bg-primary text-white rounded-lg text-sm font-semibold" @click="router.push('/')">
                浏览比赛
              </button>
            </div>
          </div>
        </div>
      </section>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { usePredictionStore } from '@/store/prediction'
import { useUserStore } from '@/store/user'

const router = useRouter()
const predictionStore = usePredictionStore()
const userStore = useUserStore()
const activeTab = ref<'ongoing' | 'settled'>('ongoing')

const displayStats = computed(() => predictionStore.stats)

function switchTab(tab: 'ongoing' | 'settled') {
  activeTab.value = tab
  predictionStore.fetchPredictions(tab)
}

onMounted(() => {
  userStore.fetchUser()
  predictionStore.fetchStats()
  predictionStore.fetchPredictions('ongoing')
})
</script>
