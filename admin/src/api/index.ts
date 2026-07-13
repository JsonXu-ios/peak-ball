import api from './request'

// ---- Auth ----
export const login = (data: { username: string; password: string }) =>
  api.post('/login', data)

export const getUserInfo = () =>
  api.get('/admin/user/info')

// ---- User Management ----
export const getUsers = (params?: Record<string, unknown>) =>
  api.get('/admin/users', { params })

export const createUser = (data: Record<string, unknown>) =>
  api.post('/admin/users', data)

export const updateUser = (id: number, data: Record<string, unknown>) =>
  api.put(`/admin/users/${id}`, data)

export const deleteUser = (id: number) =>
  api.delete(`/admin/users/${id}`)

export const updateUserStatus = (id: number, status: number) =>
  api.patch(`/admin/users/${id}/status`, { status })

export const resetPassword = (id: number, password: string) =>
  api.patch(`/admin/users/${id}/password`, { password })

// ---- Role Management ----
export const getRoles = (params?: Record<string, unknown>) =>
  api.get('/admin/roles', { params })

export const createRole = (data: Record<string, unknown>) =>
  api.post('/admin/roles', data)

export const updateRole = (id: number, data: Record<string, unknown>) =>
  api.put(`/admin/roles/${id}`, data)

export const deleteRole = (id: number) =>
  api.delete(`/admin/roles/${id}`)

export const getRoleMenus = (id: number) =>
  api.get(`/admin/roles/${id}/menus`)

export const updateRoleMenus = (id: number, menuIds: number[]) =>
  api.put(`/admin/roles/${id}/menus`, { menu_ids: menuIds })

export const getRolePermissions = (id: number) =>
  api.get(`/admin/roles/${id}/permissions`)

export const updateRolePermissions = (id: number, permissionIds: number[]) =>
  api.put(`/admin/roles/${id}/permissions`, { permission_ids: permissionIds })

// ---- Menu Management ----
export const getMenus = () =>
  api.get('/admin/menus')

export const getMenuTree = () =>
  api.get('/admin/menus/tree')

export const createMenu = (data: Record<string, unknown>) =>
  api.post('/admin/menus', data)

export const updateMenu = (id: number, data: Record<string, unknown>) =>
  api.put(`/admin/menus/${id}`, data)

export const deleteMenu = (id: number) =>
  api.delete(`/admin/menus/${id}`)

// ---- Permission Management ----
export const getPermissions = (params?: Record<string, unknown>) =>
  api.get('/admin/permissions', { params })

export const createPermission = (data: Record<string, unknown>) =>
  api.post('/admin/permissions', data)

export const updatePermission = (id: number, data: Record<string, unknown>) =>
  api.put(`/admin/permissions/${id}`, data)

export const deletePermission = (id: number) =>
  api.delete(`/admin/permissions/${id}`)

// ---- Crawler Data ----
export const getCrawlerMatches = (params?: Record<string, unknown>) =>
  api.get('/admin/crawler/matches', { params })

export const getCrawlerMatchDetail = (id: string) =>
  api.get(`/admin/crawler/matches/${id}`)

export const deleteCrawlerMatch = (id: string) =>
  api.delete(`/admin/crawler/matches/${id}`)

export const syncCrawlerData = (data: { type: string; date?: string; match_id?: string; async?: boolean; force?: boolean }) =>
  api.post('/admin/crawler/sync', data)

export const getAnalysisRuleSnapshotInfo = () =>
  api.get('/admin/crawler/analysis-rule-snapshot')

export const getAnalysisRuleSnapshotData = () =>
  api.get('/admin/crawler/analysis-rule-snapshot/data')

export const generateAnalysisRuleSnapshot = () =>
  api.post('/admin/crawler/analysis-rule-snapshot/generate')

// ---- Crawler Tasks ----
export const getCrawlerTasks = () =>
  api.get('/admin/crawler/tasks')

export const createCrawlerTask = (data: Record<string, unknown>) =>
  api.post('/admin/crawler/tasks', data)

export const updateCrawlerTask = (id: number, data: Record<string, unknown>) =>
  api.put(`/admin/crawler/tasks/${id}`, data)

export const deleteCrawlerTask = (id: number) =>
  api.delete(`/admin/crawler/tasks/${id}`)

export const runCrawlerTask = (id: number, async = true) =>
  api.post(`/admin/crawler/tasks/${id}/run?async=${async}`)

export const toggleCrawlerTask = (id: number) =>
  api.patch(`/admin/crawler/tasks/${id}/toggle`)

// ---- Crawler Logs ----
export const getCrawlerLogs = (params?: Record<string, unknown>) =>
  api.get('/admin/crawler/logs', { params })

export const getCrawlerLogDetail = (id: number) =>
  api.get(`/admin/crawler/logs/${id}`)

// ---- Operation Logs ----
export const getOperationLogs = (params?: Record<string, unknown>) =>
  api.get('/admin/logs/operations', { params })

// ---- Dashboard ----
export const getDashboardStats = () =>
  api.get('/admin/dashboard/stats')

// ---- Match Statistics ----
export const getMatchStatistics = (params?: { start_date?: string; end_date?: string }) =>
  api.get('/admin/statistics/matches', { params })
