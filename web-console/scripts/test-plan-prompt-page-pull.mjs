// Actual editor and API wrapper. All responses are local fixtures; no paid calls.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir, writeFile } from 'node:fs/promises'
import { createServer } from 'vite'
const { chromium } = createRequire(new URL('../../collector-worker/package.json', import.meta.url))('playwright-core')
const output = process.env.PLAN_PROMPT_TEST_OUTPUT || 'E:/直播伴播-local/plan-prompt-page-pull-20261105/browser'
await mkdir(output, { recursive: true })
const fixture = String.raw`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" href="data:,"><div id="app"></div><script type="module">
import {createApp,h,ref} from 'vue';
import Editor from '/src/components/PlanPromptEditor.vue';
const content=ref({persona_md:'人设原稿',facts_md:'事实原稿',red_lines_md:'红线原稿',speech_requirements_md:'主线原稿',interaction_requirements_md:'互动原稿'}),planning=ref('规划原稿');
const plan=ref(4),loading=ref(false),loaded=ref(true),saving=ref(false),visible=ref(true);
window.fixture={calls:[],content,planning,plan,loading,loaded,saving,visible,response:null,hold:false,release:null};
window.fetch=async(url,init={})=>{
 const f=window.fixture,body=init.body?JSON.parse(init.body):null;
 if(!String(url).endsWith('/prompt/render')||init.method!=='POST')throw Error('Unexpected business endpoint');
 f.calls.push({url:String(url),method:init.method,body});
 const response=f.response??{sections:{[body.page]:'生成正文 '+body.page},sources:{[body.page]:'model'}};f.response=null;
 // Deliberately ignore AbortSignal so a late transport cannot bypass scope guards.
 if(f.hold){f.hold=false;await new Promise(r=>f.release=r)}
 if(response.status)return Response.json({error:'模拟拉取失败'},{status:response.status});
 return Response.json(response);
};
createApp({render:()=>visible.value?h(Editor,{modelValue:content.value,'onUpdate:modelValue':v=>content.value=v,rewritePlanning:planning.value,'onUpdate:rewritePlanning':v=>planning.value=v,planId:plan.value,loading:loading.value,loaded:loaded.value,saving:saving.value,error:'',notice:''}):null}).mount('#app');
</script></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom' })
server.middlewares.use(async (req, res, next) => {
  if (req.url !== '/__page-pull') return next()
  res.setHeader('Content-Type', 'text/html;charset=utf-8')
  res.end(await server.transformIndexHtml(req.url, fixture))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const checks = [], errors = [], external = []
let page
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, reducedMotion: 'reduce' })
  await context.route('**/*', route => {
    const url = new URL(route.request().url())
    if (url.hostname === '127.0.0.1') return route.continue()
    external.push(url.hostname); return route.abort()
  })
  page = await context.newPage(); page.setDefaultTimeout(12000); page.on('pageerror', e => errors.push(e.message))
  const keys = ['persona_md','facts_md','red_lines_md','speech_requirements_md','interaction_requirements_md','rewrite_planning_md']
  const labels = ['主播人设','事实依据','红线禁止','直播话术要求','互动回答要求','话术规划']
  const tab = key => page.getByRole('tab', { name: labels[keys.indexOf(key)], exact: true })
  const editor = page.locator('#plan-prompt-content')
  const pull = page.getByRole('button', { name: '拉取提示词', exact: true })
  const confirm = page.getByRole('button', { name: '确认拉取', exact: true })
  const draft = () => page.evaluate(() => JSON.parse(JSON.stringify({ ...window.fixture.content.value, rewrite_planning_md: window.fixture.planning.value })))
  const count = () => page.evaluate(() => window.fixture.calls.length)
  const response = value => page.evaluate(value => window.fixture.response = value, value)
  const ready = () => editor.locator('xpath=self::*[not(@readonly)]').waitFor()
  const start = async () => { await pull.click(); if (await confirm.isVisible()) await confirm.click() }
  const success = async () => { await start(); await ready(); await page.locator('.plan-prompt-notice').waitFor() }
  const original = {persona_md:'人设原稿',facts_md:'事实原稿',red_lines_md:'红线原稿',speech_requirements_md:'主线原稿',interaction_requirements_md:'互动原稿',rewrite_planning_md:'规划原稿'}
  await page.goto(server.resolvedUrls.local[0] + '__page-pull'); await ready()
  assert.equal(await page.locator('.plan-prompt-actions button').count(), 1)
  assert.deepEqual(await page.getByRole('tab').allTextContents(), labels)
  checks.push('六个页签和一个拉取入口，无单独保存或自动保存入口')

  await pull.click(); await page.getByRole('dialog', {name:'拉取提示词并替换当前页？',exact:true}).waitFor()
  assert.match(await page.getByRole('dialog').innerText(), /仅替换「主播人设」/)
  await page.keyboard.press('Escape'); assert.equal(await count(), 0); assert.deepEqual(await draft(), original)
  checks.push('确认弹窗标明当前页，Esc取消不请求、不覆盖')

  for (const key of keys) {
    await tab(key).click(); const before = await draft(), calls = await count()
    const text = '  '+key+' 新正文\n😀 {{已有开场}}  '
    // Include unrelated response fields to verify they never overwrite other pages.
    await response({sections:Object.fromEntries(keys.map(k=>[k,k===key?text:'不得采用 '+k])),rewrite_planning_md:key==='rewrite_planning_md'?text:'不得采用规划',sources:{[key]:'model'}})
    await success(); assert.equal(await count(), calls+1)
    assert.deepEqual(await page.evaluate(()=>window.fixture.calls.at(-1).body), {page:key})
    assert.deepEqual(await draft(), {...before,[key]:text}); assert.equal(await tab(key).getAttribute('aria-selected'), 'true')
  }
  checks.push('六页逐项固定page请求，长键返回只更新本页，其他五页逐字保持且不跳页')

  for (const [key,short] of Object.entries({persona_md:'persona',facts_md:'facts',red_lines_md:'redlines',speech_requirements_md:'speech',interaction_requirements_md:'interaction'})) {
    await tab(key).click(); const before=await draft()
    await response({sections:{[short]:'短键 '+short},facts_md:'这是事实摘要，不应代替完整事实页'})
    await success(); assert.deepEqual(await draft(), {...before,[key]:'短键 '+short})
  }
  await tab('rewrite_planning_md').click(); await response({sections:{rewrite_planning_md:'分区规划'}}); await success()
  assert.equal((await draft()).rewrite_planning_md,'分区规划')
  checks.push('兼容五个短键及规划两种返回位置，不要求服务返回其他五页')

  await tab('facts_md').click(); const beforeEmpty=await draft()
  await response({sections:{facts:''},sources:{facts_md:'template'}}); await success()
  assert.deepEqual(await draft(),{...beforeEmpty,facts_md:''}); assert.match(await page.locator('.plan-prompt-notice').innerText(),/事实依据已清空/)
  checks.push('空事实是有效清空，仅事实页更新；事实摘要不冒认为完整页')

  await tab('persona_md').click()
  for (const value of [{sections:{}},{sections:{persona_md:''}},{sections:{persona_md:'  '}},{sections:{persona_md:12}},{status:500},null]) {
    const before=await draft(); await response(value===null?{sections:null}:value); await start(); await ready()
    await page.getByText('拉取未完成',{exact:true}).waitFor(); assert.deepEqual(await draft(),before)
    assert.ok(!(await page.locator('.plan-prompt-error').allTextContents()).join('').includes('EMPTY_PAGE'))
  }
  checks.push('缺项/空白/非字符串/错误响应保留全部草稿并显示中文错误')

  for (const text of ['字'.repeat(20001),'字'.repeat(19970)]) {
    const before=await draft(); await response({sections:{persona:text}}); await start(); await ready()
    await page.getByText('拉取未完成',{exact:true}).waitFor(); assert.deepEqual(await draft(),before)
  }
  checks.push('单页超长及合成后超长均在赋值前拒绝，避免部分覆盖')

  await response({sections:{persona:'回退母版正文'},sources:{persona_md:'template'},warnings:{persona_md:['出现内部术语“系统”','<img src=x onerror=alert(1)>','',null,12]}})
  await success(); assert.equal((await draft()).persona_md,'回退母版正文')
  assert.match(await page.locator('.plan-prompt-notice').innerText(),/已拉取「主播人设」/)
  assert.equal(await page.locator('.plan-prompt-warning li').count(),2); assert.equal(await page.locator('.plan-prompt-warning img').count(),0)
  await editor.fill('修改后的正文'); assert.equal(await page.locator('.plan-prompt-warning').count(),0)
  checks.push('回退母版和审核警告可见，警告按文字转义，编辑后清除旧反馈')

  const beforeHold=await draft(); await response({sections:{persona:'人设迟到结果'},sources:{persona_md:'model'}})
  await page.evaluate(()=>window.fixture.hold=true); await start(); await page.waitForFunction(()=>!!window.fixture.release)
  await tab('red_lines_md').click(); assert.equal(await page.locator('.plan-prompt-actions button').isDisabled(),true)
  await page.evaluate(()=>{window.fixture.release();window.fixture.release=null}); await ready()
  assert.deepEqual(await draft(),{...beforeHold,persona_md:'人设迟到结果'}); assert.equal(await tab('red_lines_md').getAttribute('aria-selected'),'true')
  checks.push('等待期间切页：只更新发出请求的页，当前页不被挪动或替换')

  await tab('persona_md').click(); const once=await count()
  await page.evaluate(()=>window.fixture.hold=true); await pull.click(); await confirm.dblclick()
  await page.waitForFunction(()=>!!window.fixture.release); assert.equal(await count(),once+1)
  await page.evaluate(()=>{window.fixture.release();window.fixture.release=null}); await ready()
  checks.push('确认连点与等待期重复操作仅发一次请求')

  await response({sections:{persona:'旧方案A的迟到结果'}}); await page.evaluate(()=>window.fixture.hold=true); await start(); await page.waitForFunction(()=>!!window.fixture.release)
  await page.evaluate(()=>{window.fixture.plan.value=5}); await ready()
  await page.evaluate(()=>{window.fixture.plan.value=4;window.fixture.content.value={...window.fixture.content.value,persona_md:'重新进入A后的新稿'}}); await ready()
  await page.evaluate(()=>{window.fixture.release();window.fixture.release=null}); await page.waitForTimeout(100)
  assert.equal((await draft()).persona_md,'重新进入A后的新稿')
  checks.push('方案A→B→A同ID重入也丢弃旧响应，恢复可拉取状态')

  for (const flag of ['loading','saving','loaded']) {
    await response({sections:{persona:'不该覆盖 '+flag}}); await page.evaluate(()=>window.fixture.hold=true); await start(); await page.waitForFunction(()=>!!window.fixture.release)
    const before=await draft(); await page.evaluate(flag=>window.fixture[flag].value=flag!=='loaded',flag)
    await page.evaluate(()=>{window.fixture.release();window.fixture.release=null}); await page.waitForTimeout(80)
    await page.evaluate(flag=>window.fixture[flag].value=flag==='loaded',flag); await ready(); assert.deepEqual(await draft(),before)
  }
  checks.push('开始刷新、保存或失去已加载状态均作废在途拉取，迟到不覆盖')

  await response({sections:{persona:'同方案过时响应'}}); await page.evaluate(()=>window.fixture.hold=true); await start(); await page.waitForFunction(()=>!!window.fixture.release)
  await page.evaluate(()=>window.fixture.content.value={...window.fixture.content.value,persona_md:'外部更新的草稿'})
  await page.evaluate(()=>{window.fixture.release();window.fixture.release=null}); await ready(); await page.getByText('拉取未完成',{exact:true}).waitFor()
  assert.equal((await draft()).persona_md,'外部更新的草稿')
  checks.push('同方案当前页外部更新后，旧生成结果保留人工新稿')

  await tab('facts_md').click(); assert.equal(await editor.inputValue(),''); const emptyCalls=await count()
  await response({sections:{facts:'空页直接拉取'}}); await pull.click(); await ready()
  assert.equal(await page.getByRole('dialog').count(),0); assert.equal(await count(),emptyCalls+1)
  checks.push('只有当前页为空则直接拉取，不因其他页有稿弹覆盖确认')

  await pull.click(); await page.getByRole('dialog').waitFor(); const cancelCalls=await count()
  await page.evaluate(()=>window.fixture.plan.value=5); await ready(); assert.equal(await page.getByRole('dialog').count(),0)
  assert.equal(await count(),cancelCalls)
  checks.push('确认期间切方案关闭弹窗，不向新方案误发请求')

  await response({sections:{facts:'卸载后的旧结果'}}); await page.evaluate(()=>window.fixture.hold=true); await start(); await page.waitForFunction(()=>!!window.fixture.release)
  const beforeUnmount=await draft(); await page.evaluate(()=>window.fixture.visible.value=false)
  await page.locator('.plan-prompt-editor').waitFor({state:'detached'}); await page.evaluate(()=>{window.fixture.release();window.fixture.release=null})
  await page.waitForTimeout(100); assert.deepEqual(await draft(),beforeUnmount)
  await page.evaluate(()=>window.fixture.visible.value=true); await ready()
  checks.push('组件卸载后迟到响应无赋值、无残留锁定')

  await response({sections:{persona:'界面检查'},warnings:{persona_md:['长告警😀'.repeat(100)]}}); await success()
  for (const width of [1440,390,320]) {
    await page.setViewportSize({width,height:1000})
    const size=await page.locator('.plan-prompt-editor').evaluate(el=>({scroll:el.scrollWidth,client:el.clientWidth}))
    assert.ok(size.scroll<=size.client+1,JSON.stringify(size))
    await page.locator('.plan-prompt-editor').screenshot({path:output+'/'+width+'.png'})
  }
  assert.equal(await editor.evaluate(el=>getComputedStyle(el).fontSize),'20px')
  checks.push('1440/390/320px告警与页签无横向溢出，正文保持20px')
  assert.ok((await page.evaluate(()=>window.fixture.calls)).every(c=>Object.keys(c.body).length===1&&keys.includes(c.body.page)))
  assert.deepEqual(errors,[]); assert.deepEqual(external,[])
  checks.push('所有请求均显式单页，无自动保存/额外请求/浏览器错误/外部业务请求')
  await writeFile(output+'/checks.json',JSON.stringify({passed:true,checks,errors,external,scope:'Actual editor/API; local fixtures only. Backend model count is not measured by this test.'},null,2))
  console.log(JSON.stringify({passed:true,groups:checks.length,checks}))
} catch(error) {
  if(page)await page.screenshot({path:output+'/failure.png',fullPage:true})
  await writeFile(output+'/failure.json',JSON.stringify({error:String(error),stack:error.stack,checks,errors,external},null,2));throw error
} finally {await browser.close();await server.close()}
