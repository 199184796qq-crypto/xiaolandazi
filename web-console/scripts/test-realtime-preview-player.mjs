// Isolated fixture: no credentials, live rooms, upstream models or paid TTS.
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
import { mkdir } from 'node:fs/promises'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const output = process.env.PLAYER_TEST_OUTPUT || 'E:/直播伴播-local/validation/20261008-realtime-player-progress'
await mkdir(output, { recursive: true })
const fixture = `<html><head><style>body{margin:0;padding:32px;background:#f3f7ff;font-family:Arial,'Microsoft Yahei',sans-serif}#app{max-width:1250px;margin:auto}button{font:inherit}*{box-sizing:border-box}</style></head><body><div id="app"></div><script type="module">import{createApp,h}from'/node_modules/.vite/deps/vue.js';import Panel from'/src/components/RealtimeAnchorPreviewPanel.vue';createApp({render:()=>h(Panel,{planId:2,tenantId:14,generationOptions:{buffer_seconds:120,body_min:2,body_max:6,max_chars:300}})}).mount('#app')</script></body></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 5379 }, appType: 'custom' })
server.middlewares.use((req, res, next) => { if (req.url === '/__player_fixture') { res.setHeader('Content-Type', 'text/html'); res.end(fixture) } else next() })
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true, args: ['--autoplay-policy=no-user-gesture-required'] })
const page = await browser.newPage({ viewport: { width: 1400, height: 680 } })
const errors = [], calls = []
page.on('pageerror', e => errors.push(e.message))
const run = { run_id: 'fixture', revision: 1, phase: 'opening', status: 'ready', busy: false, uncertain: false, preview_limit_ms: 120000, cursor: 0, attempts: [], accepted: [], queue: [], config: { max_retries: 1, tts: { rate: 1, buffer_seconds: 120 } } }
const longSpeech = Array.from({ length: 60 }, (_, i) => `第${i + 1}段：测试长文追加，只在固定文字框内滚动，不撑开播放器位置。`).join('\n\n')
let releaseSession, sessionRequested
const sessionGate = new Promise(resolve => { releaseSession = resolve })
const sessionSeen = new Promise(resolve => { sessionRequested = resolve })
// Keep the first real request pending so layout/click tests exercise waiting.
await page.route('**/api/**', async route => {
  const request = route.request(), path = new URL(request.url()).pathname
  calls.push(request.method() + ' ' + path)
  if (path.endsWith('/risk-annotations')) return route.fulfill({ json: { hits: [], unresolved_count: 0 } })
  if (request.method() === 'DELETE') return route.fulfill({ json: { run: null } })
  if (path.endsWith('/realtime-preview')) {
    sessionRequested(); await sessionGate
    return route.fulfill({ contentType: 'text/event-stream', body: 'event: progress\ndata: {"message":"正在准备声音…","percent":60}\n\nevent: complete\ndata: ' + JSON.stringify({ run }) + '\n\n' })
  }
  if (path.endsWith('/advance')) {
    const next = { ...run, revision: 2, phase: 'ended', accepted: [{ selected: { id: 'one', text: longSpeech }, task: {} }] }
    return route.fulfill({ json: { run: next } })
  }
  if (path.endsWith('/voices/one')) {
    return route.fulfill({ contentType: 'text/event-stream', body: 'event: progress\ndata: {"message":"正在准备声音…","percent":75}\n\nevent: complete\ndata: ' + JSON.stringify({ run_id: 'fixture', candidate_id: 'one', voice: { status: 'ready', bundle: { audio_url: '/__fixture.wav', audio_duration_ms: 3000, text: '这是一段测试话术。', timeline: [{ start_ms: 0, end_ms: 3000, text: '这是一段测试话术。' }] } } }) + '\n\n' })
  }
  return route.fulfill({ json: {} })
})
// Harmless silent PCM fixture, separate from production/user assets.
const wav = Buffer.alloc(44 + 24000 * 2 * 3)
wav.write('RIFF'); wav.writeUInt32LE(wav.length - 8, 4); wav.write('WAVEfmt ', 8); wav.writeUInt32LE(16, 16); wav.writeUInt16LE(1, 20); wav.writeUInt16LE(1, 22); wav.writeUInt32LE(24000, 24); wav.writeUInt32LE(48000, 28); wav.writeUInt16LE(2, 32); wav.writeUInt16LE(16, 34); wav.write('data', 36); wav.writeUInt32LE(wav.length - 44, 40)
await page.route('**/__fixture.wav', route => route.fulfill({ contentType: 'audio/wav', body: wav }))
try {
  await page.goto('http://127.0.0.1:5379/__player_fixture')
  const playerSize = await page.locator('.audio-player').evaluate(el => {
    const originalStyle = el.getAttribute('style')
    el.style.zoom = '1'
    el.style.width = '100%'
    const full = el.getBoundingClientRect()
    const baseline = { width: full.width, height: full.height }
    if (originalStyle === null) el.removeAttribute('style'); else el.setAttribute('style', originalStyle)
    const scaled = el.getBoundingClientRect()
    return { baseline, scaled: { width: scaled.width, height: scaled.height } }
  })
  assert.ok(Math.abs(playerSize.scaled.width - playerSize.baseline.width * .5) < 1, 'player width not halved')
  // Zoomed 1px borders/line boxes may round to a physical pixel.
  assert.ok(Math.abs(playerSize.scaled.height - playerSize.baseline.height * .5) <= 2, 'player height not halved: ' + JSON.stringify(playerSize))
  const speechBox = page.getByRole('textbox', { name: '累计生成话术' })
  const initialHeight = await speechBox.evaluate(el => el.getBoundingClientRect().height)
  assert.equal(initialHeight, 440)
  const play = page.getByRole('button', { name: '开始或继续试听' })
  await play.click(); await sessionSeen
  await page.getByText('正在准备…', { exact: true }).waitFor()
  await page.screenshot({ path: output + '/player-preparing.png', fullPage: true })
  await page.getByRole('button', { name: '暂停试听', exact: true }).click()
  releaseSession()
  await page.waitForFunction(() => document.querySelector('[role="progressbar"]')?.getAttribute('aria-valuenow') === '15')
  assert.equal(calls.some(call => call.includes('/advance')), false, 'late initial response resumed a paused producer')
  await play.click()
  await page.waitForFunction(() => !document.querySelector('audio')?.paused)
  await page.waitForFunction(() => document.querySelector('[role="progressbar"]')?.getAttribute('aria-label') === '试听播放进度')
  const scrolling = await speechBox.evaluate(el => ({ height: el.getBoundingClientRect().height, content: el.scrollHeight, viewport: el.clientHeight, top: el.scrollTop }))
  assert.equal(scrolling.height, initialHeight, 'appended speech changed window size')
  assert.ok(scrolling.content > scrolling.viewport && scrolling.top > 0, 'long speech did not scroll inside its window')
  await speechBox.evaluate(el => { el.scrollTop = 0 })
  assert.equal(await speechBox.evaluate(el => el.scrollTop), 0)
  await page.screenshot({ path: output + '/player-playing.png', fullPage: true })
  const caption = await page.locator('.playback-caption').evaluate(el => ({ align: getComputedStyle(el).textAlign, vertical: getComputedStyle(el).alignItems }))
  assert.deepEqual(caption, { align: 'center', vertical: 'center' })
  await page.getByRole('button', { name: '暂停试听', exact: true }).click()
  assert.equal(await page.locator('audio').evaluate(el => el.paused), true)
  await play.click()
  await page.waitForFunction(() => !document.querySelector('audio')?.paused)
  await page.getByRole('button', { name: '结束试听', exact: true }).click()
  assert.equal(await page.locator('audio').evaluate(el => el.hasAttribute('src')), false)
  await page.setViewportSize({ width: 390, height: 700 })
  assert.equal(await speechBox.evaluate(el => el.getBoundingClientRect().height), 320)
  await page.screenshot({ path: output + '/player-mobile.png', fullPage: true })
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
  assert.deepEqual(errors, [])
  console.log('PASS player preparation/pause/late response/resume/playback/caption/stop/mobile; all APIs and audio mocked')
} finally { releaseSession(); await browser.close(); await server.close() }
