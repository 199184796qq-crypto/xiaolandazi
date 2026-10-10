import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { startFixture } from './room-parity-fixture.mjs'

const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const output = resolve(process.env.WEB_SPEECH_IDENTITY_OUTPUT || '../work/room-voice-identity')
await mkdir(output, { recursive: true })
const version = (id, plan_id, name) => ({ id, plan_id, room_id: 15, tenant_id: 14, voice_identity: { name, rate: 1, model: 'cosyvoice-v3.5-flash', voice_id: 'raw-voice-id', source: 'clone', provider: 'aliyun', version: 'V1' }, variants: [], generation_context: {} })
const older = version(101, 4, '海妹声音')
const newer = version(102, 4, '下场新声音')
const identity = { runtime: { agent_plan_id: 4, agent_state: 'working' }, published: newer, versions: [newer, older], bindings: { 4: { style_id: 201 } }, presenters: { 201: { id: 201, tenant_id: 14, name: '小林主播' } }, speechRuntime: { room_id: 15, program: { room_id: 15, version_id: 101, running: true } } }
const server = await startFixture({ speechIdentity: identity })
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const errors = []
try {
  const page = await browser.newPage({ viewport: { width: 1680, height: 1000 } })
  page.on('pageerror', error => errors.push(error.message))
  const header = page.locator('.speech-runtime-head')
  await page.goto('http://127.0.0.1:18601/__room-parity.html?role=customer&caption=1')
  await header.getByText('小林主播', { exact: true }).waitFor()
  await header.getByText('海妹声音', { exact: true }).waitFor()
  assert.equal(await header.getByText('下场新声音', { exact: true }).count(), 0, 'newly saved voice must not replace playing version')
  assert.match(await header.locator('.speech-identity-item').nth(1).getAttribute('title'), /cosyvoice-v3.5-flash/)
  assert.equal(await header.getByText('raw-voice-id', { exact: true }).count(), 0)
  await header.screenshot({ path: resolve(output, 'desktop-header.png') })
  await page.locator('.speech-runtime-panel').screenshot({ path: resolve(output, 'desktop-panel.png') })

  // A session/plan change must discard the old identity, including delayed replies.
  await page.evaluate(() => {
    window.fixture.identity.delayPlan = 4
    window.fixture.session = 2
  })
  await page.waitForFunction(() => !!window.fixture.releaseIdentity)
  const next = version(203, 5, '小青声音')
  await page.evaluate(next => {
    const data = window.fixture.identity
    data.runtime.agent_plan_id = 5
    data.published = next
    data.bindings[5] = { style_id: 202 }
    data.presenters[202] = { id: 202, tenant_id: 14, name: '大军主播' }
    data.versions = [next]
    data.speechRuntime.program.version_id = next.id
    window.fixture.session = 3
  }, next)
  await header.getByText('大军主播', { exact: true }).waitFor()
  await header.getByText('小青声音', { exact: true }).waitFor()
  await page.evaluate(() => window.fixture.releaseIdentity())
  await page.waitForTimeout(150)
  assert.equal(await header.getByText('小林主播', { exact: true }).count(), 0)

  await page.setViewportSize({ width: 390, height: 844 })
  await header.scrollIntoViewIfNeeded()
  const panel = await page.locator('.speech-runtime-panel').boundingBox()
  const mute = await header.getByRole('button', { name: '静音网页声音' }).boundingBox()
  const chips = await header.locator('.speech-runtime-identity').boundingBox()
  assert.ok(chips.x >= panel.x && chips.x + chips.width <= panel.x + panel.width + 1)
  assert.ok(mute.x + mute.width <= panel.x + panel.width)
  await page.locator('.speech-runtime-panel').screenshot({ path: resolve(output, 'mobile-panel.png') })
  await page.evaluate(() => {
    window.fixture.identity.runtime = { agent_plan_id: 6, agent_state: 'stopped' }
    window.fixture.identity.published = null
    window.fixture.identity.speechRuntime = { room_id: 15 }
  })
  await header.getByText('未设置', { exact: true }).first().waitFor()
  await page.waitForFunction(() => [...document.querySelectorAll('.speech-identity-item b')].every(item => item.textContent === '未设置'))

  const ungranted = await browser.newPage()
  ungranted.on('pageerror', error => errors.push(error.message))
  await ungranted.goto('http://127.0.0.1:18601/__room-parity.html?role=ungranted')
  await ungranted.locator('.speech-runtime-identity').getByText('暂无查看权限', { exact: true }).first().waitFor()
  const privateReads = await ungranted.evaluate(() => window.fixture.calls.filter(call => /live-anchor-styles|live-agent-plans|published-version/.test(call.path)))
  assert.deepEqual(privateReads, [], 'unauthorized staff must not load private plan or presenter configuration')
  const liveControls = await page.evaluate(() => window.fixture.calls.filter(call => /\/runtime\/(start|stop|pause|resume)$/.test(call.path)))
  assert.deepEqual(liveControls, [])
  assert.deepEqual(errors, [])
  const result = { passed: true, checks: ['bound presenter name', 'saved voice alias instead of raw ID', 'playing voice uses exact version', 'plan/session change clears stale identity', 'delayed reads cannot overwrite new plan', 'mobile header fits and mute remains available', 'missing settings have explicit labels', 'unauthorized staff reads no private configuration', 'no live-control calls'], errors }
  await writeFile(resolve(output, 'result.json'), JSON.stringify(result, null, 2))
  console.log(JSON.stringify(result))
} finally { await browser.close(); await server.close() }
