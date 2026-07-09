<template>
  <div class="min-h-screen bg-background-dark text-white font-display">
    <router-view v-slot="{ Component }">
      <transition name="fade" mode="out-in">
        <component :is="Component" :key="route.path" />
      </transition>
    </router-view>

    <!-- Bottom Navigation Bar (iOS Style) -->
    <nav
      v-if="showBottomNav"
      class="fixed bottom-0 left-0 right-0 bg-background-dark/95 backdrop-blur-xl border-t border-slate-800 px-6 pt-3 pb-8 z-50"
    >
      <div class="flex items-center justify-between max-w-md mx-auto">
        <button
          class="flex flex-col items-center gap-1"
          :class="activeTab === 'home' ? 'text-primary' : 'text-slate-500'"
          @click="goTo('/', 'home')"
        >
          <span class="material-symbols-outlined" :style="activeTab === 'home' ? 'font-variation-settings: \'FILL\' 1' : ''">home</span>
          <span class="text-[10px] font-bold uppercase tracking-tighter">Home</span>
        </button>

        <button
          class="flex flex-col items-center gap-1"
          :class="activeTab === 'dashboard' ? 'text-primary' : 'text-slate-500'"
          @click="goTo('/dashboard', 'dashboard')"
        >
          <span class="material-symbols-outlined">analytics</span>
          <span class="text-[10px] font-bold uppercase tracking-tighter">Analysis</span>
        </button>

        <!-- Center FAB -->
        <div class="relative -top-8">
          <button
            class="size-14 bg-primary rounded-full flex items-center justify-center text-white shadow-lg shadow-primary/40 border-4 border-background-dark"
            @click="goTo('/analysis', 'analysis')"
          >
            <span class="material-symbols-outlined text-3xl">psychology</span>
          </button>
        </div>

        <button
          class="flex flex-col items-center gap-1"
          :class="activeTab === 'expert' ? 'text-primary' : 'text-slate-500'"
          @click="goTo('/expert', 'expert')"
        >
          <span class="material-symbols-outlined">military_tech</span>
          <span class="text-[10px] font-bold uppercase tracking-tighter">Expert</span>
        </button>

        <button
          class="flex flex-col items-center gap-1"
          :class="activeTab === 'profile' ? 'text-primary' : 'text-slate-500'"
          @click="goTo('/profile', 'profile')"
        >
          <span class="material-symbols-outlined">person</span>
          <span class="text-[10px] font-bold uppercase tracking-tighter">Profile</span>
        </button>
      </div>
    </nav>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

const route = useRoute()
const router = useRouter()
const activeTab = ref('home')

const showBottomNav = computed(() => {
  return !route.path.startsWith('/match/')
})

watch(
  () => route.path,
  (path) => {
    if (path === '/') activeTab.value = 'home'
    else if (path === '/analysis') activeTab.value = 'analysis'
    else if (path === '/dashboard') activeTab.value = 'dashboard'
    else if (path === '/expert') activeTab.value = 'expert'
    else if (path === '/leaderboard') activeTab.value = 'leaderboard'
    else if (path === '/profile') activeTab.value = 'profile'
    else if (path === '/news') activeTab.value = 'news'
    else if (path === '/league') activeTab.value = 'league'
    else if (path === '/search') activeTab.value = 'search'
    else if (path === '/wallet') activeTab.value = 'wallet'
    else if (path === '/notifications') activeTab.value = 'notifications'
  },
  { immediate: true },
)

function goTo(path: string, tab: string) {
  activeTab.value = tab
  router.push(path).then(() => window.scrollTo({ top: 0 }))
}
</script>

<style>
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
