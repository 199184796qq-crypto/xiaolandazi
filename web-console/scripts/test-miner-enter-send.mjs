// Actual composer with isolated mocked API; no production rooms or model calls.
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const fixture = `<html><head><meta charset="utf-8"></head><body><div id="app"></div><script type="module">
import{createApp,h}from'/node_modules/.vite/deps/vue.js';import Miner from'/src/components/SpeechMinerWorkbench.vue';
createApp({render:()=>h(Miner,{planId:4,tenantId:14})}).mount('#app');</script></body></html>`
const server = await createServer({server:{host:'127.0.0.1',port:0},appType:'custom'})
server.middlewares.use((req,res,next)=>{if(req.url==='/__enter_send'){res.setHeader('Content-Type','text/html; charset=utf-8');res.end(fixture)}else next()})
await server.listen()
const browser = await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const page = await browser.newPage({viewport:{width:1440,height:1000}})
const calls=[],errors=[]
let holdNext=false, held
page.on('pageerror',error=>errors.push(error.message))
const reply={reply:'已收到真实信息，请继续补充。',selection_mode:'single',options:[],captured_facts:[],completed:false}
await page.route('**/api/v1/**',async route=>{
  if(route.request().url().endsWith('/speech-miner/guide')){
    calls.push(route.request().postDataJSON())
    if(holdNext){holdNext=false;held=route;return}
    await route.fulfill({json:reply});return
  }
  await route.fulfill({json:{}})
})
try{
  await page.goto(server.resolvedUrls.local[0]+'__enter_send')
  const input=page.locator('.speech-miner-composer textarea'),toggle=page.getByRole('checkbox',{name:'回车即发送'}),send=page.getByRole('button',{name:'发送',exact:true})
  await input.waitFor()
  assert.equal(await toggle.isChecked(),false)
  await input.fill('未勾选回车换行');await input.press('End');await input.press('Enter')
  assert.equal(await input.inputValue(),'未勾选回车换行\n');assert.equal(calls.length,0)
  await toggle.check();await input.fill('已勾选回车发送')
  const request=page.waitForRequest('**/speech-miner/guide');await input.press('Enter');await request
  await send.waitFor({state:'visible'});await page.waitForFunction(()=>!document.querySelector('.speech-miner-composer textarea').disabled)
  assert.equal(calls.length,1);assert.equal(calls[0].message,'已勾选回车发送');assert.equal(await input.inputValue(),'')
  await input.fill('勾选后仍可换行');await input.press('End');await input.press('Shift+Enter')
  assert.equal(await input.inputValue(),'勾选后仍可换行\n');assert.equal(calls.length,1)
  await input.fill('中文选词不误发送')
  await input.evaluate(el=>{
    el.dispatchEvent(new CompositionEvent('compositionstart',{bubbles:true}))
    el.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',isComposing:true,bubbles:true,cancelable:true}))
    el.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true,cancelable:true}))
    el.dispatchEvent(new CompositionEvent('compositionend',{bubbles:true,data:'中文'}))
    el.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',keyCode:229,bubbles:true,cancelable:true}))
  })
  assert.equal(calls.length,1);assert.equal(await input.inputValue(),'中文选词不误发送')
  const second=page.waitForRequest('**/speech-miner/guide');await input.press('Enter');await second
  await page.waitForFunction(()=>!document.querySelector('.speech-miner-composer textarea').disabled)
  assert.equal(calls.length,2);assert.equal(calls[1].message,'中文选词不误发送')
  await input.fill('   ');await input.press('Enter');assert.equal(calls.length,2)
  await input.fill('长按不重复发送')
  const prevented=await input.evaluate(el=>!el.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',repeat:true,bubbles:true,cancelable:true})))
  assert.equal(prevented,true);assert.equal(calls.length,2)
  holdNext=true
  const third=page.waitForRequest('**/speech-miner/guide');await input.press('Enter');await third
  await page.waitForFunction(()=>document.querySelector('.speech-miner-composer textarea').disabled)
  await input.evaluate(el=>el.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true,cancelable:true})))
  assert.equal(calls.length,3)
  await held.fulfill({json:reply})
  await page.waitForFunction(()=>!document.querySelector('.speech-miner-composer textarea').disabled)
  await toggle.uncheck();await input.fill('恢复换行');await input.press('End');await input.press('Enter')
  assert.equal(await input.inputValue(),'恢复换行\n');assert.equal(calls.length,3)
  const buttonRequest=page.waitForRequest('**/speech-miner/guide');await send.click();await buttonRequest
  assert.equal(calls.length,4);assert.equal(calls[3].message,'恢复换行')
  assert.deepEqual(errors,[])
  console.log('PASS: default newline; checked Enter sends; Shift+Enter newline; unchecked restored; IME guarded; blank/repeat/busy cannot resend; send button unchanged')
}finally{await browser.close();await server.close()}
