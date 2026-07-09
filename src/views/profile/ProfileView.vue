<template>
  <div class="max-w-md mx-auto min-h-screen flex flex-col pb-24">
    <!-- Top Navigation -->
    <header class="flex items-center justify-between px-6 py-4 mt-2">
      <button class="w-10 h-10 flex items-center justify-center rounded-full bg-[#1c2433]" @click="router.back()">
        <span class="material-symbols-outlined text-2xl">arrow_back_ios_new</span>
      </button>
      <h1 class="text-lg font-bold">Profile &amp; Settings</h1>
      <button class="w-10 h-10 flex items-center justify-center rounded-full bg-[#1c2433]">
        <span class="material-symbols-outlined text-2xl">share</span>
      </button>
    </header>

    <!-- Profile Hero Section -->
    <section class="px-6 py-4 flex flex-col items-center">
      <div class="relative">
        <div class="w-24 h-24 rounded-full border-4 border-primary p-1">
          <img v-if="userStore.user?.avatar" :src="userStore.user.avatar" :alt="userStore.user.nickname" class="w-full h-full rounded-full object-cover" />
          <div v-else class="w-full h-full rounded-full bg-slate-700 flex items-center justify-center">
            <span class="material-symbols-outlined text-3xl text-slate-400">person</span>
          </div>
        </div>
        <div class="absolute bottom-0 right-0 bg-primary text-white rounded-full p-1 border-2 border-background-dark">
          <span class="material-symbols-outlined text-xs block">verified</span>
        </div>
      </div>
      <div class="mt-4 text-center">
        <h2 class="text-xl font-bold">{{ userStore.user?.nickname || userStore.user?.username || 'User' }}</h2>
        <div class="flex items-center justify-center gap-2 mt-1">
          <span class="bg-primary/20 text-primary text-[10px] font-bold uppercase tracking-wider px-2 py-0.5 rounded">{{ userStore.user?.badge || 'Member' }}</span>
          <span class="text-slate-500 text-sm">{{ userStore.user?.joinedAt ? 'Joined ' + new Date(userStore.user.joinedAt).toLocaleDateString('en-US', { month: 'short', year: 'numeric' }) : '' }}</span>
        </div>
      </div>
      <button class="mt-4 px-6 py-2 rounded-full border border-slate-700 text-sm font-semibold">
        Edit Profile
      </button>
    </section>

    <!-- Account Balance Card -->
    <section class="px-6 py-4">
      <div class="bg-[#1c2433] border border-slate-700 rounded-xl p-5 shadow-xl relative overflow-hidden cursor-pointer" @click="router.push('/wallet')">
        <div class="absolute top-0 right-0 w-32 h-32 bg-primary/10 rounded-full -mr-16 -mt-16 blur-3xl"></div>
        <div class="flex justify-between items-start mb-4">
          <div>
            <p class="text-slate-400 text-xs font-medium uppercase tracking-widest">Available Balance</p>
            <h3 class="text-3xl font-bold mt-1 text-white">${{ walletStore.balance?.balance?.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) || '0.00' }}</h3>
          </div>
          <div class="bg-green-500/20 text-green-500 px-2 py-1 rounded text-[10px] font-bold">
            +12.5% This Month
          </div>
        </div>
        <div class="flex gap-3">
          <button class="flex-1 bg-primary hover:bg-primary/90 text-white font-bold py-3 rounded-lg flex items-center justify-center gap-2 transition-colors" @click="router.push('/wallet')">
            <span class="material-symbols-outlined text-lg">add_circle</span>
            Deposit
          </button>
          <button class="flex-1 bg-slate-700 hover:bg-slate-600 text-white font-bold py-3 rounded-lg flex items-center justify-center gap-2 transition-colors" @click="router.push('/wallet')">
            <span class="material-symbols-outlined text-lg">payments</span>
            Withdraw
          </button>
        </div>
      </div>
    </section>

    <!-- Followed Teams -->
    <section class="py-4">
      <div class="flex items-center justify-between px-6 mb-3">
        <h3 class="font-bold text-sm uppercase tracking-wider text-slate-400">Following Teams</h3>
        <button class="text-primary text-xs font-bold">Manage</button>
      </div>
      <div class="flex gap-4 overflow-x-auto px-6 no-scrollbar pb-2">
        <div v-for="team in userStore.followedTeams" :key="team.id" class="flex flex-col items-center gap-2 min-w-[70px]">
          <div class="w-14 h-14 bg-white rounded-full flex items-center justify-center p-2 shadow-md">
            <img v-if="team.teamLogo" :src="team.teamLogo" :alt="team.teamName" class="w-10 h-10 rounded-full object-contain" />
            <div v-else class="w-10 h-10 bg-slate-200 rounded-full"></div>
          </div>
          <span class="text-[11px] font-medium truncate w-full text-center">{{ team.teamName }}</span>
        </div>
        <div class="flex flex-col items-center gap-2 min-w-[70px]">
          <button class="w-14 h-14 rounded-full border-2 border-dashed border-slate-500 flex items-center justify-center">
            <span class="material-symbols-outlined text-slate-500">add</span>
          </button>
          <span class="text-[11px] font-medium text-slate-500">Add</span>
        </div>
      </div>
    </section>

    <!-- Settings Group: Notifications -->
    <section class="px-6 py-4">
      <h3 class="font-bold text-sm uppercase tracking-wider text-slate-400 mb-3 ml-2">Notifications</h3>
      <div class="bg-[#1c2433] border border-slate-700 rounded-xl overflow-hidden">
        <div class="flex items-center justify-between p-4 border-b border-slate-700">
          <div class="flex items-center gap-3">
            <div class="w-8 h-8 rounded bg-orange-500/20 text-orange-500 flex items-center justify-center">
              <span class="material-symbols-outlined text-xl">sports_score</span>
            </div>
            <div>
              <p class="text-sm font-medium">Match Start Alerts</p>
              <p class="text-[10px] text-slate-500">15 mins before kickoff</p>
            </div>
          </div>
          <label class="relative inline-flex items-center cursor-pointer">
            <input v-model="matchAlerts" type="checkbox" class="sr-only ios-toggle" />
            <div class="w-11 h-6 bg-slate-700 rounded-full ios-toggle-label after:content-[''] after:absolute after:top-0.5 after:left-[2px] after:bg-white after:rounded-full after:h-5 after:w-5 after:transition-all"></div>
          </label>
        </div>
        <div class="flex items-center justify-between p-4">
          <div class="flex items-center gap-3">
            <div class="w-8 h-8 rounded bg-primary/20 text-primary flex items-center justify-center">
              <span class="material-symbols-outlined text-xl">notifications_active</span>
            </div>
            <div>
              <p class="text-sm font-medium">Goal Updates</p>
              <p class="text-[10px] text-slate-500">Instant push notifications</p>
            </div>
          </div>
          <label class="relative inline-flex items-center cursor-pointer">
            <input v-model="goalUpdates" type="checkbox" class="sr-only ios-toggle" />
            <div class="w-11 h-6 bg-slate-700 rounded-full ios-toggle-label after:content-[''] after:absolute after:top-0.5 after:left-[2px] after:bg-white after:rounded-full after:h-5 after:w-5 after:transition-all"></div>
          </label>
        </div>
      </div>
    </section>

    <!-- Settings Group: Support -->
    <section class="px-6 py-4">
      <h3 class="font-bold text-sm uppercase tracking-wider text-slate-400 mb-3 ml-2">Support</h3>
      <div class="bg-[#1c2433] border border-slate-700 rounded-xl overflow-hidden">
        <button class="w-full flex items-center justify-between p-4 border-b border-slate-700 hover:bg-slate-800/50 transition-colors">
          <div class="flex items-center gap-3">
            <div class="w-8 h-8 rounded bg-blue-400/20 text-blue-400 flex items-center justify-center">
              <span class="material-symbols-outlined text-xl">help_center</span>
            </div>
            <span class="text-sm font-medium">Help Center</span>
          </div>
          <span class="material-symbols-outlined text-slate-500">chevron_right</span>
        </button>
        <button class="w-full flex items-center justify-between p-4 hover:bg-slate-800/50 transition-colors">
          <div class="flex items-center gap-3">
            <div class="w-8 h-8 rounded bg-emerald-400/20 text-emerald-400 flex items-center justify-center">
              <span class="material-symbols-outlined text-xl">chat</span>
            </div>
            <span class="text-sm font-medium">Live Support</span>
          </div>
          <span class="material-symbols-outlined text-slate-500">chevron_right</span>
        </button>
      </div>
    </section>

    <!-- Danger Zone -->
    <section class="px-6 py-8">
      <button class="w-full flex items-center justify-center gap-2 p-4 text-red-500 font-bold bg-red-500/10 rounded-xl hover:bg-red-500/20 transition-colors border border-red-500/30">
        <span class="material-symbols-outlined">logout</span>
        Logout
      </button>
      <p class="text-center text-slate-600 text-xs mt-6">Version 1.0.0</p>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useUserStore } from '@/store/user'
import { useWalletStore } from '@/store/wallet'

const router = useRouter()
const userStore = useUserStore()
const walletStore = useWalletStore()

const matchAlerts = ref(true)
const goalUpdates = ref(true)

onMounted(() => {
  userStore.fetchUser()
  userStore.fetchFollowedTeams()
  walletStore.fetchBalance()
})
</script>
