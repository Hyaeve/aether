import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { hanSpacingPlugin } from './han-spacing-plugin.mjs'

export default defineConfig({
  plugins: [hanSpacingPlugin(), vue()],
  server: {
    proxy: {
      '/api': { target: 'http://127.0.0.1:15151', changeOrigin: false },
      '/stream': 'http://127.0.0.1:15151',
      '/dav': 'http://127.0.0.1:15151'
    }
  }
})
