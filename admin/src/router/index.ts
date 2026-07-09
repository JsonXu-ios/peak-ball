import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/store/auth'

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/LoginView.vue'),
    meta: { requiresAuth: false },
  },
  {
    path: '/',
    component: () => import('@/layouts/AdminLayout.vue'),
    meta: { requiresAuth: true },
    redirect: '/dashboard',
    children: [
      {
        path: 'dashboard',
        name: 'Dashboard',
        component: () => import('@/views/dashboard/DashboardView.vue'),
        meta: { title: '仪表盘', icon: 'mdi-view-dashboard' },
      },
      // System Management
      {
        path: 'system/users',
        name: 'UserManage',
        component: () => import('@/views/system/UserManage.vue'),
        meta: { title: '用户管理', icon: 'mdi-account-multiple' },
      },
      {
        path: 'system/roles',
        name: 'RoleManage',
        component: () => import('@/views/system/RoleManage.vue'),
        meta: { title: '角色管理', icon: 'mdi-shield-account' },
      },
      {
        path: 'system/menus',
        name: 'MenuManage',
        component: () => import('@/views/system/MenuManage.vue'),
        meta: { title: '菜单管理', icon: 'mdi-menu' },
      },
      {
        path: 'system/permissions',
        name: 'PermissionManage',
        component: () => import('@/views/system/PermissionManage.vue'),
        meta: { title: '权限管理', icon: 'mdi-key' },
      },
      {
        path: 'system/logs',
        name: 'OperationLog',
        component: () => import('@/views/system/OperationLog.vue'),
        meta: { title: '操作日志', icon: 'mdi-history' },
      },
      // Crawler Management
      {
        path: 'crawler/tasks',
        name: 'CrawlerTask',
        component: () => import('@/views/crawler/CrawlerTask.vue'),
        meta: { title: '爬虫任务', icon: 'mdi-robot' },
      },
      {
        path: 'crawler/logs',
        name: 'CrawlerLog',
        component: () => import('@/views/crawler/CrawlerLog.vue'),
        meta: { title: '爬虫日志', icon: 'mdi-text-box-outline' },
      },
      {
        path: 'crawler/sync',
        name: 'CrawlerSync',
        component: () => import('@/views/crawler/CrawlerSync.vue'),
        meta: { title: '数据同步', icon: 'mdi-sync' },
      },
      // Data Management
      {
        path: 'data/matches',
        name: 'MatchData',
        component: () => import('@/views/data/MatchData.vue'),
        meta: { title: '比赛数据', icon: 'mdi-soccer' },
      },
    ],
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/dashboard',
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

// Navigation guard
router.beforeEach(async (to, _from, next) => {
  const authStore = useAuthStore()

  if (to.meta.requiresAuth === false) {
    if (authStore.isLoggedIn && to.path === '/login') {
      next('/dashboard')
    } else {
      next()
    }
    return
  }

  if (!authStore.isLoggedIn) {
    next('/login')
    return
  }

  // Fetch user info if not loaded
  if (!authStore.user) {
    try {
      await authStore.fetchUserInfo()
    } catch {
      authStore.logout()
      next('/login')
      return
    }
  }

  next()
})

export default router
