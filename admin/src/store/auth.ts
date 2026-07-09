import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { login as loginApi, getUserInfo as getUserInfoApi } from '@/api'
import router from '@/router'

export interface UserInfo {
  id: number
  username: string
  nickname: string
  email: string
  avatar: string
  status: number
  roles: Array<{ id: number; name: string; code: string }>
}

export interface MenuItem {
  id: number
  parent_id: number
  name: string
  title: string
  icon: string
  path: string
  component: string
  sort: number
  hidden: boolean
  children?: MenuItem[]
}

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string>(localStorage.getItem('admin_token') || '')
  const user = ref<UserInfo | null>(null)
  const menus = ref<MenuItem[]>([])
  const permissions = ref<string[]>([])

  const isLoggedIn = computed(() => !!token.value)

  async function doLogin(username: string, password: string) {
    const { data } = await loginApi({ username, password })
    token.value = data.token
    user.value = data.user
    localStorage.setItem('admin_token', data.token)
  }

  async function fetchUserInfo() {
    const { data } = await getUserInfoApi()
    user.value = data.user
    menus.value = data.menus || []
    permissions.value = data.permissions || []
  }

  function logout() {
    token.value = ''
    user.value = null
    menus.value = []
    permissions.value = []
    localStorage.removeItem('admin_token')
    router.push('/login')
  }

  function hasPermission(code: string): boolean {
    // super admin has all permissions
    if (user.value?.roles?.some((r) => r.code === 'super_admin')) return true
    return permissions.value.includes(code)
  }

  return {
    token,
    user,
    menus,
    permissions,
    isLoggedIn,
    doLogin,
    fetchUserInfo,
    logout,
    hasPermission,
  }
})
