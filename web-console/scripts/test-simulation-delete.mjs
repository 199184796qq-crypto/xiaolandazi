import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
import { mkdir, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const output = resolve(process.env.SIMULATION_DELETE_OUTPUT || '../work/simulation-delete')
await mkdir(output, { recursive: true })
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom', logLevel: 'error' })
server.middlewares.use(async (req, res, next) => {
  if (req.url !== '/__simulation_delete') return next()
  res.setHeader('Content-Type', 'text/html; charset=utf-8')
  res.end(await server.transformIndexHtml(req.url, `<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>*{box-sizing:border-box}body{margin:0;padding:24px;background:#eef3ff;font-family:'Microsoft YaHei',sans-serif}</style></head><body><div id="app"></div><script type="module">
  import {createApp,h,ref,Fragment} from 'vue';import Workspace from '/src/components/InteractionRulesWorkspace.vue';import GlobalFeedback from '/src/components/GlobalFeedback.vue';import {applySession} from '/src/session.ts';
  applySession({actor:{user_id:14,tenant_id:14,role:'customer'},tenants:[],environment:'test'});window.reviewUpdates=0;window.addEventListener('interaction-review-updated',()=>window.reviewUpdates++);
  const plan=ref(4);window.changePlan=value=>plan.value=value;createApp({render:()=>h(Fragment,[h(Workspace,{roomId:26,planId:plan.value},{default:()=>h('p','已采纳的回答策略保留')}),h(GlobalFeedback)])}).mount('#app');</script></body></html>`))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const item = (id, question, extra = {}) => ({ id, room_id: 26, tenant_id: 14, source_type: 'initial_simulation', status: 'editing', question, original_reply: '', updated_at: '2026-10-10T08:05:44Z', review: { plan_id: 4, kind: 'semantic', context: '用户咨询', meaning: question, reason: '建议补充说明', needed_facts: [], occurrences: 1 }, ...extra })
let rows = [item(1, '半只多少钱？'), ...Array.from({ length: 30 }, (_, index) => item(index + 2, '其他模拟建议' + index)), item(32, '物流几天到？'), item(101, '真实直播问题', { source_type: 'live_uncertain' }), item(102, '已采用建议', { status: 'adopted' }), item(103, '其他方案', { review: { plan_id: 5 } })]
const writes = [], errors = []
let fail = false
try {
  const page = await browser.newPage({ viewport: { width: 1740, height: 1060 } })
  page.on('pageerror', error => errors.push(error.message))
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), url = new URL(request.url()), plan = Number(url.searchParams.get('plan_id'))
    if (request.method() === 'DELETE') {
      writes.push(url.href)
      if (fail) { await route.fulfill({ status: 500, json: { error: '模拟删除失败' } }); return }
      const id = Number(url.pathname.split('/').at(-1)) || 0
      const removed = rows.filter(row => row.room_id === 26 && row.tenant_id === 14 && row.review.plan_id === plan && row.source_type === 'initial_simulation' && row.status === 'editing' && (!id || row.id === id))
      rows = rows.filter(row => !removed.includes(row))
      await route.fulfill({ json: { deleted: removed.length } }); return
    }
    assert.equal(request.method(), 'GET', 'deletion must never call a model, adoption, or a settings write')
    const scoped = rows.filter(row => row.review.plan_id === plan && row.room_id === 26 && row.tenant_id === 14 && (!url.searchParams.get('source') || row.source_type === url.searchParams.get('source')))
    const q = url.searchParams.get('q') || '', status = url.searchParams.get('status') || 'pending', pageNo = Number(url.searchParams.get('page') || 1)
    const pending = scoped.filter(row => row.status === 'editing').length
    const visible = scoped.filter(row => (status === 'all' || (status === 'pending' ? row.status === 'editing' : row.status === status)) && (!q || row.question.includes(q)))
    await route.fulfill({ json: { items: visible.slice((pageNo - 1) * 30, pageNo * 30), pending_count: pending, total: visible.length } })
  })
  const card = id => page.locator(`[data-interaction-review-id="${id}"]`)
  const confirmation = page.getByRole('dialog')
  await page.goto(server.resolvedUrls.local[0] + '__simulation_delete')
  await page.getByRole('tab', { name: '模拟挖掘', exact: true }).click(); await card(1).waitFor()
  await page.locator('.simulation-generation-status').getByText('共32条模拟建议', { exact: true }).waitFor()
  await page.locator('.learning-toolbar').screenshot({ path: resolve(output, 'toolbar.png') })
  await card(1).getByRole('button', { name: '删除模拟建议：半只多少钱？' }).click()
  await confirmation.getByRole('button', { name: '取消', exact: true }).click()
  assert.equal(writes.length, 0)
  await card(1).getByRole('button', { name: '删除模拟建议：半只多少钱？' }).click()
  await confirmation.getByRole('button', { name: '删除', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('[data-interaction-review-id="1"]'))
  assert.equal(writes.length, 1); assert.match(writes[0], /\/interaction-learning\/1\?plan_id=4$/)
  await page.locator('.simulation-generation-status').getByText('共31条模拟建议', { exact: true }).waitFor()
  assert.equal(await page.evaluate(() => window.reviewUpdates), 1)
  await page.getByRole('button', { name: '下一页', exact: true }).click(); await card(32).waitFor()
  await card(32).getByRole('button', { name: '删除模拟建议：物流几天到？' }).click()
  await confirmation.getByRole('button', { name: '删除', exact: true }).click()
  await card(2).waitFor(); assert.equal(await page.locator('.simulation-result-card').count(), 30)
  fail = true
  await card(2).getByRole('button', { name: /删除模拟建议/ }).click()
  await confirmation.getByRole('button', { name: '删除', exact: true }).click()
  await page.getByText('模拟删除失败', { exact: true }).waitFor(); assert.ok(await card(2).isVisible())
  fail = false

  await page.getByLabel('检索纠正记录', { exact: true }).fill('找不到')
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  await page.getByText('没有找到匹配的模拟建议，换个关键词或点击“全部内容”看看吧。', { exact: true }).waitFor()
  await page.getByRole('button', { name: '全部删除', exact: true }).click()
  await confirmation.getByText(/包括其他页和筛选隐藏/).waitFor()
  await confirmation.getByRole('button', { name: '全部删除', exact: true }).click()
  await page.getByText('已删除30条待处理模拟建议。', { exact: true }).waitFor()
  assert.match(writes.at(-1), /\/interaction-learning\?plan_id=4$/)
  assert.equal(await page.locator('.simulation-result-card').count(), 0)
  assert.equal(await page.getByRole('button', { name: '全部删除', exact: true }).isDisabled(), true)
  assert.equal(await page.getByLabel('检索纠正记录', { exact: true }).inputValue(), '')
  assert.deepEqual(rows.map(row => row.id), [101, 102, 103])
  await page.getByRole('tab', { name: '直播互动', exact: true }).click(); await card(101).waitFor()
  assert.equal(await page.getByRole('button', { name: /删除模拟建议|全部删除/ }).count(), 0)
  await page.getByRole('tab', { name: '回答策略', exact: true }).click(); await page.getByText('已采纳的回答策略保留', { exact: true }).waitFor()
  await page.getByRole('tab', { name: '模拟挖掘', exact: true }).click()
  rows.push(item(104, '切换前不要删除'), item(105, '新方案模拟建议', { review: { plan_id: 5 } }))
  await page.getByRole('button', { name: '刷新', exact: true }).click(); await card(104).waitFor()
  const before = writes.length
  await card(104).getByRole('button', { name: /删除模拟建议/ }).click()
  await page.evaluate(() => window.changePlan(5))
  await confirmation.getByRole('button', { name: '删除', exact: true }).click()
  await card(105).waitFor(); assert.equal(writes.length, before, 'scope changed during confirmation must not delete')
  await page.setViewportSize({ width: 390, height: 850 })
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
  await page.locator('.interaction-learning').screenshot({ path: resolve(output, 'mobile.png') })
  assert.deepEqual(errors, [])
  const result = { passed: true, checks: ['single delete confirmation/cancel', 'real DELETE and badge update', 'deleting last row of page returns to previous page', 'failure keeps record for retry', 'bulk delete includes hidden and other pages', 'bulk clears filters and count', 'live, adopted and other-plan records preserved', 'scope change while confirming cannot delete', 'mobile without overflow', 'no model/fact/adoption calls'], errors }
  await writeFile(resolve(output, 'result.json'), JSON.stringify(result, null, 2)); console.log(JSON.stringify(result))
} catch (error) { console.log(JSON.stringify({failed: String(error), body: (await browser.contexts()[0]?.pages()[0]?.locator('body').innerText())?.slice(-4500)})); throw error } finally { await browser.close(); await server.close() }
