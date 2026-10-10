// Real page, dialog and save handler; synthetic APIs only, no production writes.
import assert from 'node:assert/strict'
import { readFile, mkdir } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const catalog = JSON.parse(await readFile(new URL('../../configs/fact-categories.json', import.meta.url), 'utf8'))
const source = await readFile(new URL('./test-interaction-full-view-location.mjs', import.meta.url), 'utf8')
let fixture = source.match(/const fixture = `([\s\S]*?)`\nconst deferredWorkspace/)[1]
fixture = fixture.replace("name:'测试方案',room_ids", "name:'测试方案',status:'active',room_ids")
fixture = fixture.replace("window.fixture={calls:[],records,delayWorkspace:location.search.includes('delayed=1')};", `
window.fixture={calls:[],records,facts:[],saveMode:'success',holdSave:false,holdName:false};
import {feedbackState} from '/src/uiFeedback.ts';window.fixture.feedback=feedbackState;`)
fixture = fixture.replace("if(init.method&&init.method!=='GET')throw Error('Unexpected write '+p);", `
if(init.method==='POST'&&p.endsWith('/fact-name-suggestion')){
 const body=JSON.parse(init.body);window.fixture.calls.at(-1).body=body;
 if(window.fixture.holdName)await new Promise(r=>window.fixture.releaseName=r);
 return Response.json({name:'发货时效'});
}
if(init.method==='POST'&&p.endsWith('/facts/adopt')){
 const body=JSON.parse(init.body);window.fixture.calls.at(-1).body=body;
 if(window.fixture.holdSave)await new Promise(r=>window.fixture.releaseSave=r);
 if(window.fixture.saveMode==='error')return Response.json({error:'测试保存失败，请重试'},{status:500});
 const saved={...body.facts[0],id:100+window.fixture.facts.length,plan_id:4,tenant_id:body.tenant_id,status:'active',version_no:1,updated_at:'2026-10-10T09:00:00Z'};
 window.fixture.facts.push(saved);return Response.json({adopted:1,results:[{status:'adopted',saved}]});
}
if(init.method&&init.method!=='GET')throw Error('Unexpected write '+p);
if(p==='/api/v1/live-agent-fact-categories')return Response.json(${JSON.stringify(catalog)});
if(p.endsWith('/realtime-options'))return Response.json({buffer_seconds:60,body_min:2,body_max:4,max_chars:300});
if(p.endsWith('/facts'))return Response.json({items:window.fixture.facts});`)
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom' })
server.middlewares.use(async (req, res, next) => {
  if (req.url !== '/__formal_fact_create') return next()
  res.setHeader('Content-Type', 'text/html; charset=utf-8')
  res.end(await server.transformIndexHtml(req.url, fixture))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const page = await browser.newPage({ viewport: { width: 1774, height: 930 }, reducedMotion: 'reduce' }), errors = []
page.on('pageerror', error => errors.push(error.message))
page.setDefaultTimeout(7000)
const modal = page.getByRole('dialog', { name: '添加事实', exact: true })
const add = page.getByRole('button', { name: '＋ 新增事实依据', exact: true })
const content = modal.getByRole('textbox', { name: '事实内容（可以说）', exact: true })
const name = modal.getByRole('textbox', { name: '事实名称', exact: true })
const save = modal.getByRole('button', { name: '保存事实依据', exact: true })
const writes = () => page.evaluate(() => window.fixture.calls.filter(call => call.method === 'POST'))
const out = new URL('../../deploy/validation/formal-fact-create/', import.meta.url)
try {
  await page.goto(server.resolvedUrls.local[0] + '__formal_fact_create')
  await page.locator('.workflow-step-knowledge').click()
  await add.waitFor(); await add.click(); await modal.waitFor()
  assert.equal(await content.evaluate(el => document.activeElement === el), true)
  assert.equal(await modal.evaluate(el => el.matches(':modal')), true, 'background must be inert')
  assert.equal(await page.evaluate(() => document.body.style.overflow), 'hidden')
  assert.ok(await save.isDisabled()); assert.match(await modal.textContent(), /先写一点真实内容/)
  assert.equal(await modal.locator('.fact-create-field').count(), 5)
  assert.equal((await modal.locator('select option').count()), 42)
  assert.equal((await writes()).length, 0)
  await mkdir(out, { recursive: true })
  await page.screenshot({ path: fileURLToPath(new URL('desktop.png', out)) })
  await content.fill('一般当天发货，下午六点后次日上午发')
  await page.waitForFunction(() => document.querySelector('[aria-label="事实名称"]').value === '发货时效')
  assert.ok(await save.isEnabled())
  assert.equal(await modal.locator('.is-content .fact-create-textarea>small').textContent(), '一般当天发货，下午六点后次日上午发'.length + '/4000')
  await name.fill('自定义发货规则')
  await modal.getByRole('combobox', { name: '事实分类' }).selectOption('shipping_time')
  await modal.getByRole('textbox', { name: '不要这样说（选填）' }).fill('当天一定送达')
  await modal.getByRole('textbox', { name: '改成这样说（选填）' }).fill('具体到货时间以物流信息为准')
  await save.focus(); await page.keyboard.press('Tab')
  assert.equal(await modal.getByRole('button', { name: '关闭添加事实' }).evaluate(el => el === document.activeElement), true)
  await page.keyboard.press('Shift+Tab'); assert.equal(await save.evaluate(el => el === document.activeElement), true)
  await page.evaluate(() => { window.fixture.saveMode = 'error' })
  await save.click(); await modal.getByRole('alert').filter({ hasText: '测试保存失败，请重试' }).waitFor()
  assert.equal(await content.inputValue(), '一般当天发货，下午六点后次日上午发')
  assert.equal(await name.inputValue(), '自定义发货规则')
  assert.equal(await page.evaluate(() => window.fixture.feedback.alertOpen), false, 'errors must stay in the native dialog: ' + await page.evaluate(() => window.fixture.feedback.alertMessage))
  await page.screenshot({ path: fileURLToPath(new URL('retry-keeps-draft.png', out)) })
  await page.evaluate(() => { window.fixture.saveMode = 'success' })
  await save.click(); await modal.waitFor({ state: 'hidden' })
  assert.equal(await page.evaluate(() => document.body.style.overflow), '')
  await page.getByText('自定义发货规则', { exact: true }).waitFor()
  const posts = await writes(), saved = posts.filter(call => call.url.includes('/facts/adopt')).at(-1)
  assert.match(saved.url, /live-agent-plans\/4\/facts\/adopt$/)
  assert.equal(saved.body.tenant_id, 14); assert.equal(saved.body.source_ref, 'manual:fact-editor')
  assert.deepEqual(saved.body.facts[0], { category: 'shipping_time', key: '自定义发货规则', value: '一般当天发货，下午六点后次日上午发', forbidden_wording: '当天一定送达', safe_rewrite: '具体到货时间以物流信息为准', status: 'pending', review_bucket: 'adoptable', source_quote: '' })
  assert.ok((await page.locator('.workflow-step-knowledge').getAttribute('class')).includes('active'), 'save must stay on the fact page')
  await add.click(); await content.waitFor(); assert.equal(await content.inputValue(), '')
  await page.keyboard.press('Escape'); await modal.waitFor({ state: 'hidden' })
  assert.equal(await add.evaluate(el => el === document.activeElement), true, 'closing restores the launching button focus')
  await add.click(); await name.fill('长内容测试'); await content.fill('真实内容'.repeat(180))
  assert.equal(await content.inputValue(), '真实内容'.repeat(180), 'reference counters must not truncate longer existing facts')
  assert.equal(await modal.locator('.is-content .fact-create-textarea>small').textContent(), '720/4000')
  await page.setViewportSize({ width: 390, height: 850 })
  assert.ok(await modal.evaluate(el => el.scrollWidth <= el.clientWidth), 'mobile dialog must not overflow sideways')
  const box = await modal.boundingBox(); assert.ok(box.x >= 0 && box.x + box.width <= 390)
  await page.screenshot({ path: fileURLToPath(new URL('mobile.png', out)) })
  await modal.getByRole('button', { name: '取消', exact: true }).click(); await modal.waitFor({ state: 'hidden' })
  assert.equal((await writes()).length, posts.length, 'cancel must not save or call a model for manually named content')
  await page.setViewportSize({ width: 1774, height: 930 }); await add.click()
  await name.fill('保存期间测试'); await content.fill('保存期间完整保留的真实内容')
  await page.evaluate(() => { window.fixture.holdSave = true })
  await save.click(); await page.waitForFunction(() => !!window.fixture.releaseSave)
  assert.ok(await modal.getByRole('button', { name: '关闭添加事实' }).isDisabled())
  assert.ok(await content.isDisabled()); assert.equal(await modal.getAttribute('aria-busy'), 'true')
  await page.keyboard.press('Escape'); assert.equal(await modal.count(), 1, 'Escape cannot close while saving')
  await page.evaluate(() => window.fixture.releaseSave()); await modal.waitFor({ state: 'hidden' })
  await page.getByText('保存期间测试', { exact: true }).waitFor()
  assert.deepEqual(errors, [])
  console.log('PASS: real page native add-fact modal; inert background/focus trap/return; guided panels/live counts; auto and manual names; scoped save of all fields; inline failure and retry; cancel/no navigation; long content; mobile; busy close guard')
} finally { await browser.close(); await server.close() }
