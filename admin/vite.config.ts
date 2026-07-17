import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import vuetify from 'vite-plugin-vuetify'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [
    vue(),
    vuetify({ autoImport: true }),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5174,
    strictPort: true,
    host: true,
    allowedHosts: ['zq1283fj623.vicp.fun', '.lazyperson.top'],
    proxy: {
      '/api': {
        target: 'http://localhost:8081',
        changeOrigin: true,
        // 摘掉浏览器的 Origin 头：经代理的请求对后端而言就是同源请求，
        // 后端 CORS 白名单不再参与，换任何访问域名/IP 都不会再 403。
        configure: (proxy) => {
          proxy.on('proxyReq', (proxyReq) => proxyReq.removeHeader('origin'))
        },
      },
      '/footballimg': {
        target: 'http://localhost:18080',
        changeOrigin: true,
      },
      '/images': {
        target: 'http://localhost:18080',
        changeOrigin: true,
      },
    },
  },
})
