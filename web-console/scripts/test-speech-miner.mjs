// Isolated interaction test: all API traffic is mocked; no rooms/models/database.
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const fixture = `<html><head><meta charset="utf-8"><style>*{box-sizing:border-box}body{padding:20px;background:#f3f5ff;font-family:Arial,'Microsoft Yahei',sans-serif}</style></head><body><div id="app"></div><script type="module">
import{createApp,h,reactive}from'/node_modules/.vite/deps/vue.js';import Miner from'/src/components/SpeechMinerWorkbench.vue';
const state=reactive({planId:4,tenantId:14,wheel:null});window.fixture={state,updated:0};createApp({render:()=>h(Miner,{...state,onUpdated:()=>window.fixture.updated++})}).mount('#app');</script></body></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 5387 }, appType: 'custom' })
server.middlewares.use((req, res, next) => { if (req.url === '/__miner_fixture') { res.setHeader('Content-Type', 'text/html; charset=utf-8'); res.end(fixture) } else next() })
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
const errors = [], calls = [], confirmations = []
let slowStart, heldRoute, firstConfirm = true
page.on('pageerror', error => errors.push(error.message))
page.on('dialog', dialog => dialog.accept())
const reply = { reply: '我们先梳理真实特点。你想补充哪方面？', selection_mode: 'single', options: [{ label: '完成挖掘', value: '完成挖掘', action: 'complete' }], captured_facts: [], completed: false }
await page.route('**/api/v1/**', async route => {
  const url = new URL(route.request().url()), input = route.request().postDataJSON(); calls.push({ path: url.pathname, input })
  if (url.pathname.endsWith('/speech-miner/guide')) {
    if (input.start && slowStart) { heldRoute = route; slowStart(); slowStart = null; return }
    const result = input.start ? reply : input.action === 'complete' ? { ...reply, reply: '请核对已收集事实。', options: [], completed: true } : { ...reply, reply: '本轮提问完成，请核对事实。', options: [], captured_facts: [{ category: 'product', key: '保存方式', value: '常温保存', source_quote: '常温保存' }], completed: true }
    await route.fulfill({ json: result }); return
  }
  if (url.pathname.endsWith('/speech-miner/confirm')) {
    confirmations.push(input)
    const result = { facts_saved: true, added: firstConfirm ? 1 : 0, wheel_ready: !firstConfirm, published: false, notice: firstConfirm ? '事实已保存，可重试轮盘' : '轮盘新版本已保存，未发布', ...(firstConfirm ? { error: '模拟轮盘暂时失败' } : { wheel_asset_id: 77, selling_points: 2, angles: 10 }) }
    firstConfirm = false
    await route.fulfill({ contentType: 'text/event-stream', body: 'event: progress\ndata: '+JSON.stringify({ percent: 55, message: '正在复核轮盘' })+'\n\nevent: complete\ndata: '+JSON.stringify(result)+'\n\n' }); return
  }
  throw new Error('Out-of-scope call: '+url.pathname)
})
try {
  await page.goto('http://127.0.0.1:5387/__miner_fixture')
  const buttons = page.locator('.speech-miner-chat-start button')
  await buttons.first().waitFor()
  assert.match(await buttons.nth(0).textContent(), /挖掘卖点/)
  assert.match(await buttons.nth(1).textContent(), /挖掘内容/)
  await buttons.first().click()
  await page.getByText(reply.reply, { exact: true }).waitFor()
  assert.equal(calls[0].input.mode, 'new_selling_point')
  assert.equal(calls[0].input.start, true)
  assert.equal(calls[0].input.message, '')
  assert.deepEqual(calls[0].input.history, [])
  assert.equal(confirmations.length, 0)
  await page.locator('.speech-miner-composer textarea').fill('本品常温保存')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await page.getByText('本轮提问完成，请核对事实。', { exact: true }).waitFor()
  assert.equal(confirmations.length, 0, 'model completion must not persist')
  await page.locator('.speech-miner-chat-start .speech-miner-save').click()
  const modal = page.getByRole('dialog', { name: '核对事实依据' })
  await modal.waitFor()
  assert.equal(confirmations.length, 0, 'opening review must not persist')
  await modal.getByLabel('事实内容').fill('密封常温保存')
  await modal.locator('footer button').last().click()
  await modal.locator('footer button').last().filter({ hasText: /重试/ }).waitFor()
  assert.equal(confirmations.length, 1)
  assert.equal(confirmations[0].confirmed, true)
  assert.equal(confirmations[0].tenant_id, 14)
  assert.equal(confirmations[0].facts[0].value, '密封常温保存')
  assert.ok(await modal.getByLabel('事实内容').isDisabled(), 'saved batch cannot change on retry')
  await modal.locator('footer button').last().click()
  await modal.waitFor({ state: 'hidden' })
  assert.equal(confirmations.length, 2)
  assert.equal(confirmations[0].session_id, confirmations[1].session_id)
  assert.deepEqual(confirmations[0].facts, confirmations[1].facts)
  assert.equal(await page.evaluate(() => window.fixture.updated), 2)
  await buttons.nth(1).click()
  await page.getByText(reply.reply, { exact: true }).waitFor()
  assert.equal(calls.at(-1).input.mode, 'fact_gaps')
  await page.getByRole('button', { name: /完成挖掘/ }).click()
  await page.getByText('请核对已收集事实。', { exact: true }).waitFor()
  assert.equal(calls.at(-1).input.action, 'complete')
  assert.equal(confirmations.length, 2, 'control cannot create/save facts')
  await page.getByRole('button', { name: '保存退出：挖掘内容', exact: true }).click()
  const held = new Promise(resolve => { slowStart = resolve })
  await buttons.first().click(); await held
  await page.evaluate(() => { window.fixture.state.planId = 5; window.fixture.state.tenantId = 15 })
  await heldRoute.fulfill({ json: { ...reply, reply: '旧方案迟到的回复' } })
  await page.waitForFunction(() => document.querySelector('.speech-miner-chat-start button:not(:disabled)'))
  // Both task and tenant have changed; late API replies cannot leak into new chat.
  assert.equal(await page.getByText('旧方案迟到的回复', { exact: true }).count(), 0)
  await buttons.first().click()
  await page.getByText(reply.reply, { exact: true }).waitFor()
  assert.match(calls.at(-1).path, /plans\/5\/speech-miner\/guide$/)
  assert.equal(calls.at(-1).input.tenant_id, 15)
  assert.ok(calls.every(call => /\/speech-miner\/(guide|confirm)$/.test(call.path)), 'no initialize/publish/hot reload API')
  assert.deepEqual(errors, [])
  console.log('PASS: entry order, guided intro, explicit editable confirmation, no auto-save/publish/full initialization, partial retry/session stability, action controls, tenant/plan stale-response isolation; mocked APIs only')
} finally { await browser.close(); await server.close() }
