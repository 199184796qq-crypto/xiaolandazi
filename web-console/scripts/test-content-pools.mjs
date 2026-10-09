// Mocked browser regression. No user plans, database or model providers touched.
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const fixture = `<html><head><meta charset="utf-8"><style>*{box-sizing:border-box}body{padding:16px;background:#f5f6ff;font-family:Arial,'Microsoft Yahei',sans-serif}</style></head><body><div id="app"></div><script type="module">
import{createApp,h,ref}from'/node_modules/.vite/deps/vue.js';import{useContentPools}from'/src/contentPools.ts';import Panel from'/src/components/ContentPoolChangesPanel.vue';import Scope from'/src/components/BenefitScopeEditor.vue';
const products=Array.from({length:20},(_,i)=>({id:i+1,link:String(i+1),name:'商品'+(i+1)}));
window.fixture={saveNavigation:0,calls:[],delayed:null,delay:false,updateResolve:null,plan:null,source:{state:'needs_update',changes:[{id:'price',area:'products',label:'1号商品 · 日常价',products:[1],before:'19元',after:'18元'},{id:'public',area:'knowledge',label:'配送 · 时效',products:[],before:'原值',after:'新值'}],products,benefits:[{benefit_id:1,all:false,products:[1,2],version_no:0}],pools:[],job:{},set_id:0,saved_set_id:0}};
window.fetch=async(url,init={})=>{url=String(url);window.fixture.calls.push({url,method:init.method||'GET',body:init.body});if(url.includes('/benefits/')){const body=JSON.parse(init.body);window.fixture.source.benefits=[{benefit_id:1,...body,version_no:1}];return Response.json(window.fixture.source.benefits[0])}if(init.method==='POST'){await new Promise(r=>window.fixture.updateResolve=r);window.fixture.source.state='updating';window.fixture.source.job={status:'running',stage:'内容池已完成 1/20'};return Response.json({state:'updating',job:window.fixture.source.job})}if(window.fixture.delay){window.fixture.delay=false;const saved=structuredClone(window.fixture.source);await new Promise(r=>window.fixture.delayed=r);return Response.json(saved)}const next=structuredClone(window.fixture.source);if(url.includes('/5/'))next.products[0].name='新方案商品';return Response.json(next)};
createApp({setup(){const plan=ref(4),room=ref({id:26,tenant_id:14});window.fixture.plan=plan;const pools=useContentPools(plan,room);window.fixture.refresh=pools.refresh;return()=>h('main',{},[h(Panel,{status:pools.status.value,error:pools.error.value,submitting:pools.submitting.value,preview:pools.preview.value,onUpdate:pools.update,onRefresh:pools.refresh,onPreview:v=>pools.preview.value=v,onOpenSave:()=>window.fixture.saveNavigation++}),h(Scope,{plan:plan.value,room:26,tenant:14,benefit:1,entityVersion:1,scope:pools.status.value?.benefits[0],products:pools.status.value?.products||[],onSaved:pools.scopeSaved})])}}).mount('#app')</script></body></html>`
const server=await createServer({server:{host:'127.0.0.1',port:0},appType:'custom'})
server.middlewares.use((req,res,next)=>{if(req.url==='/__pool_fixture'){res.setHeader('Content-Type','text/html; charset=utf-8');res.end(fixture)}else next()});await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try{
 const page=await browser.newPage({viewport:{width:1280,height:900}}),errors=[];page.on('pageerror',e=>errors.push(e.message))
 await page.goto(server.resolvedUrls.local[0]+'__pool_fixture');await page.getByText('资料有待更新内容',{exact:true}).waitFor()
 assert.equal(await page.evaluate(()=>window.fixture.calls.filter(c=>c.method==='POST').length),0,'mount must never call model update')
 await page.getByText('查看待更新资料',{exact:true}).click();await page.getByText('19元',{exact:true}).waitFor()
 await page.getByRole('button',{name:'修改范围',exact:true}).click();assert.equal(await page.locator('.scope-products input').count(),20)
 await page.locator('.scope-products input').nth(1).uncheck();await page.locator('.scope-products input').nth(2).check();await page.locator('.scope-products input').nth(19).check();await page.getByRole('button',{name:'保存范围',exact:true}).click()
 await page.waitForFunction(()=>window.fixture.calls.some(c=>c.method==='PUT'))
 const scope=JSON.parse(await page.evaluate(()=>window.fixture.calls.find(c=>c.method==='PUT').body));assert.deepEqual(scope.products,[1,3,20]);assert.equal(scope.expected_scope_version,0)
 await page.getByRole('button',{name:'修改范围',exact:true}).click();await page.getByLabel('全部商品（含以后新增商品）',{exact:true}).check();await page.getByRole('button',{name:'保存范围',exact:true}).click();await page.waitForFunction(()=>window.fixture.calls.filter(c=>c.method==='PUT').length===2)
 const update=page.getByRole('button',{name:'更新受影响内容',exact:true});await update.click();await page.getByRole('button',{name:'更新中…',exact:true}).evaluate(el=>el.click())
 assert.equal(await page.evaluate(()=>window.fixture.calls.filter(c=>c.method==='POST').length),1,'duplicate update was accepted')
 await page.evaluate(()=>window.fixture.updateResolve());await page.getByText('正在更新受影响内容',{exact:true}).waitFor();await page.getByText('内容池已完成 1/20',{exact:true}).waitFor()
 await page.evaluate(()=>{window.fixture.source.state='ready';window.fixture.source.pools=[{key:'common',label:'公共',owner:0,products:[1,2],task_count:1,reused:false,tasks:[{id:'t1',selling_point:'公共卖点',angle:'适用一二号'}]},{key:'p1',label:'1号专属',owner:1,products:[1],task_count:1,reused:true,tasks:[{id:'t2',selling_point:'专属卖点',angle:'仅适用一号'}]}];window.fixture.refresh()})
 await page.getByText('内容已更新，核对后保存智能体',{exact:true}).waitFor();
 const saveNavigation=page.getByRole('button',{name:'去保存智能体',exact:true});assert.equal(await saveNavigation.isEnabled(),true);await saveNavigation.click();assert.equal(await page.evaluate(()=>window.fixture.saveNavigation),1);assert.equal(await page.evaluate(()=>window.fixture.calls.filter(c=>c.method==='POST').length),1,'save navigation must not regenerate or publish');await page.getByText('查看公共池与商品专属池（2 个）',{exact:true}).click();await page.getByText('公共卖点 · 适用一二号',{exact:true}).waitFor()
 await page.evaluate(()=>{window.fixture.delay=true;window.fixture.source.state='failed';window.fixture.refresh()});await page.waitForFunction(()=>!!window.fixture.delayed)
 await page.evaluate(()=>{window.fixture.plan.value=5;window.fixture.source.state='synced'});await page.getByText('商品专属内容与公共内容已保存',{exact:true}).waitFor();await page.evaluate(()=>window.fixture.delayed());await page.waitForTimeout(100)
 assert.equal(await page.getByText('内容更新未完成',{exact:true}).count(),0,'old plan response overwrote current plan')
 await page.setViewportSize({width:390,height:900});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,'mobile overflow')
 assert.deepEqual(errors,[]);console.log('PASS: mount/poll makes no model calls; 20 products/multi-scope/all-scope; explicit update/duplicate guard/progress; pool preview/save navigation without model or publish; stale response isolation; mobile layout')
} finally {await browser.close();await server.close()}
