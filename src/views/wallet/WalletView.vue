<template>
  <div class="min-h-screen pb-24">
    <!-- Header -->
    <header class="px-6 py-4 flex justify-between items-center max-w-md mx-auto">
      <h1 class="text-2xl font-bold tracking-tight">Wallet</h1>
      <button
        class="w-10 h-10 rounded-full bg-slate-800 flex items-center justify-center"
        @click="router.push('/profile')"
      >
        <span class="material-symbols-outlined text-slate-400">person</span>
      </button>
    </header>

    <main class="px-6 max-w-md mx-auto">
      <!-- Balance Card -->
      <div class="relative w-full aspect-[16/9] bg-primary rounded-xl p-6 overflow-hidden mb-8 shadow-lg shadow-primary/20">
        <div class="relative z-10 h-full flex flex-col justify-between">
          <div>
            <p class="text-white/70 text-sm font-medium">Available Points</p>
            <h2 class="text-white text-4xl font-bold mt-1">
              {{ walletStore.balance.balance.toLocaleString() }}
            </h2>
          </div>
          <div class="flex justify-between items-end">
            <div>
              <p class="text-white/60 text-[10px] uppercase tracking-wider">Lifetime Earned</p>
              <p class="text-white font-semibold">{{ walletStore.balance.lifetimeEarned.toLocaleString() }} pts</p>
            </div>
            <button class="bg-white/20 hover:bg-white/30 p-2 rounded-lg backdrop-blur-sm transition-colors">
              <span class="material-symbols-outlined text-white text-xl">add</span>
            </button>
          </div>
        </div>
        <!-- Decorative Elements -->
        <div class="absolute -top-12 -right-12 w-48 h-48 bg-white/10 rounded-full blur-3xl" />
        <div class="absolute -bottom-12 -left-12 w-32 h-32 bg-black/10 rounded-full blur-2xl" />
      </div>

      <!-- Quick Actions -->
      <div class="grid grid-cols-2 gap-4 mb-8">
        <button class="bg-primary text-white py-3 rounded-lg font-semibold flex items-center justify-center gap-2 shadow-md">
          <span class="material-symbols-outlined text-sm">payments</span>
          Top Up
        </button>
        <button class="bg-primary/20 text-primary py-3 rounded-lg font-semibold flex items-center justify-center gap-2 border border-primary/20">
          <span class="material-symbols-outlined text-sm">redeem</span>
          Redeem
        </button>
      </div>

      <!-- Rewards Section -->
      <section class="mb-8">
        <div class="flex justify-between items-center mb-4">
          <h3 class="text-lg font-bold">Rewards</h3>
          <button class="text-primary text-sm font-semibold">View All</button>
        </div>
        <div class="flex gap-4 overflow-x-auto pb-4 hide-scrollbar">
          <div
            v-for="reward in walletStore.rewards"
            :key="reward.id"
            class="min-w-[140px] bg-slate-800 p-3 rounded-xl border border-slate-700"
          >
            <div class="w-full aspect-square bg-slate-700 rounded-lg mb-3 flex items-center justify-center">
              <span class="material-symbols-outlined text-3xl" :class="reward.iconColor || 'text-primary'">
                {{ reward.icon || 'workspace_premium' }}
              </span>
            </div>
            <p class="text-xs font-bold mb-1">{{ reward.name }}</p>
            <p class="text-[10px] text-slate-500 mb-2">{{ reward.description }}</p>
            <div class="text-primary text-xs font-bold">{{ reward.cost.toLocaleString() }} pts</div>
          </div>
        </div>
      </section>

      <!-- Transaction History -->
      <section>
        <div class="flex justify-between items-center mb-4">
          <h3 class="text-lg font-bold">Recent Activity</h3>
          <div class="flex gap-2">
            <button
              v-for="filter in txFilters"
              :key="filter.value"
              class="px-3 py-1 text-[10px] font-bold rounded-full"
              :class="activeTxFilter === filter.value ? 'bg-primary/20 text-primary' : 'text-slate-400'"
              @click="filterTransactions(filter.value)"
            >
              {{ filter.label }}
            </button>
          </div>
        </div>

        <div v-if="walletStore.loading" class="flex justify-center py-10">
          <span class="material-symbols-outlined text-primary text-3xl animate-spin">progress_activity</span>
        </div>

        <div v-else class="space-y-4">
          <div
            v-for="tx in walletStore.transactions"
            :key="tx.id"
            class="flex items-center justify-between p-3 bg-slate-800/50 rounded-lg border border-slate-700/50"
          >
            <div class="flex items-center gap-3">
              <div
                class="w-10 h-10 rounded-lg flex items-center justify-center"
                :class="tx.type === 'earned' ? 'bg-green-500/10' : 'bg-red-500/10'"
              >
                <span
                  class="material-symbols-outlined text-xl"
                  :class="tx.type === 'earned' ? 'text-green-500' : 'text-red-500'"
                >
                  {{ tx.type === 'earned' ? 'south_west' : 'arrow_outward' }}
                </span>
              </div>
              <div>
                <p class="text-xs font-bold">{{ tx.description }}</p>
                <p class="text-[10px] text-slate-500">{{ tx.detail }}</p>
              </div>
            </div>
            <div class="text-right">
              <p class="text-xs font-bold" :class="tx.type === 'earned' ? 'text-green-500' : 'text-red-500'">
                {{ tx.type === 'earned' ? '+' : '-' }}{{ Math.abs(tx.amount) }} pts
              </p>
              <p class="text-[10px] text-slate-500 uppercase">{{ formatTime(tx.createdAt) }}</p>
            </div>
          </div>

          <div v-if="!walletStore.transactions.length" class="flex flex-col items-center py-10 text-slate-500">
            <span class="material-symbols-outlined text-3xl mb-2">receipt_long</span>
            <p class="text-sm">No transactions yet</p>
          </div>
        </div>
      </section>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useWalletStore } from '@/store/wallet'

const router = useRouter()
const walletStore = useWalletStore()

const txFilters = [
  { label: 'All', value: '' },
  { label: 'Earned', value: 'earned' },
  { label: 'Spent', value: 'spent' },
]
const activeTxFilter = ref('')

function filterTransactions(type: string) {
  activeTxFilter.value = type
  walletStore.fetchTransactions(type || undefined)
}

function formatTime(dateStr: string): string {
  if (!dateStr) return ''
  const now = Date.now()
  const diff = now - new Date(dateStr).getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}

onMounted(() => {
  walletStore.fetchBalance()
  walletStore.fetchTransactions()
  walletStore.fetchRewards()
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
