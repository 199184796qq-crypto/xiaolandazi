// Real ShopView and checkout dialog; all financial APIs use synthetic records.
import assert from 'node:assert/strict'
import { mkdir } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const fixture = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><link rel="icon" href="data:,"><div id="app-global-breadcrumbs"></div><div id="app-global-switches"></div><div id="app"></div><script type="module">
import{createApp,h}from'vue';import{createRouter,createMemoryHistory}from'vue-router';import ShopView from'/src/views/ShopView.vue';import{applySession}from'/src/session.ts';import'/src/style.css';
const router=createRouter({history:createMemoryHistory(),routes:[{path:'/:pathMatch(.*)*',component:ShopView}]});
applySession({actor:{user_id:14,tenant_id:14,role:'customer'},tenants:[],environment:'test'});
await router.push('/shop');await router.isReady();createApp({render:()=>h(ShopView)}).use(router).mount('#app');</script></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom' })
server.middlewares.use(async (req, res, next) => {
  if (req.url !== '/__wallet_dialog') return next()
  res.setHeader('Content-Type', 'text/html; charset=utf-8')
  res.end(await server.transformIndexHtml(req.url, fixture))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const page = await browser.newPage({ viewport: { width: 1540, height: 960 } }), errors = [], posts = []
page.setDefaultTimeout(8000); page.on('pageerror', error => errors.push(error.message))
const makeOrder = (id, price, type = 'time_card') => ({ id, order_no: 'ORD-17916199-' + id + '-20261010', tenant_id: 14, order_type: type, status: 'pending', payment_status: 'pending', fulfillment_status: 'pending', currency: 'CNY', list_amount_cents: 1800, discount_amount_cents: 1800 - price, payable_amount_cents: price, paid_amount_cents: 0, refunded_amount_cents: 0, refundable_amount_cents: 0, refundable_seconds: 0, created_at: '2026-10-10T09:00:00Z', updated_at: '2026-10-10T09:00:00Z', items: [{ id, order_id: id, product_name: type === 'device' ? '测试设备' : price === 0 ? '10 小时体验卡' : '30 小时时长卡', quantity: 1, duration_seconds: price === 0 ? 36000 : 108000, validity_days: 30, product_type: type, product_id: id, product_version_id: 1, unit_list_price_cents: 1800, unit_paid_price_cents: price, discount_bps: 10000 }], payments: [], devices: [], shipments: [] })
const orders = [makeOrder(1, 0), makeOrder(2, 1800), makeOrder(3, 1800, 'device')]
await page.route('**/api/v1/**', async route => {
  const request = route.request(), url = new URL(request.url())
  if (request.method() === 'POST') { posts.push({ route, path: url.pathname, body: request.postDataJSON() }); return }
  if (url.pathname === '/api/v1/shop/orders') return route.fulfill({ json: { items: orders } })
  if (url.pathname === '/api/v1/account') return route.fulfill({ json: { profile: { user_id: 14, tenant_id: 14 } } })
  await route.fulfill({ json: { items: [], total: 0 } })
})
const row = id => page.locator('.shop-orders-table tbody tr').filter({ hasText: orders.find(order => order.id === id).order_no })
const dialog = page.getByRole('dialog', { name: '钱包余额支付', exact: true })
const freeConfirm = () => dialog.getByRole('button', { name: '确认免费领取', exact: true })
const out = new URL('../../deploy/validation/wallet-payment/', import.meta.url)
async function open(id) { await row(id).getByRole('button', { name: '支付', exact: true }).click(); await dialog.waitFor() }
async function waitPost(count) { await page.waitForFunction(() => document.querySelector('.wallet-payment-dialog')?.getAttribute('aria-busy') === 'true'); assert.equal(posts.length, count) }
async function succeed(index, id) {
  const order = orders.find(item => item.id === id); order.payment_status = 'paid'; order.status = 'paid'; order.fulfillment_status = 'fulfilled'; order.paid_amount_cents = order.payable_amount_cents
  await posts[index].route.fulfill({ json: { order, payment_method: order.payable_amount_cents ? 'wallet' : 'free' } })
  await page.getByRole('heading', { name: '订单详情', exact: true }).waitFor()
  assert.equal(await dialog.count(), 0)
  await page.locator('.shop-payment-panel').getByRole('button', { name: '关闭', exact: true }).click()
}
try {
  await page.goto(server.resolvedUrls.local[0] + '__wallet_dialog'); await row(1).waitFor()
  await open(1)
  assert.equal(await dialog.locator('.wallet-info-card').count(), 4)
  assert.equal(await dialog.locator('.is-product strong').textContent(), '10 小时体验卡')
  assert.equal(await dialog.locator('.is-duration strong').textContent(), '10 小时')
  assert.equal(await dialog.locator('.is-amount strong').textContent(), '¥0.00')
  assert.match(await dialog.locator('#wallet-payment-description').textContent(), /无需扣除钱包余额/)
  assert.equal(await dialog.locator('.is-order strong').getAttribute('title'), orders[0].order_no)
  assert.ok(await dialog.getByRole('button', { name: '取消', exact: true }).evaluate(el => el === document.activeElement))
  const cancel = await dialog.getByRole('button', { name: '取消', exact: true }).boundingBox(), confirm = await freeConfirm().boundingBox()
  assert.ok(confirm.x > cancel.x && confirm.x > 900, 'fixed footer actions remain aligned right on desktop')
  await mkdir(out, { recursive: true }); await page.screenshot({ path: fileURLToPath(new URL('desktop-free.png', out)) })
  await freeConfirm().focus(); await page.keyboard.press('Tab')
  assert.ok(await dialog.getByRole('button', { name: '关闭钱包余额支付', exact: true }).evaluate(el => el === document.activeElement), 'keyboard focus stays inside the modal')
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  assert.equal(await dialog.count(), 0); assert.equal(posts.length, 0)
  assert.ok(await row(1).getByRole('button', { name: '支付', exact: true }).evaluate(el => el === document.activeElement))
  assert.equal(await page.evaluate(() => document.body.style.overflow), '')
  await open(1); await page.keyboard.press('Escape'); assert.equal(await dialog.count(), 0); assert.equal(posts.length, 0)
  await open(1); await page.setViewportSize({ width: 390, height: 850 })
  assert.equal(await dialog.evaluate(el => el.scrollWidth > el.clientWidth), false)
  const mobileBounds = await dialog.boundingBox(), mobileConfirm = await freeConfirm().boundingBox()
  assert.ok(mobileBounds.x >= 0 && mobileBounds.x + mobileBounds.width <= 390)
  assert.ok(mobileConfirm.y + mobileConfirm.height <= 850, 'mobile confirmation stays accessible')
  await page.screenshot({ path: fileURLToPath(new URL('mobile-free.png', out)) })
  await freeConfirm().click(); await waitPost(1)
  assert.equal(posts[0].path, '/api/v1/shop/orders/1/free-claim'); assert.deepEqual(posts[0].body, {})
  await page.evaluate(() => { const button = document.querySelector('.wallet-payment-confirm'); button.dispatchEvent(new MouseEvent('click', { bubbles: true })); button.dispatchEvent(new MouseEvent('click', { bubbles: true })) })
  assert.equal(posts.length, 1, 'repeated confirmation cannot duplicate a financial request')
  assert.ok(await dialog.getByRole('button', { name: '取消', exact: true }).isDisabled())
  await page.keyboard.press('Escape'); assert.ok(await dialog.isVisible(), 'pending claim cannot be dismissed')
  await posts[0].route.fulfill({ status: 409, json: { error: '测试领取失败，请重试' } })
  await dialog.getByRole('alert').filter({ hasText: '测试领取失败，请重试' }).waitFor()
  assert.ok(await freeConfirm().isEnabled()); assert.equal(await dialog.locator('.is-amount strong').textContent(), '¥0.00')
  await freeConfirm().click(); await waitPost(2); await succeed(1, 1)
  await page.getByText('领取成功，权益已发放。', { exact: true }).waitFor()
  await page.setViewportSize({ width: 1540, height: 960 }); await open(2)
  assert.equal(await dialog.locator('.is-amount strong').textContent(), '¥18.00')
  assert.equal(await freeConfirm().count(), 0)
  await dialog.getByText('时长卡领取后手动启用', { exact: true }).waitFor()
  await page.screenshot({ path: fileURLToPath(new URL('desktop-paid.png', out)) })
  await dialog.getByRole('button', { name: '确认支付 ¥18.00', exact: true }).click(); await waitPost(3)
  assert.equal(posts[2].path, '/api/v1/shop/orders/2/wallet-pay'); assert.match(posts[2].body.idempotency_key, /^wallet-time-card-/)
  await succeed(2, 2)
  await page.getByText(/钱包支付成功，30 小时时长卡/).waitFor()
  await row(3).getByRole('button', { name: '支付', exact: true }).click()
  await page.getByRole('heading', { name: '模拟支付', exact: true }).waitFor()
  assert.equal(await dialog.count(), 0, 'paid device sandbox is preserved')
  assert.deepEqual(errors, [])
  console.log('PASS: real checkout, free/paid accurate details, four cards, desktop/mobile layout, focus and dismissal, claim error/retry, single financial request, correct free-claim/wallet-pay APIs, paid device sandbox preserved; synthetic APIs only')
} finally { await browser.close(); await server.close() }
