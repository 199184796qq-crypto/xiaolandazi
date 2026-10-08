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
  initializing: false, saving: false, publishing: false, published: false,
  initializeProgress: 0, initializeLabel: '',
  initializeDisabled: false, saveDisabled: false, publishDisabled: false,
  saveHint: '保存当前配置与内容', publishHint: '将当前内容发布使用',
}

test('只缩短初始化按钮至半宽，保留窄屏文字空间和原高度', async () => {
  const source = await readFile(new URL('../src/components/AnchorControlBar.vue', import.meta.url), 'utf8')
  assert.ok(source.includes('.control-initialize{width:50%;min-width:min(100%,360px);justify-self:start;'))
  assert.ok(source.includes('min-height:clamp(62px,4.1vw,76px)'))
  assert.ok(source.includes('.control-initialize{grid-column:auto;width:100%}'))
  assert.equal(/\.control-(?:save|publish)\{width:50%/.test(source),false)
})

test('三张操作卡保留三个原操作标签，无旧说明文字', async () => {
  const html = await renderToString(createSSRApp(component, ready))
  assert.equal((html.match(/<button\b/g) || []).length, 3)
  for (const label of ['初始化直播智能体', '保存新版本', '发布到直播间']) assert.ok(html.includes(label))
  for (const style of ['control-initialize', 'control-save', 'control-publish', 'control-arrow']) assert.ok(html.includes(style))
  assert.equal(html.includes('原始资料不自动修改'), false)
  assert.equal(html.includes('已发布到直播间'), false)
  assert.equal((html.match(/<button\b[^>]*\bdisabled/g) || []).length, 0)
})

test('已发布状态来自传入版本状态，不写死显示已发布', async () => {
  const html = await renderToString(createSSRApp(component, { ...ready, published: true }))
  assert.ok(html.includes('已发布到直播间'))
  assert.ok(html.includes('title="将当前内容发布使用"'))
})

test('处理中标签及原禁用条件保持，不能因美化激活禁用按钮', async () => {
  const html = await renderToString(createSSRApp(component, {
    ...ready, initializing: true, saving: true, publishing: true,
    initializeProgress: 46, initializeLabel: '事实包初始化…',
    initializeDisabled: true, saveDisabled: true, publishDisabled: true,
  }))
  for (const label of ['事实包初始化…', '46%', '保存中…', '发布中…']) assert.ok(html.includes(label))
  assert.equal(html.includes('初始化直播智能体'), false)
  assert.ok(html.includes('control-progress-fill'))
  assert.equal((html.match(/<button\b[^>]*\bdisabled/g) || []).length, 3)
  assert.equal((html.match(/aria-busy="true"/g) || []).length, 3)
})

test('禁用状态分别控制，辅助版本提示保留且作为文本安全转义', async () => {
  const html = await renderToString(createSSRApp(component, {
    ...ready, initializeDisabled: true, saveHint: '<script>unexpected</script>',
  }))
  assert.equal((html.match(/<button\b[^>]*\bdisabled/g) || []).length, 1)
  assert.equal(html.includes('<script>unexpected</script>'), false)
  assert.ok(html.includes('&lt;script&gt;unexpected&lt;/script&gt;'))
})
