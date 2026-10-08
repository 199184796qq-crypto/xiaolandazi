// All data is synthetic; model and persistence endpoints are mocked.
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const fixture = `<html><head><meta charset="utf-8"><style>*{box-sizing:border-box}body{padding:16px;font-family:Arial,'Microsoft Yahei',sans-serif}</style></head><body><div id="app"></div><script type="module">
import{createApp,h,reactive}from'/node_modules/.vite/deps/vue.js';import Miner from'/src/components/SpeechMinerWorkbench.vue';
const state=reactive({planId:4,tenantId:14});window.fixture={state,updated:0};createApp({render:()=>h(Miner,{...state,onUpdated:()=>window.fixture.updated++})}).mount('#app');</script></body></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom' })
server.middlewares.use((req,res,next) => { if (req.url === '/__save_exit') { res.setHeader('Content-Type','text/html; charset=utf-8'); res.end(fixture) } else next() })
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } })
const errors = [], calls = [], saves = []
let heldStart = null, holdStart = false, partialSave = false, failAnswer = false
page.on('pageerror', e => errors.push(e.message))
await page.route('**/api/v1/**', async route => {
  const path = new URL(route.request().url()).pathname, data = route.request().postDataJSON()
  calls.push({ path, data })
  if (path.endsWith('/speech-miner/guide')) {
    if (data.start && holdStart) { heldStart = route; return }
    if (failAnswer && !data.start) { failAnswer = false; await route.fulfill({ status: 500, json: { error: '模拟提取失败' } }); return }
    await route.fulfill({ json: {
      reply: data.start ? '模式开场：' + data.mode : '已经记下真实信息', completed: false,
      options: [], captured_facts: data.start ? [] : [{ category: 'product', key: '保存方式', value: data.message, source_quote: data.message }],
    } }); return
  }
  if (path.endsWith('/speech-miner/confirm')) {
    saves.push(data)
    await route.fulfill({ contentType: 'text/event-stream', body: 'event: complete\ndata: ' + JSON.stringify({ facts_saved: true, wheel_ready: !partialSave, notice: partialSave ? '可以重试保存' : '挖掘数据已保存', ...(partialSave ? { error: '模拟整理失败' } : {}) }) + '\n\n' }); return
  }
  throw new Error('Unexpected API: ' + path)
})
try {
  await page.goto(server.resolvedUrls.local[0] + '__save_exit')
  const buttons = page.locator('.speech-miner-chat-start > button'), input = page.locator('.speech-miner-composer textarea')
  await buttons.first().waitFor()
  for (const [index, mode] of ['new_selling_point','fact_gaps','audience_reverse'].entries()) {
    holdStart = true; heldStart = null
    await buttons.nth(index).click()
    await page.waitForFunction(() => document.querySelector('.speech-miner-messages .is-agent'))
    assert.match(await page.locator('.speech-miner-messages .is-agent').textContent(), /我们先/)
    assert.equal(await page.locator('.speech-miner-messages article').first().getAttribute('class'), 'is-user')
    assert.equal(await page.locator('.speech-miner-messages article').first().textContent(), ['开始挖掘卖点','开始挖掘内容','开始人群反推'][index])
    assert.match(await buttons.nth(index).textContent(), /保存退出/)
    assert.equal(await buttons.filter({ hasText: '保存退出' }).count(), 1)
    for (let other = 0; other < 3; other++) if (other !== index) assert.ok(await buttons.nth(other).isDisabled())
    while (!heldStart) await new Promise(resolve => setTimeout(resolve, 10))
    await heldStart.fulfill({ json: { reply: '模式开场：' + mode, options: [], captured_facts: [] } }); holdStart = false
    await page.getByText('模式开场：' + mode, { exact: true }).waitFor()
    assert.equal(await page.locator('.speech-miner-messages .is-user').count(), 1, 'initial user bubble must survive the model response')
    const before = saves.length
    if (index === 0) {
      await buttons.nth(index).click()
      assert.equal(saves.length, before, 'empty session must not save fabricated facts')
    } else {
      await input.fill('密封常温保存-' + mode)
      if (index === 2) partialSave = true
      await buttons.nth(index).click()
      if (partialSave) {
        await page.getByText('模拟整理失败', { exact: true }).waitFor()
        assert.match(await buttons.nth(index).textContent(), /保存退出/)
        assert.ok(await input.isDisabled())
        const first = saves.at(-1)
        partialSave = false
        await buttons.nth(index).click()
        await page.getByText('挖掘数据已保存', { exact: true }).waitFor()
        assert.equal(saves.at(-1).session_id, first.session_id)
        assert.deepEqual(saves.at(-1).facts, first.facts)
      } else await page.getByText('挖掘数据已保存', { exact: true }).waitFor()
      assert.equal(saves.at(-1).facts[0].value, '密封常温保存-' + mode)
      assert.equal(saves.at(-1).confirmed, true)
    }
    for (let other = 0; other < 3; other++) assert.ok(await buttons.nth(other).isEnabled())
    assert.equal(await buttons.filter({ hasText: '保存退出' }).count(), 0)
    assert.equal(await page.getByRole('dialog').count(), 0, 'save-and-exit must not require another popup')
    assert.equal(await page.locator('.speech-miner-options button').count(), 0)
    assert.equal(await input.inputValue(), '')
  }
  await buttons.nth(0).click()
  await page.getByText('模式开场：new_selling_point', { exact: true }).waitFor()
  failAnswer = true
  await input.fill('不丢失的未发送内容')
  const count = saves.length
  await buttons.nth(0).click()
  await page.locator('.speech-miner-error').waitFor()
  assert.equal(await input.inputValue(), '不丢失的未发送内容')
  assert.equal(saves.length, count)
  assert.match(await buttons.nth(0).textContent(), /保存退出/)
  await buttons.nth(0).click()
  await page.getByText('挖掘数据已保存', { exact: true }).waitFor()
  assert.equal(saves.at(-1).facts[0].value, '不丢失的未发送内容')
  assert.ok(calls.every(call => /\/speech-miner\/(guide|confirm)$/.test(call.path)))
  assert.deepEqual(errors, [])
  console.log('PASS: all three immediate openings; active-only save-exit; empty exit; unsent input extraction; successful reset; failed save/input preserved; idempotent retry; no publish')
} finally { await browser.close(); await server.close() }
