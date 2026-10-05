import { defineConfig, mergeConfig } from 'vite'
import { createHash } from 'node:crypto'
import base from './vite.config'

// Publish the room dashboard only; leave the separate, unpublished style
// evaluation work in the checkout and out of the production bundle.
export default mergeConfig(base, defineConfig({
  plugins: [{
    name: 'pin-room-parity-unrelated-source', enforce: 'pre',
    transform(code, id) {
      if (!id.replaceAll('\\', '/').endsWith('/src/views/LiveStrategyView.vue')) return
      const pinned = code
        .replace(/^.*<small v-if="anchorStyleTestResult\.style_purity">纯风格边界检查：.*\r?\n/m, '')
        .replace(/^.*<small v-if="anchorStyleTestResult\.runtime_evaluation">影子评测：.*\r?\n/m, '')
        .replace(/^.*<small v-if="anchorStyleTestResult\.style_vector_evaluation\?\.available">风格向量影子分.*\r?\n/m, '')
      const hashes = [pinned, pinned.replaceAll('\r\n', '\n'), pinned.replaceAll('\r\n', '\n').replaceAll('\n', '\r\n')]
        .map(text => createHash('sha256').update(text).digest('hex'))
      if (!hashes.includes('22d1bc28ae47ed25023376ac537ef8b6657bc040f2631e3a09e2c5fbcf08654a')) throw Error('Unrelated LiveStrategy source differs from deployed baseline')
      return {code:pinned,map:null}
    },
  }],
  build: {outDir:'../artifacts/room-parity-dist'},
}))
