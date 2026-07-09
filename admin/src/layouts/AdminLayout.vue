<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/store/auth'
import { useTheme } from 'vuetify'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const theme = useTheme()

const drawer = ref(true)
const rail = ref(false)

const isDark = computed(() => theme.global.current.value.dark)

function toggleTheme() {
  theme.global.name.value = isDark.value ? 'light' : 'dark'
}

// Sidebar menu items — built from routes (static for now, dynamic can come from server menus)
const menuItems = computed(() => [
  {
    title: '仪表盘',
    icon: 'mdi-view-dashboard',
    to: '/dashboard',
  },
  {
    title: '系统管理',
    icon: 'mdi-cog',
    children: [
      { title: '用户管理', icon: 'mdi-account-multiple', to: '/system/users' },
      { title: '角色管理', icon: 'mdi-shield-account', to: '/system/roles' },
      { title: '菜单管理', icon: 'mdi-menu', to: '/system/menus' },
      { title: '权限管理', icon: 'mdi-key', to: '/system/permissions' },
      { title: '操作日志', icon: 'mdi-history', to: '/system/logs' },
    ],
  },
  {
    title: '爬虫管理',
    icon: 'mdi-spider-web',
    children: [
      { title: '爬虫任务', icon: 'mdi-robot', to: '/crawler/tasks' },
      { title: '爬虫日志', icon: 'mdi-text-box-outline', to: '/crawler/logs' },
      { title: '数据同步', icon: 'mdi-sync', to: '/crawler/sync' },
    ],
  },
  {
    title: '数据管理',
    icon: 'mdi-database',
    children: [
      { title: '比赛数据', icon: 'mdi-soccer', to: '/data/matches' },
    ],
  },
])

const pageTitle = computed(() => {
  return (route.meta.title as string) || '管理后台'
})

function handleLogout() {
  authStore.logout()
}

function navigateTo(path: string) {
  router.push(path)
}
</script>

<template>
  <v-layout class="rounded rounded-md">
    <!-- Sidebar Navigation -->
    <v-navigation-drawer
      v-model="drawer"
      :rail="rail"
      permanent
      color="surface"
    >
      <!-- Logo Area -->
      <v-list-item
        :prepend-icon="rail ? 'mdi-soccer' : undefined"
        class="py-4"
        @click="rail = !rail"
      >
        <template v-if="!rail">
          <div class="d-flex align-center">
            <v-icon color="primary" size="32" class="mr-3">mdi-soccer</v-icon>
            <div>
              <div class="text-subtitle-1 font-weight-bold">足球数据</div>
              <div class="text-caption text-medium-emphasis">管理后台</div>
            </div>
          </div>
        </template>
      </v-list-item>

      <v-divider />

      <!-- Menu Items -->
      <v-list density="compact" nav>
        <template v-for="item in menuItems" :key="item.title">
          <!-- Menu group with children -->
          <v-list-group v-if="item.children" :value="item.title">
            <template #activator="{ props }">
              <v-list-item
                v-bind="props"
                :prepend-icon="item.icon"
                :title="item.title"
              />
            </template>
            <v-list-item
              v-for="child in item.children"
              :key="child.to"
              :prepend-icon="child.icon"
              :title="child.title"
              :value="child.to"
              :active="route.path === child.to"
              @click="navigateTo(child.to)"
            />
          </v-list-group>

          <!-- Single menu item -->
          <v-list-item
            v-else
            :prepend-icon="item.icon"
            :title="item.title"
            :value="item.to"
            :active="route.path === item.to"
            @click="navigateTo(item.to!)"
          />
        </template>
      </v-list>
    </v-navigation-drawer>

    <!-- App Bar -->
    <v-app-bar flat density="compact" color="surface">
      <v-app-bar-nav-icon @click="drawer = !drawer" />

      <v-toolbar-title class="text-subtitle-1">
        {{ pageTitle }}
      </v-toolbar-title>

      <v-spacer />

      <!-- Theme Toggle -->
      <v-btn icon @click="toggleTheme">
        <v-icon>{{ isDark ? 'mdi-weather-sunny' : 'mdi-weather-night' }}</v-icon>
      </v-btn>

      <!-- User Menu -->
      <v-menu>
        <template #activator="{ props }">
          <v-btn v-bind="props" variant="text" class="ml-2">
            <v-avatar size="32" color="primary" class="mr-2">
              <span class="text-body-2">{{ authStore.user?.nickname?.charAt(0) || 'A' }}</span>
            </v-avatar>
            <span class="text-body-2">{{ authStore.user?.nickname || authStore.user?.username }}</span>
            <v-icon end>mdi-chevron-down</v-icon>
          </v-btn>
        </template>
        <v-list density="compact">
          <v-list-item prepend-icon="mdi-account" title="个人信息" />
          <v-divider />
          <v-list-item
            prepend-icon="mdi-logout"
            title="退出登录"
            @click="handleLogout"
          />
        </v-list>
      </v-menu>
    </v-app-bar>

    <!-- Main Content -->
    <v-main>
      <v-container fluid class="pa-6">
        <router-view v-slot="{ Component }">
          <transition name="fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </v-container>
    </v-main>
  </v-layout>
</template>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
