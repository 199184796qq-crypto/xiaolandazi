import vue from '@vitejs/plugin-vue'
import { defineConfig, loadEnv } from 'vite'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const coreToken = process.env.CORE_INTERNAL_TOKEN || env.CORE_INTERNAL_TOKEN || 'local-core-dev-token'
  return {
    plugins: [vue()],
    server: {
      host: '127.0.0.1',
      port: 5176,
      proxy: {
        '/core': {
          target: 'http://127.0.0.1:8081',
          changeOrigin: true,
          rewrite: (path) => path.replace(/^\/core/, ''),
          configure(proxy) {
            proxy.on('proxyReq', (proxyReq) => proxyReq.setHeader('X-Core-Token', coreToken))
          },
        },
        '/management': {
          target: 'http://127.0.0.1:8080',
          changeOrigin: true,
          rewrite: (path) => path.replace(/^\/management/, ''),
        },
      },
    },
  }
})
