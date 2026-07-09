<template>
  <div class="min-h-screen pb-24">
    <!-- Top Navigation Bar -->
    <div class="sticky top-0 z-50 bg-background-dark/80 backdrop-blur-md border-b border-slate-800">
      <div class="flex items-center p-4 justify-between max-w-md mx-auto">
        <div class="flex items-center gap-3">
          <span class="material-symbols-outlined cursor-pointer" @click="router.back()">arrow_back_ios</span>
          <div>
            <h2 class="text-sm font-bold leading-tight">Expert Insights</h2>
            <p class="text-[10px] text-slate-500 uppercase tracking-widest">Community</p>
          </div>
        </div>
        <div class="flex gap-4">
          <button @click="router.push('/notifications')">
            <span class="material-symbols-outlined text-primary">notifications</span>
          </button>
          <button class="p-1 rounded-full hover:bg-slate-800">
            <span class="material-symbols-outlined">share</span>
          </button>
        </div>
      </div>
    </div>

    <div class="max-w-md mx-auto px-4 py-6">
      <!-- Expert Prdiction Card -->
      <div class="bg-primary rounded-2xl p-5 mb-6 text-white relative overflow-hidden">
        <div class="absolute top-0 right-0 -translate-y-1/2 translate-x-1/4 size-32 bg-white/10 rounded-full"></div>
        <div class="relative z-10">
          <div class="flex items-center gap-2 mb-3">
            <span class="material-symbols-outlined text-white text-xl">verified</span>
            <h3 class="font-bold">Expert Prediction</h3>
          </div>
          <p class="text-sm font-medium mb-4 opacity-90">
            "Our expert network covers major leagues. Check back for the latest insights and tips from top predictors."
          </p>
          <div class="flex items-center justify-between">
            <div>
              <p class="text-[10px] uppercase opacity-70 mb-0.5">Featured Tip</p>
              <p class="font-bold">Top Picks Today</p>
            </div>
            <button class="bg-white text-primary px-6 py-2 rounded-xl font-bold text-xs">View All</button>
          </div>
        </div>
      </div>

      <!-- Navigation Tabs -->
      <div class="flex border-b border-slate-800 mb-6">
        <button
          v-for="tab in tabs"
          :key="tab"
          class="flex-1 pb-3 text-sm font-medium border-b-2"
          :class="activeTab === tab
            ? 'font-bold border-primary text-primary'
            : 'text-slate-500 border-transparent'"
          @click="activeTab = tab"
        >
          {{ tab }}
        </button>
      </div>

      <!-- Loading -->
      <div v-if="expertStore.loading" class="flex justify-center py-12">
        <span class="material-symbols-outlined text-primary text-4xl animate-spin">progress_activity</span>
      </div>

      <!-- Expert List -->
      <div v-else class="space-y-4">
        <div
          v-for="expert in expertStore.experts"
          :key="expert.id"
          class="bg-slate-900/50 rounded-2xl p-5 border border-slate-800"
        >
          <div class="flex items-center gap-3 mb-4">
            <div class="size-12 rounded-full bg-slate-700 overflow-hidden border-2 border-primary/30">
              <img v-if="expert.avatar" :src="expert.avatar" :alt="expert.name" class="w-full h-full object-cover" />
              <div v-else class="w-full h-full flex items-center justify-center">
                <span class="material-symbols-outlined text-slate-400">person</span>
              </div>
            </div>
            <div class="flex-1">
              <div class="flex items-center gap-2">
                <span class="font-bold text-sm">{{ expert.name }}</span>
                <span v-if="expert.verified" class="material-symbols-outlined text-primary text-sm">verified</span>
              </div>
              <p class="text-[10px] text-slate-500">{{ expert.specialty }}</p>
            </div>
            <div class="text-right">
              <p class="text-sm font-bold text-primary">{{ expert.accuracy }}%</p>
              <p class="text-[10px] text-slate-500">Accuracy</p>
            </div>
          </div>

          <p class="text-sm text-slate-300 mb-4">{{ expert.specialty }}</p>

          <div class="flex items-center justify-between pt-3 border-t border-slate-800">
            <div class="flex items-center gap-4">
              <div class="flex items-center gap-1 text-slate-500">
                <span class="material-symbols-outlined text-sm">thumb_up</span>
                <span class="text-xs">{{ expert.followers }}</span>
              </div>
              <div class="flex items-center gap-1 text-slate-500">
                <span class="material-symbols-outlined text-sm">local_fire_department</span>
                <span class="text-xs">{{ expert.streak }} streak</span>
              </div>
            </div>
            <button class="text-primary text-xs font-bold flex items-center gap-1">
              View Full <span class="material-symbols-outlined text-sm">chevron_right</span>
            </button>
          </div>
        </div>

        <div v-if="!expertStore.experts.length" class="flex flex-col items-center py-12 text-slate-500">
          <span class="material-symbols-outlined text-4xl mb-2">groups</span>
          <p class="text-sm">暂无专家数据</p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useExpertStore } from '@/store/expert'

const router = useRouter()
const expertStore = useExpertStore()

const tabs = ['Trending', 'Following', 'Latest']
const activeTab = ref('Trending')

onMounted(() => {
  expertStore.fetchExperts()
})
</script>
