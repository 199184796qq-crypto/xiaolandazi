import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { startFixture } from './room-parity-fixture.mjs'

const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const output = resolve(process.env.WEB_AUDIO_RECOVERY_OUTPUT || '../work/room-audio-recovery')
await mkdir(output, { recursive: true })
const roomViewSource = process.env.WEB_AUDIO_RECOVERY_BASELINE ? await readFile(process.env.WEB_AUDIO_RECOVERY_BASELINE, 'utf8') : undefined
const server = await startFixture({ roomViewSource })
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true, args: ['--mute-audio'] })
const results = []
async function frame(page, value = 3000) {
  await page.evaluate(value => {
    const bytes = new Uint8Array(960)
    new DataView(bytes.buffer).setInt16(0, value, true)
    window.fixture.pcmController.enqueue(bytes)
  }, value)
}
async function scenario(name, run) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
  page.setDefaultTimeout(4000)
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  try {
    await page.clock.install()
    await page.goto('http://127.0.0.1:18601/__room-parity.html?role=customer&waiting=1&caption=1&pcm=1')
    await page.waitForFunction(() => !!window.fixture.pcmController)
    await frame(page)
    await page.waitForFunction(() => window.fixture.sources.length === 1)
    await page.getByRole('button', { name: '静音网页声音', exact: true }).click()
    const before = await page.evaluate(() => ({ streams: window.fixture.pcmStreams.length, sources: window.fixture.sources.length }))
    await run(page, before)
    assert.deepEqual(errors, [])
    const calls = await page.evaluate(() => window.fixture.calls)
    assert.ok(!calls.some(c => /\/runtime\/(start|stop|pause|resume)$/.test(c.path)), 'local mute must not change the real live session')
    results.push({ name, passed: true })
  } catch (err) {
    results.push({ name, passed: false, error: err.message })
  } finally {
    await page.close()
  }
}
try {
  await scenario('interrupted audio context resumes on explicit unmute', async (page, before) => {
    await page.evaluate(() => { window.fixture.audioContexts.at(-1).state = 'interrupted' })
    await page.getByRole('button', { name: '恢复网页声音', exact: true }).click()
    await page.waitForFunction(() => window.fixture.audioContexts.at(-1).state === 'running', null, { timeout: 1500 })
    await page.waitForFunction(n => window.fixture.pcmStreams.length > n, before.streams)
    await frame(page, 1234)
    await page.waitForFunction(n => window.fixture.sources.length > n, before.sources)
    assert.equal(await page.evaluate(() => window.fixture.sources.at(-1).sample), 1234)
  })
  await scenario('unmute replaces a hung old registration and ignores its late response', async (page, before) => {
    await page.evaluate(() => { window.fixture.hangNextRegister = true })
    await page.getByRole('button', { name: '恢复网页声音', exact: true }).click()
    await page.waitForFunction(() => window.fixture.registrationWaiters.length === 1)
    await page.getByRole('button', { name: '静音网页声音', exact: true }).click()
    await page.getByRole('button', { name: '恢复网页声音', exact: true }).click()
    await page.waitForFunction(n => window.fixture.pcmStreams.length > n, before.streams, { timeout: 1800 })
    await frame(page, 2345)
    await page.waitForFunction(n => window.fixture.sources.length > n, before.sources)
    const streams = await page.evaluate(() => window.fixture.pcmStreams.length)
    await page.evaluate(() => window.fixture.registrationWaiters.shift()())
    await page.clock.runFor(180)
    assert.equal(await page.evaluate(() => window.fixture.pcmStreams.length), streams, 'late registration opened a stale subscription')
    await frame(page, 3456)
    await page.waitForFunction(() => window.fixture.sources.at(-1).sample === 3456)
  })
  await scenario('closed context is replaced and first audio is fresh', async (page, before) => {
    await page.evaluate(() => { window.fixture.audioContexts.at(-1).state = 'closed' })
    await page.getByRole('button', { name: '恢复网页声音', exact: true }).click()
    await page.waitForFunction(() => window.fixture.audioContexts.length === 2)
    await page.waitForFunction(n => window.fixture.pcmStreams.length > n, before.streams)
    await frame(page, 4567)
    await page.waitForFunction(n => window.fixture.sources.length > n, before.sources)
    assert.equal(await page.evaluate(() => window.fixture.sources.at(-1).sample), 4567)
  })
  await scenario('failed resume remains retryable without page reload', async (page, before) => {
    await page.evaluate(() => { window.fixture.audioContexts.at(-1).state = 'suspended'; window.fixture.resumeError = '模拟输出设备暂不可用' })
    await page.getByRole('button', { name: '恢复网页声音', exact: true }).click()
    await page.getByRole('button', { name: '恢复网页声音', exact: true }).waitFor({ timeout: 1500 })
    await page.evaluate(() => { window.fixture.resumeError = '' })
    await page.getByRole('button', { name: '恢复网页声音', exact: true }).click()
    await page.waitForFunction(() => window.fixture.audioContexts.at(-1).state === 'running')
    await page.waitForFunction(n => window.fixture.pcmStreams.length > n, before.streams)
    await frame(page, 5678)
    await page.waitForFunction(n => window.fixture.sources.length > n, before.sources)
  })
  await scenario('long mute unregisters, cancels PCM and prevents all automatic audio reconnects', async (page, before) => {
    const muted = await page.evaluate(() => ({ calls: window.fixture.calls.length, receivers: window.fixture.activeReceivers, cancelled: window.fixture.cancelledStreams }))
    assert.equal(muted.receivers.length, 0)
    assert.ok(muted.cancelled.includes(0), 'mute kept the old PCM response body open')
    await page.evaluate(() => { window.fixture.audioEngine = { room_id: 15, phase: 'mainline', sequence: 300, mainline_cursor_ms: 6000 } })
    await page.clock.fastForward(90_000)
    const after = await page.evaluate(n => ({ audioCalls: window.fixture.calls.slice(n).filter(c => c.path.includes('/receivers/') || c.path.endsWith('/composite.pcm')), streamCount: window.fixture.pcmStreams.length, sources: window.fixture.sources.length }), muted.calls)
    assert.deepEqual(after.audioCalls, [], 'heartbeat or watchdog reopened muted audio')
    assert.equal(after.streamCount, before.streams)
    assert.equal(after.sources, before.sources)
    await page.getByRole('button', { name: '恢复网页声音', exact: true }).click()
    await page.waitForFunction(n => window.fixture.pcmStreams.length > n, before.streams)
    await frame(page, 6789)
    await page.waitForFunction(n => window.fixture.sources.length > n, before.sources)
    assert.equal(await page.evaluate(() => window.fixture.sources.at(-1).sample), 6789)
  })
  await scenario('late unregister cannot delete a new registration', async (page, before) => {
    await page.getByRole('button', { name: '恢复网页声音', exact: true }).click()
    await page.waitForFunction(n => window.fixture.pcmStreams.length > n, before.streams)
    await page.evaluate(() => { window.fixture.hangNextUnregister = true })
    await page.getByRole('button', { name: '静音网页声音', exact: true }).click()
    await page.waitForFunction(() => window.fixture.unregisterWaiters.length === 1)
    await page.getByRole('button', { name: '恢复网页声音', exact: true }).click()
    await page.waitForFunction(n => window.fixture.pcmStreams.length > n + 1, before.streams)
    await page.evaluate(() => window.fixture.unregisterWaiters.shift()())
    assert.equal(await page.evaluate(() => window.fixture.activeReceivers.length), 1, 'old unregister removed the current receiver')
    await frame(page, 7890)
    await page.waitForFunction(() => window.fixture.sources.at(-1).sample === 7890)
    const ids = await page.evaluate(() => window.fixture.calls.filter(c => c.path.endsWith('/receivers/register')).map(c => c.body.receiver_id))
    assert.equal(new Set(ids).size, ids.length, 'listening attempts reused a receiver ID')
  })
  await scenario('hung registration times out and retries without a page reload', async (page, before) => {
    await page.evaluate(() => { window.fixture.hangNextRegister = true })
    await page.getByRole('button', { name: '恢复网页声音', exact: true }).click()
    await page.waitForFunction(() => window.fixture.registrationWaiters.length === 1)
    await page.clock.fastForward(8100)
    await page.clock.runFor(1000)
    await page.waitForFunction(n => window.fixture.pcmStreams.length > n, before.streams)
    await frame(page, 8901)
    await page.waitForFunction(() => window.fixture.sources.at(-1).sample === 8901)
  })
  const realPage = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
  try {
    // Real Chromium Web Audio, with only transport/API simulated and hardware
    // output muted by the launch flag. onended verifies its output clock ran.
    await realPage.goto('http://127.0.0.1:18601/__room-parity.html?role=customer&waiting=1&caption=1')
    await realPage.waitForFunction(() => !!window.fixture.pcmController)
    await realPage.getByRole('button', { name: '静音网页声音', exact: true }).click()
    await realPage.evaluate(() => window.__livecompanion_shared_audio_context_v1.suspend())
    await realPage.getByRole('button', { name: '恢复网页声音', exact: true }).click()
    await realPage.waitForFunction(() => window.__livecompanion_shared_audio_context_v1.state === 'running' && window.fixture.pcmStreams.length > 1)
    await frame(realPage, 9000)
    await realPage.locator('.speech-runtime-initializing').waitFor({ state: 'hidden', timeout: 5000 })
    await realPage.getByText('正在回答保存问题哟', { exact: true }).waitFor()
    results.push({ name: 'real Chromium audio context resumes and completes fresh PCM output', passed: true })
  } catch (err) {
    results.push({ name: 'real Chromium audio context resumes and completes fresh PCM output', passed: false, error: err.message })
  } finally { await realPage.close() }
  await writeFile(resolve(output, 'result.json'), JSON.stringify({ results, passed: results.every(r => r.passed) }, null, 2))
  console.log(JSON.stringify({ results, passed: results.every(r => r.passed) }))
  if (results.some(r => !r.passed)) process.exitCode = 1
} finally { await browser.close(); await server.close() }
