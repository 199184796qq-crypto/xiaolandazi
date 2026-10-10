// Real workspace and panels; held synthetic API responses, no paid models.
import assert from 'node:assert/strict'
import { mkdir } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const fixture = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><style>*{box-sizing:border-box}body{margin:0;padding:28px;background:#eef2ff;font-family:Arial,'Microsoft Yahei',sans-serif}</style><div id="app"></div><script type="module">
import{createApp,h,ref}from'vue';import Workspace from'/src/components/InteractionRulesWorkspace.vue';import{applySession}from'/src/session.ts';import{feedbackState}from'/src/uiFeedback.ts';
const room=ref(26),plan=ref(4),visible=ref(true);window.fixture={events:0,adoptions:0,feedback:feedbackState,setScope(r,p){room.value=r;plan.value=p},hide(){visible.value=false}};
applySession({actor:{user_id:14,tenant_id:14,role:'customer'},tenants:[],environment:'test'});
window.addEventListener('interaction-review-updated',()=>window.fixture.events++);
createApp({render:()=>visible.value?h(Workspace,{roomId:room.value,planId:plan.value,onAdopted:()=>window.fixture.adoptions++},{default:()=>h('p','已采用的互动规则')}):null}).mount('#app');</script></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom' })
server.middlewares.use(async (req, res, next) => {
  if (req.url !== '/__simulation_progress') return next()
  res.setHeader('Content-Type', 'text/html; charset=utf-8')
  res.end(await server.transformIndexHtml(req.url, fixture))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const page = await browser.newPage({ viewport: { width: 1700, height: 960 } }), errors = [], posts = [], heldLists = []
page.on('pageerror', error => errors.push(error.message))
page.setDefaultTimeout(7000)
let records = [], holdLists = false
const makeRecord = (id, room = 26, plan = 4) => ({ id, room_id: room, tenant_id: 14, source_type: 'initial_simulation', status: 'editing', question: '模拟问题' + id, original_reply: '', updated_at: '2026-10-10T09:00:00Z', review: { plan_id: plan, kind: 'missing_fact', reason: '需要补充已确认的业务信息', occurrences: 1 } })
await page.route('**/api/v1/**', async route => {
  const request = route.request(), url = new URL(request.url())
  if (request.method() === 'POST') {
    assert.match(url.pathname, /\/rooms\/\d+\/interaction-learning\/simulate$/)
    posts.push({ route, path: url.pathname, body: request.postDataJSON() }); return
  }
  assert.match(url.pathname, /\/rooms\/\d+\/interaction-learning$/)
  const room = Number(url.pathname.split('/')[5]), plan = Number(url.searchParams.get('plan_id'))
  const items = records.filter(record => record.room_id === room && record.review.plan_id === plan && (!url.searchParams.get('source') || record.source_type === url.searchParams.get('source')))
  const result = { items, pending_count: items.length, total: items.length }
  if (holdLists) { heldLists.push({ route, result }); return }
  await route.fulfill({ json: result })
})
const start = page.getByRole('button', { name: '模拟挖掘', exact: true })
const progress = page.getByRole('progressbar', { name: '模拟挖掘进度' })
const state = page.getByRole('region', { name: '模拟挖掘工作状态' })
async function waitForPosts(count) { await page.waitForFunction(() => document.querySelector('.simulation-start')?.disabled); assert.equal(posts.length, count) }
const out = new URL('../../deploy/validation/simulation-mining/', import.meta.url)
try {
  await page.goto(server.resolvedUrls.local[0] + '__simulation_progress')
  await page.getByRole('tab', { name: '模拟挖掘', exact: true }).click()
  await start.waitFor(); assert.equal(posts.length, 0, 'tab navigation must not trigger a model')
  assert.equal(await start.count(), 1, 'one explicit mining entry')
  assert.deepEqual(await page.locator('.interaction-learning>header>.actions button').allTextContents(), ['刷新'])
  await page.getByRole('button', { name: '价格', exact: true }).click()
  await start.click(); await waitForPosts(1); await progress.waitFor()
  assert.deepEqual(posts[0].body, { plan_id: 4 }); assert.match(posts[0].path, /rooms\/26\//)
  assert.equal(await progress.getAttribute('aria-valuenow'), null, 'pending progress must be indeterminate')
  assert.equal(await page.getByLabel('检索纠正记录', { exact: true }).inputValue(), '')
  assert.ok(await start.isDisabled())
  assert.notEqual(await page.locator('.mining-worker-swing').evaluate(el => getComputedStyle(el).animationName), 'none')
  await page.evaluate(() => { const button = document.querySelector('.simulation-start'); button.dispatchEvent(new MouseEvent('click', { bubbles: true })); button.dispatchEvent(new MouseEvent('click', { bubbles: true })) })
  assert.equal(posts.length, 1, 'repeated clicks must not duplicate the paid request')
  await mkdir(out, { recursive: true }); await page.screenshot({ path: fileURLToPath(new URL('desktop-running.png', out)) })
  await page.setViewportSize({ width: 390, height: 850 })
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
  await page.screenshot({ path: fileURLToPath(new URL('mobile-running.png', out)) })
  await page.setViewportSize({ width: 1700, height: 960 })
  await page.getByRole('tab', { name: '直播互动', exact: true }).click(); assert.equal(await state.isVisible(), false)
  await page.getByRole('tab', { name: '模拟挖掘', exact: true }).click(); assert.ok(await start.isDisabled()); assert.ok(await state.isVisible())
  records = [makeRecord(1), makeRecord(2)]; holdLists = true
  await posts[0].route.fulfill({ json: { generated: 2 } })
  await state.getByText('正在整理模拟建议', { exact: true }).waitFor()
  assert.ok(await start.isDisabled()); assert.equal(await progress.getAttribute('aria-valuenow'), null)
  assert.ok(heldLists.length > 0); holdLists = false
  for (const item of heldLists.splice(0)) await item.route.fulfill({ json: item.result })
  await state.getByText('模拟挖掘完成', { exact: true }).waitFor()
  assert.equal(await progress.getAttribute('aria-valuenow'), '100')
  assert.ok(await start.isEnabled()); assert.equal(await page.getByRole('img', { name: '待处理问题', exact: true }).count(), 2)
  assert.equal(await page.evaluate(() => window.fixture.adoptions), 0)
  assert.equal(await page.evaluate(() => window.fixture.events), 1)
  await page.screenshot({ path: fileURLToPath(new URL('desktop-complete.png', out)) })
  await start.click(); await waitForPosts(2)
  await posts[1].route.fulfill({ status: 503, json: { error: '模拟测试失败，请重试' } })
  await page.getByRole('alert').filter({ hasText: '模拟测试失败，请重试' }).waitFor()
  assert.ok(await start.isEnabled()); assert.equal(await progress.count(), 0)
  assert.equal(await page.evaluate(() => window.fixture.feedback.alertOpen), false)
  await start.click(); await waitForPosts(3); records = []
  await posts[2].route.fulfill({ json: { generated: 0 } }); await state.getByText('模拟挖掘完成', { exact: true }).waitFor()
  assert.match(await state.textContent(), /已生成0条/)
  await start.click(); await waitForPosts(4)
  await page.evaluate(() => window.fixture.setScope(27, 5)); await page.waitForFunction(() => !document.querySelector('.simulation-start')?.disabled)
  assert.equal(await state.count(), 0)
  await start.click(); await waitForPosts(5)
  await posts[3].route.fulfill({ json: { generated: 88 } })
  assert.ok(await start.isDisabled(), 'old response cannot clear a newer operation')
  assert.doesNotMatch(await state.textContent(), /88/)
  records = [makeRecord(3, 27, 5)]
  await posts[4].route.fulfill({ json: { generated: 1 } }); await state.getByText('模拟挖掘完成', { exact: true }).waitFor()
  assert.match(posts[4].path, /rooms\/27\//); assert.equal(posts[4].body.plan_id, 5)
  assert.equal(await page.getByText('模拟问题1', { exact: true }).count(), 0)
  await page.emulateMedia({ reducedMotion: 'reduce' }); await start.click(); await waitForPosts(6)
  assert.equal(await page.locator('.mining-worker-swing').evaluate(el => getComputedStyle(el).animationName), 'none')
  const eventsBefore = await page.evaluate(() => window.fixture.events)
  await page.evaluate(() => window.fixture.hide()); await posts[5].route.fulfill({ json: { generated: 99 } })
  await page.waitForTimeout(50)
  assert.equal(await state.count(), 0); assert.equal(await page.evaluate(() => window.fixture.events), eventsBefore)
  assert.deepEqual(errors, [])
  console.log('PASS: left title starts one simulation; no duplicate right button; no model on tab navigation; worker animation and honest indeterminate/complete progress; results/red dots; tab continuity; failure/retry/zero results; room-plan stale-response isolation; mobile; reduced motion; unmount')
} finally { await browser.close(); await server.close() }
