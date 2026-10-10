// Browser-only preferences: synthetic content, no production API or model calls.
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const fixture = `<html><head><meta charset="utf-8"><style>*{box-sizing:border-box}body{padding:20px;background:#f3f5ff;font-family:Arial,'Microsoft Yahei',sans-serif}</style></head><body><div id="app"></div><script type="module">
import{createApp,h,reactive}from'/node_modules/.vite/deps/vue.js';import Miner from'/src/components/SpeechMinerWorkbench.vue';import{applySession}from'/src/session.ts';
const login=user=>applySession({actor:{user_id:user,tenant_id:14,role:'customer'}});login(101);
const points=['规格与价格','口味与包装','新鲜制作'].map(name=>({卖点名:name,痛点:'了解商品信息',目标人群:'消费者',角度:['已确认的真实内容']}));
const state=reactive({visible:true,planId:4,tenantId:14,roomId:26});window.fixture={state,login};
createApp({render:()=>state.visible?h(Miner,{planId:state.planId,tenantId:state.tenantId,roomId:state.roomId,wheel:{asset_id:77,wheel_json:JSON.stringify({卖点列表:points})}}):null}).mount('#app');
</script></body></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom' })
server.middlewares.use((req, res, next) => {
  if (req.url === '/__height_memory') { res.setHeader('Content-Type', 'text/html; charset=utf-8'); res.end(fixture) } else next()
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const page = await browser.newPage({ viewport: { width: 1600, height: 1050 } })
const errors = [], apiCalls = []
page.on('pageerror', error => errors.push(error.message))
await page.route('**/api/v1/**', route => { apiCalls.push(route.request().url()); return route.abort() })
const list = page.getByRole('separator', { name: '调整卖点列表高度' })
const chat = page.getByRole('separator', { name: '调整对话记录区高度' })
async function showResults() { await page.locator('.speech-miner-pool-select select').selectOption('legacy'); await list.waitFor() }
async function heights(expected) {
  assert.equal(Number(await list.getAttribute('aria-valuenow')), expected[0])
  assert.equal(Number(await chat.getAttribute('aria-valuenow')), expected[1])
  assert.equal((await page.locator('.speech-miner-point-list').boundingBox()).height, expected[0])
  assert.equal((await page.locator('.speech-miner-messages').boundingBox()).height, expected[1])
}
async function drag(handle, delta) {
  await handle.scrollIntoViewIfNeeded()
  const box = await handle.boundingBox(), x = box.x + box.width / 2, y = box.y + box.height / 2
  await page.mouse.move(x, y); await page.mouse.down(); await page.mouse.move(x, y + delta, { steps: 6 }); await page.mouse.up()
}
async function remount() {
  await page.evaluate(() => { window.fixture.state.visible = false })
  await page.locator('.speech-miner').waitFor({ state: 'hidden' })
  await page.evaluate(() => { window.fixture.state.visible = true })
  await showResults()
}
try {
  await page.goto(server.resolvedUrls.local[0] + '__height_memory')
  await showResults(); await heights([580, 460])
  await drag(list, 120); await drag(chat, 100); await heights([700, 560])
  await remount(); await heights([700, 560])
  await page.reload(); await showResults(); await heights([700, 560])
  await list.focus(); await page.keyboard.press('ArrowDown')
  await chat.focus(); await page.keyboard.press('ArrowUp')
  await remount(); await heights([740, 520])
  await page.evaluate(() => { window.fixture.login(102) }); await heights([580, 460])
  await drag(chat, 80); await heights([580, 540])
  await page.evaluate(() => { window.fixture.login(101) }); await heights([740, 520])
  await page.evaluate(() => { window.fixture.state.tenantId = 15 }); await showResults(); await heights([580, 460])
  await page.evaluate(() => { window.fixture.state.tenantId = 14 }); await showResults(); await heights([740, 520])
  await page.evaluate(() => { window.fixture.state.planId = 5; window.fixture.state.roomId = 27 }); await showResults(); await heights([740, 520])
  await chat.dblclick(); await remount(); await heights([740, 460])
  await page.evaluate(() => localStorage.setItem('speech-miner-heights:v1:101:14', '{broken'))
  await remount(); await heights([580, 460])
  await page.evaluate(() => localStorage.setItem('speech-miner-heights:v1:101:14', JSON.stringify({ listHeight: 50000, chatHeight: -20 })))
  await remount(); await heights([1000, 280])
  await page.evaluate(() => {
    const original = Storage.prototype.setItem
    Storage.prototype.setItem = function () { throw new DOMException('Blocked', 'SecurityError') }
    window.fixture.restoreStorage = () => { Storage.prototype.setItem = original }
  })
  await chat.focus(); await page.keyboard.press('ArrowDown'); await heights([1000, 320])
  await page.evaluate(() => window.fixture.restoreStorage())
  // Leaving while dragging saves the last size and removes global drag listeners.
  await list.focus(); await page.keyboard.press('Home')
  await chat.scrollIntoViewIfNeeded()
  const box = await chat.boundingBox()
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2); await page.mouse.down()
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2 + 60, { steps: 4 })
  await remount(); await page.mouse.up(); await heights([360, 380])
  assert.equal(await page.evaluate(() => document.body.className), '')
  assert.deepEqual(apiCalls, []); assert.deepEqual(errors, [])
  console.log('PASS: independent pointer/keyboard heights persist on reentry and reload; account/tenant isolation; shared across plans; reset persistence; invalid storage and bounds; blocked storage; unmount during drag; no API calls')
} finally { await browser.close(); await server.close() }
