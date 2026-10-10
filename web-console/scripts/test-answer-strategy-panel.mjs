// Real LiveStrategyView; synthetic strategy records and learning API responses only.
import assert from 'node:assert/strict'
import { readFile, mkdir } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const cases = [
  ['回答策略：儿童食用咨询', 'plan:4:response_strategy:children_consumption', '当用户询问小孩是否可以食用该产品时，应依据已确认事实回答，并说明适当的食用方式。', 'children', '儿童相关'],
  ['回答策略：指定日期到货询问', 'plan:4:response_strategy:delivery_date_inquiry', '根据已确认的发货时效说明发货安排，结合收货地区差异，按实际物流情况客观回复，不承诺未经确认的到货时间。', 'timing', '时效相关'],
  ['回答策略：切块咨询', 'response_strategy:cutting', '说明默认发货形式和定制选项。用户有切块需求时，引导用户在订单中备注。', 'product', '产品相关'],
  ['术语理解：抽/抽真空', 'term:抽:语境判断', '结合当前语境判断“抽”的含义，在包装场景中解释为抽真空，并询问用户选择的包装方式。', 'language', '术语理解'],
  ['快递选择', 'response_strategy:shipping', '说明已确认的快递与发货安排。', 'logistics', '物流相关'],
  ['人群适用咨询', 'response_strategy:audience', '依据已确认的信息说明适用对象。', 'audience', '人群相关'],
  ['过敏安全提醒', 'response_strategy:safety', '遇到过敏相关问题，提示先核对已确认的原料信息。', 'safety', '安全相关'],
  ['售后退换保障', 'response_strategy:refund', '根据已确认的售后条件回复申请流程。', 'after_sales', '售后保障'],
  ['售前咨询', 'response_strategy:consultation', '先理解用户的问题，再给出简洁清晰的回应。', 'consultation', '咨询相关'],
  ['自定义说明', 'custom:alpha', '先理解具体情况，然后给出清晰的回应。', 'general', '通用策略'],
  ['价格换算规则', 'response_strategy:pricing', '按已确认的计算规则回复。', 'price', '价格相关'],
  ['赠品福利活动', 'response_strategy:gift', '依据当前活动适用条件说明。', 'benefits', '福利活动'],
]
const memories = cases.map(([target, key, text], index) => ({ id: index + 1, tenant_id: 14, room_id: 26, memory_type: 'semantic', memory_key: key, target, status: 'active', created_at: '2026-10-10T02:00:00Z', updated_at: '2026-10-10T09:00:00Z', current_version: { id: index + 101, version_no: 1, status: 'active', content_text: text } }))
memories.push({ ...memories[0], id: 99, memory_type: 'fact', target: '不应进入回答策略的事实纠正' })
const source = await readFile(new URL('./test-interaction-full-view-location.mjs', import.meta.url), 'utf8')
let fixture = source.match(/const fixture = `([\s\S]*?)`\nconst deferredWorkspace/)[1]
fixture = fixture.replace("name:'测试方案',room_ids", "name:'测试方案',status:'active',room_ids")
fixture = fixture.replace("window.fixture={calls:[],records,delayWorkspace:location.search.includes('delayed=1')};", `
records.splice(0);window.fixture={calls:[],records,memories:${JSON.stringify(memories)},preview:'依据已确认事实回复儿童食用问题，年龄较小的儿童建议由成人陪同。'};
Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async text=>{window.fixture.copiedText=text}}});`)
fixture = fixture.replace("window.fixture.calls.push({url:String(url),method:init.method||'GET'});", "window.fixture.calls.push({url:String(url),method:init.method||'GET',body:init.body});")
fixture = fixture.replace("if(init.method&&init.method!=='GET')throw Error('Unexpected write '+p);", `
if(init.method==='POST'){
 if(p.endsWith('/agent-learning/sessions'))return Response.json({id:501});
 if(p.endsWith('/agent-learning/sessions/501/turns'))return Response.json({session:{id:501},result:{result_text:window.fixture.preview,memory_type:'semantic'}});
 if(p.endsWith('/agent-learning/sessions/501/adopt')){window.fixture.memories[0].current_version.content_text=window.fixture.preview;window.fixture.memories[0].current_version.version_no=2;return Response.json({});}
 if(p.endsWith('/agent-memories/3/deactivate')){window.fixture.memories.find(m=>m.id===3).status='inactive';return Response.json({});}
 throw Error('Unexpected write '+p);
}
if(p.endsWith('/agent-memories'))return Response.json({items:window.fixture.memories});
if(p.endsWith('/realtime-options'))return Response.json({buffer_seconds:60,body_min:2,body_max:4,max_chars:300});`)
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom' })
server.middlewares.use(async (req, res, next) => {
  if (req.url !== '/__answer_strategies') return next()
  res.setHeader('Content-Type', 'text/html;charset=utf-8')
  res.end(await server.transformIndexHtml(req.url, fixture))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const page = await browser.newPage({ viewport: { width: 1800, height: 1120 }, reducedMotion: 'reduce' }), errors = []
page.on('pageerror', error => errors.push(error.message)); page.setDefaultTimeout(7000)
const panel = page.getByRole('region', { name: '回答策略管理', exact: true })
const card = id => panel.locator(`[data-answer-strategy-id="${id}"]`)
const writes = () => page.evaluate(() => window.fixture.calls.filter(call => call.method !== 'GET'))
const out = new URL('../../deploy/validation/answer-strategies/', import.meta.url)
try {
  await page.goto(server.resolvedUrls.local[0] + '__answer_strategies')
  await page.locator('.workflow-step-memory').click(); await card(1).waitFor()
  assert.equal(await page.getByRole('tab', { name: '回答策略', exact: true }).getAttribute('aria-selected'), 'true')
  assert.equal(await page.getByRole('tab', { name: '互动采纳', exact: true }).count(), 0)
  assert.equal(await panel.locator('.answer-strategy-card').count(), 12)
  assert.equal(await card(99).count(), 0, 'other memory types keep their routing')
  for (const [index, [, , , category, label]] of cases.entries()) {
    assert.equal(await card(index + 1).getAttribute('data-answer-strategy-category'), category)
    assert.equal(await card(index + 1).locator('.answer-strategy-category strong').textContent(), label)
    assert.equal(await card(index + 1).locator('.answer-strategy-category svg').count(), 1)
    assert.equal(await card(index + 1).locator('.answer-strategy-status strong').textContent(), '已启用')
  }
  const bounds = await card(1).boundingBox(), text = await card(1).locator('.answer-strategy-content').boundingBox(), actions = await card(1).locator('.answer-strategy-controls').boundingBox()
  assert.ok(bounds.height < 240 && actions.x >= text.x + text.width, 'desktop strategy rows remain compact with right-hand controls')
  await panel.scrollIntoViewIfNeeded(); await mkdir(out, { recursive: true })
  await page.screenshot({ path: fileURLToPath(new URL('desktop.png', out)) })
  await card(1).getByRole('button', { name: '复制策略内容', exact: true }).click()
  await card(1).getByRole('button', { name: '策略内容已复制', exact: true }).waitFor()
  assert.equal(await page.evaluate(() => window.fixture.copiedText), cases[0][0] + '\n' + cases[0][2])
  await card(1).getByLabel('查看策略详情', { exact: true }).click(); await card(1).getByText('策略详情', { exact: true }).waitFor()
  assert.equal((await writes()).length, 0, 'search, copying and details do not call a model')
  await card(1).getByLabel('查看策略详情', { exact: true }).click()
  await panel.getByLabel('搜索回答策略', { exact: true }).fill('物流相关')
  assert.equal(await panel.locator('.answer-strategy-card').count(), 1); await card(5).waitFor()
  await panel.getByLabel('搜索回答策略', { exact: true }).fill('delivery_date_inquiry'); await card(2).waitFor()
  assert.equal(await panel.locator('.answer-strategy-card').count(), 1)
  await panel.getByLabel('搜索回答策略', { exact: true }).fill('找不到这个策略'); await panel.getByText('没有找到匹配的回答策略', { exact: true }).waitFor()
  await panel.getByLabel('搜索回答策略', { exact: true }).fill('')
  await card(1).getByRole('button', { name: '修改', exact: true }).click()
  await card(1).locator('textarea').fill('说明儿童食用方式，以已确认事实为准。')
  await card(1).getByRole('button', { name: '生成修改预览', exact: true }).click()
  await card(1).getByText('修改后预览', { exact: true }).waitFor()
  let calls = await writes()
  assert.equal(calls.length, 2); assert.match(calls[0].url, /\/rooms\/26\/agent-learning\/sessions$/)
  assert.equal(JSON.parse(calls[0].body).source_ref, 'agent_memory:1')
  assert.equal(JSON.parse(calls[1].body).feedback, '说明儿童食用方式，以已确认事实为准。')
  await card(1).getByRole('button', { name: '采用修改', exact: true }).click()
  await card(1).locator('.answer-strategy-version').filter({ hasText: 'V2' }).waitFor()
  assert.equal(await page.getByRole('tab', { name: '回答策略', exact: true }).getAttribute('aria-selected'), 'true')
  assert.equal(await card(1).locator('textarea').count(), 0)
  await card(3).getByRole('button', { name: '删除', exact: true }).click()
  assert.equal((await writes()).length, 3, 'first delete click only asks for inline confirmation')
  await card(3).getByRole('button', { name: '确认删除', exact: true }).click(); await card(3).waitFor({ state: 'detached' })
  assert.match((await writes()).at(-1).url, /\/rooms\/26\/agent-memories\/3\/deactivate$/)
  await panel.getByRole('button', { name: '刷新策略', exact: true }).click(); await card(1).waitFor()
  await page.setViewportSize({ width: 390, height: 850 }); await panel.scrollIntoViewIfNeeded()
  assert.equal(await panel.evaluate(el => el.scrollWidth > el.clientWidth), false)
  assert.equal(await card(1).evaluate(el => el.scrollWidth > el.clientWidth), false)
  await page.screenshot({ path: fileURLToPath(new URL('mobile.png', out)) })
  await page.getByRole('tab', { name: '模拟挖掘', exact: true }).click()
  await page.getByRole('button', { name: '模拟挖掘', exact: true }).waitFor()
  assert.equal((await writes()).length, 4, 'tab navigation does not run mining')
  await page.getByRole('tab', { name: '回答策略', exact: true }).click(); await card(1).waitFor()
  assert.deepEqual(errors, [])
  console.log('PASS: renamed answer-strategy tab, twelve category icons and generic fallback, true enabled state, search by title/category/key, exact clipboard text, record details, real-view edit preview/adoption, in-place two-step deletion and refresh, mobile layout, no model on navigation')
} finally { await browser.close(); await server.close() }
