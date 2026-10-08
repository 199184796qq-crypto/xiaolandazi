import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { existsSync, readFileSync } from 'node:fs'
import { resolve, extname, sep } from 'node:path'
import { createRequire } from 'node:module'

const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const root = resolve(process.env.DEVICE_BINDING_WEB_ROOT || 'dist')
const screenshots = resolve(process.env.DEVICE_BINDING_SCREENSHOTS || '../work/device-room-picker')
const base = { tenant_id: 14, sku_code: 'xldzplus', lifecycle_status: 'ACTIVE', connection_status: 'online', work_status: 'idle', display_status: '连续待机', stop_reason: '', last_heartbeat_at: new Date().toISOString() }
let devices = [
  { ...base, id: 7, device_name: '小蓝直播助手', room_id: 10, binding_role: 'primary' },
  { ...base, id: 8, device_name: '二号设备', room_id: 20, binding_role: 'primary' },
  { ...base, id: 9, device_name: '待绑定设备', binding_role: '' },
  { ...base, id: 11, device_name: '监听设备', room_id: 20, binding_role: 'listener' },
  { ...base, id: 12, device_name: '无主设备房间的监听设备', room_id: 40, binding_role: 'listener' },
]
const rooms = [
  { id: 10, tenant_id: 14, name: '绸都杨鸭子', external_room_id: 'room-10', status: 'live' },
  { id: 20, tenant_id: 14, name: '已有主设备直播间', external_room_id: 'room-20', status: 'offline' },
  { id: 30, tenant_id: 14, name: '空直播间', external_room_id: 'room-30', status: 'offline' },
  { id: 40, tenant_id: 14, name: '暂无主设备直播间', external_room_id: 'room-40', status: 'offline' },
  { id: 99, tenant_id: 99, name: '其他用户直播间', external_room_id: 'room-99', status: 'offline' },
]
const calls = [], errors = []
const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url, 'http://localhost'), method = req.method
    if (url.pathname.startsWith('/api/')) {
      let raw = ''; for await (const data of req) raw += data
      const body = raw ? JSON.parse(raw) : null
      calls.push({ path: url.pathname, method, body })
      let value = { items: [] }, status = 200
      if (url.pathname === '/api/v1/bootstrap') value = { actor: { user_id: 28, tenant_id: 14, username: 'fixture', display_name: '本地验收', role: 'customer', must_change_password: false, phone: '13800000000', province: '浙江省', city: '杭州市', district: '西湖区', address: '本地验收' }, tenants: [{ id: 14, name: '本地验收租户', status: 'active' }], environment: 'test' }
      else if (url.pathname === '/api/v1/system/public-config') value = { browser_title: '设备绑定本地验收' }
      else if (url.pathname === '/api/v1/runtime/core-status') value = { available: true, status: 'online', active_rooms: 0, core_boot_id: 'fixture', checked_at: new Date().toISOString() }
      else if (url.pathname === '/api/v1/account') value = { profile: { user_id: 28, role: 'customer' }, quota: { total_seconds: 3600 } }
      else if (url.pathname === '/api/v1/account/ui-preferences') value = { user_id: 28, sidebar_collapsed: false, live_plan_panel_collapsed: false, agent_drawer_collapsed: true, ...body }
      else if (url.pathname === '/api/v1/live/devices') value = devices
      else if (url.pathname === '/api/v1/rooms') value = { items: rooms }
      else if (/\/addressing$/.test(url.pathname)) value = { mode: 'auto' }
      else if (/\/live\/devices\/\d+\/bind$/.test(url.pathname)) {
        await new Promise(resolve => setTimeout(resolve, 120))
        const device = devices.find(d => d.id === Number(url.pathname.split('/').at(-2)))
        if (method === 'DELETE') { delete device.room_id; device.binding_role = ''; value = { ok: true } }
        else {
          const primary = devices.find(d => d.id !== device.id && d.room_id === body.room_id && d.binding_role === 'primary')
          const promote = device.room_id === body.room_id && device.binding_role === 'listener' && body.binding_role === 'primary'
          if (promote && primary) primary.binding_role = 'listener'
          device.binding_role = !primary || promote ? 'primary' : 'listener'
          device.room_id = body.room_id
          value = device
        }
      } else if (method !== 'GET') { status = 400; value = { error: 'Unexpected local fixture mutation' } }
      res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(value)); return
    }
    let target = resolve(root, '.' + url.pathname)
    if (!target.startsWith(root + sep) && target !== root) throw Error('outside fixture root')
    if (!existsSync(target) || !extname(target)) target = resolve(root, 'index.html')
    const file = readFileSync(target)
    res.writeHead(200, { 'Content-Type': ({ '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css' })[extname(target)] || 'application/octet-stream' })
    res.end(file)
  } catch (error) { if (!res.headersSent) res.writeHead(500); res.end(String(error)) }
})
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH || 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
try {
  const page = await browser.newPage({ viewport: { width: 1500, height: 1100 } })
  page.setDefaultTimeout(10000)
  await page.addInitScript(() => {
    window.fixtureBrowserErrors = []
    window.addEventListener('error', event => window.fixtureBrowserErrors.push({ message: event.message, resource: event.target?.src || event.target?.href, file: event.filename }), true)
    window.addEventListener('unhandledrejection', event => window.fixtureBrowserErrors.push({ rejection: String(event.reason) }))
  })
  page.on('pageerror', error => errors.push(error.message))
  page.on('console', message => { if (message.type() === 'error') errors.push(message.text()) })
  let confirms = 0
  page.on('dialog', async dialog => { confirms++; await dialog.accept() })
  await page.goto(`http://127.0.0.1:${server.address().port}/operations/live/devices`)
  const card = page.locator('.device-console-card[data-device-id="7"]')
  const picker = card.locator('#device-room-7'), action = card.locator('.device-console-room-action')
  await picker.waitFor()
  assert.equal(await picker.inputValue(), '10')
  assert.equal(await action.textContent(), '解除绑定')
  assert.equal(await picker.locator('option').count(), 5, 'all four own rooms, including offline rooms')
  assert.ok(!(await picker.textContent()).includes('其他用户'), 'other tenant room excluded')
  assert.equal(await card.locator('select[aria-label="设备角色"]').count(), 1, 'manual role selection preserved')
  await card.screenshot({ path: resolve(screenshots, 'device-room-desktop.png') })

  const mutations = () => calls.filter(c => c.method !== 'GET' && /\/live\/devices\/\d+\/bind$/.test(c.path))
  await picker.selectOption('30')
  assert.equal(await action.textContent(), '绑定')
  assert.equal(mutations().length, 0, 'selection alone must not bind')
  assert.equal(await card.locator('#device-role-7').isEnabled(), false, 'role changes wait until selected room is bound')
  await action.click()
  await page.waitForFunction(() => document.querySelector('#device-room-7')?.value === '30' && document.querySelector('.device-console-room-action')?.textContent === '解除绑定')
  assert.equal(devices[0].binding_role, 'primary')
  assert.deepEqual(mutations()[0].body, { room_id: 30, binding_role: 'primary' })

  await picker.selectOption('20')
  assert.equal(await action.textContent(), '切换绑定')
  await action.click()
  await page.waitForFunction(() => document.querySelector('#device-role-7')?.value === 'listener' && document.querySelector('.device-console-room-action')?.textContent === '解除绑定' && !document.querySelector('#device-role-7')?.disabled)
  assert.equal(devices[0].binding_role, 'listener')
  assert.equal(devices[1].binding_role, 'primary', 'joining occupied room retains its primary')
  assert.deepEqual(mutations()[1].body, { room_id: 20, binding_role: 'listener' })

  await card.locator('#device-role-7').selectOption('primary')
  await card.getByRole('button', { name: '保存角色', exact: true }).click()
  await page.waitForFunction(() => document.querySelector('#device-role-7')?.value === 'primary' && document.querySelector('#device-role-8')?.value === 'listener' && !document.querySelector('#device-role-7')?.disabled)
  assert.equal(devices[0].binding_role, 'primary')
  assert.equal(devices[1].binding_role, 'listener', 'explicit saved promotion remains supported')
  assert.equal(confirms, 1, 'existing promotion confirmation retained')

  await action.click()
  await page.waitForFunction(() => document.querySelector('#device-room-7')?.value === '')
  assert.equal(mutations().at(-1).method, 'DELETE', 'current selected room uses unbind endpoint')
  assert.equal(confirms, 1, 'single-click unbind needs no second dialog')
  assert.equal(await action.isEnabled(), false, 'empty room selection cannot mutate')
  await picker.selectOption('40')
  assert.equal(await action.textContent(), '切换绑定', 'occupied listener-only room still shows switch binding')
  await action.click()
  await page.waitForFunction(() => document.querySelector('#device-room-7')?.value === '40' && document.querySelector('#device-role-7')?.value === 'primary' && document.querySelector('.device-console-room-action')?.textContent === '解除绑定' && !document.querySelector('#device-role-7')?.disabled)
  assert.equal(devices[0].binding_role, 'primary', 'room with no primary automatically gets a primary')

  await page.setViewportSize({ width: 430, height: 1050 })
  const assistantClose = page.locator('.system-agent-drawer header button.icon-button')
  if (await assistantClose.isVisible()) await assistantClose.click()
  await card.screenshot({ path: resolve(screenshots, 'device-room-mobile.png') })
  const geometry = await card.evaluate(el => ({ right: el.getBoundingClientRect().right, action: el.querySelector('.device-console-room-action').getBoundingClientRect().toJSON(), picker: el.querySelector('.device-console-room-picker select').getBoundingClientRect().toJSON(), overflowing: [...el.querySelectorAll('*')].filter(child => child.getBoundingClientRect().right > el.getBoundingClientRect().right + 1).map(child => child.className) }))
  assert.deepEqual(geometry.overflowing, [], 'visible mobile controls stay inside card; decorative pseudo-element may extend behind its clipped background')
  assert.ok(geometry.action.x >= geometry.picker.right - 1, 'binding button stays to the right of picker')
  assert.ok(Math.abs(geometry.action.bottom - geometry.picker.bottom) < 2, 'binding action aligns with the selected room')
  assert.deepEqual(errors, [])
  console.log('PASS: published browser bundle; three action states, own-room filtering, single-click binding/unbinding, primary/listener defaults, retained manual promotion, desktop/mobile geometry; all mutations stayed in local fixture.')
} catch (error) {
  for (const page of browser.contexts().flatMap(context => context.pages())) { console.error('Fixture alerts:', await page.locator('[role="alertdialog"]').allTextContents()); console.error('Fixture browser events:', await page.evaluate(() => window.fixtureBrowserErrors)) }
  console.error('Fixture page errors:', errors)
  throw error
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)) }
