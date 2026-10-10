import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
import { mkdir, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const output = resolve(process.env.HISTORY_ACTIONS_OUTPUT || '../work/history-actions')
await mkdir(output, { recursive: true })
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom', logLevel: 'error' })
server.middlewares.use(async (req, res, next) => {
  if (req.url !== '/__history_actions') return next()
  res.setHeader('Content-Type', 'text/html; charset=utf-8')
  res.end(await server.transformIndexHtml(req.url, `<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>*{box-sizing:border-box}body{margin:0;padding:24px;background:#eef3ff;font-family:'Microsoft YaHei',sans-serif}</style></head><body><div id="app"></div><script type="module">
  import {createApp,h,ref,Fragment} from 'vue';import Workspace from '/src/components/InteractionRulesWorkspace.vue';import GlobalFeedback from '/src/components/GlobalFeedback.vue';import {applySession} from '/src/session.ts';
  applySession({actor:{user_id:14,tenant_id:14,role:'customer'},tenants:[],environment:'test'});window.reviewUpdates=0;window.addEventListener('interaction-review-updated',()=>window.reviewUpdates++);
  const plan=ref(4);window.changePlan=value=>plan.value=value;createApp({render:()=>h(Fragment,[h(Workspace,{roomId:26,planId:plan.value},{default:()=>h('p','已采纳的事实依据和回答策略')}),h(GlobalFeedback)])}).mount('#app');</script></body></html>`))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const item = (id, question, status, source = 'initial_simulation', plan = 4) => ({ id, room_id: 26, tenant_id: 14, source_type: source, status, question, original_reply: '', updated_at: '2026-10-10T08:05:44Z', review: { plan_id: plan, kind: 'semantic', context: '用户咨询', meaning: question, reason: '建议补充说明', suggested_rule: '原来的纠正建议', needed_facts: [], occurrences: 1 } })
const rows = [item(8, '辣不辣哦', 'closed'), item(9, '骨头多不多', 'adopted'), item(10, '发啥子快递', 'closed', 'live_uncertain'), item(11, '正在处理的问题', 'editing'), item(110, '新方案历史', 'closed', 'initial_simulation', 5)]
const results = new Map([[9, { id: 90, session_id: 9, memory_type: 'semantic', result_text: '已确认的原始规则' }]])
const writes = [], errors = [], created = new Map()
let deleteFailure = false, draftFailure = false, nextID = 100
try {
  const page = await browser.newPage({ viewport: { width: 1740, height: 1060 } })
  page.on('pageerror', error => errors.push(error.message))
  await page.route('**/api/v1/**', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname, method = req.method()
    const id = Number(path.split('/').at(-2)), plan = Number(url.searchParams.get('plan_id'))
    if (method !== 'GET') writes.push({ path, method, body: req.postDataJSON() })
    if (method === 'DELETE') {
      assert.ok(path.endsWith('/history'))
      if (deleteFailure) return route.fulfill({ status: 500, json: { error: '历史删除失败' } })
      const row = rows.find(row => row.id === id && row.review.plan_id === plan)
      assert.ok(row); row.hidden = true
      for (const draft of rows) if (draft.review.history_origin_id === id && draft.status === 'editing') draft.hidden = true
      return route.fulfill({ json: { deleted: 1 } })
    }
    if (path.endsWith('/correction')) {
      if (draftFailure) return route.fulfill({ status: 500, json: { error: '纠正草稿创建失败' } })
      const original = rows.find(row => row.id === id && row.review.plan_id === plan && !row.hidden)
      assert.ok(original)
      let draft = created.get(id)
      if (!draft) { draft = { ...original, id: nextID++, status: 'editing', review: { ...original.review, history_origin_id: id } }; created.set(id, draft); rows.push(draft) }
      return route.fulfill({ json: draft })
    }
    if (path.endsWith('/turns')) {
      const row = rows.find(row => row.id === id && !row.hidden)
      assert.ok(row); assert.equal(row.status, 'editing', 'a processed history record must never be edited in place')
      const result = { id: id + 1000, session_id: id, memory_type: 'semantic', result_text: req.postDataJSON().feedback }
      results.set(id, result); return route.fulfill({ json: { session: row, result } })
    }
    if (path.endsWith('/adopt')) {
      const row = rows.find(row => row.id === id && !row.hidden)
      assert.ok(results.has(id)); row.status = 'adopted'; return route.fulfill({ json: {} })
    }
    assert.equal(method, 'GET', `unexpected mutation: ${path}`)
    if (path.includes('/agent-learning/sessions/')) return route.fulfill({ json: { session: rows.find(row => row.id === Number(path.split('/').at(-1))), latest: results.get(Number(path.split('/').at(-1))) || null } })
    const scoped = rows.filter(row => row.review.plan_id === plan && !row.hidden && (!url.searchParams.get('source') || row.source_type === url.searchParams.get('source')))
    const q = url.searchParams.get('q') || '', status = url.searchParams.get('status') || 'pending'
    const visible = scoped.filter(row => (status === 'all' || (status === 'pending' ? row.status === 'editing' : row.status === status)) && row.question.includes(q))
    return route.fulfill({ json: { items: visible, pending_count: scoped.filter(row => row.status === 'editing').length, total: visible.length } })
  })
  const card = id => page.locator(`[data-interaction-review-id="${id}"]`)
  const confirmation = page.getByRole('dialog')
  await page.goto(server.resolvedUrls.local[0] + '__history_actions')
  await page.getByRole('tab', { name: '处理历史', exact: true }).click(); await card(8).waitFor()
  for (const id of [8, 9, 10, 11]) { assert.ok(await card(id).getByRole('button', { name: '纠正', exact: true }).isVisible()); assert.ok(await card(id).getByRole('button', { name: /删除历史记录/ }).isVisible()) }
  await page.locator('.interaction-learning').screenshot({ path: resolve(output, 'desktop.png') })
  await card(9).getByRole('button', { name: '纠正', exact: true }).click()
  await page.waitForFunction(() => document.querySelector('#review-9')?.value === '已确认的原始规则')
  assert.equal(writes.length, 0, 'opening the form must not mutate processed history')
  await card(9).getByRole('textbox').fill('新的口味回答规则')
  draftFailure = true
  await card(9).getByRole('button', { name: '生成纠正预览', exact: true }).click()
  await page.getByText('纠正草稿创建失败', { exact: true }).waitFor()
  assert.equal(writes.filter(write => write.path.endsWith('/turns')).length, 0)
  draftFailure = false
  await card(9).getByRole('button', { name: '生成纠正预览', exact: true }).click()
  await card(9).locator('.preview').waitFor()
  assert.equal(rows.find(row => row.id === 9).status, 'adopted')
  assert.ok(writes.some(write => write.path.endsWith('/100/turns') && write.body.feedback === '新的口味回答规则'))
  assert.equal(writes.some(write => write.path.endsWith('/adopt')), false)
  await card(9).getByRole('button', { name: '确认并采用', exact: true }).click()
  await page.getByText('纠正已采用，后续回答使用最新规则。', { exact: true }).waitFor()
  await card(100).waitFor(); assert.equal(rows.find(row => row.id === 9).status, 'adopted')
  assert.ok(writes.some(write => write.path.endsWith('/100/adopt')))
  assert.equal(await page.getByRole('tab', { name: '处理历史', exact: true }).getAttribute('aria-selected'), 'true')
  await card(8).getByRole('button', { name: '纠正', exact: true }).click(); await card(8).getByRole('textbox').fill('不辣的选项说明')
  await card(8).getByRole('button', { name: '生成纠正预览', exact: true }).click(); await card(8).locator('.preview').waitFor()
  await card(8).getByRole('button', { name: /删除历史记录/ }).click()
  await confirmation.getByRole('button', { name: '取消', exact: true }).click(); assert.ok(await card(8).isVisible())
  deleteFailure = true
  await card(8).getByRole('button', { name: /删除历史记录/ }).click(); await confirmation.getByRole('button', { name: '删除', exact: true }).click()
  await page.getByText('历史删除失败', { exact: true }).waitFor(); assert.ok(await card(8).isVisible())
  deleteFailure = false
  await card(8).getByRole('button', { name: /删除历史记录/ }).click(); await confirmation.getByRole('button', { name: '删除', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('[data-interaction-review-id="8"]'))
  assert.ok(rows.find(row => row.id === 101).hidden, 'deletion cancels its unfinished correction draft')
  await card(9).getByRole('button', { name: /删除历史记录/ }).click(); await confirmation.getByRole('button', { name: '删除', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('[data-interaction-review-id="9"]'))
  assert.equal(rows.find(row => row.id === 100).status, 'adopted', 'completed new correction remains in history')
  const before = writes.length
  await card(10).getByRole('button', { name: /删除历史记录/ }).click(); await page.evaluate(() => window.changePlan(5))
  await confirmation.getByRole('button', { name: '删除', exact: true }).click(); await card(110).waitFor()
  assert.equal(writes.length, before, 'changing plan before confirmation cannot delete the old record')
  await page.setViewportSize({ width: 390, height: 850 })
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
  await page.locator('.interaction-learning').screenshot({ path: resolve(output, 'mobile.png') })
  await page.getByRole('tab', { name: '回答策略', exact: true }).click(); await page.getByText('已采纳的事实依据和回答策略', { exact: true }).waitFor()
  assert.deepEqual(errors, [])
  const report = { passed: true, checks: ['closed/adopted/live/pending history actions', 'open correction without mutations', 'prefill previous confirmed result', 'draft failure preserves record and input', 'new correction preview before explicit adoption', 'original adopted history preserved', 'adoption stays on history tab', 'delete confirmation/cancel/retry', 'deleting history cancels its pending draft', 'completed correction and adopted assets retained', 'scope change cannot delete', 'mobile no overflow'], errors }
  await writeFile(resolve(output, 'result.json'), JSON.stringify(report, null, 2)); console.log(JSON.stringify(report))
} finally { await browser.close(); await server.close() }
