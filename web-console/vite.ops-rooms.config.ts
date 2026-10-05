import { defineConfig, mergeConfig } from 'vite'
import { createHash } from 'node:crypto'
import base from './vite.config'

// Keep another pending task's shadow-style UI out of this scoped deployment.
export default mergeConfig(base, defineConfig({
  plugins: [{
    name: 'pin-deployed-live-strategy', enforce: 'pre',
    transform(code, id) {
      if (!id.replaceAll('\\', '/').endsWith('/src/views/LiveStrategyView.vue')) return
      const pinned = code.replace(/^.*<small v-if="anchorStyleTestResult\.runtime_evaluation">影子评测：.*\r?\n/m, '')
      const hashes = [pinned, pinned.replaceAll('\r\n', '\n'), pinned.replaceAll('\r\n', '\n').replaceAll('\n', '\r\n')]
        .map(text => createHash('sha256').update(text).digest('hex'))
      if (!hashes.includes('22d1bc28ae47ed25023376ac537ef8b6657bc040f2631e3a09e2c5fbcf08654a')) throw Error('LiveStrategy source differs from deployed baseline; refuse unrelated rollout')
      return { code: pinned, map: null }
    },
  }],
  build: { outDir: '../artifacts/ops-rooms-dist' },
}))
