import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const view = await readFile(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
const css = [...view.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)].map(match => match[1]).join('\n')
const card = `<article><header class="strategy-benefit-formal-card-head"><div class="strategy-benefit-formal-title"><strong>包邮活动福利</strong><small>当前活动版本</small></div></header><div class="strategy-benefit-formal-detail"><div class="strategy-benefit-formal-detail-item"><span>活动价</span><strong>未设置</strong></div><div class="strategy-benefit-formal-detail-item"><span>福利</span><strong>免邮费</strong></div></div><button class="strategy-benefit-copy">复制福利卡</button><button class="strategy-benefit-formal-expand">展开条件与有效方式</button></article>`
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const page = await browser.newPage({ viewport: { width: 1100, height: 800 } })
try {
  await page.setContent(`<html><head><style>*{box-sizing:border-box}body{margin:20px}${css}</style></head><body><div class="strategy-benefit-formal-list">${card}${card}</div></body></html>`)
  const first = page.locator('.strategy-benefit-formal-list > article').first()
  const width = (await first.boundingBox()).width
  const oldStyle = await page.addStyleTag({ content: '.strategy-benefit-formal-list {grid-template-columns:repeat(2,minmax(0,1fr))!important}' })
  const oldWidth = (await first.boundingBox()).width
  assert.ok(Math.abs(width / oldWidth - 2 / 3) < .02, `width ratio ${width / oldWidth}`)
  await oldStyle.evaluate(el => el.remove())
  for (const [viewport, columns] of [[1600,3],[800,2],[390,1]]) {
    await page.setViewportSize({ width: viewport, height: 800 })
    const count = await page.locator('.strategy-benefit-formal-list').evaluate(el => getComputedStyle(el).gridTemplateColumns.split(' ').length)
    assert.equal(count, columns)
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
    assert.ok(await first.getByText('复制福利卡').isVisible())
  }
  console.log('PASS: cards about 2/3 previous width; desktop 3 columns; tablet 2; mobile 1; no horizontal overflow')
} finally { await browser.close() }
