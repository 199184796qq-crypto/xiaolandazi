// Real component interaction with synthetic assets; no API writes or model calls.
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
import { readFile } from 'node:fs/promises'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const fixture = `<html><head><meta charset="utf-8"><style>*{box-sizing:border-box}body{padding:16px;font-family:Arial,'Microsoft Yahei',sans-serif}</style></head><body><div id="app"></div><script type="module">
import{createApp,h,ref,reactive}from'/node_modules/.vite/deps/vue.js';import Miner from'/src/components/SpeechMinerWorkbench.vue';
const points=Array.from({length:8},(_,i)=>({卖点名:'卖点'+i,痛点:'真实信息',目标人群:'关注信息的人群',角度:Array.from({length:7},(_,j)=>'内容'+i+'-'+j)}));
const reviews=reactive([{claim_sha256:'confirmed',source_key:'wheel/0/angle/0',status:'confirmed'},{claim_sha256:'pending',source_key:'facts/0/0',text:'内容7-6',severity:'P1',status:'pending',semantic_decision:'evidence_required',sources:[{source_key:'wheel/7/angle/6',text:'内容7-6'}]},{claim_sha256:'name',source_key:'wheel/3/name',severity:'P1',status:'pending'}]);
const miner=ref(null);window.fixture={reviews,locate:hash=>miner.value.locateRisk(hash)};
createApp({render:()=>h(Miner,{ref:miner,planId:4,wheel:{asset_id:77,wheel_json:JSON.stringify({卖点列表:points})},riskReviews:reviews})}).mount('#app');</script></body></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom' })
server.middlewares.use((req, res, next) => { if (req.url === '/__risk_location') { res.setHeader('Content-Type', 'text/html; charset=utf-8'); res.end(fixture) } else next() })
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } })
const errors = [], calls = []
page.on('pageerror', error => errors.push(error.message))
await page.route('**/api/v1/**', route => { calls.push(route.request().url()); return route.abort() })
try {
  await page.goto(server.resolvedUrls.local[0] + '__risk_location')
  const cards = page.locator('.speech-miner-point-list > article')
  await cards.nth(7).waitFor()
  await page.getByRole('button', { name: '全部收缩', exact: true }).click()
  assert.equal(await page.evaluate(() => window.fixture.locate('confirmed')), false)
  assert.equal(await cards.filter({ has: page.locator('.speech-miner-angle-area') }).count(), 0)
  assert.equal(await page.evaluate(() => window.fixture.locate('pending')), true)
  const target = page.locator('[data-wheel-source-key="wheel/7/angle/6"]')
  await target.waitFor()
  assert.equal(await cards.nth(7).getAttribute('class'), 'is-risk-located')
  assert.equal(await cards.nth(7).locator('.speech-miner-angle-area li').count(), 7, 'hidden angles must expand')
  assert.ok((await target.getAttribute('class')).includes('is-risk-located'))
  assert.equal(await target.evaluate(el => document.activeElement === el), true)
  await page.waitForFunction(() => document.querySelector('.speech-miner-point-list').scrollTop > 0)
  assert.equal(await page.locator('.speech-miner-point-list > article.is-collapsed').count(), 7, 'other cards stay collapsed')
  assert.equal(await page.evaluate(() => window.fixture.locate('name')), true)
  assert.equal(await page.locator('[data-wheel-source-key="wheel/3/name"]').evaluate(el => document.activeElement === el), true)
  assert.equal(await cards.locator('li.is-risk-located').count(), 0, 'next location removes previous highlight')
  await page.evaluate(() => { window.fixture.reviews[2].status = 'confirmed'; window.fixture.reviews.splice(0, 0) })
  await page.waitForFunction(() => !document.querySelector('article.is-risk-located'))
  assert.equal(await page.evaluate(() => window.fixture.locate('name')), false)
  assert.equal(await page.evaluate(() => window.fixture.locate('missing')), false)
  const view = await readFile(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
  assert.match(view, /workflow-step-miner[^>]+@click="openWheelRiskLocation"/)
  assert.match(view, /workflow-step-knowledge[^>]+@click="openFactRiskLocation"/)
  assert.ok(view.includes('ref="speechMinerWorkbench"'))
  assert.ok(view.includes('watch([anchorSellingPointWheelLoading, speechMinerWorkbench]'))
  assert.deepEqual(errors, [])
  assert.deepEqual(calls, [])
  console.log('PASS: collapsed/hidden risk navigation; merged source; exact focus/highlight; internal scrolling; confirmed/missing excluded; tab wiring; no API writes')
} finally { await browser.close(); await server.close() }
