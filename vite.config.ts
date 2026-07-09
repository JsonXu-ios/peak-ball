import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import path from 'path'

const allowedHosts = Array.from(
  new Set(
    [
      'zq1283fj623.vicp.fun',
      ...(process.env.VITE_ALLOWED_HOSTS ?? '').split(','),
      ...(process.env.__VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS ?? '').split(','),
    ]
      .map((host) => host.trim())
      .filter(Boolean),
  ),
)

const proxy = {
  '/api': {
    target: 'http://localhost:18080',
    changeOrigin: true,
  },
  '/footballimg': {
    target: 'http://localhost:18080',
    changeOrigin: true,
  },
  '/images': {
    target: 'http://localhost:18080',
    changeOrigin: true,
  },
}

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [
    vue(),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    host: true,
    allowedHosts,
    proxy,
  },
  preview: {
    port: 5173,
    strictPort: true,
    host: true,
    allowedHosts,
    proxy,
  },
})
