// Real workspace; synthetic paginated records only, no account writes or paid models.
import assert from 'node:assert/strict'
import { mkdir } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const fixture = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><style>*{box-sizing:border-box}body{margin:0;padding:24px;background:#eef3ff;font-family:Arial,'Microsoft Yahei',sans-serif}</style><div id="app"></div><script type="module">
import{createApp,h}from'vue';import Workspace from'/src/components/InteractionRulesWorkspace.vue';import{applySession}from'/src/session.ts';
applySession({actor:{user_id:14,tenant_id:14,role:'customer'},tenants:[],environment:'test'});
createApp({render:()=>h(Workspace,{roomId:26,planId:4},{default:()=>h('p','已采用的互动规则')})}).mount('#app');</script></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom' })
server.middlewares.use(async (req, res, next) => {
  if (req.url !== '/__suggestion_cards') return next()
  res.setHeader('Content-Type', 'text/html; charset=utf-8')
  res.end(await server.transformIndexHtml(req.url, fixture))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const page = await browser.newPage({ viewport: { width: 1740, height: 1060 } }), errors = [], reads = [], writes = []
page.on('pageerror', error => errors.push(error.message)); page.setDefaultTimeout(7000)
const record = (id, question, context, reason, needed, kind = 'missing_fact') => ({ id, room_id: 26, tenant_id: 14, source_type: 'initial_simulation', status: 'editing', question, original_reply: '', updated_at: '2026-10-10T08:05:44Z', review: { plan_id: 4, kind, context, reason, meaning: context, needed_facts: needed ? [needed] : [], occurrences: 1 } })
const records = [
  record(1, '半只多少钱？', '用户询问半只装价格', '商品卡中有半只装的规格和价格，但建议确认', '商品价格'),
  record(2, '有外包装盒子吗？送礼用。', '用户询问是否有礼盒包装', '事实库中有关于产品外包装样式及礼盒装的说明，但建议补充回答条件', '产品外包装样式描述，以及是否有礼盒装或适合送礼的说明'),
  record(3, '如果不满意可以退货吗？', '用户询问退换货政策', '建议明确退换货的条件、时效及运费承担规则', '退换货政策、申请流程和注意事项'),
  record(4, '现在下单有赠品吗？', '用户询问赠品活动', '需要确认当前活动的赠品内容及适用条件', '当前赠品活动规则'),
  record(5, '辣不辣哦？', '用户询问口味', '解释川渝问法与已确认的口味选项', '', 'semantic'),
  record(6, '发啥子快递？', '用户询问物流信息', '识别方言问法并回答已确认的快递安排', '', 'semantic'),
  ...Array.from({ length: 24 }, (_, i) => record(7 + i, '一般问题' + i, '用户问普通问题', '需要补充已确认的信息', '已确认信息')),
  record(31, '超过下午六点什么时候发货？', '用户询问发货安排', '说明已确认的发货截单规则', '发货时效'),
]
await page.route('**/api/v1/**', async route => {
  const request = route.request(), url = new URL(request.url())
  if (request.method() !== 'GET') { writes.push(url.pathname); await route.fulfill({ status: 500, json: { error: 'Unexpected write' } }); return }
  reads.push(url.href)
  assert.match(url.pathname, /\/rooms\/26\/interaction-learning$/)
  const q = url.searchParams.get('q') || '', currentPage = Number(url.searchParams.get('page') || 1)
  const items = records.filter(item => (!url.searchParams.get('source') || item.source_type === url.searchParams.get('source')) && (!q || JSON.stringify(item).includes(q)))
  await route.fulfill({ json: { items: items.slice((currentPage - 1) * 30, currentPage * 30), total: items.length, pending_count: records.length } })
})
const card = id => page.locator(`[data-interaction-review-id="${id}"]`)
const status = page.locator('.simulation-generation-status')
const out = new URL('../../deploy/validation/simulation-cards/', import.meta.url)
async function filter(label, count) {
  await page.getByRole('button', { name: label, exact: true }).click()
  await page.waitForFunction(expected => document.querySelector('.simulation-generation-status')?.textContent.includes('筛选到' + expected + '条'), count)
  assert.equal(await page.locator('.simulation-result-card').count(), count)
  assert.equal(await page.getByRole('button', { name: label, exact: true }).getAttribute('aria-pressed'), 'true')
}
try {
  await page.goto(server.resolvedUrls.local[0] + '__suggestion_cards')
  await page.getByRole('tab', { name: '模拟挖掘', exact: true }).click(); await card(1).waitFor()
  assert.equal(await page.locator('.simulation-result-card').count(), 30)
  assert.equal(await page.getByRole('button', { name: '模拟挖掘', exact: true }).count(), 1)
  for (const [id, label] of [[1, '价格相关'], [2, '物流相关'], [3, '售后相关'], [4, '赠品相关'], [5, '其他问题'], [6, '物流相关']]) {
    assert.equal(await card(id).locator('.simulation-topic-rail strong').textContent(), label)
    assert.equal(await card(id).getByRole('img', { name: '待处理问题', exact: true }).count(), 1)
  }
  const railColors = await Promise.all([1, 2, 3, 4].map(id => card(id).locator('.simulation-topic-rail').evaluate(el => getComputedStyle(el).color)))
  assert.equal(new Set(railColors).size, 4)
  const rect = await card(1).boundingBox(), info = await card(1).locator('.review-card-info').boundingBox(), actions = await card(1).locator('.review-card-actions').boundingBox()
  assert.ok(rect.height < 210 && actions.x > info.x + info.width - 1, 'desktop cards remain compact with actions on the right')
  const reason = await card(1).locator('.model-suggestion span').boundingBox(), adopt = await card(1).getByRole('button', { name: '采纳模型建议', exact: true }).boundingBox()
  assert.ok(adopt.x - reason.x - reason.width < 25, 'adoption stays adjacent to the displayed model suggestion')
  await mkdir(out, { recursive: true }); await page.screenshot({ path: fileURLToPath(new URL('desktop.png', out)) })
  await card(2).getByLabel('查看记录详情', { exact: true }).click()
  await card(2).getByText('记录详情', { exact: true }).waitFor()
  assert.ok(await card(2).getByText('用户询问是否有礼盒包装', { exact: true }).last().isVisible())
  await card(2).getByLabel('查看记录详情', { exact: true }).click()
  await filter('物流', 3)
  await card(31).waitFor(); assert.ok(reads.some(value => new URL(value).searchParams.get('page') === '2'), 'filters include older matching records after page one')
  await filter('价格', 1); await filter('售后', 1); await filter('赠品', 1)
  await page.getByLabel('检索纠正记录', { exact: true }).fill('没有这个词')
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  await page.getByText('没有找到匹配的模拟建议，换个关键词或点击“全部内容”看看吧。', { exact: true }).waitFor()
  await page.getByRole('button', { name: '全部内容', exact: true }).click(); await card(1).waitFor()
  assert.equal(await page.getByLabel('检索纠正记录', { exact: true }).inputValue(), '')
  await page.getByRole('button', { name: '下一页', exact: true }).click(); await card(31).waitFor()
  assert.equal(await page.locator('.simulation-result-card').count(), 1)
  await page.getByRole('button', { name: '上一页', exact: true }).click(); await card(1).waitFor()
  await page.setViewportSize({ width: 390, height: 850 }); await page.evaluate(() => scrollTo(0, 0))
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
  assert.ok(await card(1).getByRole('button', { name: '填写纠正', exact: true }).isVisible())
  await page.screenshot({ path: fileURLToPath(new URL('mobile.png', out)) })
  await page.getByRole('tab', { name: '处理历史', exact: true }).click(); await page.getByLabel('处理状态', { exact: true }).waitFor()
  assert.equal(await page.locator('.simulation-quick-filters').count(), 0)
  assert.equal(await page.locator('.simulation-topic-rail').count(), 0)
  assert.deepEqual(writes, [], 'navigation, filtering and record details never call a model or write facts')
  assert.deepEqual(errors, [])
  console.log('PASS: categorized compact cards, adjacent adoption, record details, exact pending dots, older-page topic filtering, query/empty/reset/pagination, mobile without overflow, history compatibility; no model or mutation calls')
} finally { await browser.close(); await server.close() }
