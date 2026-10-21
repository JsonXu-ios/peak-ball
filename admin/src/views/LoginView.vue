<script setup lang="ts">
import { ref, reactive } from 'vue'
import { isAxiosError } from 'axios'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/store/auth'

const router = useRouter()
const authStore = useAuthStore()

const loading = ref(false)
const showPassword = ref(false)
const errorMsg = ref('')

const form = reactive({
  username: '',
  password: '',
})

async function handleLogin() {
  if (!form.username || !form.password) {
    errorMsg.value = '请输入用户名和密码'
    return
  }

  loading.value = true
  errorMsg.value = ''

  try {
    await authStore.doLogin(form.username, form.password)
    await authStore.fetchUserInfo()
    router.push('/dashboard')
  } catch (err: unknown) {
    if (isAxiosError(err)) {
      const status = err.response?.status
      if (err.code === 'ECONNABORTED' || err.code === 'ETIMEDOUT') {
        errorMsg.value = '登录请求超时，请检查后台服务后重试'
      } else if (!err.response || (status && status >= 500)) {
        errorMsg.value = '后台服务暂不可用，请检查管理 API（28081）和数据库是否已启动'
      } else if (status === 401) {
        errorMsg.value = '用户名或密码错误'
      } else if (status === 403) {
        errorMsg.value = '登录被拒绝，请检查账号是否被禁用或访问是否受限'
      } else {
        errorMsg.value = err.response.data?.error || '登录失败，请稍后重试'
      }
    } else {
      errorMsg.value = '登录失败，请稍后重试'
    }
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <v-container fluid class="fill-height bg-background">
    <v-row align="center" justify="center">
      <v-col cols="12" sm="8" md="5" lg="4" xl="3">
        <v-card class="pa-6" elevation="8">
          <!-- Header -->
          <div class="text-center mb-6">
            <v-icon color="primary" size="64" class="mb-4">mdi-soccer</v-icon>
            <h1 class="text-h5 font-weight-bold">足球数据管理后台</h1>
            <p class="text-body-2 text-medium-emphasis mt-2">请登录您的管理员账号</p>
          </div>

          <!-- Error Alert -->
          <v-alert
            v-if="errorMsg"
            type="error"
            variant="tonal"
            density="compact"
            closable
            class="mb-4"
            @click:close="errorMsg = ''"
          >
            {{ errorMsg }}
          </v-alert>

          <!-- Login Form -->
          <v-form @submit.prevent="handleLogin">
            <v-text-field
              v-model="form.username"
              label="用户名"
              prepend-inner-icon="mdi-account"
              placeholder="请输入用户名"
              autofocus
              class="mb-2"
            />

            <v-text-field
              v-model="form.password"
              :type="showPassword ? 'text' : 'password'"
              label="密码"
              prepend-inner-icon="mdi-lock"
              :append-inner-icon="showPassword ? 'mdi-eye-off' : 'mdi-eye'"
              placeholder="请输入密码"
              class="mb-4"
              @click:append-inner="showPassword = !showPassword"
            />

            <v-btn
              type="submit"
              color="primary"
              size="large"
              block
              :loading="loading"
            >
              登 录
            </v-btn>
          </v-form>

          <!-- Footer -->
          <div class="text-center mt-6">
            <p class="text-caption text-medium-emphasis">
              默认账号: admin / admin123
            </p>
          </div>
        </v-card>
      </v-col>
    </v-row>
  </v-container>
</template>
