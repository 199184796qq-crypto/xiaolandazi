// Real HTTP/WebAudio regression fixture; no production credentials or paid models.
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { createRequire } from 'node:module'
import { mkdir, writeFile } from 'node:fs/promises'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const output = process.env.PREVIEW_RECOVERY_OUTPUT || '../work/preview-recovery'
await mkdir(output, { recursive: true })
const limit = 8000, calls = [], stores = new Map(), errors = []
let failRead = false, raceCreate = false, delayRead = false, releaseRead
const run = (plan, round) => ({ run_id: `fixture-${plan}-${round}`, stream_controlled: true, preview_limit_ms: limit, revision: 1, phase: 'body', status: 'ready', busy: false, uncertain: false, attempts: [], queue: [], accepted: [], config: { tts: { rate: 1, buffer_seconds: 120 } } })
function seed(plan, existing = true) {
  const item = { run: existing ? run(plan, 1) : null, played: existing ? 3000 : 0, sent: existing ? 3000 : 0, paused: true, finished: false, connected: false, round: 1, created: 0, receiver: null }
  stores.set(plan, item); return item
}
const existing = seed(4); seed(5, false)
const fixture = `<html><head><style>body{padding:32px;background:#f3f7ff;font-family:Arial,'Microsoft Yahei',sans-serif}#app{max-width:1200px;margin:auto}</style></head><body><div id="app"></div><script type="module">import{createApp,h,ref}from'/node_modules/.vite/deps/vue.js';import Panel from'/src/components/RealtimeAnchorPreviewPanel.vue';createApp({setup(){const plan=ref(4);return()=>h('main',[h('button',{onClick:()=>plan.value=5},'切换方案'),h(Panel,{planId:plan.value,tenantId:14,generationOptions:{buffer_seconds:120,body_min:2,body_max:6,max_chars:300}})])}}).mount('#app')</script></body></html>`
const server = await createServer({ configFile: false, plugins: [vue()], appType: 'custom', server: { host: '127.0.0.1', port: 5384, strictPort: true } })
server.middlewares.use((req, res, next) => {
  const path = new URL(req.url, 'http://localhost').pathname
  if (path === '/__recovery_fixture') { res.setHeader('Content-Type', 'text/html; charset=utf-8'); res.end(fixture); return }
  if (!path.startsWith('/api/')) return next()
  const plan = Number(path.match(/live-agent-plans\/(\d+)/)?.[1]), item = stores.get(plan)
  calls.push({ method: req.method, path })
  const json = (value, code = 200) => { res.statusCode = code; res.setHeader('Content-Type', 'application/json'); res.end(JSON.stringify(value)) }
  if (!item) return json({ error: 'wrong plan' }, 404)
  if (path.endsWith('/realtime-preview') && req.method === 'GET') {
    if (failRead) return json({ error: '读取试听进度失败' }, 503)
    const reply = () => json({ run: item.run, playback: item.run ? { played_ms: item.played, paused: item.paused, finished: item.finished, connected: item.connected, progress: 95, part: 'voice' } : null })
    if (delayRead && plan === 4) { delayRead = false; releaseRead = reply; return }
    return reply()
  }
  if (path.endsWith('/realtime-preview') && req.method === 'POST') {
    let body = ''; req.on('data', chunk => body += chunk); req.on('end', () => {
      assert.equal(JSON.parse(body).stream_version, 1)
      if (raceCreate) { raceCreate = false; item.run = run(plan, ++item.round); item.played = 0; item.sent = 0; item.finished = false; item.paused = true }
      if (item.run) return json({ error: '已有本场大循环，请继续或先结束本场' }, 409)
      item.run = run(plan, ++item.round); item.played = 0; item.sent = 0; item.finished = false; item.created++
      json({ run: item.run })
    }); return
  }
  if (path.endsWith('/playback')) {
    let body = ''; req.on('data', chunk => body += chunk); req.on('end', () => {
      const input = JSON.parse(body); calls.at(-1).action = input.action
      item.played = Math.max(item.played, Math.min(input.played_ms, item.sent))
      if (input.action === 'resume' && item.finished) return json({ error: '本场试听已停止，请重新开启' }, 409)
      if (input.action === 'resume') item.paused = false
      if (['pause', 'limit', 'stop'].includes(input.action)) { item.paused = true; item.receiver?.end() }
      if (['limit', 'stop'].includes(input.action)) item.finished = true
      json({ played_ms: item.played, paused: item.paused })
    }); return
  }
  if (path.endsWith('/stream')) {
    if (item.connected || item.finished) return json({ error: 'receiver unavailable' }, 409)
    item.connected = true; item.paused = false; item.receiver = res; item.sent = item.played
    res.setHeader('Content-Type', 'application/x-ndjson'); res.flushHeaders()
    res.write(JSON.stringify({ type: 'state', run: item.run, ready_ms: limit - item.played, part: 'voice', progress: 100 }) + '\n')
    const timer = setInterval(() => {
      if (item.paused || res.destroyed || item.sent - item.played >= 800) return
      const samples = Math.min(4800, Math.floor((limit - item.sent) * 24 + .000001))
      if (samples <= 0) return
      res.write(JSON.stringify({ type: 'audio', candidate_id: 'one', sample_rate: 24000, channels: 1, pcm: Buffer.alloc(samples * 2).toString('base64'), clip_ms: item.sent, stream_ms: item.sent, caption: item.sent < 6000 ? '正在播放恢复的声音' : '恢复后继续播放' }) + '\n')
      item.sent += samples / 24
    }, 100)
    res.on('close', () => { clearInterval(timer); item.connected = false; item.paused = true; item.receiver = null }); return
  }
  if (req.method === 'DELETE') { item.receiver?.end(); item.run = null; return json({ run: null }) }
  return json({ error: 'unexpected fixture call' }, 404)
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true, args: ['--mute-audio', '--autoplay-policy=no-user-gesture-required'] })
const page = await browser.newPage({ viewport: { width: 1400, height: 750 } })
page.on('pageerror', e => errors.push(e.message))
const play = page.getByRole('button', { name: '开始或继续试听' })
const pause = page.getByRole('button', { name: '暂停试听', exact: true })
const stop = page.getByRole('button', { name: '结束试听', exact: true })
const creations = () => calls.filter(c => c.method === 'POST' && c.path.endsWith('/realtime-preview')).length
async function until(condition, label) {
  const deadline = Date.now() + 5000
  while (!condition()) { if (Date.now() > deadline) throw new Error(label); await new Promise(resolve => setTimeout(resolve, 20)) }
}
const clock = ms => `${Math.floor(ms / 60000).toString().padStart(2, '0')}:${Math.floor(ms / 1000 % 60).toString().padStart(2, '0')}`
try {
  await page.goto('http://127.0.0.1:5384/__recovery_fixture')
  await page.getByText('已读取上次试听，点击播放继续；点击结束可重新开始。').waitFor()
  assert.equal(await page.locator('.player-clock strong').textContent(), '00:03')
  assert.equal(await stop.isEnabled(), true)
  assert.equal(creations(), 0)
  await play.click(); await page.getByText('正在播放恢复的声音', { exact: true }).waitFor()
  await page.waitForTimeout(650); await pause.click(); await until(() => existing.paused && !existing.connected, 'pause did not disconnect')
  const saved = existing.played
  assert.ok(saved > 3000 && saved < 5000)
  await page.reload(); await page.getByText('已读取上次试听，点击播放继续；点击结束可重新开始。').waitFor()
  assert.equal(await page.locator('.player-clock strong').textContent(), clock(saved))
  assert.equal(creations(), 0, 'refresh issued another paid session request')
  await play.click(); await page.getByText('恢复后继续播放', { exact: true }).waitFor()
  await page.getByText('本场试听已播放完成', { exact: true }).waitFor({ timeout: 15000 })
  assert.ok(Math.abs(existing.played - limit) <= 1 / 24, 'restored playback extended the original listening budget')
  assert.equal(await page.locator('.player-clock strong').textContent(), '00:08')
  await page.reload(); await page.getByText('本场试听已播放完成', { exact: true }).waitFor()
  assert.equal(creations(), 0)
  await page.screenshot({ path: output + '/恢复已完成试听.png', fullPage: true })
  await play.click(); await page.getByText('正在播放恢复的声音', { exact: true }).waitFor()
  assert.equal(existing.created, 1, 'explicit restart did not create exactly one new session')
  await stop.click(); await until(() => !existing.run, 'stop did not remove recovered run')

  failRead = true
  await page.reload(); await page.getByText('读取试听进度失败', { exact: true }).waitFor()
  const beforeFailure = creations(); await play.click(); await page.waitForTimeout(250)
  assert.equal(creations(), beforeFailure, 'failed state lookup caused a duplicate paid request')
  failRead = false
  await page.reload()
  raceCreate = true; await play.click()
  await page.getByText('已读取上次试听，点击播放继续；点击结束可重新开始。').waitFor()
  assert.equal(existing.created, 1, 'creation conflict retried paid generation')
  await play.click(); await page.getByText('正在播放恢复的声音', { exact: true }).waitFor()
  assert.equal(existing.created, 1, 'conflict recovery could not resume the winning session')
  await stop.click(); await until(() => !existing.run, 'race run stop failed')

  seed(4).connected = true
  await page.reload(); await page.getByText('已读取上次试听，点击播放继续；点击结束可重新开始。').waitFor()
  const beforeOtherReceiver = calls.filter(c => c.path.endsWith('/playback')).length
  await play.click(); await page.getByText('本场正在另一个页面播放，请先暂停原页面，或点击结束试听。', { exact: true }).waitFor()
  assert.equal(calls.filter(c => c.path.endsWith('/playback')).length, beforeOtherReceiver, 'recovery paused another active page')

  delayRead = true
  const switched = await browser.newPage()
  switched.on('pageerror', e => errors.push(e.message))
  await switched.goto('http://127.0.0.1:5384/__recovery_fixture')
  await until(() => !!releaseRead, 'initial lookup not pending')
  await switched.getByRole('button', { name: '切换方案', exact: true }).click()
  releaseRead(); releaseRead = undefined; await switched.waitForTimeout(250)
  assert.equal(await switched.getByRole('button', { name: '结束试听', exact: true }).isDisabled(), true, 'late old-plan lookup replaced current plan')
  const beforeSwitch = calls.length
  await switched.getByRole('button', { name: '开始或继续试听' }).click()
  await switched.getByText('正在播放恢复的声音', { exact: true }).waitFor()
  await switched.getByRole('button', { name: '结束试听', exact: true }).click()
  assert.equal(calls.slice(beforeSwitch).some(c => c.path.includes('/live-agent-plans/4/')), false, 'current page controlled a prior plan')
  assert.deepEqual(errors, [])
  const report = { passed: true, recovered_partial_playback: true, refresh_paid_creations: 0, original_budget_ms: limit, restored_finished_run: true, explicit_restart_creations: 1, conflict_recovered: true, failed_lookup_no_create: true, active_other_page_untouched: true, stale_plan_response_ignored: true, page_errors: errors }
  await writeFile(output + '/验证.json', JSON.stringify(report, null, 2))
  console.log(JSON.stringify(report))
} catch (e) {
  console.log(JSON.stringify({ error: e.message, page_errors: errors, calls: calls.slice(-12), pages: await Promise.all(browser.contexts().flatMap(c => c.pages()).map(async p => ({ url: p.url(), text: await p.locator('body').innerText() }))) }))
  throw e
} finally { for (const item of stores.values()) item.receiver?.end(); releaseRead?.(); await browser.close(); await server.close() }
