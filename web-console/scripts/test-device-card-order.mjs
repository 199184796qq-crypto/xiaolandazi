// Actual view, native drag events and browser storage; no real device mutations.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'
import { mkdir, writeFile } from 'node:fs/promises'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const root = fileURLToPath(new URL('..', import.meta.url))
const output = process.env.DEVICE_ORDER_OUTPUT || '../work/device-card-order'
await mkdir(output, { recursive: true })
const plugin = { name: 'device-card-order-fixture', configureServer(server) {
  server.middlewares.use(async (req, res, next) => {
    if (!req.url?.startsWith('/__device-order')) return next()
    res.setHeader('Content-Type', 'text/html;charset=utf-8')
    res.end(await server.transformIndexHtml('/__device-order', `<!doctype html><html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><body><div id="app"></div><script type="module">
      import {createApp,h} from 'vue'; import {createRouter,createMemoryHistory} from 'vue-router';
      import Device from '/src/views/DeviceBindingView.vue'; import {session} from '/src/session'; import '/src/style.css';
      const params=new URLSearchParams(location.search),count=Number(params.get('count')||3),user=Number(params.get('user')||28),tenant=Number(params.get('tenant')||14);
      session.bootstrap={actor:{role:'customer',tenant_id:tenant,user_id:user}};
      const ids=count===3?[9,8,7]:Array.from({length:count},(_,i)=>count-i);
      window.fixture={reads:0,writes:[],reverse:false,devices:ids.map(id=>({id,tenant_id:tenant,device_name:id===8?'备用设备8':'小蓝搭子'+id,sku_code:'xldzplus',connection_status:'offline',display_status:'离线',room_id:23,binding_role:id===9?'primary':'listener'}))};
      const interval=window.setInterval.bind(window);window.setInterval=(fn,ms,...rest)=>interval(fn,ms===10000?500:ms,...rest);
      window.fetch=async(url,init={})=>{
        const path=new URL(url,location.origin).pathname;
        if(init.method&&init.method!=='GET'){window.fixture.writes.push(path);return Response.json({error:'Unexpected device mutation'},{status:400});}
        if(path.endsWith('/live/devices')){window.fixture.reads++;return Response.json(window.fixture.reverse?[...window.fixture.devices].reverse():window.fixture.devices);}
        if(path.endsWith('/rooms'))return Response.json({items:[{id:23,tenant_id:tenant,name:'绸都杨鸭子',status:'offline',external_room_id:'fixture'}]});
        if(path.endsWith('/addressing'))return Response.json({mode:'auto'});
        return Response.json({});
      };
      const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:{render:()=>null}},{path:'/rooms/:id',name:'room-detail',component:{render:()=>null}}]});
      createApp({render:()=>h(Device)}).use(router).mount('#app');
    </script></body></html>`))
  })
} }
const server = await createServer({ root, configFile: false, plugins: [vue(), plugin], server: { host: '127.0.0.1', port: 0 }, logLevel: 'error' })
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const context = await browser.newContext({ viewport: { width: 1650, height: 1050 } })
const page = await context.newPage(), errors = []
page.on('pageerror', e => errors.push(e.message))
const url = server.resolvedUrls.local[0] + '__device-order'
const cards = p => p.locator('.device-console-card')
const ids = p => cards(p).evaluateAll(items => items.map(el => Number(el.dataset.deviceId)))
async function expectOrder(p, expected) {
  await p.waitForFunction(expected => JSON.stringify([...document.querySelectorAll('.device-console-card')].map(el => Number(el.dataset.deviceId))) === JSON.stringify(expected), expected)
}
async function visit(suffix = '') { await page.goto(url + suffix); await cards(page).first().waitFor() }
try {
  await visit(); assert.deepEqual(await ids(page), [9, 8, 7])
  await page.locator('#device-sort-7').dragTo(page.locator('[data-device-id="9"] .device-card-order-controls'))
  await expectOrder(page, [7, 9, 8])
  assert.deepEqual(await page.evaluate(() => JSON.parse(localStorage.getItem('xiaolan-ui:28:14:device-card-order'))), [7, 9, 8])
  await page.evaluate(() => { window.fixture.reverse = true; window.fixture.devices.find(d => d.id === 7).connection_status = 'online' })
  await page.locator('[data-device-id="7"] .device-console-status.online').waitFor()
  assert.deepEqual(await ids(page), [7, 9, 8], 'status polling overwrote manual order')
  await page.reload(); await expectOrder(page, [7, 9, 8])
  const search = page.getByPlaceholder('搜索设备名称 / 型号 / 状态')
  await search.fill('小蓝'); await expectOrder(page, [7, 9])
  await page.locator('#device-sort-9').dragTo(page.locator('[data-device-id="7"] .device-card-order-controls'))
  await expectOrder(page, [9, 7]); await search.fill(''); await expectOrder(page, [9, 7, 8])
  await page.locator('#device-sort-9').press('ArrowRight'); await expectOrder(page, [7, 9, 8])
  await page.getByRole('button', { name: '前移设备 8', exact: true }).click(); await expectOrder(page, [7, 8, 9])
  await page.screenshot({ path: output + '/桌面排序.png', fullPage: true })
  await page.evaluate(() => {
    window.fixture.devices.unshift({ ...window.fixture.devices[0], id: 10, device_name: '新设备10' })
    window.fixture.devices = window.fixture.devices.filter(d => d.id !== 8)
  })
  await expectOrder(page, [7, 9, 10])
  await page.reload(); await expectOrder(page, [7, 8, 9])
  await page.setViewportSize({ width: 430, height: 1000 })
  await page.getByRole('button', { name: '后移设备 7', exact: true }).click(); await expectOrder(page, [8, 7, 9])
  await page.locator('[data-device-id="8"]').screenshot({ path: output + '/手机排序.png' })
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, 'mobile card overflowed')
  assert.deepEqual(await page.evaluate(() => window.fixture.writes), [])
  await page.setViewportSize({ width: 1650, height: 1050 })
  await visit('?user=29'); await expectOrder(page, [9, 8, 7])
  await visit('?tenant=15'); await expectOrder(page, [9, 8, 7])
  await visit(); await expectOrder(page, [8, 7, 9])
  const other = await context.newPage(); other.on('pageerror', e => errors.push(e.message))
  await other.goto(url); await expectOrder(other, [8, 7, 9])
  await other.getByRole('button', { name: '前移设备 7', exact: true }).click()
  await expectOrder(other, [7, 8, 9]); await expectOrder(page, [7, 8, 9])
  await other.close()
  await visit('?user=30&count=12'); await expectOrder(page, [12, 11, 10, 9, 8, 7, 6, 5, 4])
  await page.getByRole('button', { name: '后移设备 4', exact: true }).click(); await expectOrder(page, [4, 2, 1])
  await page.getByRole('button', { name: '前移设备 4', exact: true }).click(); await expectOrder(page, [12, 11, 10, 9, 8, 7, 6, 5, 4])
  assert.deepEqual(await page.evaluate(() => window.fixture.writes), [])
  assert.deepEqual(errors, [])
  const report = { passed: true, native_drag: true, reload_persistence: true, polling_keeps_order: true, search_keeps_hidden_devices: true, keyboard_and_mobile_buttons: true, new_devices_appended: true, removed_devices_ignored: true, account_and_tenant_isolation: true, same_browser_tab_sync: true, pagination_boundary_move: true, device_mutations: 0, page_errors: errors }
  await writeFile(output + '/验证.json', JSON.stringify(report, null, 2))
  console.log(JSON.stringify(report))
} finally { await browser.close(); await server.close() }
