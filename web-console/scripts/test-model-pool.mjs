import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
const require = createRequire(process.env.PLAYWRIGHT_CORE_PACKAGE || new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const profile = { id:'pool-fixture', name:'测试账号', model:'qwen3.7-flash', protocol:'openai_chat', base_url:'https://example.test/v1', enabled:true, timeout_ms:120000, max_tokens:6000, system_prompt:'', has_api_key:true }
let entries = [{ id:profile.id, account_name:'测试账号A', profile, max_rooms:2, enabled:true, revision:1, used_rooms:1, available_rooms:1, has_api_key:true }]
const leases = [{ lease_id:'lease-fixture', entry_id:profile.id, account_name:'测试账号A', model:profile.model, tenant_id:14, room_id:15, runtime_id:1, tenant_name:'测试客户', room_name:'测试直播间', ai_status:'paused', acquired_at:'2026-10-14T12:00:00Z' }]
let saved, failDelete = false
const server = await createServer({ server:{host:'127.0.0.1',port:0}, appType:'custom' })
server.middlewares.use((req,res,next) => {
  if(req.url !== '/__model_pool') return next()
  res.setHeader('Content-Type','text/html; charset=utf-8')
  res.end(`<html><head><meta charset="utf-8"><style>body{margin:0;background:#f5f7fc;font-family:'Microsoft YaHei',sans-serif}*{box-sizing:border-box}</style></head><body><div id="app"></div><script type="module">import {createApp} from '/node_modules/.vite/deps/vue.js';import {createRouter,createMemoryHistory} from '/node_modules/.vite/deps/vue-router.js';import View from '/src/views/ModelPoolSettingsView.vue';createApp(View).use(createRouter({history:createMemoryHistory(),routes:[]})).mount('#app')</script></body></html>`)
})
await server.listen()
const browser = await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const page = await browser.newPage({viewport:{width:1500,height:1000}}), errors=[]
page.on('pageerror',e=>errors.push(e.message))
await page.route('**/api/v1/system/model-pool**', async route => {
  const request = route.request(), url = new URL(request.url()), method=request.method()
  let response={entries,leases},status=200
  if(url.pathname.endsWith('/models')) response={models:[{id:'qwen3.7-flash'},{id:'qwen3.8-flash'}]}
  else if(url.pathname.endsWith('/import-current')) { const entry={...entries[0],id:'pool-import',account_name:'当前口播模型账号',profile:{...profile,id:'pool-import'},enabled:false,max_rooms:0,used_rooms:0,available_rooms:0}; entries.push(entry); response=entry }
  else if(method==='POST') { saved=request.postDataJSON(); const entry={...saved,id:saved.id||'pool-new',profile:{...saved.profile,api_key:undefined},revision:saved.revision+1,used_rooms:0,available_rooms:saved.max_rooms,has_api_key:true}; entries=entries.filter(e=>e.id!==entry.id).concat(entry); response=entry }
  else if(method==='DELETE') { if(failDelete){status=409;response={error:'配置已改变，请刷新'}} else {entries=entries.filter(e=>!url.pathname.endsWith(e.id));response={deleted:true}} }
  await route.fulfill({status,contentType:'application/json',body:JSON.stringify(response)})
})
try {
  await page.goto(server.resolvedUrls.local[0]+'__model_pool')
  await page.getByText('已暂停 · 保留名额').waitFor()
  assert.equal(await page.getByRole('button',{name:'删除',exact:true}).isDisabled(),true)
  await page.getByRole('button',{name:'＋ 添加账号模型'}).click()
  await page.getByLabel('账号名称',{exact:true}).fill('测试账号B')
  await page.getByLabel('账号 API Key',{exact:true}).fill('fixture-secret-no-real-key')
  await page.getByRole('button',{name:'读取可选模型'}).click()
  await page.locator('.model-picker select').selectOption('qwen3.7-flash')
  assert.equal(await page.getByRole('button',{name:'保存配置',exact:true}).isDisabled(),true,'X must be manually filled')
  await page.getByLabel('最多分配直播间数').fill('3')
  await page.getByRole('button',{name:'保存配置',exact:true}).click()
  await page.getByRole('status').filter({hasText:'配置已保存'}).waitFor()
  assert.equal(saved.max_rooms,3);assert.equal(saved.profile.api_key,'fixture-secret-no-real-key')
  assert.equal(await page.getByText('fixture-secret-no-real-key').count(),0)
  const row=page.locator('tr').filter({hasText:'测试账号B'})
  await row.getByRole('button',{name:'编辑',exact:true}).click()
  assert.equal(await page.getByLabel('账号 API Key',{exact:true}).inputValue(),'')
  await page.getByRole('button',{name:'取消',exact:true}).click()
  failDelete=true
  await row.getByRole('button',{name:'删除',exact:true}).click()
  await page.getByRole('button',{name:'确认删除',exact:true}).click()
  await page.getByRole('alert').filter({hasText:'配置已改变'}).waitFor()
  await page.getByRole('button',{name:'取消',exact:true}).click()
  await page.getByRole('button',{name:'导入当前口播账号'}).click()
  await page.getByLabel('账号名称',{exact:true}).waitFor()
  assert.equal(await page.getByLabel('最多分配直播间数').inputValue(),'')
  assert.equal(await page.getByRole('button',{name:'保存配置',exact:true}).isDisabled(),true)
  assert.equal(await page.getByLabel('账号 API Key',{exact:true}).inputValue(),'')
  await page.getByRole('button',{name:'取消',exact:true}).click()
  if(process.env.MODEL_POOL_SCREENSHOT) await page.screenshot({path:process.env.MODEL_POOL_SCREENSHOT,fullPage:true})
  await page.setViewportSize({width:390,height:844})
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'page must not overflow phone')
  await page.getByRole('button',{name:'＋ 添加账号模型'}).click()
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'editor must fit phone')
  assert.deepEqual(errors,[])
  console.log('Model pool UI passed: account/model/X save, credential masking, occupied-delete guard, revision errors, explicit import capacity, desktop and mobile.')
} finally { await browser.close(); await server.close() }
