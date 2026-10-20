import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { readFile } from 'node:fs/promises'

let server, component
before(async () => {
  server = await createServer({ server: { middlewareMode: true }, appType: 'custom' })
  component = (await server.ssrLoadModule('/src/components/AnchorControlBar.vue')).default
})
after(async () => { await server?.close() })

const ready = {
  initializing: false, saving: false,
  initializeProgress: 0, initializeLabel: '',
  initializeDisabled: false, saveDisabled: false,
  saveHint: '保存当前配置与内容',
}

test('初始化使用相连的主按钮和菜单箭头，保留窄屏空间和原高度', async () => {
  const source = await readFile(new URL('../src/components/AnchorControlBar.vue', import.meta.url), 'utf8')
  assert.ok(source.includes('class="control-menu-toggle"'))
  assert.ok(source.includes('v-if="menuOpen"'))
  assert.ok(source.includes('min-height:clamp(62px,4.1vw,76px)'))
  assert.ok(source.includes('.control-initialize-wrap{grid-column:auto;width:100%}'))
  assert.equal(/\.control-(?:save|publish)\{width:50%/.test(source),false)
})

test('默认只保留初始化与保存，不出现单独发布操作', async () => {
  const html = await renderToString(createSSRApp(component, ready))
  assert.equal((html.match(/<button\b/g) || []).length, 3)
  for (const label of ['初始化直播智能体', '保存方案']) assert.ok(html.includes(label))
  for (const style of ['control-initialize', 'control-save', 'control-menu-toggle']) assert.ok(html.includes(style))
  assert.ok(html.includes('aria-haspopup="menu"'))
  assert.ok(html.includes('aria-expanded="false"'))
  assert.equal(html.includes('全部重置'), false)
  assert.equal(html.includes('发布到直播间'), false)
  assert.equal(html.includes('原始资料不自动修改'), false)
  assert.equal(html.includes('已发布到直播间'), false)
  assert.equal((html.match(/<button\b[^>]*\bdisabled/g) || []).length, 0)
})

test('页面顶部统一保存时，紧凑初始化区不重复显示保存按钮', async () => {
  const html = await renderToString(createSSRApp(component, { ...ready, mode: 'initialize', compact: true }))
  assert.equal((html.match(/<button\b/g) || []).length, 2)
  assert.ok(html.includes('is-compact'))
  assert.ok(html.includes('初始化直播智能体'))
  assert.equal(html.includes('control-save'), false)
})

test('处理中标签及原禁用条件保持，不能因美化激活禁用按钮', async () => {
  const html = await renderToString(createSSRApp(component, {
    ...ready, initializing: true, saving: true,
    initializeProgress: 46, initializeLabel: '事实包初始化…',
    initializeDisabled: true, saveDisabled: true,
  }))
  for (const label of ['事实包初始化…', '46%', '保存中…']) assert.ok(html.includes(label))
  assert.equal(html.includes('初始化直播智能体'), false)
  assert.ok(html.includes('control-progress-fill'))
  assert.equal((html.match(/<button\b[^>]*\bdisabled/g) || []).length, 3)
  assert.equal((html.match(/aria-busy="true"/g) || []).length, 2)
})

test('禁用状态分别控制，辅助版本提示保留且作为文本安全转义', async () => {
  const html = await renderToString(createSSRApp(component, {
    ...ready, initializeDisabled: true, saveHint: '<script>unexpected</script>',
  }))
  assert.equal((html.match(/<button\b[^>]*\bdisabled/g) || []).length, 2)
  assert.equal(html.includes('<script>unexpected</script>'), false)
  assert.ok(html.includes('&lt;script&gt;unexpected&lt;/script&gt;'))
})
