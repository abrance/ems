import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 构建产物与现网 web.zip 同构：index.html + assets/，可直接打进 application/manifest/web.zip
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    host: '0.0.0.0',
    port: 5173,
    // 后端 dev 配置监听 :15880（manifest/config/config-dev.yaml）
    proxy: {
      '/api': { target: 'http://127.0.0.1:15880', changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    chunkSizeWarningLimit: 1500,
  },
})
