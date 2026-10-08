import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
import {startFixture} from './room-parity-fixture.mjs'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url)),{chromium}=require('playwright-core')
const server=await startFixture(),browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try {
 const page=await browser.newPage({viewport:{width:1680,height:1000}}),errors=[]
 page.on('pageerror',e=>errors.push(e.message))
 await page.goto('http://127.0.0.1:18601/__room-parity.html?role=customer&start_test=1&pcm=1')
 await page.getByRole('button',{name:'开始',exact:true}).click()
 await page.locator('.speech-runtime-initializing').waitFor()
 await page.evaluate(()=>window.fixture.failure={session_id:1,code:'plan_sources_changed',message:'事实或轮盘已变化，请保存并发布新版本',occurred_at:new Date().toISOString()})
 const dialog=page.getByRole('alertdialog')
 await dialog.waitFor()
 assert.match(await dialog.innerText(),/保存并发布/)
 assert.equal(await page.locator('.speech-runtime-initializing').count(),0,'failure must stop waiting')
 await dialog.getByRole('button',{name:'知道了'}).click()
 await page.waitForTimeout(5500)
 assert.equal(await dialog.count(),0,'same failed session must not repeatedly reopen')
 await page.evaluate(()=>{window.fixture.session=2;window.fixture.failure={session_id:2,code:'formal_mainline_failed',message:'本场声音生成失败，请重试',occurred_at:new Date().toISOString()}})
 await dialog.waitFor()
 assert.match(await dialog.innerText(),/本场声音生成失败/)
 await dialog.getByRole('button',{name:'知道了'}).click()
 await page.evaluate(async()=>{
  const api=await import('/src/api.ts');const original=window.fetch;
  window.fetch=async(url,init)=>String(url).includes('/fixture-save')?Response.json({error:'保存失败，未生成或发布新版本'},{status:409}):original(url,init);
  await api.request('/fixture-save',{method:'POST'}).catch(()=>{});
 })
 await dialog.waitFor()
 assert.match(await dialog.innerText(),/未生成或发布/)
 assert.deepEqual(errors,[])
 console.log('PASS: failed warmup stops spinner; current-session dialog; poll dedupe; new failure; HTTP 409 dialog')
} finally {await browser.close();await server.close()}
