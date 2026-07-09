<template>
  <div class="min-h-screen pb-24">
    <!-- Header -->
    <header class="px-5 py-4 flex items-center justify-between max-w-md mx-auto">
      <div class="flex items-center gap-3">
        <button
          class="w-10 h-10 flex items-center justify-center rounded-full bg-primary/20 text-primary"
          @click="router.back()"
        >
          <span class="material-symbols-outlined">chevron_left</span>
        </button>
        <h1 class="text-xl font-bold tracking-tight">Notifications</h1>
      </div>
      <button class="w-10 h-10 flex items-center justify-center rounded-full bg-primary/20 text-primary" @click="router.push('/profile')">
        <span class="material-symbols-outlined">settings</span>
      </button>
    </header>

    <div class="max-w-md mx-auto">
      <!-- DND Toggle -->
      <div class="px-5 mb-4">
        <div class="bg-primary/10 rounded-xl p-4 flex items-center justify-between border border-primary/20">
          <div class="flex items-center gap-3">
            <div class="w-10 h-10 rounded-lg bg-primary/20 flex items-center justify-center text-primary">
              <span class="material-symbols-outlined">notifications_off</span>
            </div>
            <div>
              <h3 class="text-sm font-semibold">Do Not Disturb</h3>
              <p class="text-xs text-slate-400">Silence all match alerts</p>
            </div>
          </div>
          <label class="relative inline-flex items-center cursor-pointer">
            <input v-model="dndEnabled" type="checkbox" class="sr-only peer" />
            <div class="w-11 h-6 bg-slate-700 rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-primary" />
          </label>
        </div>
      </div>

      <!-- Tabs -->
      <nav class="px-5 mb-6">
        <div class="flex bg-slate-800 p-1 rounded-xl">
          <button
            v-for="tab in tabs"
            :key="tab.value"
            class="flex-1 py-2 text-xs font-semibold rounded-lg transition-all"
            :class="activeTab === tab.value ? 'bg-primary text-white shadow-sm' : 'text-slate-400'"
            @click="switchTab(tab.value)"
          >
            {{ tab.label }}
          </button>
        </div>
      </nav>

      <!-- Content -->
      <main class="px-5 space-y-4">
        <!-- Loading -->
        <div v-if="notificationStore.loading" class="flex justify-center py-20">
          <span class="material-symbols-outlined text-primary text-4xl animate-spin">progress_activity</span>
        </div>

        <template v-else>
          <!-- Mark all read -->
          <div class="flex items-center justify-between pt-2" v-if="notificationStore.notifications.length">
            <h2 class="text-xs font-bold uppercase tracking-wider text-slate-400">
              {{ activeTab === 'match' ? 'Match Alerts' : activeTab === 'expert' ? 'Expert Updates' : 'System' }}
            </h2>
            <button
              class="text-xs text-primary font-medium"
              @click="notificationStore.markAllAsRead()"
            >
              Mark all read
            </button>
          </div>

          <!-- Notification List -->
          <div
            v-for="notification in notificationStore.notifications"
            :key="notification.id"
            class="relative group"
            @click="notificationStore.markAsRead(notification.id)"
          >
            <!-- Unread indicator -->
            <div v-if="!notification.isRead" class="absolute -left-1 top-4 bottom-4 w-1 bg-primary rounded-full" />

            <div
              class="p-4 rounded-xl border shadow-sm"
              :class="notification.isRead
                ? 'bg-slate-800/50 border-slate-700/50 opacity-80'
                : 'bg-slate-800/50 border-slate-700/50'"
            >
              <div class="flex justify-between items-start mb-2">
                <div class="flex items-center gap-2">
                  <span
                    class="material-symbols-outlined text-xl"
                    :class="notificationIconColor(notification.type)"
                  >
                    {{ notificationIcon(notification.type) }}
                  </span>
                  <span
                    class="text-xs font-bold uppercase"
                    :class="notificationIconColor(notification.type)"
                  >
                    {{ notification.title }}
                  </span>
                </div>
                <span class="text-[10px] text-slate-400">{{ formatTime(notification.createdAt) }}</span>
              </div>
              <h3 class="font-bold text-sm mb-1">{{ notification.title }}</h3>
              <p class="text-xs text-slate-400 leading-relaxed">{{ notification.message }}</p>

              <!-- Action buttons for match alerts -->
              <div v-if="notification.type === 'goal'" class="mt-3 flex gap-2">
                <button class="px-3 py-1.5 bg-primary/20 text-primary text-[10px] font-bold rounded-full uppercase tracking-tight">
                  View Highlights
                </button>
                <button
                  v-if="notification.matchId"
                  class="px-3 py-1.5 bg-slate-700/50 text-slate-300 text-[10px] font-bold rounded-full uppercase tracking-tight"
                  @click.stop="router.push(`/match/${notification.matchId}`)"
                >
                  Match Center
                </button>
              </div>
            </div>
          </div>

          <!-- Empty State -->
          <div
            v-if="!notificationStore.notifications.length"
            class="flex flex-col items-center justify-center py-20 text-slate-500"
          >
            <span class="material-symbols-outlined text-4xl mb-2">notifications_none</span>
            <p class="text-sm">No notifications yet</p>
          </div>
        </template>
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useNotificationStore } from '@/store/notification'

const router = useRouter()
const notificationStore = useNotificationStore()

const dndEnabled = ref(false)

const tabs = [
  { label: 'Match Alerts', value: 'match' },
  { label: 'Experts', value: 'expert' },
  { label: 'System', value: 'system' },
]
const activeTab = ref('match')

function switchTab(tab: string) {
  activeTab.value = tab
  notificationStore.fetchNotifications(tab)
}

function notificationIcon(type: string): string {
  switch (type) {
    case 'goal': return 'sports_soccer'
    case 'red_card': return 'square'
    case 'expert_tip': return 'person'
    case 'reward': return 'emoji_events'
    case 'lineup': return 'groups'
    default: return 'notifications'
  }
}

function notificationIconColor(type: string): string {
  switch (type) {
    case 'goal': return 'text-primary'
    case 'red_card': return 'text-red-500'
    case 'expert_tip': return 'text-slate-500'
    case 'reward': return 'text-amber-500'
    case 'lineup': return 'text-primary'
    default: return 'text-slate-400'
  }
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
  notificationStore.fetchNotifications('match')
  notificationStore.fetchUnreadCount()
})
</script>
