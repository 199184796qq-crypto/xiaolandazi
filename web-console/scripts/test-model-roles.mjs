import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
const require = createRequire(process.env.PLAYWRIGHT_CORE_PACKAGE || new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const profile = (id, name, model) => ({ id, name, model, protocol:'openai_chat', base_url:'https://example.com/v1', enabled:true, timeout_ms:120000, max_tokens:16000, chat_token_field:'max_tokens', system_prompt:'', has_api_key:true })
let config = { profiles:[profile('max','Qwen38Max','qwen3.8-max'),profile('flash','qwen3.7-flash','qwen3.7-flash'),profile('other','备用连接','other-model')], system_default_id:'max', default_id:'flash', revision:1 }
let saved
const server = await createServer({ server:{host:'127.0.0.1',port:0}, appType:'custom' })
server.middlewares.use((req,res,next) => {
  if(req.url !== '/__model_roles') return next()
  res.setHeader('Content-Type','text/html; charset=utf-8')
  res.end(`<html><head><meta charset="utf-8"><style>body{margin:0;padding:24px;background:#f5f7fc;font-family:'Microsoft YaHei',sans-serif}*{box-sizing:border-box}</style></head><body><div id="app"></div><script type="module">import {createApp} from '/node_modules/.vite/deps/vue.js';import View from '/src/views/SpeechModelsView.vue';createApp(View).mount('#app')</script></body></html>`)
})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const page=await browser.newPage({viewport:{width:1700,height:1100}}),errors=[]
page.on('pageerror',error=>errors.push(error.message))
await page.route('**/api/v1/system/speech-models',async route=>{
  if(route.request().method()==='PUT') { saved=route.request().postDataJSON(); config={...saved,revision:config.revision+1} }
  await route.fulfill({status:200,contentType:'application/json',body:JSON.stringify({config,key_storage_ready:true})})
})
try {
  await page.goto(server.resolvedUrls.local[0]+'__model_roles')
  await page.getByRole('heading',{name:'系统智能体、口播生成与审核模型'}).waitFor()
  const system=page.getByLabel('系统智能体模型',{exact:true}),speech=page.getByLabel('口播生成模型',{exact:true}),review=page.getByLabel('审核模型',{exact:true})
  await page.waitForFunction(()=>document.querySelectorAll('.default-model select option').length>2)
  assert.equal(await system.inputValue(),'max');assert.equal(await speech.inputValue(),'flash')
  assert.equal(await review.inputValue(),'flash','legacy review must retain current writer')
  assert.equal(await page.getByText('话术与实时互动的默认模型',{exact:true}).count(),0)
  if(process.env.MODEL_ROLES_SCREENSHOT) await page.screenshot({path:process.env.MODEL_ROLES_SCREENSHOT,fullPage:true})
  await system.selectOption('other')
  await review.selectOption('max')
  await page.getByRole('button',{name:'保存全部配置'}).click()
  await page.getByRole('status').filter({hasText:'已保存'}).waitFor()
  assert.equal(saved.system_default_id,'other');assert.equal(saved.default_id,'flash');assert.equal(saved.profiles.length,3)
  assert.equal(saved.review_default_id,'max')
  await page.getByRole('button',{name:'刷新',exact:true}).click()
  await page.waitForFunction(()=>document.querySelector('.default-model select').value==='other')
  assert.equal(await speech.inputValue(),'flash');assert.equal(await review.inputValue(),'max')
  await speech.selectOption('other')
  await page.getByRole('button',{name:'保存全部配置'}).click()
  await page.getByRole('status').filter({hasText:'已保存'}).waitFor()
  assert.equal(saved.default_id,'other');assert.equal(saved.review_default_id,'max');assert.equal(saved.system_default_id,'other')
  assert.deepEqual(errors,[])
  console.log('Three independent role selectors; legacy review retained; switching/saving/reloading writer keeps reviewer; passed')
} finally { await browser.close();await server.close() }
