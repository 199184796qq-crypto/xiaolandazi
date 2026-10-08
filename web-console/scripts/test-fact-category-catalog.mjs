import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
let catalog = JSON.parse(await readFile(new URL('../../configs/fact-categories.json', import.meta.url), 'utf8'))
const fixture = `<html><body><div id="app"></div><script type="module">import{createApp,h,ref}from'/node_modules/.vite/deps/vue.js';import Badge from '/src/components/FactCategoryBadge.vue';import{factCategoryState,factCategoryGroups,factCategoryDefinition,loadFactCategories}from'/src/factCategories.ts';const chosen=ref('other');window.fixture={state:factCategoryState,load:loadFactCategories,lookup:factCategoryDefinition,chosen};createApp({render:()=>h('section',[h('select',{value:chosen.value,onChange:e=>chosen.value=e.target.value},factCategoryGroups(chosen.value).map(group=>h('optgroup',{label:group.label},group.options.map(item=>h('option',{value:item.value},item.label))))),h(Badge,{category:chosen.value,label:factCategoryDefinition(chosen.value).label,family:factCategoryDefinition(chosen.value).family}),h('p',factCategoryState.error)])}).mount('#app');await loadFactCategories();</script></body></html>`
const server=await createServer({server:{host:'127.0.0.1',port:0},appType:'custom'})
server.middlewares.use((req,res,next)=>{if(req.url==='/__catalog'){res.setHeader('Content-Type','text/html; charset=utf-8');res.end(fixture)}else next()});await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const page=await browser.newPage({viewport:{width:1280,height:800}}),errors=[],calls=[]
let fail=false
page.on('pageerror',error=>errors.push(error.message))
await page.route('**/api/v1/**',async route=>{
  calls.push(route.request().method()+' '+new URL(route.request().url()).pathname)
  assert.equal(new URL(route.request().url()).pathname,'/api/v1/live-agent-fact-categories')
  await route.fulfill(fail?{status:500,json:{error:'模拟分类配置损坏'}}:{json:catalog})
})
try {
  await page.goto(server.resolvedUrls.local[0]+'__catalog')
  await page.waitForFunction(()=>document.querySelectorAll('select option').length===42)
  assert.equal(await page.locator('optgroup').count(),7)
  await page.locator('select').selectOption('shipping_time')
  assert.equal(await page.locator('.fact-category-badge').textContent(),'发货时间 / 截单时间')
  assert.ok((await page.locator('.fact-category-badge').getAttribute('class')).includes('is-fulfillment'))
  catalog={...catalog,version:'test-hot-reload',categories:[...catalog.categories,{value:'custom_color',label:'自定义颜色信息',description:'新的配置项',group:'商品信息',family:'product'}]}
  await page.evaluate(()=>window.fixture.load())
  assert.equal(await page.locator('select option').count(),43)
  await page.locator('select').selectOption('custom_color')
  assert.equal(await page.locator('.fact-category-badge').textContent(),'自定义颜色信息')
  assert.ok((await page.locator('.fact-category-badge').getAttribute('class')).includes('is-product'))
  await page.evaluate(()=>window.fixture.chosen.value='retired_category')
  await page.locator('select option[value="retired_category"]').waitFor({state:'attached'})
  assert.equal(await page.locator('select').inputValue(),'retired_category','historical unknown category must not silently change to other')
  fail=true;await page.evaluate(()=>window.fixture.load())
  assert.ok((await page.evaluate(()=>window.fixture.state.error)).length>0)
  assert.equal(await page.locator('select option[value="custom_color"]').count(),1,'existing UI state retained with visible error')
  fail=false;await page.evaluate(()=>window.fixture.load())
  assert.equal(await page.evaluate(()=>window.fixture.state.error),'')
  assert.ok(calls.every(call=>call.startsWith('GET ')))
  assert.deepEqual(errors,[])
  console.log('PASS: server JSON -> 42 grouped choices; family colors; hot-added category without build; historical values retained; visible failure/retry; read-only requests')
} finally {await browser.close();await server.close()}
