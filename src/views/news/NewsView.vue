<template>
  <div class="min-h-screen pb-24">
    <!-- Header -->
    <header class="sticky top-0 z-50 bg-background-dark/80 backdrop-blur-md border-b border-slate-800 px-5 pb-4 pt-4">
      <div class="flex justify-between items-end mb-4 max-w-md mx-auto">
        <h1 class="text-3xl font-bold tracking-tight">Football News</h1>
        <div class="flex gap-3">
          <button
            class="w-10 h-10 rounded-full bg-slate-800 flex items-center justify-center"
            @click="router.push('/search')"
          >
            <span class="material-symbols-outlined text-xl">search</span>
          </button>
          <button class="w-10 h-10 rounded-full bg-slate-800 flex items-center justify-center">
            <span class="material-symbols-outlined text-xl">tune</span>
          </button>
        </div>
      </div>
      <!-- Segmented Control -->
      <div class="p-1 bg-slate-800 rounded-xl flex max-w-md mx-auto">
        <button
          v-for="tab in tabs"
          :key="tab"
          class="flex-1 py-2 text-sm font-semibold rounded-lg transition-all"
          :class="activeTab === tab ? 'bg-primary shadow-sm text-white' : 'text-slate-400'"
          @click="switchTab(tab)"
        >
          {{ tab }}
        </button>
      </div>
    </header>

    <main class="px-5 max-w-md mx-auto">
      <!-- Loading -->
      <div v-if="newsStore.loading" class="flex justify-center py-20">
        <span class="material-symbols-outlined text-primary text-4xl animate-spin">progress_activity</span>
      </div>

      <template v-else>
        <!-- Latest Tab -->
        <template v-if="activeTab === 'Latest'">
          <!-- Featured Hero -->
          <section v-if="featuredNews" class="mb-8 mt-6">
            <div class="relative w-full aspect-[4/5] rounded-xl overflow-hidden shadow-2xl">
              <img
                :alt="featuredNews.title"
                :src="featuredNews.imageUrl || '/images/news_hero.jpg'"
                class="absolute inset-0 w-full h-full object-cover"
              />
              <div class="absolute inset-0 bg-gradient-to-t from-background-dark via-transparent to-transparent" />
              <div class="absolute bottom-0 left-0 p-5 w-full">
                <div class="flex items-center gap-2 mb-2">
                  <span
                    v-if="featuredNews.isHot"
                    class="bg-primary text-white text-[10px] font-bold px-2 py-0.5 rounded-full uppercase tracking-widest"
                  >Breaking</span>
                  <span class="text-slate-300 text-xs">{{ formatTime(featuredNews.createdAt) }}</span>
                </div>
                <h2 class="text-2xl font-bold leading-tight text-white mb-2">{{ featuredNews.title }}</h2>
                <p class="text-slate-300 text-sm line-clamp-2">{{ featuredNews.summary }}</p>
              </div>
            </div>
          </section>

          <!-- Latest News Grid -->
          <section class="mb-8">
            <h3 class="text-lg font-bold mb-4">Latest Stories</h3>
            <div class="grid grid-cols-2 gap-4">
              <div v-for="item in regularNews" :key="item.id" class="group cursor-pointer">
                <div class="aspect-square rounded-xl overflow-hidden mb-2">
                  <img
                    :alt="item.title"
                    :src="item.imageUrl || '/images/news_default.jpg'"
                    class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-300"
                  />
                </div>
                <span class="text-[10px] font-bold text-slate-500 uppercase tracking-wider">{{ item.category }}</span>
                <h4 class="text-sm font-bold line-clamp-2 mt-1">{{ item.title }}</h4>
              </div>
            </div>
          </section>
        </template>

        <!-- Transfers Tab -->
        <template v-if="activeTab === 'Transfers'">
          <section class="mb-8 mt-6">
            <h3 class="text-lg font-bold mb-4">Transfer Rumors</h3>
            <div class="space-y-4">
              <div
                v-for="rumor in newsStore.transferRumors"
                :key="rumor.id"
                class="bg-slate-800/50 rounded-xl p-4 border border-slate-700/50"
              >
                <div class="flex justify-between items-start mb-4">
                  <div class="flex -space-x-2">
                    <div class="w-10 h-10 rounded-full border-2 border-slate-800 bg-slate-200 overflow-hidden">
                      <img :alt="rumor.fromClub" :src="rumor.fromClubLogo || '/images/club_placeholder.png'" class="w-full h-full object-cover" />
                    </div>
                    <div class="w-10 h-10 rounded-full border-2 border-slate-800 bg-primary flex items-center justify-center">
                      <span class="material-symbols-outlined text-white text-xs">trending_flat</span>
                    </div>
                    <div class="w-10 h-10 rounded-full border-2 border-slate-800 bg-slate-200 overflow-hidden">
                      <img :alt="rumor.toClub" :src="rumor.toClubLogo || '/images/club_placeholder.png'" class="w-full h-full object-cover" />
                    </div>
                  </div>
                  <div class="text-right">
                    <div class="text-[10px] uppercase font-bold text-slate-500">Value</div>
                    <div class="text-sm font-bold text-primary">{{ rumor.value }}</div>
                  </div>
                </div>
                <h4 class="font-bold text-base mb-1">{{ rumor.playerName }}</h4>
                <p class="text-xs text-slate-500 mb-4">{{ rumor.fromClub }} → {{ rumor.toClub }}</p>
                <div class="space-y-1.5">
                  <div class="flex justify-between text-[10px] font-bold uppercase tracking-wider">
                    <span>Trust Level</span>
                    <span :class="trustColor(rumor.trustLevel)">{{ rumor.trustLevel }}% ({{ rumor.tier }})</span>
                  </div>
                  <div class="w-full h-1.5 bg-slate-700 rounded-full overflow-hidden">
                    <div
                      class="h-full rounded-full"
                      :class="trustBgColor(rumor.trustLevel)"
                      :style="{ width: rumor.trustLevel + '%' }"
                    />
                  </div>
                </div>
              </div>
            </div>
          </section>
        </template>

        <!-- Official Tab -->
        <template v-if="activeTab === 'Official'">
          <section class="mb-8 mt-6">
            <h3 class="text-lg font-bold mb-4">Official Announcements</h3>
            <div class="space-y-4">
              <div
                v-for="item in officialNews"
                :key="item.id"
                class="flex gap-4 bg-slate-900/40 p-3 rounded-xl border border-slate-800"
              >
                <div class="relative w-20 h-20 flex-shrink-0 rounded-lg overflow-hidden">
                  <img :alt="item.title" :src="item.imageUrl || '/images/news_default.jpg'" class="w-full h-full object-cover" />
                  <div class="absolute top-1 right-1 bg-white rounded-full flex items-center justify-center p-0.5">
                    <span class="material-symbols-outlined text-primary text-[10px]">verified</span>
                  </div>
                </div>
                <div class="flex flex-col justify-center">
                  <span class="text-[10px] font-bold text-primary uppercase mb-1">{{ item.club || item.source }}</span>
                  <h4 class="font-bold text-sm leading-snug mb-1">{{ item.title }}</h4>
                  <span class="text-xs text-slate-500">Official • {{ formatTime(item.createdAt) }}</span>
                </div>
              </div>
            </div>
          </section>
        </template>
      </template>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useNewsStore } from '@/store/news'
import type { News } from '@/types/news'

const router = useRouter()
const newsStore = useNewsStore()

const tabs = ['Latest', 'Transfers', 'Official']
const activeTab = ref('Latest')

const featuredNews = computed<News | undefined>(() => {
  return newsStore.newsList.find((n) => n.isHot) ?? newsStore.newsList[0]
})

const regularNews = computed(() => {
  return newsStore.newsList.filter((n) => n.id !== featuredNews.value?.id)
})

const officialNews = computed(() => {
  return newsStore.newsList.filter((n) => n.category === 'official')
})

function switchTab(tab: string) {
  activeTab.value = tab
  if (tab === 'Transfers') {
    newsStore.fetchTransferRumors()
  } else if (tab === 'Official') {
    newsStore.fetchNews('official')
  } else {
    newsStore.fetchNews()
  }
}

function trustColor(level: number): string {
  if (level >= 80) return 'text-primary'
  if (level >= 50) return 'text-amber-500'
  return 'text-red-400'
}

function trustBgColor(level: number): string {
  if (level >= 80) return 'bg-primary'
  if (level >= 50) return 'bg-amber-500'
  return 'bg-red-400'
}

function formatTime(dateStr: string): string {
  if (!dateStr) return ''
  const now = Date.now()
  const diff = now - new Date(dateStr).getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 60) return `${mins} mins ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours} hours ago`
  return `${Math.floor(hours / 24)} days ago`
}

onMounted(() => {
  newsStore.fetchNews()
})
</script>
