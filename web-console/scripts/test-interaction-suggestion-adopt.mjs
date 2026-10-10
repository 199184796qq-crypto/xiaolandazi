// Real strategy view; synthetic responses intercept every write.
import assert from 'node:assert/strict'
import { readFile, mkdir } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const source = await readFile(new URL('./test-interaction-full-view-location.mjs', import.meta.url), 'utf8')
let fixture = source.match(/const fixture = `([\s\S]*?)`\nconst deferredWorkspace/)[1]
fixture = fixture.replace("name:'测试方案',room_ids", "name:'测试方案',status:'active',room_ids")
fixture = fixture.replace("window.fixture={calls:[],records,delayWorkspace:location.search.includes('delayed=1')};", `
records[0].source_type='initial_simulation';records[0].question='跟实体店味道一样不';records[0].review.reason='已有线下门店规模事实，但尚未明确线上和门店产品的口味、工艺是否一致。';
records[0].review.meaning='线上线下口味比较';records[0].review.suggested_rule='需要用户补充后才能答复';records[0].review.needed_facts=['口味、工艺是否一致'];
window.fixture={calls:[],records,facts:[],hold:false,result:'success'};`)
fixture = fixture.replace("window.fixture.calls.push({url:String(url),method:init.method||'GET'});", "window.fixture.calls.push({url:String(url),method:init.method||'GET',body:init.body});")
fixture = fixture.replace("if(init.method&&init.method!=='GET')throw Error('Unexpected write '+p);", `
if(init.method==='POST'&&p.endsWith('/facts/adopt')){
 const body=JSON.parse(init.body);window.fixture.payload=body;
 if(window.fixture.result==='error')return Response.json({error:'模拟采纳失败'},{status:503});
 if(window.fixture.result==='conflict')return Response.json({adopted:0,conflicts:1,results:[{status:'conflict',message:'同名事实存在不同内容，请核对'}]});
 if(window.fixture.result==='unchanged')return Response.json({adopted:0,skipped:1,results:[{status:'unchanged'}]});
 if(window.fixture.hold)await new Promise(r=>window.fixture.releaseSave=r);
 const saved={...body.facts[0],id:90,source_ref:body.source_ref,plan_id:4,tenant_id:body.tenant_id,status:'active',version_no:1,updated_at:'2026-10-10T06:00:00Z'};
 window.fixture.facts.push(saved);return Response.json({adopted:1,results:[{status:'adopted',saved}]});
}
if(init.method&&init.method!=='GET')throw Error('Unexpected write '+p);
if(p.endsWith('/facts'))return Response.json({items:window.fixture.facts});
if(p==='/api/v1/live-agent-fact-categories')return Response.json({schema_version:1,version:'test',categories:[{value:'other',label:'其它事实',description:'补充信息',group:'业务事实',family:'other'}]});
if(p.endsWith('/content-pools'))return Response.json({state:'synced',products:[],benefits:[],pools:[],changes:[]});`)
const scoped = `<!doctype html><html><meta charset="utf-8"><div id="app"></div><script type="module">
import{createApp,h,ref}from'vue';import Workspace from'/src/components/InteractionRulesWorkspace.vue';import{applySession}from'/src/session.ts';
const room=ref(26),plan=ref(4);window.fixture={calls:[],events:[],setScope(r,p){room.value=r;plan.value=p}};
applySession({actor:{user_id:14,tenant_id:14,role:'customer'},tenants:[],environment:'test'});
window.fetch=async(url,init={})=>{window.fixture.calls.push({url:String(url),method:init.method||'GET',body:init.body});
 if(init.method==='POST'){await new Promise(r=>window.fixture.releaseSave=r);return Response.json({adopted:1,results:[{status:'adopted'}]})}
 return Response.json({items:[{id:9,room_id:room.value,tenant_id:14,source_type:'initial_simulation',status:'editing',question:'待确认问题',review:{plan_id:plan.value,kind:'missing_fact',reason:'当前条件尚未明确，请补充业务事实。'},updated_at:'2026-10-10T06:00:00Z'}],pending_count:1,total:1});
};createApp({render:()=>h(Workspace,{roomId:room.value,planId:plan.value,onFactsUpdated:e=>window.fixture.events.push(e)})}).mount('#app');</script></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom' })
server.middlewares.use(async (req, res, next) => {
  if (!req.url.startsWith('/__suggestion')) return next()
  res.setHeader('Content-Type', 'text/html;charset=utf-8')
  res.end(await server.transformIndexHtml(req.url.split('?')[0], req.url.includes('_scoped') ? scoped : fixture))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
try {
  const page = await browser.newPage({ viewport: { width: 1700, height: 1000 }, reducedMotion: 'reduce' }), errors = []
  page.setDefaultTimeout(6000)
  page.on('pageerror', error => errors.push(error.message))
  const base = server.resolvedUrls.local[0] + '__suggestion'
  const row = page.locator('[data-interaction-review-id="27"]')
  const button = row.locator('.suggestion-adopt')
  const writes = () => page.evaluate(() => window.fixture.calls.filter(c => c.method === 'POST'))
  await page.goto(base)
  await page.locator('.interaction-pending-badge').waitFor()
  await page.locator('.workflow-step-memory').click()
  await page.waitForFunction(() => document.activeElement?.getAttribute('data-interaction-review-id') === '27')
  assert.equal(await button.textContent(), '采纳')
  assert.equal(await row.locator('.model-suggestion .suggestion-adopt').count(), 1, 'action follows the displayed model suggestion')
  assert.equal((await writes()).length, 0)
  const reason = await row.locator('.model-suggestion span').textContent()
  await page.evaluate(() => window.fixture.result = 'error')
  await button.click()
  const position = await page.evaluate(() => window.scrollY)
  await row.getByRole('alert').filter({ hasText: '模拟采纳失败' }).waitFor()
  assert.equal(await button.isEnabled(), true)
  await page.evaluate(() => window.fixture.result = 'conflict')
  await button.click()
  await row.getByRole('alert').filter({ hasText: '同名事实存在不同内容' }).waitFor()
  assert.equal(await button.isEnabled(), true, 'conflicts never claim success or overwrite another fact')
  await page.evaluate(() => { window.fixture.result = 'success'; window.fixture.hold = true })
  await button.click()
  await page.waitForFunction(() => !!window.fixture.releaseSave)
  assert.equal(await button.textContent(), '采纳中…')
  const count = (await writes()).length
  await button.evaluate(el => el.click())
  assert.equal((await writes()).length, count)
  assert.equal(await page.getByRole('dialog').count(), 0, 'no confirmation or input modal is needed')
  const payload = await page.evaluate(() => window.fixture.payload)
  assert.equal(payload.tenant_id, 14)
  assert.equal(payload.source_ref, 'manual:interaction-learning-suggestion:room:26:record:27')
  assert.equal(payload.facts[0].key, '互动建议：跟实体店味道一样不')
  assert.equal(payload.facts[0].value, reason, 'exact adjacent suggestion is saved; no inferred answer, needed fact or rule replaces it')
  assert.equal(payload.facts[0].source_quote, '')
  await page.evaluate(() => window.fixture.releaseSave())
  await row.getByRole('status').filter({ hasText: '已采纳，已加入当前方案的事实依据。' }).waitFor()
  assert.equal(await button.textContent(), '已采纳')
  assert.equal(await button.isDisabled(), true)
  assert.equal(await page.getByRole('tab', { name: '模拟挖掘', exact: true }).getAttribute('aria-selected'), 'true')
  assert.equal(await page.locator('.workflow-step-memory.active').count(), 1)
  assert.ok(Math.abs(await page.evaluate(() => window.scrollY) - position) < 2)
  await page.waitForFunction(() => window.fixture.calls.filter(c => c.url.includes('/facts?')).length >= 2)
  // Persisted provenance identifies an adopted suggestion when tabs are revisited.
  await page.getByRole('tab', { name: '直播互动', exact: true }).click()
  await page.getByRole('tab', { name: '模拟挖掘', exact: true }).click()
  await button.waitFor()
  assert.equal(await button.textContent(), '已采纳')
  assert.equal(await button.isDisabled(), true)
  assert.equal(await row.getByRole('button', { name: '补充事实依据', exact: true }).isEnabled(), true)
  const out = new URL('../../deploy/validation/interaction-suggestion-adopt/', import.meta.url)
  await mkdir(out, { recursive: true })
  await row.screenshot({ path: fileURLToPath(new URL('adopted.png', out)) })
  assert.ok((await writes()).every(c => new URL(c.url, base).pathname === '/api/v1/live-agent-plans/4/facts/adopt'), 'no close, preview generation or paid update requests')
  await page.evaluate(() => { window.fixture.result = 'unchanged'; window.fixture.hold = false })
  await page.getByRole('tab', { name: '直播互动', exact: true }).click()
  await page.locator('[data-interaction-review-id="25"] .suggestion-adopt').click()
  await page.locator('[data-interaction-review-id="25"]').getByRole('status').filter({ hasText: '这条建议已经在当前方案的事实依据里啦。' }).waitFor()
  await page.evaluate(() => { window.fixture.records[0].status = 'closed' })
  await page.getByRole('tab', { name: '处理历史', exact: true }).click()
  await row.waitFor()
  assert.equal(await row.locator('.suggestion-adopt').count(), 0, 'processed records remain read-only')

  await page.goto(base + '_scoped')
  await page.getByRole('tab', { name: '模拟挖掘', exact: true }).click()
  await page.getByRole('button', { name: '采纳模型建议', exact: true }).click()
  await page.waitForFunction(() => !!window.fixture.releaseSave)
  await page.evaluate(() => window.fixture.setScope(30, 5))
  await page.getByRole('button', { name: '采纳模型建议', exact: true }).waitFor()
  assert.equal(await page.getByRole('button', { name: '采纳模型建议', exact: true }).isEnabled(), true)
  await page.evaluate(() => window.fixture.releaseSave())
  await page.waitForFunction(() => window.fixture.events.length === 0)
  assert.equal(await page.getByText('已采纳，已加入当前方案的事实依据。', { exact: true }).count(), 0)
  assert.equal((await writes()).length, 1)
  assert.ok((await writes())[0].url.includes('/live-agent-plans/4/facts/adopt'))
  assert.deepEqual(errors, [])
  console.log('PASS: adjacent one-click adoption; exact displayed suggestion; scoped facts only; no modal/model call/navigation; duplicate guard; inline error/conflict retry; persisted adopted state; unchanged response; history read-only; late response cannot affect a different plan')
} finally { await browser.close(); await server.close() }
