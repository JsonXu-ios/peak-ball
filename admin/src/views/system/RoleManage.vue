<script setup lang="ts">
import { ref, onMounted, reactive } from 'vue'
import {
  getRoles,
  createRole,
  updateRole,
  deleteRole,
  getMenuTree,
  getRoleMenus,
  updateRoleMenus,
  getPermissions,
  getRolePermissions,
  updateRolePermissions,
} from '@/api'

interface Role {
  id: number
  name: string
  code: string
  description: string
  sort: number
  status: number
}

interface MenuItem {
  id: number
  title: string
  children?: MenuItem[]
}

interface Permission {
  id: number
  name: string
  code: string
  category: string
}

const roles = ref<Role[]>([])
const loading = ref(false)

const dialog = ref(false)
const dialogTitle = ref('新增角色')
const editingId = ref<number | null>(null)
const formData = reactive({
  name: '',
  code: '',
  description: '',
  sort: 0,
  status: 1,
})

// Menu assignment
const menuDialog = ref(false)
const menuTree = ref<MenuItem[]>([])
const selectedMenus = ref<number[]>([])
const menuRoleId = ref(0)

// Permission assignment
const permDialog = ref(false)
const allPermissions = ref<Permission[]>([])
const selectedPerms = ref<number[]>([])
const permRoleId = ref(0)

const deleteDialog = ref(false)
const deleteRoleId = ref(0)

async function fetchRoles() {
  loading.value = true
  try {
    const { data } = await getRoles()
    roles.value = data.list || []
  } finally {
    loading.value = false
  }
}

onMounted(fetchRoles)

function openCreate() {
  dialogTitle.value = '新增角色'
  editingId.value = null
  Object.assign(formData, { name: '', code: '', description: '', sort: 0, status: 1 })
  dialog.value = true
}

function openEdit(role: Role) {
  dialogTitle.value = '编辑角色'
  editingId.value = role.id
  Object.assign(formData, {
    name: role.name,
    code: role.code,
    description: role.description,
    sort: role.sort,
    status: role.status,
  })
  dialog.value = true
}

async function handleSave() {
  if (editingId.value) {
    await updateRole(editingId.value, { ...formData })
  } else {
    await createRole({ ...formData })
  }
  dialog.value = false
  fetchRoles()
}

async function openMenuAssign(roleId: number) {
  menuRoleId.value = roleId
  const [treeRes, menuRes] = await Promise.all([getMenuTree(), getRoleMenus(roleId)])
  menuTree.value = treeRes.data.list || []
  selectedMenus.value = menuRes.data.menu_ids || []
  menuDialog.value = true
}

async function saveMenuAssign() {
  await updateRoleMenus(menuRoleId.value, selectedMenus.value)
  menuDialog.value = false
}

async function openPermAssign(roleId: number) {
  permRoleId.value = roleId
  const [permRes, rolePermRes] = await Promise.all([getPermissions(), getRolePermissions(roleId)])
  allPermissions.value = permRes.data.list || []
  selectedPerms.value = rolePermRes.data.permission_ids || []
  permDialog.value = true
}

async function savePermAssign() {
  await updateRolePermissions(permRoleId.value, selectedPerms.value)
  permDialog.value = false
}

function openDelete(id: number) {
  deleteRoleId.value = id
  deleteDialog.value = true
}

async function handleDelete() {
  await deleteRole(deleteRoleId.value)
  deleteDialog.value = false
  fetchRoles()
}
</script>

<template>
  <div>
    <div class="d-flex align-center justify-space-between mb-4">
      <h2 class="text-h5 font-weight-bold">角色管理</h2>
      <v-btn color="primary" prepend-icon="mdi-plus" @click="openCreate">新增角色</v-btn>
    </div>

    <v-card>
      <v-data-table
        :headers="[
          { title: 'ID', key: 'id', width: 60 },
          { title: '角色名称', key: 'name' },
          { title: '角色编码', key: 'code' },
          { title: '描述', key: 'description' },
          { title: '排序', key: 'sort', width: 80 },
          { title: '状态', key: 'status', width: 80 },
          { title: '操作', key: 'actions', sortable: false, width: 350 },
        ]"
        :items="roles"
        :loading="loading"
      >
        <template #item.status="{ item }">
          <v-chip :color="item.status === 1 ? 'success' : 'error'" size="small" variant="tonal">
            {{ item.status === 1 ? '启用' : '禁用' }}
          </v-chip>
        </template>

        <template #item.actions="{ item }">
          <v-btn size="small" variant="text" color="primary" @click="openEdit(item)">编辑</v-btn>
          <v-btn size="small" variant="text" color="info" @click="openMenuAssign(item.id)">分配菜单</v-btn>
          <v-btn size="small" variant="text" color="warning" @click="openPermAssign(item.id)">分配权限</v-btn>
          <v-btn size="small" variant="text" color="error" :disabled="item.id === 1" @click="openDelete(item.id)">删除</v-btn>
        </template>
      </v-data-table>
    </v-card>

    <!-- Create/Edit Role Dialog -->
    <v-dialog v-model="dialog" max-width="500">
      <v-card>
        <v-card-title>{{ dialogTitle }}</v-card-title>
        <v-card-text>
          <v-text-field v-model="formData.name" label="角色名称" class="mb-2" />
          <v-text-field v-model="formData.code" label="角色编码" :disabled="!!editingId" class="mb-2" />
          <v-textarea v-model="formData.description" label="描述" rows="2" class="mb-2" />
          <v-text-field v-model.number="formData.sort" label="排序" type="number" class="mb-2" />
          <v-switch v-model="formData.status" :true-value="1" :false-value="0" label="启用" color="primary" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="dialog = false">取消</v-btn>
          <v-btn color="primary" @click="handleSave">保存</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- Menu Assignment Dialog -->
    <v-dialog v-model="menuDialog" max-width="500">
      <v-card>
        <v-card-title>分配菜单</v-card-title>
        <v-card-text style="max-height: 400px; overflow-y: auto;">
          <v-treeview
            v-model:selected="selectedMenus"
            :items="menuTree"
            item-title="title"
            item-value="id"
            item-children="children"
            selectable
            select-strategy="leaf"
            open-all
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="menuDialog = false">取消</v-btn>
          <v-btn color="primary" @click="saveMenuAssign">保存</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- Permission Assignment Dialog -->
    <v-dialog v-model="permDialog" max-width="600">
      <v-card>
        <v-card-title>分配权限</v-card-title>
        <v-card-text style="max-height: 400px; overflow-y: auto;">
          <v-checkbox
            v-for="perm in allPermissions"
            :key="perm.id"
            v-model="selectedPerms"
            :value="perm.id"
            :label="`${perm.name} (${perm.code})`"
            density="compact"
            hide-details
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="permDialog = false">取消</v-btn>
          <v-btn color="primary" @click="savePermAssign">保存</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- Delete Dialog -->
    <v-dialog v-model="deleteDialog" max-width="400">
      <v-card>
        <v-card-title>确认删除</v-card-title>
        <v-card-text>确定要删除该角色吗？</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="deleteDialog = false">取消</v-btn>
          <v-btn color="error" @click="handleDelete">删除</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>
