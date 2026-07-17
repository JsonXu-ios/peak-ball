import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      name: 'home',
      component: () => import('../views/match/MatchClassicHome.vue')
    },
    {
      path: '/analysis',
      name: 'analysis-home',
      component: () => import('../views/match/MatchListHome.vue')
    },
    {
      path: '/picks',
      name: 'pick-entry',
      component: () => import('../views/picks/PickEntryView.vue')
    },
    {
      path: '/dashboard',
      name: 'dashboard',
      component: () => import('../views/dashboard/DashboardView.vue')
    },
    {
      path: '/leaderboard',
      name: 'leaderboard',
      component: () => import('../views/leaderboard/LeaderboardView.vue')
    },
    {
      path: '/match/:id',
      name: 'match-detail',
      component: () => import('../views/detail/MatchDetailView.vue')
    },
    {
      path: '/expert',
      name: 'expert',
      component: () => import('../views/expert/ExpertCommunityView.vue')
    },
    {
      path: '/profile',
      name: 'profile',
      component: () => import('../views/profile/ProfileView.vue')
    },
    {
      path: '/news',
      name: 'news',
      component: () => import('../views/news/NewsView.vue')
    },
    {
      path: '/league',
      name: 'league',
      component: () => import('../views/league/LeagueStandingsView.vue')
    },
    {
      path: '/search',
      name: 'search',
      component: () => import('../views/search/SearchView.vue')
    },
    {
      path: '/wallet',
      name: 'wallet',
      component: () => import('../views/wallet/WalletView.vue')
    },
    {
      path: '/notifications',
      name: 'notifications',
      component: () => import('../views/notification/NotificationView.vue')
    }
  ]
})

export default router
