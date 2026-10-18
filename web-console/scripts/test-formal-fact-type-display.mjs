import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { createServer } from 'vite'

const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const fixture = `<html><body><div id="app" style="width:120px"></div><script type="module">
import {createApp,h,reactive} from '/node_modules/.vite/deps/vue.js';
import Select from '/src/components/FormalFactTypeSelect.vue';
import {factCategoryState as state} from '/src/factCategories.ts';
const fact=reactive({id:1,key:'测试事实',category:'appointment'});
state.items=[{value:'appointment',label:'预约规则',group:'餐饮到店',description:'预约',family:'fulfillment'},
{value:'audience',label:'目标人群 / 用户需求',group:'商品信息',description:'适用人群',family:'product'}];
const feedback=reactive({suggestion:null});
window.fixture={fact,state,feedback,writes:[],fail:false,hold:false};
createApp({render:()=>h(Select,{fact,suggestion:feedback.suggestion,save:async(_,category)=>{
window.fixture.writes.push(category);
if(window.fixture.hold)await new Promise(resolve=>window.fixture.release=resolve);
if(window.fixture.fail)throw Error('分类版本冲突，请重试');
fact.category=category;
}})}).mount('#app');</script></body></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 5187 }, appType: 'custom' })
server.middlewares.use((req, res, next) => {
  if (req.url !== '/__type') return next()
  res.setHeader('Content-Type', 'text/html; charset=utf-8')
  res.end(fixture)
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const page = await browser.newPage(), errors = []
page.on('pageerror', e => errors.push(e.message))
try {
  await page.goto(server.resolvedUrls.local[0] + '__type')
  const select = page.getByRole('combobox'), label = page.locator('.fact-type-selected')
  await label.getByText('预约规则', { exact: true }).waitFor()
  await page.evaluate(() => window.fixture.fact.category = 'retired_category')
  await label.getByText('未匹配分类', { exact: true }).waitFor()
  assert.equal(await select.inputValue(), 'retired_category')
  assert.ok((await select.getAttribute('title')).includes('retired_category'))
  await page.evaluate(() => { window.fixture.state.items = []; window.fixture.state.loading = true })
  await label.getByText('正在加载分类…', { exact: true }).waitFor()
  assert.ok(await select.isDisabled())
  await page.evaluate(() => { window.fixture.state.loading = false; window.fixture.state.error = '读取失败' })
  await label.getByText('分类加载失败', { exact: true }).waitFor()
  assert.ok(await select.isDisabled())
  assert.equal(await page.evaluate(() => window.fixture.writes.length), 0)
  await page.reload()
  await label.getByText('预约规则', { exact: true }).waitFor()
  await page.evaluate(() => window.fixture.fail = true)
  await select.selectOption('audience')
  await page.getByRole('button', { name: '保存失败，查看分类说明' }).waitFor()
  assert.equal(await page.locator('.fact-type-select').getByText('分类版本冲突，请重试').count(), 0)
  await page.getByRole('button', { name: '保存失败，查看分类说明' }).click()
  await page.getByRole('dialog').getByText('分类版本冲突，请重试').waitFor()
  await page.keyboard.press('Escape')
  assert.equal(await select.inputValue(), 'appointment')
  assert.equal(await label.innerText(), '预约规则')
  await page.evaluate(() => { window.fixture.fail = false; window.fixture.hold = true })
  await select.selectOption('audience')
  await page.getByRole('status').getByText('保存中…').waitFor()
  assert.ok(await select.isDisabled())
  await page.evaluate(() => window.fixture.release())
  await label.getByText('目标人群 / 用户需求', { exact: true }).waitFor()
  assert.equal(await select.inputValue(), 'audience')
  const baseHeight = await page.locator('.fact-type-select').evaluate(e => e.getBoundingClientRect().height)
  await page.evaluate(() => window.fixture.feedback.suggestion = { id: 1, category: 'audience', label: '目标人群 / 用户需求', confidence: .5, low_conf: true, reason: '事实内容同时包含门店数量/地址（stores）和经营历史（business_history），两者权重相当，难以单一归类。'.repeat(8) })
  const pending = page.getByRole('button', { name: '待确认，查看分类说明' })
  await pending.waitFor()
  assert.equal(await page.locator('.fact-type-select').getByText(/business_history/).count(), 0)
  assert.ok(await page.locator('.fact-type-select').evaluate(e => e.getBoundingClientRect().height) <= baseHeight + 40, 'long explanation must not stretch the table cell')
  for (const width of [320, 390, 1024, 1600, 2000]) {
    await page.setViewportSize({ width, height: 700 })
    const sizes = await label.evaluate(e => ({ client: e.clientHeight, scroll: e.scrollHeight, width: e.clientWidth, content: e.scrollWidth, font: getComputedStyle(e).fontSize, color: getComputedStyle(e).color }))
    assert.equal(sizes.font, '16px')
    assert.equal(sizes.color, 'rgb(17, 17, 17)')
    assert.ok(sizes.client >= sizes.scroll && sizes.width >= sizes.content, 'full selected label wraps without clipping')
    await pending.focus()
    await page.keyboard.press('Enter')
    const dialog = page.getByRole('dialog')
    await dialog.getByRole('heading', { name: '事实类型待确认' }).waitFor()
    const reason = await dialog.locator('.fact-type-feedback-reason').innerText()
    assert.ok(reason.includes('经营历史') && !reason.includes('business_history') && !reason.includes('stores'))
    const bounds = await dialog.boundingBox()
    assert.ok(bounds.x >= 0 && bounds.x + bounds.width <= width, 'details dialog must fit the viewport')
    assert.ok(await dialog.locator('.fact-type-feedback-reason').evaluate(e => getComputedStyle(e).fontSize === '16px' && e.scrollWidth <= e.clientWidth))
    await page.keyboard.press('Escape')
    assert.equal(await pending.evaluate(e => document.activeElement === e), true, 'escape restores trigger focus')
  }
  await page.evaluate(() => window.fixture.feedback.suggestion = { id: 1, category: '', confidence: 0, low_conf: true, error: true, reason: 'HTTP 500 internal_server_error' })
  await page.getByRole('button', { name: '分类失败，查看分类说明' }).click()
  await page.getByRole('dialog').getByRole('heading', { name: '分类建议未完成' }).waitFor()
  assert.equal(await page.getByRole('dialog').getByText(/internal_server_error/).count(), 0)
  await page.getByRole('button', { name: '知道了', exact: true }).click()
  assert.equal(await page.evaluate(() => window.fixture.writes.length), 2, 'viewing feedback must not write classifications')
  assert.equal(await select.locator('option[value="audience"]').evaluate(e => getComputedStyle(e).color), 'rgb(17, 17, 17)')
  await select.focus()
  assert.ok(await select.evaluate(e => e === document.activeElement))
  assert.deepEqual(errors, [])
  console.log('PASS: compact long-reason and error badges; Chinese details; native modal keyboard/focus; viewport widths; no viewing writes; labels/loading/error/save/retry/native select preserved')
} finally { await browser.close(); await server.close() }
