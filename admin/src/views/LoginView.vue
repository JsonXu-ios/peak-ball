<script setup lang="ts">
import { ref, reactive } from 'vue'
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
    const error = err as { response?: { data?: { error?: string } } }
    errorMsg.value = error.response?.data?.error || '登录失败，请检查用户名和密码'
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
