import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'

let server, panel
before(async () => {
  server = await createServer({ server: { middlewareMode: true }, appType: 'custom' })
  panel = (await server.ssrLoadModule('/src/components/RealtimeGenerationOptionsPanel.vue')).default
})
after(async () => { await server?.close() })

test('用户前台显示四个数字输入，对应三组参数', async () => {
  const html = await renderToString(createSSRApp(panel, { modelValue: { buffer_seconds: 45, body_min: 3, body_max: 5, max_chars: 480 } }))
  assert.equal((html.match(/<input\b/g) || []).length, 4)
  for (const label of ['正式主线缓存', '中间讲解段数', '单段最大字数']) assert.ok(html.includes(label))
  for (const value of ['45', '3', '5', '480']) assert.ok(html.includes(`value="${value}"`))
  assert.equal(html.includes('保存版本后确定'), false)
})
test('读取、保存时禁用输入，不偷偷替换正在运行的场次参数', async () => {
  const html = await renderToString(createSSRApp(panel, { modelValue: null, loading: true }))
  assert.ok(/<fieldset[^>]*disabled/.test(html))
  assert.ok(html.includes('正在读取参数'))
  assert.ok(html.includes('当前场次保持原参数'))
})
test('上下限错误及未保存变更有明确提示', async () => {
  const html = await renderToString(createSSRApp(panel, { modelValue: { buffer_seconds: 45, body_min: 5, body_max: 3, max_chars: 480 }, changed: true }))
  assert.ok(html.includes('最少不能超过最多'))
  assert.ok(html.includes('有未保存参数'))
  assert.ok(html.includes('role="alert"'))
})
