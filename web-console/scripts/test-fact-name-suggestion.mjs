import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const source = await readFile(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
const createForm = await readFile(new URL('../src/components/FormalFactCreateDialog.vue', import.meta.url), 'utf8')
assert.match(source, /<FormalFactCreateDialog v-if="creatingFormalFact"/)
assert.match(createForm, /placeholder="请输入事实的具体内容/)
assert.match(source, /await formalFactName\.ensure\(\)/)
assert.match(source, /await fullShowFactName\.ensure\(\)/)
const fixture = `<html><body><div id="app"></div><script type="module">
import {createApp,h,ref} from '/node_modules/.vite/deps/vue.js';import {useFactNameSuggestion} from '/src/factNameSuggestion.ts';
createApp({setup(){const draft=ref({category:'other',key:'',value:''}),enabled=ref(true),plan=ref(4),saved=ref([]),saveError=ref('');const naming=useFactNameSuggestion(draft,enabled,()=>({planId:plan.value,tenantId:14}));window.fixture={draft,enabled,plan,naming,saved};
async function save(){if(!draft.value.value.trim()){saveError.value='内容不能为空';return}const key=await naming.ensure();if(!key){saveError.value='名称不能为空';return}saved.value.push({...draft.value,key})}
return()=>h('section',[h('input',{id:'name',value:draft.value.key,onInput:e=>{draft.value.key=e.target.value;naming.edited()}}),h('textarea',{id:'content',value:draft.value.value,onInput:e=>draft.value.value=e.target.value}),h('p',{id:'loading'},naming.loading.value?'loading':''),h('p',{id:'error'},naming.error.value||saveError.value),h('button',{id:'retry',onClick:naming.ensure},'重试'),h('button',{id:'save',onClick:save},'保存')])}}).mount('#app');</script></body></html>`
const server = await createServer({server:{host:'127.0.0.1',port:0},appType:'custom'})
server.middlewares.use((req,res,next)=>{if(req.url==='/__name'){res.setHeader('Content-Type','text/html; charset=utf-8');res.end(fixture)}else next()})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const page=await browser.newPage(),calls=[],errors=[]
let fail=false,hold=false,held=[]
page.on('pageerror',e=>errors.push(e.message))
await page.route('**/api/v1/**',async route=>{
  assert.equal(route.request().method(),'POST')
  assert.match(new URL(route.request().url()).pathname,/\/live-agent-plans\/\d+\/fact-name-suggestion$/)
  const input=route.request().postDataJSON();calls.push(input)
  const answer=input.content.includes('发货')?'发货时效':input.content.includes('京东')?'物流快递':'保存建议'
  if(hold){held.push({route,answer});return}
  await route.fulfill(fail?{status:502,json:{error:'测试提炼失败，可手动填写'}}:{json:{name:answer}})
})
try {
  await page.goto(server.resolvedUrls.local[0]+'__name')
  await page.locator('#save').click()
  assert.equal(await page.evaluate(()=>window.fixture.saved.value.length),0)
  await page.locator('#content').fill('一般当天发货，下午六点后次日上午发')
  await page.waitForFunction(()=>document.querySelector('#name').value==='发货时效')
  assert.equal(calls.length,1)
  assert.equal(await page.locator('#content').inputValue(),'一般当天发货，下午六点后次日上午发')
  await page.locator('#name').fill('我的发货规则')
  await page.locator('#content').fill('使用京东物流')
  await page.waitForTimeout(1200)
  assert.equal(await page.locator('#name').inputValue(),'我的发货规则')
  assert.equal(calls.length,1,'manual names must not call or be overwritten by model')
  await page.locator('#name').fill('')
  await page.waitForFunction(()=>document.querySelector('#name').value==='物流快递')
  hold=true
  await page.locator('#content').fill('开袋当天吃完')
  await page.waitForFunction(()=>document.querySelector('#loading').textContent==='loading')
  await page.locator('#name').fill('手动保存建议')
  for(const item of held.splice(0))await item.route.fulfill({json:{name:item.answer}}).catch(()=>{})
  await page.waitForTimeout(100)
  assert.equal(await page.locator('#name').inputValue(),'手动保存建议','late responses must not overwrite user edits')
  hold=false;fail=true
  await page.locator('#name').fill('')
  await page.waitForFunction(()=>document.querySelector('#error').textContent.includes('测试提炼失败'))
  assert.equal(await page.locator('#name').inputValue(),'')
  await page.locator('#save').click()
  await page.waitForTimeout(100)
  assert.equal(await page.evaluate(()=>window.fixture.saved.value.length),0,'no empty-name save')
  fail=false
  await page.locator('#retry').click()
  await page.waitForFunction(()=>document.querySelector('#name').value==='保存建议')
  const before=calls.length
  await page.locator('#content').fill('明天发货')
  await page.locator('#save').click()
  await page.waitForFunction(()=>window.fixture.saved.value.length===1)
  assert.equal(calls.length,before+1,'save reuses current content naming without duplicate debounce request')
  assert.deepEqual(await page.evaluate(()=>window.fixture.saved.value[0]),{category:'other',key:'发货时效',value:'明天发货'})
  hold=true
  await page.locator('#content').fill('京东物流配送')
  await page.waitForFunction(()=>document.querySelector('#loading').textContent==='loading')
  await page.evaluate(()=>{window.fixture.enabled.value=false;window.fixture.plan.value=99;window.fixture.draft.value={category:'other',key:'新方案手动名称',value:'新方案真实内容'}})
  for(const item of held.splice(0))await item.route.fulfill({json:{name:item.answer}}).catch(()=>{})
  await page.waitForTimeout(100)
  assert.equal(await page.locator('#name').inputValue(),'新方案手动名称')
  assert.equal(await page.locator('#content').inputValue(),'新方案真实内容')
  assert.deepEqual(errors,[])
  console.log('PASS: guided modal fields; auto name only; manual edits; stale response/scope protection; failure/retry; save awaits current name; empty save blocked')
}finally{await browser.close();await server.close()}
