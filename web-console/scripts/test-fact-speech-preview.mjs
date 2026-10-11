import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const fixture = `<html><head><meta charset="utf-8"></head><body><div id="app"></div><script type="module">
import {createApp,ref,h} from '/node_modules/.vite/deps/vue.js';import Dialog from '/src/components/FactSpeechPreviewDialog.vue';
createApp({setup(){const open=ref(false),fact={id:5,version_no:2,key:'退货条件',value:'包装未破损时可申请7天无理由退货',forbidden_wording:'无条件退货'};return ()=>h('div',[h('button',{onClick:()=>open.value=true},'话术预览'),open.value?h(Dialog,{fact,planId:4,roomId:26,tenantId:14,onClose:()=>open.value=false}):null])}}).mount('#app');</script></body></html>`
const server = await createServer({server:{host:'127.0.0.1',port:0},appType:'custom'})
server.middlewares.use((req,res,next)=>{if(req.url==='/__preview'){res.setHeader('Content-Type','text/html; charset=utf-8');res.end(fixture)}else next()})
await server.listen()
const browser = await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const page = await browser.newPage({viewport:{width:1280,height:900}}), errors=[], requests=[]
page.on('pageerror',error=>errors.push(error.message))
let mode='pass', pendingResolve
await page.route('**/api/v1/**',async route=>{
  const r=route.request();requests.push({url:r.url(),method:r.method(),body:r.postDataJSON()})
  assert.match(r.url(),/live-agent-plans\/4\/facts\/5\/speech-preview$/)
  if(mode==='pending') await new Promise(resolve=>pendingResolve=resolve)
  if(mode==='error'){await route.fulfill({status:502,contentType:'application/json',body:JSON.stringify({error:'话术模型连接失败，请重试'})});return}
  await route.fulfill({contentType:'application/json',body:JSON.stringify({fact_id:5,version_no:2,text:'咱们这个退货有条件，一次性包装未破损时，可以申请7天无理由退货。',approved:mode!=='warning',issues:mode==='warning'?[{kind:'fact_boundary',quote:'无条件退货',reason:'未保留包装条件'}]:[],review_suggestions:mode==='advice'?[{quote:'可以申请7天无理由退货',reason:'可自行核对具体申请条件'}]:[],model_alias:'真实口播模型',model_name:'test-model',room_name:'测试直播间',notice:'仅预览文字'})})
})
try {
  await page.goto(server.resolvedUrls.local[0]+'__preview')
  const opener=page.getByRole('button',{name:'话术预览',exact:true})
  await opener.click()
  const dialog=page.getByRole('dialog',{name:'话术预览',exact:true})
  await dialog.locator('.speech').waitFor()
  assert.equal(requests.length,1)
  assert.deepEqual(requests[0].body,{room_id:26,tenant_id:14,version_no:2})
  assert.match(await dialog.textContent(),/真实口播模型/)
  assert.match(await dialog.locator('.speech').textContent(),/包装未破损/)
  await page.keyboard.press('Escape');await dialog.waitFor({state:'detached'})
  assert.equal(await opener.evaluate(el=>el===document.activeElement),true)
  mode='error';await opener.click();await dialog.getByRole('alert').waitFor()
  assert.match(await dialog.getByRole('alert').textContent(),/连接失败/)
  mode='warning';await dialog.getByRole('button',{name:'重新生成',exact:true}).click()
  await dialog.locator('.issues').waitFor();assert.match(await dialog.locator('.issues').textContent(),/未保留包装条件/)
  mode='advice';await dialog.getByRole('button',{name:'重新生成',exact:true}).click();await dialog.getByRole('complementary',{name:'后台审核意见'}).waitFor()
  assert.match(await dialog.locator('.review-suggestions').textContent(),/已放行.*供你判断/s);assert.equal(await dialog.locator('.issues').count(),0);assert.equal(await dialog.locator('.warning').count(),0)
  await dialog.getByRole('button',{name:'关闭话术预览'}).click()
  mode='pending';await opener.click();await dialog.getByRole('status').waitFor()
  await page.waitForFunction(()=>document.querySelector('dialog')?.getAttribute('aria-busy')==='true')
  await dialog.getByRole('button',{name:'关闭话术预览'}).click();await dialog.waitFor({state:'detached'})
  pendingResolve?.();await page.waitForTimeout(150)
  assert.equal(await dialog.count(),0,'late response must not reopen the modal')
  await page.setViewportSize({width:390,height:844});mode='pass';await opener.click();await dialog.locator('.speech').waitFor()
  assert.ok(await dialog.evaluate(el=>el.scrollWidth<=el.clientWidth+1),'mobile dialog must fit')
  assert.equal(requests.every(r=>r.method==='POST'),true)
  assert.deepEqual(errors,[])
  console.log('Fact speech preview: current scope, real endpoint, result, error retry, review warning, Escape, abort, focus restore and mobile layout passed')
} finally {pendingResolve?.();await browser.close();await server.close()}
