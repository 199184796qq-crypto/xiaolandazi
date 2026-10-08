import assert from 'node:assert/strict'
import { createServer as createHttpServer } from 'node:http'
import { readFileSync, mkdirSync } from 'node:fs'
import { resolve } from 'node:path'
import { createRequire } from 'node:module'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'

const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const schedule = JSON.parse(readFileSync(new URL('../../configs/prompts/anchor-style/interaction-scheduling.json', import.meta.url), 'utf8'))
schedule.version = 1
schedule.plan_id = 3
let preferences = { tenant_id: 2, room_id: 7, overall_interaction: 50, question_preference: 0, welcome_preference: 50, engagement_preference: 50, chat_preference: 50, conversion_preference: 50, auto_heat: true, schedule }
const saves = [], errors = []
const vite = await createServer({ configFile: false, root: resolve('.'), plugins: [vue()], server: { middlewareMode: true }, appType: 'custom' })
const html = `<!doctype html><html lang="zh"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>本地验证：互动权重</title><style>body{margin:20px;background:#f4f6fb;font-family:Arial,sans-serif}#app{max-width:980px;margin:auto}*{box-sizing:border-box}</style><div id="app"></div><script type="module">import {createApp} from 'vue';import Component from '/src/components/RoomInteractionPreferences.vue';createApp(Component,{roomId:7}).mount('#app');</script></html>`
const server = createHttpServer(async (req, res) => {
  try {
    if (req.url.startsWith('/api/v1/rooms/7/interaction-preferences')) {
      let status = 200
      if (req.method === 'PUT') {
        let raw = ''; for await (const data of req) raw += data
        const input = JSON.parse(raw)
        saves.push(input)
        if (Object.values(input.schedule.events).some(rule => rule.weight < 0 || rule.weight > 100)) status = 400
        else preferences = { ...preferences, ...input, schedule: { ...input.schedule, version: input.schedule.version + 1 } }
      }
      res.writeHead(status, { 'Content-Type': 'application/json' })
      res.end(JSON.stringify(status === 200 ? preferences : { error: '处理权重必须在0到100之间' }))
      return
    }
    if (req.url === '/' || req.url === '/index.html') {
      res.writeHead(200, { 'Content-Type': 'text/html' })
      res.end(await vite.transformIndexHtml('/index.html', html))
      return
    }
    vite.middlewares(req, res, () => { res.writeHead(404); res.end() })
  } catch (error) { errors.push(String(error)); if (!res.headersSent) res.writeHead(500); res.end(String(error)) }
})
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
let browser
try {
  browser = await chromium.launch({ executablePath: process.env.CHROME_PATH || 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
  const page = await browser.newPage({ viewport: { width: 1100, height: 1500 } })
  page.on('pageerror', error => errors.push(String(error)))
  await page.goto(`http://127.0.0.1:${server.address().port}`, { waitUntil: 'networkidle' })
  await page.locator('summary').click()
  assert.equal(await page.locator('.event-rule-row').count(), 7)
  assert.equal(await page.getByLabel('整体互动频率').count(), 1)
  await page.getByLabel('投诉与负面反馈优先级').selectOption('P1')
  await page.waitForFunction(() => document.querySelector('summary').textContent.includes('V2'))
  assert.equal(saves[0].schedule.events.complaint.priority, 'P1')
  assert.equal(saves[0].question_preference, 0)
  assert.equal(saves[0].schedule.events.product_question.weight, 90)
  assert.ok(saves[0].schedule.events.welcome.goals.some(goal => goal.action))
  const weight = page.getByLabel('暖场互动处理权重')
  await weight.fill('66'); await weight.press('Tab')
  await page.waitForFunction(() => document.querySelector('summary').textContent.includes('V3'))
  assert.equal(preferences.schedule.events.warmup.weight, 66)
  await page.getByLabel('点赞与关注', { exact: true }).uncheck()
  await page.waitForFunction(() => document.querySelector('summary').textContent.includes('V4'))
  assert.equal(preferences.schedule.events.like_follow.enabled, false)
  assert.equal(preferences.question_preference, 0)
  assert.match(await page.locator('.auto-adjust-copy small').innerText(), /下次启动/)
  await page.setViewportSize({ width: 430, height: 1800 })
  for (const control of await page.locator('.event-rule-row select, .event-rule-row input[type=number]').all()) {
    const bounds = await control.boundingBox()
    assert.ok(bounds.x >= 0 && bounds.x + bounds.width <= 430, 'mobile control overflow')
  }
  const output = process.env.INTERACTION_SCREENSHOT
  if (output) { mkdirSync(resolve(output, '..'), { recursive: true }); await page.screenshot({ path: output, fullPage: true }) }
  assert.deepEqual(errors, [])
  console.log(JSON.stringify({ passed: true, saves: saves.length, preserved_question_disabled: true, goal_rules_preserved: true, mobile_width: 430 }))
} finally {
  if (browser) await browser.close()
  await vite.close()
  await new Promise(resolve => server.close(resolve))
}
