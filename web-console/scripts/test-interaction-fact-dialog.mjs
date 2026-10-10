// Real view and components; all API reads and writes use synthetic fixtures.
import assert from 'node:assert/strict'
import { readFile, mkdir } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const categoryCatalog = { schema_version: 1, version: 'test-v1', categories: [
  { value: 'product', label: '商品事实', description: '商品的已确认信息', group: '业务事实', family: 'product' },
  { value: 'other', label: '其它事实', description: '补充信息', group: '业务事实', family: 'other' },
] }
const source = await readFile(new URL('./test-interaction-full-view-location.mjs', import.meta.url), 'utf8')
let fixture = source.match(/const fixture = `([\s\S]*?)`\nconst deferredWorkspace/)[1]
fixture = fixture.replace("name:'测试方案',room_ids", "name:'测试方案',status:'active',room_ids")
fixture = fixture.replace("window.fixture={calls:[],records,delayWorkspace:location.search.includes('delayed=1')};", `
records[0].source_type='initial_simulation';records[0].question='跟实体店味道一样不';records[0].review.needed_facts=['线上售卖产品与线下实体门店产品的口味、工艺是否一致'];
window.fixture={calls:[],records,facts:[],saveResult:'success',hold:false};`)
fixture = fixture.replace("if(init.method&&init.method!=='GET')throw Error('Unexpected write '+p);", `
if(init.method==='POST'&&p.endsWith('/facts/adopt')){
 const body=JSON.parse(init.body);window.fixture.payload=body;
 if(window.fixture.saveResult==='conflict')return Response.json({adopted:0,conflicts:1,results:[{status:'conflict',message:'同名事实已有不同内容，请核对'}]});
 if(window.fixture.saveResult==='error')return Response.json({error:'模拟保存失败，请重试'},{status:500});
 if(window.fixture.saveResult==='unchanged')return Response.json({adopted:0,skipped:1,results:[{status:'unchanged'}]});
 if(window.fixture.hold)await new Promise(r=>window.fixture.releaseSave=r);
 const saved={...body.facts[0],id:90,plan_id:4,tenant_id:body.tenant_id,status:'active',version_no:1,updated_at:'2026-10-10T06:00:00Z'};
 window.fixture.facts.push(saved);return Response.json({adopted:1,results:[{status:'adopted',saved}]});
}
if(init.method&&init.method!=='GET')throw Error('Unexpected write '+p);
if(p==='/api/v1/live-agent-fact-categories')return Response.json(${JSON.stringify(categoryCatalog)});
if(p.endsWith('/facts'))return Response.json({items:window.fixture.facts});
if(p.endsWith('/content-pools'))return Response.json({state:window.fixture.facts.length?'needs_update':'synced',products:[],benefits:[],pools:[],changes:[]});`)
const scopedFixture = `<!doctype html><html><meta charset="utf-8"><link rel="icon" href="data:,"><div id="app"></div><script type="module">
import{createApp,h,ref}from'vue';import Workspace from'/src/components/InteractionRulesWorkspace.vue';import{applySession}from'/src/session.ts';
const room=ref(26),plan=ref(4);window.fixture={calls:[],events:[],categoryError:false,hold:false,setScope(r,p){room.value=r;plan.value=p}};
applySession({actor:{user_id:14,tenant_id:14,role:'customer'},tenants:[],environment:'test'});
window.fixture.setActor=()=>applySession({actor:{user_id:15,tenant_id:15,role:'customer'},tenants:[],environment:'test'});
window.fetch=async(url,init={})=>{const p=new URL(url,location.origin).pathname;window.fixture.calls.push({url:String(url),method:init.method||'GET',body:init.body});
 if(p==='/api/v1/live-agent-fact-categories')return window.fixture.categoryError?Response.json({error:'分类暂时不可用'},{status:503}):Response.json(${JSON.stringify(categoryCatalog)});
 if(init.method==='POST'){if(window.fixture.hold)await new Promise(r=>window.fixture.releaseSave=r);return Response.json({adopted:1,results:[{status:'adopted'}]})}
 return Response.json({items:[{id:9,room_id:room.value,tenant_id:14,status:'editing',source_type:'initial_simulation',question:'需要补充的事实',updated_at:'2026-10-10T06:00:00Z',review:{plan_id:plan.value,kind:'missing_fact',needed_facts:['产品口味']}}],total:1,pending_count:1});
};
createApp({render:()=>h(Workspace,{roomId:room.value,planId:plan.value,onFactsUpdated:e=>window.fixture.events.push(e)})}).mount('#app');</script></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom' })
server.middlewares.use(async (req, res, next) => {
  if (!req.url.startsWith('/__fact_dialog')) return next()
  res.setHeader('Content-Type', 'text/html;charset=utf-8')
  res.end(await server.transformIndexHtml(req.url.split('?')[0], req.url.includes('_scoped') ? scopedFixture : fixture))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
try {
  const page = await browser.newPage({ viewport: { width: 1700, height: 1000 }, reducedMotion: 'reduce' }), errors = []
  page.on('pageerror', error => errors.push(error.message))
  page.setDefaultTimeout(6000)
  const base = server.resolvedUrls.local[0] + '__fact_dialog'
  const dialog = page.getByRole('dialog', { name: '补充事实依据', exact: true })
  const row = page.locator('[data-interaction-review-id="27"]')
  const open = row.getByRole('button', { name: '补充事实依据', exact: true })
  const save = dialog.getByRole('button', { name: '保存事实依据', exact: true })
  const writes = () => page.evaluate(() => window.fixture.calls.filter(c => c.method === 'POST'))
  await page.goto(base)
  await page.locator('.interaction-pending-badge').waitFor()
  await page.locator('.workflow-step-memory').click()
  await page.waitForFunction(() => document.activeElement?.getAttribute('data-interaction-review-id') === '27')
  assert.equal(await page.getByRole('tab', { name: '模拟挖掘', exact: true }).getAttribute('aria-selected'), 'true')
  await open.click()
  await dialog.waitFor()
  const position = await page.evaluate(() => window.scrollY)
  assert.equal(await dialog.getByLabel('事实内容', { exact: true }).inputValue(), '', 'suggested facts never become asserted fact content')
  assert.equal(await save.isDisabled(), true)
  assert.equal(await page.evaluate(() => document.activeElement.tagName), 'TEXTAREA')
  assert.equal(await page.locator('.workflow-step-memory.active').count(), 1, 'opening keeps the current module')
  assert.equal((await writes()).length, 0, 'opening has no writes or model calls')
  // Native modal keeps focus inside it and restores focus without scrolling.
  for (let i = 0; i < 8; i++) { await page.keyboard.press('Tab'); assert.equal(await page.evaluate(() => document.querySelector('dialog').contains(document.activeElement)), true) }
  await page.keyboard.press('Escape')
  await dialog.waitFor({ state: 'detached' })
  assert.equal(await open.evaluate(el => el === document.activeElement), true)
  assert.ok(Math.abs((await page.evaluate(() => window.scrollY)) - position) < 2)
  await open.click()
  await dialog.getByLabel('事实分类', { exact: true }).selectOption('product')
  await dialog.getByLabel('事实名称', { exact: true }).fill(' 线上线下口味与工艺 ')
  await dialog.getByLabel('事实内容', { exact: true }).fill(' 线上线下使用相同配方，线上为真空包装，口感会受保存与复热方式影响。 ')
  const out = new URL('../../deploy/validation/interaction-fact-dialog/', import.meta.url)
  await mkdir(out, { recursive: true })
  await dialog.screenshot({ path: fileURLToPath(new URL('desktop-dialog.png', out)) })
  await page.evaluate(() => window.fixture.saveResult = 'conflict')
  await save.click()
  await dialog.getByRole('alert').filter({ hasText: '同名事实已有不同内容，请核对' }).waitFor()
  assert.ok((await dialog.getByLabel('事实内容', { exact: true }).inputValue()).includes('真空包装'))
  await page.evaluate(() => window.fixture.saveResult = 'error')
  await save.click()
  await dialog.getByRole('alert').filter({ hasText: '模拟保存失败，请重试' }).waitFor()
  assert.equal(await save.isEnabled(), true, 'failed save keeps the draft and allows retry')
  await dialog.screenshot({ path: fileURLToPath(new URL('failed-save-keeps-draft.png', out)) })
  await page.evaluate(() => { window.fixture.saveResult = 'success'; window.fixture.hold = true })
  await save.click()
  await page.waitForFunction(() => !!window.fixture.releaseSave)
  const beforeDuplicate = (await writes()).length
  await dialog.locator('form').evaluate(el => el.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })))
  await page.keyboard.press('Escape')
  assert.equal(await dialog.count(), 1, 'cannot dismiss the request while saving')
  assert.equal((await writes()).length, beforeDuplicate, 'duplicate submits do not make another request')
  const payload = await page.evaluate(() => window.fixture.payload)
  assert.equal(payload.tenant_id, 14)
  assert.equal(payload.source_ref, 'manual:interaction-learning:room:26:record:27')
  assert.equal(payload.facts[0].category, 'product')
  assert.equal(payload.facts[0].key, '线上线下口味与工艺')
  assert.equal(payload.facts[0].value, '线上线下使用相同配方，线上为真空包装，口感会受保存与复热方式影响。')
  assert.equal(payload.facts[0].source_quote, '', 'model suggestion is not cited as user evidence')
  await page.evaluate(() => window.fixture.releaseSave())
  await dialog.waitFor({ state: 'detached' })
  await row.getByRole('status').filter({ hasText: '事实依据已保存到当前方案' }).waitFor()
  assert.equal(await page.getByRole('tab', { name: '模拟挖掘', exact: true }).getAttribute('aria-selected'), 'true')
  assert.equal(await open.evaluate(el => el === document.activeElement), true)
  assert.ok(Math.abs((await page.evaluate(() => window.scrollY)) - position) < 2, 'saving leaves the page position unchanged')
  assert.equal(await row.locator('.pending-question-dot').count(), 1, 'adding a fact does not silently dismiss the pending question')
  const calls = await page.evaluate(() => window.fixture.calls)
  assert.ok(calls.filter(c => new URL(c.url, base).pathname.endsWith('/facts')).length >= 2, 'root reloads the formal facts after saving')
  assert.ok((await writes()).every(c => new URL(c.url, base).pathname === '/api/v1/live-agent-plans/4/facts/adopt'), 'only the scoped fact save was written')
  await page.evaluate(() => { window.fixture.saveResult = 'unchanged'; window.fixture.hold = false })
  await open.click()
  await dialog.getByLabel('事实内容', { exact: true }).fill('重复的已保存事实')
  await save.click()
  await dialog.waitFor({ state: 'detached' })
  await row.getByRole('status').filter({ hasText: '当前方案已包含这条事实' }).waitFor()
  await page.setViewportSize({ width: 390, height: 850 })
  await open.click()
  await dialog.waitFor()
  const mobileBox = await dialog.boundingBox()
  assert.ok(mobileBox.x >= 0 && mobileBox.x + mobileBox.width <= 390 && mobileBox.height <= 826)
  assert.equal(await dialog.evaluate(el => el.scrollWidth > el.clientWidth), false)
  await page.screenshot({ path: fileURLToPath(new URL('mobile-dialog.png', out)) })
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await page.setViewportSize({ width: 1700, height: 1000 })
  await page.getByRole('tab', { name: '直播互动', exact: true }).click()
  await page.locator('[data-interaction-review-id="25"]').getByRole('button', { name: '补充事实依据', exact: true }).click()
  await dialog.waitFor()
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  assert.equal(await page.getByRole('tab', { name: '直播互动', exact: true }).getAttribute('aria-selected'), 'true')

  // Scope changes and late responses cannot update or write the newly selected plan.
  await page.goto(base + '_scoped')
  await page.getByRole('tab', { name: '模拟挖掘', exact: true }).click()
  const scopedOpen = page.getByRole('button', { name: '补充事实依据', exact: true })
  await page.evaluate(() => window.fixture.categoryError = true)
  await scopedOpen.click()
  await dialog.getByRole('alert').filter({ hasText: '分类暂时不可用' }).waitFor()
  assert.equal(await save.isDisabled(), true)
  await page.evaluate(() => window.fixture.categoryError = false)
  await dialog.getByRole('button', { name: '重试读取分类', exact: true }).click()
  await dialog.getByLabel('事实内容', { exact: true }).fill('用户填写的真实产品口味')
  await page.evaluate(() => window.fixture.hold = true)
  await save.click()
  await page.waitForFunction(() => !!window.fixture.releaseSave)
  await page.evaluate(() => window.fixture.setScope(30, 5))
  await dialog.waitFor({ state: 'detached' })
  await page.evaluate(() => window.fixture.releaseSave())
  await scopedOpen.waitFor()
  assert.equal(await page.evaluate(() => window.fixture.events.length), 0, 'late success does not refresh a different plan')
  const scopedCalls = await writes()
  assert.equal(scopedCalls.length, 1)
  assert.ok(scopedCalls[0].url.includes('/live-agent-plans/4/facts/adopt'))
  await scopedOpen.click()
  assert.equal(await dialog.getByLabel('事实内容', { exact: true }).inputValue(), '', 'scope switch drops the previous draft')
  await page.evaluate(() => window.fixture.setActor())
  await dialog.waitFor({ state: 'detached' })
  assert.deepEqual(errors, [])
  console.log('PASS: in-place simulation/live modal; empty fact content; scoped manual save; root fact refresh; focus/scroll retained; conflict/error retry; duplicates guarded; pending issue preserved; unchanged result; mobile; category retry; plan/actor change and stale response isolation; no real API writes or model calls')
} finally { await browser.close(); await server.close() }
