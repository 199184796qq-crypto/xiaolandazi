import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
import {mkdir} from 'node:fs/promises'
import {startFixture} from './room-parity-fixture.mjs'
const require=createRequire(process.env.DEVICE_TEST_RUNTIME||new URL('../../collector-worker/package.json',import.meta.url))
const {chromium}=require('playwright-core')
const base={tenant_id:14,room_id:15,lifecycle_status:'ACTIVE',connection_status:'online',work_status:'idle',display_status:'连续待机',stop_reason:''}
const server=await startFixture({roomDevices:[{...base,id:7,sn:'xidz0001',device_name:'小蓝主机',binding_role:'primary'},{...base,id:8,sn:'xidz0002',device_name:'监听设备',binding_role:'listener'},{...base,id:9,sn:'xidz0003',device_name:'备用设备',room_id:undefined,binding_role:''}]})
const browser=await chromium.launch({executablePath:process.env.CHROME_PATH||'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try{
 const page=await browser.newPage({viewport:{width:1680,height:1000}}),errors=[];page.on('pageerror',e=>errors.push(e.message));await page.goto('http://127.0.0.1:18601/__room-parity.html?role=customer')
 const card=page.locator('.xiaozhi-device-card');await card.locator('.room-device-binding-row.bound').first().waitFor();assert.equal(await card.locator('.room-device-binding-row').count(),1,'even multiple bound devices start with one selector');assert.match(await card.locator('.device-runtime-status').innerText(),/连续待机/)
 await card.getByRole('button',{name:'增加设备选择行',exact:true}).click();await card.locator('.room-device-binding-row.bound').nth(1).waitFor();await card.getByRole('button',{name:'增加设备选择行',exact:true}).click();assert.equal(await card.locator('.room-device-binding-row').count(),3)
 await mkdir('../deploy/validation/room-device-bindings',{recursive:true});await card.screenshot({path:'../deploy/validation/room-device-bindings/full-room-desktop.png'})
 for(const width of [1680,1100,390]){
  await page.setViewportSize({width,height:1000});const over=await card.evaluate(el=>[...el.querySelectorAll('select,button')].filter(c=>c.getBoundingClientRect().right>el.getBoundingClientRect().right+1||c.getBoundingClientRect().left<el.getBoundingClientRect().left-1).map(c=>c.outerHTML));assert.deepEqual(over,[],width+' controls fit in actual room card')
 }
 await card.screenshot({path:'../deploy/validation/room-device-bindings/full-room-mobile.png'});assert.deepEqual(errors,[]);console.log('PASS: actual RoomDetailView starts with one row; manual plus reveals bindings; +/- and binding controls fit desktop/tablet/mobile.')
}finally{await browser.close();await server.close()}
