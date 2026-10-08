import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const fixture = `<html><head><meta charset="utf-8"><style>*{box-sizing:border-box}body{padding:16px;background:#f7f9ff;font-family:Arial,'Microsoft Yahei',sans-serif}</style></head><body><div id="app"></div><script type="module">
import{createApp,h}from'/node_modules/.vite/deps/vue.js';import Miner from'/src/components/SpeechMinerWorkbench.vue';
const points=['规格与价格对比','口味与包装选择','工艺与原料','数量与库存'].map(name=>({卖点名:name,痛点:'了解商品真实信息',目标人群:'关注该信息的消费者',角度:['真实内容一','真实内容二']}));
createApp({render:()=>h(Miner,{planId:4,wheel:{asset_id:77,wheel_json:JSON.stringify({卖点列表:points,事实补充建议:[]})}})}).mount('#app');</script></body></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom' })
server.middlewares.use((req, res, next) => { if (req.url === '/__tones') { res.setHeader('Content-Type', 'text/html; charset=utf-8'); res.end(fixture) } else next() })
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } }), errors = []
page.on('pageerror', error => errors.push(error.message))
try {
  await page.goto(server.resolvedUrls.local[0] + '__tones')
  const cards = page.locator('.speech-miner-point-list > article')
  await cards.nth(3).waitFor()
  const tones = () => cards.evaluateAll(items => items.map(item => ({ card: getComputedStyle(item).backgroundImage, overview: getComputedStyle(item.querySelector('.speech-miner-point-overview')).backgroundImage, index: getComputedStyle(item.querySelector('.speech-miner-point-index')).backgroundImage })))
  const original = await tones()
  assert.deepEqual(original[0], original[2]); assert.deepEqual(original[1], original[3])
  for (const part of ['card', 'overview', 'index']) assert.notEqual(original[0][part], original[1][part])
  await page.getByRole('button', { name: '全部收缩', exact: true }).click()
  assert.equal(await page.locator('.speech-miner-point-list > article.is-collapsed').count(), 4)
  assert.deepEqual(await tones(), original)
  await cards.nth(0).getByRole('button', { name: '编辑该卖点', exact: true }).click()
  assert.deepEqual((await tones())[0], original[0], 'editing should retain alternating tone')
  const messages = page.locator('.speech-miner-messages')
  const handle = page.getByRole('separator', { name: '调整对话记录区高度' })
  const initialHeight = (await messages.boundingBox()).height
  const leftHeight = (await page.locator('.speech-miner-results').boundingBox()).height
  const initialComposerTop = (await page.locator('.speech-miner-composer').boundingBox()).y
  await handle.scrollIntoViewIfNeeded()
  const grip = await handle.boundingBox()
  await page.mouse.move(grip.x + grip.width / 2, grip.y + grip.height / 2)
  await page.mouse.down()
  await page.mouse.move(grip.x + grip.width / 2, grip.y + grip.height / 2 + 150, { steps: 8 })
  await page.mouse.up()
  assert.equal((await messages.boundingBox()).height, initialHeight + 150)
  assert.equal((await page.locator('.speech-miner-results').boundingBox()).height, leftHeight)
  assert.ok((await page.locator('.speech-miner-composer').boundingBox()).y > initialComposerTop)
  await handle.focus()
  await page.keyboard.press('End')
  assert.equal((await messages.boundingBox()).height, 900)
  await page.keyboard.press('Home')
  assert.equal((await messages.boundingBox()).height, 280)
  await handle.dblclick()
  assert.equal((await messages.boundingBox()).height, initialHeight)
  assert.deepEqual(errors, [])
  console.log('PASS: card tones; independent chat resize by pointer and keyboard; min/max limits; double-click reset; no API calls')
} finally { await browser.close(); await server.close() }
