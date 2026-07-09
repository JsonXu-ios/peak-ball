<script setup lang="ts">
import { ref, onMounted, reactive } from 'vue'
import { getPermissions, createPermission, updatePermission, deletePermission } from '@/api'

interface Permission {
  id: number
  name: string
  code: string
  description: string
  category: string
  status: number
}

const permissions = ref<Permission[]>([])
const loading = ref(false)
const keyword = ref('')

const dialog = ref(false)
const dialogTitle = ref('新增权限')
const editingId = ref<number | null>(null)
const deleteDialog = ref(false)
const deletePermId = ref(0)

const formData = reactive({
  name: '',
  code: '',
  description: '',
  category: '',
  status: 1,
})

const categories = ['system', 'crawler', 'data', 'content']

async function fetchPermissions() {
  loading.value = true
  try {
    const { data } = await getPermissions({ keyword: keyword.value })
    permissions.value = data.list || []
  } finally {
    loading.value = false
  }
}

onMounted(fetchPermissions)

function openCreate() {
  dialogTitle.value = '新增权限'
  editingId.value = null
  Object.assign(formData, { name: '', code: '', description: '', category: 'system', status: 1 })
  dialog.value = true
}

function openEdit(perm: Permission) {
  dialogTitle.value = '编辑权限'
  editingId.value = perm.id
  Object.assign(formData, { ...perm })
  dialog.value = true
}

async function handleSave() {
  if (editingId.value) {
    await updatePermission(editingId.value, { ...formData })
  } else {
    await createPermission({ ...formData })
  }
  dialog.value = false
  fetchPermissions()
}

function openDelete(id: number) {
  deletePermId.value = id
  deleteDialog.value = true
}

async function handleDelete() {
  await deletePermission(deletePermId.value)
  deleteDialog.value = false
  fetchPermissions()
}
</script>

<template>
  <div>
    <div class="d-flex align-center justify-space-between mb-4">
      <h2 class="text-h5 font-weight-bold">权限管理</h2>
      <v-btn color="primary" prepend-icon="mdi-plus" @click="openCreate">新增权限</v-btn>
    </div>

    <v-card class="mb-4">
      <v-card-text>
        <v-row>
          <v-col cols="12" md="4">
            <v-text-field
              v-model="keyword"
              label="搜索权限"
              prepend-inner-icon="mdi-magnify"
              clearable
              hide-details
              @keyup.enter="fetchPermissions"
            />
          </v-col>
          <v-col cols="12" md="2">
            <v-btn color="primary" @click="fetchPermissions">搜索</v-btn>
          </v-col>
        </v-row>
      </v-card-text>
    </v-card>

    <v-card>
      <v-data-table
        :headers="[
          { title: 'ID', key: 'id', width: 60 },
          { title: '权限名称', key: 'name' },
          { title: '权限编码', key: 'code' },
          { title: '分类', key: 'category' },
          { title: '描述', key: 'description' },
          { title: '状态', key: 'status', width: 80 },
          { title: '操作', key: 'actions', sortable: false, width: 180 },
        ]"
        :items="permissions"
        :loading="loading"
      >
        <template #item.category="{ item }">
          <v-chip size="small" color="info" variant="tonal">{{ item.category }}</v-chip>
        </template>

        <template #item.status="{ item }">
          <v-chip :color="item.status === 1 ? 'success' : 'error'" size="small" variant="tonal">
            {{ item.status === 1 ? '启用' : '禁用' }}
          </v-chip>
        </template>

        <template #item.actions="{ item }">
          <v-btn size="small" variant="text" color="primary" @click="openEdit(item)">编辑</v-btn>
          <v-btn size="small" variant="text" color="error" @click="openDelete(item.id)">删除</v-btn>
        </template>
      </v-data-table>
    </v-card>

    <!-- Create/Edit Dialog -->
    <v-dialog v-model="dialog" max-width="500">
      <v-card>
        <v-card-title>{{ dialogTitle }}</v-card-title>
        <v-card-text>
          <v-text-field v-model="formData.name" label="权限名称" class="mb-2" />
          <v-text-field v-model="formData.code" label="权限编码" placeholder="例: system:user:list" class="mb-2" />
          <v-select v-model="formData.category" :items="categories" label="分类" class="mb-2" />
          <v-textarea v-model="formData.description" label="描述" rows="2" class="mb-2" />
          <v-switch v-model="formData.status" :true-value="1" :false-value="0" label="启用" color="primary" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="dialog = false">取消</v-btn>
          <v-btn color="primary" @click="handleSave">保存</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- Delete Dialog -->
    <v-dialog v-model="deleteDialog" max-width="400">
      <v-card>
        <v-card-title>确认删除</v-card-title>
        <v-card-text>确定要删除该权限吗？</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="deleteDialog = false">取消</v-btn>
          <v-btn color="error" @click="handleDelete">删除</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>
