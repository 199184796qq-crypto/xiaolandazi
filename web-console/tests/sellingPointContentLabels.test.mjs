import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'
let server, miner, overview
before(async () => {
  server = await createServer({ server: { middlewareMode: true }, appType: 'custom' })
  miner = (await server.ssrLoadModule('/src/components/SpeechMinerWorkbench.vue')).default
  overview = (await server.ssrLoadModule('/src/components/SellingPointWheelOverview.vue')).default
})
after(async () => { await server?.close() })
const wheel = { asset_id: 77, wheel_json: JSON.stringify({ 卖点列表: [{ 卖点名: '工艺与原料', 痛点: '了解真实工艺', 目标人群: '关注制作的消费者', 角度: ['先卤后炸', '八个月土麻鸭', '工艺信息', '原料信息', '规格说明'] }], 事实补充建议: [] }) }
test('miner displays content count and expansion without exposing internal angle term', async () => {
  const html = await renderToString(createSSRApp(miner, { planId: 4, tenantId: 14, wheel }))
  assert.match(html, /1 个卖点 · 5 条内容/)
  assert.ok(html.includes('展开其余 1 条内容'))
  assert.ok(html.includes('全部内容'))
  assert.equal(html.includes('角度'), false)
  assert.ok(html.includes('先卤后炸'))
  assert.ok(html.includes('八个月土麻鸭'))
  assert.equal(JSON.parse(wheel.wheel_json).卖点列表[0].角度.length, 5)
})
test('overview retains all internal angle data while labelling it content', async () => {
  const html = await renderToString(createSSRApp(overview, { wheel }))
  assert.ok(html.includes('卖点与内容'))
  assert.ok(html.includes('5 条内容'))
  assert.equal(html.includes('角度'), false)
  for (const text of JSON.parse(wheel.wheel_json).卖点列表[0].角度) assert.ok(html.includes(text))
})
