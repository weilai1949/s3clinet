/// <reference types="vitest/config" />
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

// 前端构建。输出到 dist，Go 后端把 dist 作为静态资源托管。
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 1949,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  test: {
    environment: 'happy-dom',
  },
})
