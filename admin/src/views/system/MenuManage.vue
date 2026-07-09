<script setup lang="ts">
import { ref, onMounted, reactive } from 'vue'
import { getMenus, createMenu, updateMenu, deleteMenu } from '@/api'

interface MenuItem {
  id: number
  parent_id: number
  name: string
  title: string
  icon: string
  path: string
  component: string
  sort: number
  status: number
  menu_type: string
  hidden: boolean
}

const menus = ref<MenuItem[]>([])
const loading = ref(false)
const dialog = ref(false)
const dialogTitle = ref('新增菜单')
const editingId = ref<number | null>(null)
const deleteDialog = ref(false)
const deleteMenuId = ref(0)

const formData = reactive({
  parent_id: 0,
  name: '',
  title: '',
  icon: '',
  path: '',
  component: '',
  sort: 0,
  status: 1,
  menu_type: 'menu',
  hidden: false,
})

// Flatten menu list to parent options
const parentOptions = ref<Array<{ title: string; value: number }>>([])

async function fetchMenus() {
  loading.value = true
  try {
    const { data } = await getMenus()
    menus.value = data.list || []
    parentOptions.value = [
      { title: '顶级菜单', value: 0 },
      ...menus.value.map((m) => ({ title: m.title, value: m.id })),
    ]
  } finally {
    loading.value = false
  }
}

onMounted(fetchMenus)

function openCreate() {
  dialogTitle.value = '新增菜单'
  editingId.value = null
  Object.assign(formData, {
    parent_id: 0,
    name: '',
    title: '',
    icon: '',
    path: '',
    component: '',
    sort: 0,
    status: 1,
    menu_type: 'menu',
    hidden: false,
  })
  dialog.value = true
}

function openEdit(menu: MenuItem) {
  dialogTitle.value = '编辑菜单'
  editingId.value = menu.id
  Object.assign(formData, { ...menu })
  dialog.value = true
}

async function handleSave() {
  if (editingId.value) {
    await updateMenu(editingId.value, { ...formData })
  } else {
    await createMenu({ ...formData })
  }
  dialog.value = false
  fetchMenus()
}

function openDelete(id: number) {
  deleteMenuId.value = id
  deleteDialog.value = true
}

async function handleDelete() {
  await deleteMenu(deleteMenuId.value)
  deleteDialog.value = false
  fetchMenus()
}

function getParentTitle(parentId: number) {
  if (parentId === 0) return '顶级'
  const parent = menus.value.find((m) => m.id === parentId)
  return parent?.title ?? '-'
}
</script>

<template>
  <div>
    <div class="d-flex align-center justify-space-between mb-4">
      <h2 class="text-h5 font-weight-bold">菜单管理</h2>
      <v-btn color="primary" prepend-icon="mdi-plus" @click="openCreate">新增菜单</v-btn>
    </div>

    <v-card>
      <v-data-table
        :headers="[
          { title: 'ID', key: 'id', width: 60 },
          { title: '菜单名称', key: 'title' },
          { title: '路由名', key: 'name' },
          { title: '图标', key: 'icon', width: 60 },
          { title: '路径', key: 'path' },
          { title: '父菜单', key: 'parent_id' },
          { title: '排序', key: 'sort', width: 70 },
          { title: '类型', key: 'menu_type', width: 80 },
          { title: '状态', key: 'status', width: 80 },
          { title: '操作', key: 'actions', sortable: false, width: 180 },
        ]"
        :items="menus"
        :loading="loading"
      >
        <template #item.icon="{ item }">
          <v-icon v-if="item.icon" size="small">{{ item.icon }}</v-icon>
        </template>

        <template #item.parent_id="{ item }">
          {{ getParentTitle(item.parent_id) }}
        </template>

        <template #item.menu_type="{ item }">
          <v-chip size="small" :color="item.menu_type === 'menu' ? 'primary' : 'warning'" variant="tonal">
            {{ item.menu_type === 'menu' ? '菜单' : '按钮' }}
          </v-chip>
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
    <v-dialog v-model="dialog" max-width="600">
      <v-card>
        <v-card-title>{{ dialogTitle }}</v-card-title>
        <v-card-text>
          <v-select v-model="formData.parent_id" :items="parentOptions" label="父菜单" class="mb-2" />
          <v-row>
            <v-col cols="6">
              <v-text-field v-model="formData.name" label="路由名称" />
            </v-col>
            <v-col cols="6">
              <v-text-field v-model="formData.title" label="菜单标题" />
            </v-col>
          </v-row>
          <v-row>
            <v-col cols="6">
              <v-text-field v-model="formData.icon" label="图标" placeholder="mdi-xxx">
                <template #prepend-inner>
                  <v-icon v-if="formData.icon" size="small">{{ formData.icon }}</v-icon>
                </template>
              </v-text-field>
            </v-col>
            <v-col cols="6">
              <v-text-field v-model="formData.path" label="路由路径" />
            </v-col>
          </v-row>
          <v-text-field v-model="formData.component" label="组件路径" class="mb-2" />
          <v-row>
            <v-col cols="4">
              <v-text-field v-model.number="formData.sort" label="排序" type="number" />
            </v-col>
            <v-col cols="4">
              <v-select v-model="formData.menu_type" :items="['menu', 'button']" label="类型" />
            </v-col>
            <v-col cols="4">
              <v-switch v-model="formData.hidden" label="隐藏" color="primary" />
            </v-col>
          </v-row>
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
        <v-card-text>确定要删除该菜单吗？含子菜单的菜单无法删除。</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="deleteDialog = false">取消</v-btn>
          <v-btn color="error" @click="handleDelete">删除</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>
