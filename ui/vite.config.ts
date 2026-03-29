import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue()],
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: false,
        xfwd: true,
      },
      '/auth': {
        target: 'http://localhost:8080',
        changeOrigin: false,
        xfwd: true,
      },
    },
  },
  build: {
    outDir: '../internal/web/static',
    emptyOutDir: true,
  },
})
