import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url))
const {chromium}=require('playwright-core')
const settings=await readFile(new URL('../src/views/SystemSettingsView.vue',import.meta.url),'utf8')
assert.match(settings,/v-model="settingsDraft\.browser_title"/)
assert.match(settings,/const globalDisplaySettingKeys[\s\S]*?'browser_title'/)
assert.match(settings,/key: 'browser_title', label: '浏览器标签页标题', max: 100/)
const fixture='<html><head><title>初始标题</title></head><body><script type="module">import {installBrowserTitle} from "/src/browserTitle.ts";window.stopTitle=installBrowserTitle();</script></body></html>'
const server=await createServer({server:{host:'127.0.0.1',port:0},appType:'custom'})
server.middlewares.use((req,res,next)=>{if(req.url==='/__title'){res.setHeader('Content-Type','text/html; charset=utf-8');res.end(fixture)}else next()})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const page=await browser.newPage(),errors=[]
let title='后台配置的标题',fail=false,hold=false,held=[],calls=0
page.on('pageerror',e=>errors.push(e.message))
await page.route('**/api/v1/system/public-config',async route=>{
  calls++;assert.equal(route.request().method(),'GET')
  if(hold){held.push(route);return}
  await route.fulfill(fail?{status:500,json:{error:'暂不可用'}}:{json:{browser_title:title}})
})
try{
  await page.goto(server.resolvedUrls.local[0]+'__title')
  await page.waitForFunction(()=>document.title==='后台配置的标题')
  title='保存后的新标题'
  await page.evaluate(()=>window.dispatchEvent(new CustomEvent('system-config-updated')))
  await page.waitForFunction(()=>document.title==='保存后的新标题')
  title='另一页面修改的标题'
  await page.evaluate(()=>window.dispatchEvent(new Event('focus')))
  await page.waitForFunction(()=>document.title==='另一页面修改的标题')
  fail=true
  const before=calls
  await page.evaluate(()=>window.dispatchEvent(new CustomEvent('system-config-updated')))
  await page.waitForFunction(()=>true)
  await page.waitForTimeout(100)
  assert.ok(calls>before);assert.equal(await page.title(),'另一页面修改的标题')
  fail=false;title=''
  await page.evaluate(()=>window.dispatchEvent(new CustomEvent('system-config-updated')))
  await page.waitForTimeout(100)
  assert.equal(await page.title(),'另一页面修改的标题')
  hold=true
  await page.evaluate(()=>window.dispatchEvent(new CustomEvent('system-config-updated')))
  await page.waitForTimeout(100)
  hold=false;title='最新保存的标题'
  await page.evaluate(()=>window.dispatchEvent(new CustomEvent('system-config-updated')))
  await page.waitForFunction(()=>document.title==='最新保存的标题')
  for(const route of held.splice(0))await route.fulfill({json:{browser_title:'旧请求标题'}})
  await page.waitForTimeout(100)
  assert.equal(await page.title(),'最新保存的标题','old response cannot overwrite new settings')
  title='<img src=x onerror="alert(1)">'
  await page.evaluate(()=>window.dispatchEvent(new CustomEvent('system-config-updated')))
  await page.waitForFunction(expected=>document.title===expected,title)
  assert.equal(await page.locator('img').count(),0,'title is plain text, not HTML')
  await page.evaluate(()=>window.stopTitle())
  title='停止后不更新'
  await page.evaluate(()=>window.dispatchEvent(new CustomEvent('system-config-updated')))
  await page.waitForTimeout(100)
  assert.notEqual(await page.title(),title)
  assert.deepEqual(errors,[])
  console.log('PASS: admin field/save key; public title before login; live saved/focus updates; failure/blank fallback; stale response guard; plain text; cleanup')
}finally{await browser.close();await server.close()}
