// Full strategy page and real dialog; local API fixtures only.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir, writeFile } from 'node:fs/promises'
import { createServer } from 'vite'
const { chromium } = createRequire(new URL('../../collector-worker/package.json', import.meta.url))('playwright-core')
const output = process.env.PLAN_TEST_OUTPUT || 'E:/直播伴播-local/product-card-create-20261030/browser'
await mkdir(output, { recursive: true })
const fixture = String.raw`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" href="data:,"><div id="app"></div><script type="module">
import {createApp,h} from 'vue';import {createRouter,createMemoryHistory} from 'vue-router';
import View from '/src/views/LiveStrategyView.vue';import {applySession} from '/src/session.ts';import {feedbackState} from '/src/uiFeedback.ts';import '/src/style.css';
const plans=[4,5].map(id=>({id,tenant_id:14,name:'测试方案'+id,status:'active',room_count:0,term_count:0}));
window.fixture={calls:[],products:JSON.parse(sessionStorage.getItem('product-card-data')||'null')||{4:[],5:[]},mode:'success',holdSave:false,holdRead:false,feedback:feedbackState};
window.fetch=async(url,init={})=>{
 const f=window.fixture,u=new URL(url,location.origin),p=u.pathname,method=init.method||'GET',body=init.body?JSON.parse(init.body):null,plan=Number(p.match(/live-agent-plans\/(\d+)/)?.[1]||0);
 f.calls.push({path:p,method,body});
 if(method==='POST'&&p.endsWith('/fact-gap-check'))return Response.json({items:[],issues:[],gaps:[],checks:[]});
 if(method==='POST'&&p.endsWith('/product-links/adopt')){
  if(f.holdSave){f.holdSave=false;await new Promise(r=>f.releaseSave=r)}
  if(f.mode==='error')return Response.json({error:'测试保存失败，请重试'},{status:503});
  const candidate=body.links[0];
  if(f.mode==='conflict'||f.mode==='unchanged')return Response.json({adopted:0,results:[{status:f.mode,message:'这个编号已有商品，请使用其他编号'}]});
  const saved={...candidate,id:100+Object.values(f.products).flat().length,tenant_id:f.mode==='wrong-scope'?24:body.tenant_id,plan_id:plan,room_roles:[],status:'active',version_no:1,attributes:[],source_type:'analysis_adoption',created_at:'2026-10-30T04:00:00Z',updated_at:'2026-10-30T04:00:00Z'};
  if(f.mode!=='wrong-scope'){f.products[plan].push(saved);sessionStorage.setItem('product-card-data',JSON.stringify(f.products))}
  return Response.json({adopted:1,results:[{status:'adopted',saved}]});
 }
 if(method!=='GET')throw Error('Unexpected mutation '+method+' '+p);
 if(p.endsWith('/product-links')){const items=structuredClone(f.products[plan]||[]);if(f.holdRead){f.holdRead=false;await new Promise(r=>f.releaseRead=r)}return Response.json({items})}
 if(p==='/api/v1/rooms')return Response.json({items:[]});
 if(p.endsWith('/ui-preferences'))return Response.json({});
 if(p==='/api/v1/live-agent-plans')return Response.json({items:plans});
 if(p.endsWith('/voice-rate'))return Response.json({tenant_id:14,plan_id:plan,rate:1});
 if(p.endsWith('/content-mode'))return Response.json({content_mode:'ai_dynamic',available_modes:['user_audio','ai_pregenerated','ai_dynamic'],dynamic_authorized:true});
 if(p.endsWith('/content-pools'))return Response.json({state:'synced',changes:[],products:[],benefits:[],pools:[],set_id:9,saved_set_id:9,job:{}});
 if(p.endsWith('/realtime-options'))return Response.json({buffer_seconds:60,body_min:2,body_max:4,max_chars:300});
 return Response.json({items:[],plugins:[],checks:[],groups:[],catalog:[],profiles:[],version:null,source:null,addressing:[],greeting:'你好'});
};
applySession({actor:{user_id:14,tenant_id:14,role:'customer',username:'test'},tenants:[{id:14,name:'测试'}],environment:'test'});
const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:View}]});await router.push('/');await router.isReady();
createApp({render:()=>h(View)}).use(router).mount('#app');</script></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom', plugins: [{
  name: 'product-create-inspect', enforce: 'pre', transform(code, id) {
    if (!id.endsWith('/src/views/LiveStrategyView.vue')) return
    return code.replace('</script>', 'window.fixture.view={setPlan:id=>handlePlanSwitch({target:{value:String(id)}}),plan:()=>currentRoomPlanId.value,refresh:refreshFormalProductLinks};</script>')
  },
}] })
server.middlewares.use(async (req, res, next) => {
  if (req.url !== '/__product-create') return next()
  res.setHeader('Content-Type', 'text/html;charset=utf-8'); res.end(await server.transformIndexHtml(req.url, fixture))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const checks = [], errors = []
try {
  const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, reducedMotion: 'reduce' }); page.setDefaultTimeout(10000)
  page.on('pageerror', e => errors.push(e.message))
  const add = page.getByRole('button', { name: '添加商品卡', exact: true }), modal = page.getByRole('dialog', { name: '添加商品卡', exact: true })
  const save = modal.getByRole('button', { name: '保存商品卡', exact: true })
  const writes = () => page.evaluate(() => window.fixture.calls.filter(call => call.method !== 'GET' && !call.path.endsWith('/fact-gap-check')))
  const cards = page.locator('.strategy-product-formal .product-card')
  const fill = async (key, name) => { await modal.getByRole('textbox', { name: '商品编号', exact: false }).fill(key); await modal.getByRole('textbox', { name: '商品名称', exact: false }).fill(name) }
  const switchPlan = async id => { await page.evaluate(id => window.fixture.view.setPlan(id), id); await page.waitForFunction(id => window.fixture.view.plan() === id, id) }
  await page.goto(server.resolvedUrls.local[0] + '__product-create')
  await add.waitFor(); await page.waitForFunction(() => window.fixture.view.plan() === 4)
  assert.equal(await add.isEnabled(), true); assert.equal(await cards.count(), 0); assert.equal((await writes()).length, 0, JSON.stringify(await writes()))
  await page.locator('.strategy-product-formal').screenshot({ path: output + '/empty-desktop.png' })
  await add.click(); assert.equal(await modal.evaluate(el => el.matches(':modal')), true)
  assert.equal(await modal.getByRole('textbox', { name: '商品名称', exact: false }).evaluate(el => el === document.activeElement), true)
  assert.equal(await save.isDisabled(), true); await fill('1号链接', '测试商品'); await modal.getByRole('button', { name: '取消', exact: true }).click()
  assert.equal((await writes()).length, 0); assert.equal(await add.evaluate(el => el === document.activeElement), true)
  checks.push('真实方案页无房间也有添加入口；空列表可添加，原生模态/必填/取消/焦点正确，打开取消不写入')
  await add.click(); assert.equal(await modal.getByRole('textbox', { name: '商品名称', exact: false }).inputValue(), '')
  await fill(' 1号链接 ', ' 测试商品 ')
  await modal.getByRole('textbox', { name: '规格（选填）', exact: true }).fill('500克/袋')
  await modal.getByRole('textbox', { name: '销售价（选填）', exact: true }).fill('39.9元')
  await modal.getByRole('textbox', { name: '数量（选填）', exact: true }).fill('2袋/份')
  await modal.getByRole('textbox', { name: '适用说明（选填）', exact: true }).fill('家庭分享')
  await page.evaluate(() => window.fixture.mode = 'error'); await save.click()
  await modal.getByRole('alert').filter({ hasText: '测试保存失败' }).waitFor(); assert.equal(await modal.getByRole('textbox', { name: '商品名称', exact: false }).inputValue(), ' 测试商品 ')
  assert.equal(await page.evaluate(() => window.fixture.feedback.alertOpen), false)
  checks.push('保存失败显示弹窗内错误并保留全部草稿，不弹不可见的全局错误框')
  await page.evaluate(() => { window.fixture.mode = 'success'; window.fixture.holdSave = true }); await save.click()
  await page.waitForFunction(() => !!window.fixture.releaseSave); assert.equal(await modal.getByRole('button', { name: '保存中…' }).isDisabled(), true)
  await page.keyboard.press('Escape'); assert.equal(await modal.isVisible(), true)
  assert.equal(await modal.getByRole('textbox', { name: '商品名称', exact: false }).isDisabled(), true)
  await page.evaluate(() => window.fixture.releaseSave()); await modal.waitFor({ state: 'hidden' }); await cards.first().waitFor()
  assert.equal(await cards.count(), 1); assert.match(await cards.first().textContent(), /测试商品/)
  const sent = (await writes()).at(-1); assert.equal(sent.path, '/api/v1/live-agent-plans/4/product-links/adopt'); assert.equal(sent.body.tenant_id, 14)
  assert.deepEqual(sent.body.links[0], { link_key: '1号链接', product_name: '测试商品', spec: '500克/袋', daily_price: '39.9元', quantity: '2袋/份', audience: '家庭分享', import_mode: 'merge', review_bucket: 'adoptable', review_reason: '用户手动填写', source_quotes: ['用户手动添加商品卡'] })
  assert.equal(await page.evaluate(() => document.body.style.overflow), '')
  checks.push('提交防重、保存中禁止关闭/编辑；租户+方案及六字段正确，非替换保存成功立即回显商品卡')
  await page.reload(); await cards.first().waitFor(); assert.equal(await cards.count(), 1)
  await add.click(); await fill('1号链接', '不同商品'); const countBeforeDuplicate = (await writes()).length; await save.click()
  await modal.getByRole('alert').filter({ hasText: '编号已存在' }).waitFor(); assert.equal((await writes()).length, countBeforeDuplicate)
  await modal.getByRole('button', { name: '取消', exact: true }).click()
  checks.push('重新读取持久化结果；已有商品时入口仍在，重复编号本地阻止且不覆盖旧卡')
  for (const mode of ['conflict', 'unchanged', 'wrong-scope']) {
    await add.click(); await fill('2号链接', '第二个商品'); await page.evaluate(mode => window.fixture.mode = mode, mode); await save.click(); await modal.getByRole('alert').waitFor()
    assert.equal(await cards.count(), 1); assert.equal(await modal.getByRole('textbox', { name: '商品名称', exact: false }).inputValue(), '第二个商品')
    await modal.getByRole('button', { name: '取消', exact: true }).click()
  }
  checks.push('服务端冲突/已存在/错误租户回执均不当成新增成功，保留草稿和旧卡')
  await page.evaluate(() => { window.fixture.mode = 'success'; window.fixture.holdSave = true; window.fixture.releaseSave = null })
  await add.click(); await fill('2号链接', '第二个商品'); await save.click(); await page.waitForFunction(() => !!window.fixture.releaseSave)
  await switchPlan(5); await modal.waitFor({ state: 'hidden' }); await page.waitForFunction(() => document.querySelectorAll('.strategy-product-formal .product-card').length === 0)
  await page.evaluate(() => window.fixture.releaseSave()); await page.waitForFunction(() => window.fixture.products[4].length === 2)
  assert.equal(await cards.count(), 0); await switchPlan(4); await page.waitForFunction(() => document.querySelectorAll('.strategy-product-formal .product-card').length === 2)
  checks.push('保存中切换方案关闭旧弹窗；迟到响应仅保存原方案，不污染当前方案，切回可读取')
  await page.evaluate(() => { window.fixture.holdRead = true; void window.fixture.view.refresh() }); await page.waitForFunction(() => !!window.fixture.releaseRead)
  await add.click(); await fill('3号链接', '第三个商品'); await save.click(); await modal.waitFor({ state: 'hidden' }); await page.waitForFunction(() => document.querySelectorAll('.strategy-product-formal .product-card').length === 3)
  await page.evaluate(() => window.fixture.releaseRead()); await page.waitForTimeout(100); assert.equal(await cards.count(), 3)
  checks.push('添加成功后迟到的旧商品列表不能覆盖新卡')
  for (const width of [1600, 1024, 700, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 }); await add.scrollIntoViewIfNeeded()
    const box = await add.boundingBox(); assert.ok(box.x >= 0 && box.x + box.width <= width + 1)
    assert.ok(await add.evaluate(el => parseFloat(getComputedStyle(el).fontSize) >= 16))
    await add.click(); const bounds = await modal.boundingBox(); assert.ok(bounds.x >= 0 && bounds.x + bounds.width <= width + 1)
    assert.ok(await modal.getByRole('textbox', { name: '商品名称', exact: false }).evaluate(el => parseFloat(getComputedStyle(el).fontSize) >= 16))
    if (width === 1600) await page.screenshot({ path: output + '/dialog-desktop.png' })
    if (width === 390) await page.screenshot({ path: output + '/dialog-mobile.png' })
    await page.keyboard.press('Escape'); await modal.waitFor({ state: 'hidden' })
  }
  checks.push('五种宽度下按钮/弹窗不溢出、字号至少16px、Escape可关闭')
  assert.deepEqual(errors, []); assert.ok((await writes()).every(call => call.path.endsWith('/product-links/adopt')))
  await writeFile(output + '/checks.json', JSON.stringify({ checks, errors, actualProductionRequests: false, paidModelCalls: false }, null, 2))
  console.log('PASS: ' + checks.join('；'))
} finally { await browser.close(); await server.close() }
