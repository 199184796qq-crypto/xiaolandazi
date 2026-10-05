import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { existsSync, mkdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { startFixture } from './product-cards-fixture.mjs'

const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const executablePath = [process.env.CHROME_PATH, 'C:/Program Files/Google/Chrome/Application/chrome.exe'].find(path => path && existsSync(path))
const server = await startFixture()
const browser = await chromium.launch({ executablePath, headless: true })
try {
  const page = await browser.newPage({ viewport: { width: 1680, height: 1000 } })
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.goto('http://127.0.0.1:18605/__product-cards.html')
  const cards = page.locator('.product-card')
  await cards.first().waitFor()
  assert.equal(await cards.count(), 2)
  assert.equal(await cards.first().getByRole('button', { name: /^修改/ }).count(), 6)
  const size = await cards.first().boundingBox()
  assert.ok(size.width < 460 && size.height < 470, `compact dimensions ${JSON.stringify(size)}`)
  await cards.first().hover()
  await page.waitForTimeout(250)
  assert.ok(await cards.first().evaluate(element => getComputedStyle(element).transform !== 'none'))
  assert.ok(await cards.first().evaluate(element => getComputedStyle(element).boxShadow.includes('20px')))

  // All fields, including clothing, remain category-neutral; saves preserve other fields and the version guard.
  for (const [label, value] of [['商品名称', '通勤圆领短袖上衣'], ['规格', 'S/M/L/XL，白色/深蓝'], ['日常价', '89元'], ['数量', '1件/2件可选'], ['适用', '日常通勤与休闲穿着'], ['链接编号', '3号链接']]) {
    await page.getByRole('button', { name: '修改2号链接的' + label, exact: true }).click()
    const input = page.getByRole('textbox', { name: '编辑2号链接的' + label, exact: true })
    await input.fill(value)
    if (label === '日常价') await page.locator('#outside').click()
    else await input.press('Enter')
    await page.getByText('已自动保存', { exact: true }).waitFor()
    assert.equal(await input.count(), 0)
  }
  assert.equal(await page.evaluate(() => window.fixture.calls.length), 6)
  const saved = await page.evaluate(() => window.fixture.items.value.find(item => item.id === 2))
  assert.equal(saved.link_key, '3号链接')
  assert.equal(saved.version_no, 11)
  assert.equal(saved.spec, 'S/M/L/XL，白色/深蓝')
  const requests = await page.evaluate(() => window.fixture.calls)
  assert.deepEqual(requests.map(request => request.body.expected_version_no), [5, 6, 7, 8, 9, 10])
  assert.ok(requests.every(request => request.url.includes('/20/product-links/2') && request.body.tenant_id === 14))

  // No-op and Escape do not create versions. Required values cannot accidentally be cleared.
  await page.getByRole('button', { name: '修改1号链接的规格', exact: true }).click()
  await page.getByRole('textbox', { name: '编辑1号链接的规格', exact: true }).press('Enter')
  await page.getByRole('button', { name: '修改1号链接的商品名称', exact: true }).click()
  let input = page.getByRole('textbox', { name: '编辑1号链接的商品名称', exact: true })
  await input.fill('')
  await input.press('Enter')
  await page.getByText('商品名称不能为空', { exact: true }).waitFor()
  assert.equal(await page.evaluate(() => window.fixture.calls.length), 6)
  await input.press('Escape')

  // A failed save retains the draft; switching fields commits once, not one request per keystroke.
  await page.evaluate(() => { window.fixture.failSave = true })
  await page.getByRole('button', { name: '修改1号链接的规格', exact: true }).click()
  input = page.getByRole('textbox', { name: '编辑1号链接的规格', exact: true })
  await input.fill('5L/10L组合')
  await input.press('Enter')
  await cards.first().getByText('保存失败，请重试', { exact: true }).waitFor()
  assert.equal(await input.inputValue(), '5L/10L组合')
  await page.evaluate(() => { window.fixture.failSave = false; window.fixture.delay = 100 })
  await page.getByRole('button', { name: '修改1号链接的数量', exact: true }).click()
  await page.getByRole('textbox', { name: '编辑1号链接的数量', exact: true }).waitFor()
  input = page.getByRole('textbox', { name: '编辑1号链接的数量', exact: true })
  await input.fill('1桶/2桶组合')
  await input.press('Enter')
  await page.getByText('已自动保存', { exact: true }).waitFor()
  assert.equal(await page.evaluate(() => window.fixture.items.value.find(item => item.id === 1).version_no), 4)
  assert.equal(await page.evaluate(() => window.fixture.calls.length), 9)

  // Cancel and failures never remove the card; success uses the scoped existing DELETE endpoint.
  await page.evaluate(() => { window.fixture.confirmation = false })
  await page.getByRole('button', { name: '删除3号链接', exact: true }).click()
  assert.equal(await cards.count(), 2)
  assert.equal(await page.evaluate(() => window.fixture.calls.length), 9)
  await page.evaluate(() => { window.fixture.confirmation = true; window.fixture.failDelete = true })
  await page.getByRole('button', { name: '删除3号链接', exact: true }).click()
  await cards.nth(1).getByText('删除失败，请重试', { exact: true }).waitFor()
  assert.equal(await cards.count(), 2)
  await page.evaluate(() => { window.fixture.failDelete = false })
  await page.getByRole('button', { name: '删除3号链接', exact: true }).click()
  await page.waitForFunction(() => window.fixture.items.value.length === 1)
  assert.equal(await cards.count(), 1)
  const deleted = await page.evaluate(() => window.fixture.calls.at(-1))
  assert.equal(deleted.method, 'DELETE')
  assert.equal(deleted.url, '/api/v1/live-agent-plans/20/product-links/2?tenant_id=14')

  // Common facts stay visible; category-specific facts are behind the arrow and support child CRUD.
  await cards.first().getByRole('button', { name: /个性属性/ }).click()
  await cards.first().getByText('压榨工艺', { exact: true }).waitFor()
  assert.equal(await cards.first().getByText('传统熟榨', { exact: true }).count(), 1)
  await cards.first().getByRole('button', { name: /添加个性属性/ }).click()
  await cards.first().getByLabel('属性名称').fill('原料产地')
  await cards.first().getByLabel('属性值').fill('四川')
  await cards.first().getByRole('button', { name: '保存属性', exact: true }).click()
  await cards.first().getByText('个性属性已保存，并会进入话术事实', { exact: true }).waitFor()
  assert.equal(await cards.first().getByText('原料产地', { exact: true }).count(), 1)
  await cards.first().getByRole('button', { name: '修改个性属性压榨工艺', exact: true }).click()
  await cards.first().getByLabel('属性值').fill('小榨熟香工艺')
  await cards.first().getByRole('button', { name: '保存属性', exact: true }).click()
  await cards.first().getByText('小榨熟香工艺', { exact: true }).waitFor()
  await cards.first().getByRole('button', { name: '删除个性属性原料', exact: true }).click()
  await cards.first().getByText('个性属性已删除干净', { exact: true }).waitFor()
  assert.equal(await cards.first().getByText('原料', { exact: true }).count(), 0)
  assert.deepEqual(errors, [])

  // Narrow screens must not overflow.
  await page.setViewportSize({ width: 390, height: 844 })
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
  const artifacts = fileURLToPath(new URL('../../artifacts/', import.meta.url))
  mkdirSync(artifacts, { recursive: true })
  await page.screenshot({ path: artifacts + '/product-cards-mobile.png', fullPage: true })
  await page.setViewportSize({ width: 1680, height: 1000 })
  await page.screenshot({ path: artifacts + '/product-cards-compact.png', fullPage: true })
  console.log('PASS: compact layout, common fields, expandable personalized attributes with child CRUD, automatic save, version guards, deletion, mobile layout')
} finally {
  await browser.close()
  await server.close()
}
