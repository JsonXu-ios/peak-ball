<template>
  <div class="relative flex h-full w-full max-w-[480px] mx-auto flex-col min-h-screen pb-24">
    <!-- Top App Bar -->
    <div class="sticky top-0 z-50 bg-background-dark/80 backdrop-blur-md">
      <div class="flex items-center p-4 pb-2 justify-between">
        <div class="flex size-10 shrink-0 items-center justify-center cursor-pointer" @click="router.back()">
          <span class="material-symbols-outlined">arrow_back_ios</span>
        </div>
        <h2 class="text-lg font-bold leading-tight tracking-[-0.015em] flex-1 text-center">Global Leaderboard</h2>
        <div class="flex size-10 items-center justify-end cursor-pointer">
          <span class="material-symbols-outlined">filter_list</span>
        </div>
      </div>
      <!-- Tabs -->
      <div class="px-4 py-2">
        <div class="flex bg-slate-800 rounded-lg p-1">
          <button
            class="flex-1 py-2 text-sm font-bold rounded-md"
            :class="activeTab === 'weekly' ? 'bg-primary text-white shadow-sm' : 'text-slate-400'"
            @click="switchPeriod('weekly')"
          >
            Weekly
          </button>
          <button
            class="flex-1 py-2 text-sm font-bold"
            :class="activeTab === 'monthly' ? 'bg-primary text-white shadow-sm' : 'text-slate-400'"
            @click="switchPeriod('monthly')"
          >
            Monthly
          </button>
        </div>
      </div>
    </div>

    <!-- Podium Section -->
    <div class="relative px-4 pt-8 pb-10 podium-gradient">
      <div v-if="predictionStore.loading" class="flex justify-center py-12">
        <span class="material-symbols-outlined text-4xl text-primary animate-spin">progress_activity</span>
      </div>
      <div v-else-if="podium.length" class="flex items-end justify-center gap-2">
        <!-- 2nd Place -->
        <div v-if="podium.find(e => e.rank === 2)" class="flex flex-col items-center flex-1">
          <div class="relative mb-3">
            <div class="size-20 rounded-full border-4 border-[#C0C0C0] overflow-hidden bg-slate-800">
              <img v-if="podium.find(e => e.rank === 2)?.user?.avatar" :src="podium.find(e => e.rank === 2)!.user!.avatar" class="w-full h-full object-cover" />
            </div>
            <div class="absolute -bottom-2 left-1/2 -translate-x-1/2 bg-[#C0C0C0] text-background-dark text-[10px] font-black px-2 py-0.5 rounded-full uppercase">Silver</div>
          </div>
          <p class="font-bold text-sm truncate w-full text-center">{{ podium.find(e => e.rank === 2)?.user?.nickname || podium.find(e => e.rank === 2)?.user?.username }}</p>
          <p class="text-primary text-xs font-medium">{{ podium.find(e => e.rank === 2)?.accuracy }}% Acc.</p>
        </div>
        <!-- 1st Place -->
        <div v-if="podium.find(e => e.rank === 1)" class="flex flex-col items-center flex-1 -mt-6">
          <div class="relative mb-4">
            <div class="absolute -top-6 left-1/2 -translate-x-1/2 text-[#FFD700]">
              <span class="material-symbols-outlined text-4xl" style="font-variation-settings: 'FILL' 1">emoji_events</span>
            </div>
            <div class="size-28 rounded-full border-4 border-[#FFD700] overflow-hidden bg-slate-800 shadow-[0_0_20px_rgba(255,215,0,0.3)]">
              <img v-if="podium.find(e => e.rank === 1)?.user?.avatar" :src="podium.find(e => e.rank === 1)!.user!.avatar" class="w-full h-full object-cover" />
            </div>
            <div class="absolute -bottom-2 left-1/2 -translate-x-1/2 bg-[#FFD700] text-background-dark text-[12px] font-black px-3 py-1 rounded-full uppercase">Expert</div>
          </div>
          <p class="font-bold text-lg truncate w-full text-center">{{ podium.find(e => e.rank === 1)?.user?.nickname || podium.find(e => e.rank === 1)?.user?.username }}</p>
          <p class="text-primary text-sm font-bold">{{ podium.find(e => e.rank === 1)?.accuracy }}% Accuracy</p>
        </div>
        <!-- 3rd Place -->
        <div v-if="podium.find(e => e.rank === 3)" class="flex flex-col items-center flex-1">
          <div class="relative mb-3">
            <div class="size-16 rounded-full border-4 border-[#CD7F32] overflow-hidden bg-slate-800">
              <img v-if="podium.find(e => e.rank === 3)?.user?.avatar" :src="podium.find(e => e.rank === 3)!.user!.avatar" class="w-full h-full object-cover" />
            </div>
            <div class="absolute -bottom-2 left-1/2 -translate-x-1/2 bg-[#CD7F32] text-background-dark text-[10px] font-black px-2 py-0.5 rounded-full uppercase">Bronze</div>
          </div>
          <p class="font-bold text-sm truncate w-full text-center">{{ podium.find(e => e.rank === 3)?.user?.nickname || podium.find(e => e.rank === 3)?.user?.username }}</p>
          <p class="text-primary text-xs font-medium">{{ podium.find(e => e.rank === 3)?.accuracy }}% Acc.</p>
        </div>
      </div>
      <div v-else class="flex flex-col items-center py-12 text-slate-500">
        <span class="material-symbols-outlined text-4xl mb-2">emoji_events</span>
        <p class="text-sm">暂无排行榜数据</p>
      </div>
    </div>

    <!-- Leaderboard List -->
    <div class="flex flex-col px-4 gap-3">
      <div class="flex justify-between items-center text-[10px] font-bold uppercase tracking-wider text-slate-500 px-2">
        <div class="flex gap-8">
          <span>Rank</span>
          <span>User</span>
        </div>
        <div class="flex gap-10">
          <span>Pts</span>
          <span>Accuracy</span>
        </div>
      </div>

      <div
        v-for="item in restList"
        :key="item.id"
        class="flex items-center justify-between p-3 bg-slate-800/50 rounded-xl border border-slate-700/50"
      >
        <div class="flex items-center gap-4">
          <span class="text-slate-400 font-bold w-4">{{ item.rank }}</span>
          <div class="relative size-10">
            <div class="size-full rounded-full bg-slate-700 overflow-hidden">
              <img v-if="item.user?.avatar" :src="item.user.avatar" class="w-full h-full object-cover" />
            </div>
          </div>
          <div>
            <p class="text-sm font-bold">{{ item.user?.nickname || item.user?.username || 'User' }}</p>
            <div class="flex items-center gap-1">
              <span class="text-[10px] text-slate-500">{{ item.user?.country || '' }}</span>
              <span
                class="material-symbols-outlined text-[12px]"
                :class="item.trend === 'up' ? 'text-green-500' : item.trend === 'down' ? 'text-red-500' : 'text-slate-400'"
              >
                {{ item.trend === 'up' ? 'trending_up' : item.trend === 'down' ? 'trending_down' : 'remove' }}
              </span>
            </div>
          </div>
        </div>
        <div class="flex items-center gap-8 text-right">
          <span class="text-sm font-bold">{{ item.points.toLocaleString() }}</span>
          <span class="text-sm font-bold text-primary">{{ item.accuracy }}%</span>
        </div>
      </div>
    </div>

    <!-- Sticky My Rank Footer -->
    <div class="fixed bottom-0 left-1/2 -translate-x-1/2 w-full max-w-[480px] p-4 bg-background-dark/95 backdrop-blur-xl border-t border-slate-800">
      <div class="flex items-center justify-between bg-primary p-4 rounded-2xl text-white shadow-lg shadow-primary/20">
        <div class="flex items-center gap-4">
          <span class="font-black text-lg">128</span>
          <div class="size-10 rounded-full border-2 border-white/30 overflow-hidden bg-slate-700"></div>
          <div>
            <p class="text-sm font-bold">You (Predictor01)</p>
            <p class="text-[10px] text-white/70">Top 15% of Analysts</p>
          </div>
        </div>
        <div class="text-right">
          <p class="text-xs font-medium text-white/80">My Accuracy</p>
          <p class="text-lg font-black">72.4%</p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { usePredictionStore } from '@/store/prediction'

const router = useRouter()
const predictionStore = usePredictionStore()
const activeTab = ref<'weekly' | 'monthly'>('weekly')

/** 前三名 */
const podium = computed(() => predictionStore.leaderboard.filter(e => e.rank <= 3))
/** 第4名及以后 */
const restList = computed(() => predictionStore.leaderboard.filter(e => e.rank > 3))

function switchPeriod(period: 'weekly' | 'monthly') {
  activeTab.value = period
  predictionStore.fetchLeaderboard(period)
}

onMounted(() => {
  predictionStore.fetchLeaderboard('weekly')
})
</script>
