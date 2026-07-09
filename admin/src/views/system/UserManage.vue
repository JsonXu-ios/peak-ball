<script setup lang="ts">
import { ref, onMounted, reactive } from 'vue'
import {
  getUsers,
  createUser,
  updateUser,
  deleteUser,
  updateUserStatus,
  resetPassword,
  getRoles,
} from '@/api'

interface Role {
  id: number
  name: string
  code: string
}

interface User {
  id: number
  username: string
  nickname: string
  email: string
  phone: string
  status: number
  created_at: string
  roles: Role[]
}

const users = ref<User[]>([])
const roles = ref<Role[]>([])
const total = ref(0)
const loading = ref(false)
const keyword = ref('')
const page = ref(1)
const pageSize = ref(10)

// Dialog state
const dialog = ref(false)
const dialogTitle = ref('新增用户')
const editingId = ref<number | null>(null)
const formData = reactive({
  username: '',
  password: '',
  nickname: '',
  email: '',
  phone: '',
  role_ids: [] as number[],
})

const resetDialog = ref(false)
const resetUserId = ref(0)
const newPassword = ref('')

const deleteDialog = ref(false)
const deleteUserId = ref(0)

async function fetchUsers() {
  loading.value = true
  try {
    const { data } = await getUsers({
      page: page.value,
      page_size: pageSize.value,
      keyword: keyword.value,
    })
    users.value = data.list || []
    total.value = data.total || 0
  } finally {
    loading.value = false
  }
}

async function fetchRoles() {
  const { data } = await getRoles()
  roles.value = data.list || []
}

onMounted(() => {
  fetchUsers()
  fetchRoles()
})

function openCreate() {
  dialogTitle.value = '新增用户'
  editingId.value = null
  Object.assign(formData, {
    username: '',
    password: '',
    nickname: '',
    email: '',
    phone: '',
    role_ids: [],
  })
  dialog.value = true
}

function openEdit(user: User) {
  dialogTitle.value = '编辑用户'
  editingId.value = user.id
  Object.assign(formData, {
    username: user.username,
    password: '',
    nickname: user.nickname,
    email: user.email,
    phone: user.phone,
    role_ids: user.roles?.map((r) => r.id) || [],
  })
  dialog.value = true
}

async function handleSave() {
  try {
    if (editingId.value) {
      await updateUser(editingId.value, {
        nickname: formData.nickname,
        email: formData.email,
        phone: formData.phone,
        role_ids: formData.role_ids,
      })
    } else {
      await createUser({ ...formData })
    }
    dialog.value = false
    fetchUsers()
  } catch {
    // error handled by interceptor
  }
}

async function handleToggleStatus(user: User) {
  const newStatus = user.status === 1 ? 0 : 1
  await updateUserStatus(user.id, newStatus)
  fetchUsers()
}

function openResetPassword(id: number) {
  resetUserId.value = id
  newPassword.value = ''
  resetDialog.value = true
}

async function handleResetPassword() {
  await resetPassword(resetUserId.value, newPassword.value)
  resetDialog.value = false
}

function openDelete(id: number) {
  deleteUserId.value = id
  deleteDialog.value = true
}

async function handleDelete() {
  await deleteUser(deleteUserId.value)
  deleteDialog.value = false
  fetchUsers()
}

function handleSearch() {
  page.value = 1
  fetchUsers()
}
</script>

<template>
  <div>
    <div class="d-flex align-center justify-space-between mb-4">
      <h2 class="text-h5 font-weight-bold">用户管理</h2>
      <v-btn color="primary" prepend-icon="mdi-plus" @click="openCreate">
        新增用户
      </v-btn>
    </div>

    <!-- Search Bar -->
    <v-card class="mb-4">
      <v-card-text>
        <v-row>
          <v-col cols="12" md="4">
            <v-text-field
              v-model="keyword"
              label="搜索用户"
              prepend-inner-icon="mdi-magnify"
              placeholder="用户名/昵称/邮箱"
              clearable
              hide-details
              @keyup.enter="handleSearch"
              @click:clear="keyword = ''; handleSearch()"
            />
          </v-col>
          <v-col cols="12" md="2">
            <v-btn color="primary" @click="handleSearch">搜索</v-btn>
          </v-col>
        </v-row>
      </v-card-text>
    </v-card>

    <!-- Users Table -->
    <v-card>
      <v-data-table-server
        :headers="[
          { title: 'ID', key: 'id', width: 60 },
          { title: '用户名', key: 'username' },
          { title: '昵称', key: 'nickname' },
          { title: '邮箱', key: 'email' },
          { title: '角色', key: 'roles' },
          { title: '状态', key: 'status', width: 100 },
          { title: '创建时间', key: 'created_at' },
          { title: '操作', key: 'actions', sortable: false, width: 280 },
        ]"
        :items="users"
        :items-length="total"
        :loading="loading"
        :items-per-page="pageSize"
        :page="page"
        @update:page="page = $event; fetchUsers()"
        @update:items-per-page="pageSize = $event; fetchUsers()"
      >
        <template #item.roles="{ item }">
          <v-chip
            v-for="role in item.roles"
            :key="role.id"
            size="small"
            color="primary"
            variant="tonal"
            class="mr-1"
          >
            {{ role.name }}
          </v-chip>
        </template>

        <template #item.status="{ item }">
          <v-chip
            :color="item.status === 1 ? 'success' : 'error'"
            size="small"
            variant="tonal"
          >
            {{ item.status === 1 ? '启用' : '禁用' }}
          </v-chip>
        </template>

        <template #item.created_at="{ item }">
          {{ new Date(item.created_at).toLocaleDateString() }}
        </template>

        <template #item.actions="{ item }">
          <v-btn size="small" variant="text" color="primary" @click="openEdit(item)">
            编辑
          </v-btn>
          <v-btn
            size="small"
            variant="text"
            :color="item.status === 1 ? 'warning' : 'success'"
            @click="handleToggleStatus(item)"
          >
            {{ item.status === 1 ? '禁用' : '启用' }}
          </v-btn>
          <v-btn size="small" variant="text" color="info" @click="openResetPassword(item.id)">
            重置密码
          </v-btn>
          <v-btn
            size="small"
            variant="text"
            color="error"
            :disabled="item.id === 1"
            @click="openDelete(item.id)"
          >
            删除
          </v-btn>
        </template>
      </v-data-table-server>
    </v-card>

    <!-- Create/Edit Dialog -->
    <v-dialog v-model="dialog" max-width="600">
      <v-card>
        <v-card-title>{{ dialogTitle }}</v-card-title>
        <v-card-text>
          <v-text-field
            v-model="formData.username"
            label="用户名"
            :disabled="!!editingId"
            class="mb-2"
          />
          <v-text-field
            v-if="!editingId"
            v-model="formData.password"
            label="密码"
            type="password"
            class="mb-2"
          />
          <v-text-field v-model="formData.nickname" label="昵称" class="mb-2" />
          <v-text-field v-model="formData.email" label="邮箱" class="mb-2" />
          <v-text-field v-model="formData.phone" label="手机" class="mb-2" />
          <v-select
            v-model="formData.role_ids"
            :items="roles"
            item-title="name"
            item-value="id"
            label="角色"
            multiple
            chips
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="dialog = false">取消</v-btn>
          <v-btn color="primary" @click="handleSave">保存</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- Reset Password Dialog -->
    <v-dialog v-model="resetDialog" max-width="400">
      <v-card>
        <v-card-title>重置密码</v-card-title>
        <v-card-text>
          <v-text-field
            v-model="newPassword"
            label="新密码"
            type="password"
            placeholder="至少6位字符"
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="resetDialog = false">取消</v-btn>
          <v-btn color="primary" :disabled="newPassword.length < 6" @click="handleResetPassword">
            确认重置
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- Delete Confirmation -->
    <v-dialog v-model="deleteDialog" max-width="400">
      <v-card>
        <v-card-title>确认删除</v-card-title>
        <v-card-text>确定要删除该用户吗？此操作不可恢复。</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="deleteDialog = false">取消</v-btn>
          <v-btn color="error" @click="handleDelete">删除</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>
