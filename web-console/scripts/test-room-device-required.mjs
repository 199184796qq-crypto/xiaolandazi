import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { readFile } from 'node:fs/promises'
import { startFixture } from './room-parity-fixture.mjs'

const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const device = { id: 7, room_id: 15, binding_role: 'primary', connection_status: 'online', work_status: 'idle', tenant_id: 14, sn: 'TEST-BOX' }
const roomViewSource = process.env.DEVICE_REQUIRED_VIEW_BASELINE ? await readFile(process.env.DEVICE_REQUIRED_VIEW_BASELINE, 'utf8') : undefined
const fixture = await startFixture({ roomDevices: [device], roomViewSource })
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true, args: ['--mute-audio'] })
const results = []
async function scenario(name, run) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } }); page.setDefaultTimeout(6000)
  const errors = []; page.on('pageerror', e => errors.push(e.message))
  try {
    await page.clock.install(); await page.goto('http://127.0.0.1:18601/__room-parity.html?role=customer&waiting=1&pcm=1&start_test=1')
    await page.waitForFunction(() => !!window.fixture.pcmController)
    await run(page); assert.deepEqual(errors, []); results.push({ name, passed: true })
  } catch (err) {
    console.error(name, await page.locator('.companion-control-card').innerText().catch(() => 'missing controls'), await page.evaluate(() => ({ waiting: window.fixture.waiting, runtimeCalls: window.fixture.calls.filter(c => c.path.endsWith('/runtime')).length })))
    throw err
  } finally { await page.close() }
}
try {
  await scenario('no box or offline box prevents a paid start request; online bound box enables it', async page => {
    await page.evaluate(() => { window.fixture.waiting = false; window.fixture.devices = [] })
    await page.clock.runFor(5200)
    await page.getByRole('button', { name: '开始', exact: true }).waitFor()
    assert.equal(await page.getByRole('button', { name: '开始', exact: true }).isDisabled(), true)
    await page.getByText('请先给直播间绑定主盒子', { exact: true }).waitFor()
    await page.evaluate(d => { window.fixture.devices = [{ ...d, connection_status: 'offline' }] }, device)
    await page.clock.runFor(5200)
    assert.equal(await page.getByRole('button', { name: '开始', exact: true }).isDisabled(), true)
    await page.evaluate(d => { window.fixture.devices = [d] }, device); await page.clock.runFor(5200)
    assert.equal(await page.getByRole('button', { name: '开始', exact: true }).isEnabled(), true)
    assert.ok(!(await page.evaluate(() => window.fixture.calls)).some(c => /\/runtime\/(start|resume)$/.test(c.path)))
  })
  await scenario('one health request failure preserves PCM and sends no start or stop', async page => {
    await page.evaluate(async () => {
      const health = await import('/src/coreRuntime.ts')
      const original = window.fetch; let fail = false
      window.fetch = async (url, opts) => {
        if (new URL(String(url), location.origin).pathname === '/api/v1/runtime/core-status') { if (fail) throw new Error('temporary health failure'); return Response.json({ available: true, core_boot_id: 'same-boot' }) }
        return original(url, opts)
      }
      await health.refreshCoreRuntimeStatus(); fail = true; await health.refreshCoreRuntimeStatus(); fail = false; await health.refreshCoreRuntimeStatus()
    })
    assert.deepEqual(await page.evaluate(() => window.fixture.cancelledStreams), [])
    assert.equal(await page.evaluate(() => window.fixture.pcmStreams.length), 1)
    assert.ok(!(await page.evaluate(() => window.fixture.calls)).some(c => /\/runtime\/(start|stop|pause|resume)$/.test(c.path)))
  })
  await scenario('late stopped snapshot cannot overwrite a newer working snapshot', async page => {
    await page.locator('.companion-control-buttons .pause').waitFor({ state: 'attached' })
    await page.evaluate(() => {
      const original = window.fetch; let held = false
      window.fetch = async (url, opts) => {
        if (!held && new URL(String(url), location.origin).pathname === '/api/v1/rooms/15/runtime') {
          held = true
          const body = await (await original(url, opts)).json()
          return new Promise(resolve => { window.fixture.releaseOldRuntime = () => resolve(Response.json({ ...body, agent_state: 'stopped' })) })
        }
        return original(url, opts)
      }
    })
    await page.clock.runFor(5100); await page.waitForFunction(() => !!window.fixture.releaseOldRuntime)
    await page.clock.runFor(5100); await page.locator('.companion-control-buttons .pause').waitFor({ state: 'attached' })
    await page.evaluate(() => window.fixture.releaseOldRuntime())
    await page.clock.runFor(100)
    assert.equal(await page.locator('.companion-control-buttons .pause').count(), 1)
    assert.equal(await page.getByRole('button', { name: '开始', exact: true }).count(), 0)
    assert.ok(!(await page.evaluate(() => window.fixture.calls)).some(c => /\/runtime\/(start|stop|pause|resume)$/.test(c.path)))
  })
  console.log(JSON.stringify({ passed: true, results }))
} finally { await browser.close(); await fixture.close() }
