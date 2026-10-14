import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url))
const {chromium}=require('playwright-core')
let policy={capacity:3,one_rate:30,two_rate:60,full_rate:70,cooldown_seconds:180}, revision='fixture-v1', saved,conflict=false,writes=0
const server=await createServer({server:{host:'127.0.0.1',port:0,hmr:false},appType:'custom'})
server.middlewares.use((req,res,next)=>{if(req.url!='/__library')return next();res.setHeader('Content-Type','text/html; charset=utf-8');res.end(`<html><head><meta charset="utf-8"><style>body{margin:0;padding:16px;font-family:'Microsoft YaHei';background:#f5f7fc}*{box-sizing:border-box}</style></head><body><div id="app"></div><script type="module">import {createApp} from '/node_modules/.vite/deps/vue.js';import View from '/src/components/AnchorVoiceCacheSettingsPanel.vue';createApp(View).mount('#app')</script></body></html>`)})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const page=await browser.newPage({viewport:{width:1500,height:1100}}),errors=[]
page.on('pageerror',e=>errors.push(e.message))
await page.route('**/api/v1/**',async route=>{
 const req=route.request(),path=new URL(req.url()).pathname
 let body={},status=200
 if(path.endsWith('/speech-library-policy')){
  if(req.method()==='PUT'){writes++;saved=req.postDataJSON();if(conflict){status=409;body={error:'话术库设置已变化，请刷新后重新修改'}}else{policy=saved.policy;revision='fixture-v2'}}
  if(status===200)body={policy,revision,stats:{Texts:141,Audio:141,PlayedReuses:23}}
 }else if(path.endsWith('/anchor-voice-cache-policy'))body={retention_hours:5,default_hours:5,mainline_cache_reuse_enabled:true,interaction_cache_reuse_enabled:true}
 else if(path.endsWith('/user-voice-cache-policies'))body={items:[]}
 await route.fulfill({status,contentType:'application/json',body:JSON.stringify(body)})
})
try {
 await page.goto(server.resolvedUrls.local[0]+'__library')
 const section=page.locator('.library-settings');await section.getByText('当前有效文字',{exact:false}).waitFor()
 assert.equal(await page.getByLabel('主线话术与声音缓存复用',{exact:true}).isChecked(),true)
 assert.equal(await page.getByLabel('互动话术与声音缓存复用',{exact:true}).isChecked(),true)
 assert.equal(await section.getByLabel('已有一条时复用率（%）',{exact:true}).inputValue(),'30')
 await section.getByLabel('已有一条时复用率（%）',{exact:true}).fill('25')
 await section.getByRole('button',{name:'保存话术库设置',exact:true}).click()
 await section.getByRole('status').waitFor();assert.equal(saved.policy.one_rate,25);assert.equal(saved.expected_revision,'fixture-v1');assert.equal(writes,1)
 assert.equal(await page.getByLabel('临时声音保留时间（小时）',{exact:true}).inputValue(),'5')
 await page.screenshot({path:process.env.LIBRARY_SCREENSHOT_DIR+'/settings-desktop.png',fullPage:true})
 conflict=true;await section.getByLabel('已有一条时复用率（%）',{exact:true}).fill('26');await section.getByRole('button',{name:'保存话术库设置',exact:true}).click()
 await section.getByRole('alert').waitFor();assert.match(await section.getByRole('alert').innerText(),/已变化/);assert.equal(policy.one_rate,25)
 await section.getByLabel('已有一条时复用率（%）',{exact:true}).fill('99');await section.getByRole('button',{name:'保存话术库设置',exact:true}).click();assert.equal(writes,2)
 await page.setViewportSize({width:390,height:844});await section.getByRole('button',{name:'刷新话术库设置',exact:true}).click();await section.getByText('当前有效文字',{exact:false}).waitFor()
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'mobile page overflow')
 await page.screenshot({path:process.env.LIBRARY_SCREENSHOT_DIR+'/settings-mobile.png',fullPage:true})
 assert.deepEqual(errors,[])
 console.log('PASS: default gates preserved; policy load/save/CAS/error/integer validation; 1500px and 390px layouts; no real API or model calls')
}finally{await browser.close();await server.close()}
