import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [vue()],
  server: {
    host: '127.0.0.1',
    port: 5173,
    allowedHosts: ['scarcity-stem-tamper.ngrok-free.dev'],
    proxy: {
      '/core-audio': {
        target: 'http://127.0.0.1:8081',
        changeOrigin: true,
        rewrite: path => path.replace(/^\/core-audio/, ''),
      },
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
})
