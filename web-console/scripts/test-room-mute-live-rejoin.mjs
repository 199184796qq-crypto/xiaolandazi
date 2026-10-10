import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { startFixture } from './room-parity-fixture.mjs'

const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const output = resolve(process.env.WEB_MUTE_TEST_OUTPUT || '../work/room-mute-live-rejoin')
await mkdir(output, { recursive: true })
const roomViewSource = process.env.WEB_MUTE_BASELINE_SOURCE ? await readFile(process.env.WEB_MUTE_BASELINE_SOURCE, 'utf8') : undefined
const server = await startFixture({ roomViewSource })
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
try {
  const page = await browser.newPage({ viewport: { width: 1680, height: 1000 } })
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.goto('http://127.0.0.1:18601/__room-parity.html?role=customer&waiting=1&caption=1&pcm=1')
  await page.waitForFunction(() => !!window.fixture.pcmController)
  const send = async (sample, frames = 1, stream = -1) => page.evaluate(({ sample, frames, stream }) => {
    const pcm = new Uint8Array(960 * frames)
    const view = new DataView(pcm.buffer)
    for (let offset = 0; offset < pcm.length; offset += 960) view.setInt16(offset, sample, true)
    const controller = stream < 0 ? window.fixture.pcmController : window.fixture.pcmStreams[stream]
    controller.enqueue(pcm)
  }, { sample, frames, stream })
  await send(1000, 120) // A burst intentionally schedules 2.4 seconds of old speech.
  await page.waitForFunction(() => window.fixture.sources.length === 120)
  await page.getByText('正在回答保存问题哟', { exact: true }).waitFor()
  await page.getByRole('button', { name: '静音网页声音', exact: true }).click()
  const muted = await page.evaluate(() => ({ count: window.fixture.sources.length, queued: window.fixture.sources.filter(s => !s.ended && !s.stopped).length, streams: window.fixture.pcmStreams.length }))
  assert.equal(muted.queued, 0, 'mute must cancel old queued audio rather than only set gain to zero')
  await send(2000, 300) // Six seconds during mute must not become a playback backlog.
  await page.waitForTimeout(120)
  assert.equal(await page.evaluate(() => window.fixture.sources.length), muted.count, 'muted incoming PCM must not create queued sources')
  await page.evaluate(() => { window.fixture.audioEngine = { room_id: 15, phase: 'mainline', speech_feed: { current: { segment_id: 'live-now', text: '盒子正在说的新一句', tone: 'normal' } } } })
  await page.getByText('盒子正在说的新一句', { exact: true }).waitFor()
  await page.getByRole('button', { name: '恢复网页声音', exact: true }).click()
  await page.waitForFunction(count => window.fixture.pcmStreams.length > count, muted.streams)
  assert.equal(await page.evaluate(() => window.fixture.sources.length), muted.count, 'unmute must not replay old audio')
  // Deliberately resolve a stale pending read after abort: generation guard must
  // reject it even if a proxy/browser already handed that read back to JS.
  await send(7777, 2, 0)
  await page.waitForTimeout(80)
  assert.equal(await page.evaluate(() => window.fixture.sources.length), muted.count, 'old stream read leaked into the fresh live stream')
  const begin = await page.evaluate(() => performance.now())
  await send(3000)
  await page.waitForFunction(count => window.fixture.sources.length === count + 1, muted.count)
  const rejoin = await page.evaluate(() => ({ source: window.fixture.sources.at(-1), now: performance.now(), streams: window.fixture.pcmStreams.length, calls: window.fixture.calls }))
  assert.equal(rejoin.source.sample, 3000, 'first unmuted audio is not the new live frame')
  assert.ok(rejoin.now - begin < 1000, 'fresh live audio waited behind old buffered speech')
  assert.equal(await page.getByText('盒子正在说的新一句', { exact: true }).count(), 1, 'caption no longer follows the box/Core')
  assert.equal(rejoin.calls.filter(c => /\/runtime\/(start|stop|pause|resume)$/.test(c.path)).length, 0, 'mute/rejoin changed the real live session')
  // Rapid toggles: the final state is mute and no delayed async unmute is allowed.
  await page.getByRole('button', { name: '静音网页声音', exact: true }).click()
  await page.getByRole('button', { name: '恢复网页声音', exact: true }).click()
  await page.getByRole('button', { name: '静音网页声音', exact: true }).click()
  const before = await page.evaluate(() => window.fixture.sources.length)
  await send(4000, 3)
  await page.waitForTimeout(100)
  assert.equal(await page.getByRole('button', { name: '恢复网页声音', exact: true }).count(), 1)
  assert.equal(await page.evaluate(() => window.fixture.sources.length), before, 'async unmute reopened sound after another mute')
  assert.deepEqual(errors, [])
  const result = { passed: true, queuedBeforeMuteMS: 2400, mutedIncomingMS: 6000, firstFreshSample: rejoin.source.sample, receiveToScheduleMS: rejoin.now - begin, checks: ['mute clears queued speech', 'muted stream does not queue audio', 'unmute joins a fresh subscription', 'late reads from old stream are discarded', 'fresh PCM schedules without waiting for old backlog', 'captions still follow Core/box', 'rapid toggles keep final mute state', 'no live-session control calls'], errors }
  await writeFile(resolve(output, 'result.json'), JSON.stringify(result, null, 2))
  console.log(JSON.stringify(result))
} finally { await browser.close(); await server.close() }
