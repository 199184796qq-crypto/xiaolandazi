// Exercise real multi-select UI. API responses are mocked; no database/model writes.
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const fixture = `<html><head><meta charset="utf-8"><style>body{padding:16px}</style></head><body><div id="app"></div><script type="module">import{createApp,h}from'/node_modules/.vite/deps/vue.js';import Miner from'/src/components/SpeechMinerWorkbench.vue';createApp({render:()=>h(Miner,{planId:4,tenantId:14})}).mount('#app')</script></body></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom' })
server.middlewares.use((req,res,next) => { if (req.url === '/__multiple') {res.setHeader('Content-Type','text/html; charset=utf-8');res.end(fixture)} else next() })
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } }), calls = [], errors = []
page.on('pageerror', error => errors.push(error.message))
const multiple = {reply:'他们看重门店哪些方面？可以选多项。',selection_mode:'multiple',options:[{label:'数量多',value:'数量多',action:'answer'},{label:'总店开得久',value:'总店开得久',action:'answer'},{label:'门店地址具体',value:'门店地址具体',action:'answer'},{label:'我不清楚，问下一个',value:'我不清楚，问下一个',action:'skip'}],captured_facts:[],completed:false}
await page.route('**/api/v1/**', async route => {
  assert.ok(route.request().url().includes('/speech-miner/guide'))
  const data = route.request().postDataJSON(); calls.push(data)
  const reply = data.action === 'skip' ? { ...multiple, reply: '这一款的固定辣度是哪一种？',selection_mode:'single',options:[{label:'微辣',value:'微辣',action:'answer'},{label:'中辣',value:'中辣',action:'answer'}] } : multiple
  await route.fulfill({json:reply})
})
try {
  await page.goto(server.resolvedUrls.local[0] + '__multiple')
  await page.getByRole('button', {name:'按目标人群反推卖点',exact:true}).click()
  await page.getByText('可多选', {exact:true}).waitFor()
  assert.equal(await page.locator('.speech-miner-messages article').first().textContent(),'开始人群反推')
  await page.locator('.speech-miner-option').filter({hasText:'数量多'}).click()
  await page.locator('.speech-miner-option').filter({hasText:'总店开得久'}).click()
  assert.equal(calls.length,1,'selecting must not immediately submit')
  assert.equal(await page.locator('.speech-miner-option[aria-pressed="true"]').count(),2)
  await page.getByRole('button',{name:'提交所选',exact:true}).click()
  await page.waitForFunction(() => document.querySelector('.speech-miner-composer textarea:not(:disabled)'))
  assert.equal(calls.at(-1).message,'数量多；总店开得久')
  assert.equal(calls.at(-1).action,'answer')
  assert.equal(await page.locator('.speech-miner-option[aria-pressed="true"]').count(),0)
  await page.locator('.speech-miner-option').filter({hasText:'数量多'}).click()
  await page.getByRole('button',{name:/我不清楚，问下一个/}).click()
  await page.getByText('这一款的固定辣度是哪一种？',{exact:true}).waitFor()
  assert.equal(calls.at(-1).action,'skip')
  assert.equal(calls.at(-1).message,'我不清楚，问下一个','skip must not bundle selected answers')
  assert.equal(await page.locator('.speech-miner-multi-submit').count(),0)
  await page.getByRole('button',{name:/微辣/}).click()
  await page.getByText('可多选',{exact:true}).waitFor()
  assert.equal(calls.at(-1).message,'微辣','mutually exclusive answer still sends directly')
  assert.deepEqual(errors,[])
  console.log('PASS: first user bubble; multiple coexisting answers selected before submit; skip independent; exclusive choices remain single; no save/publish')
} finally { await browser.close(); await server.close() }
