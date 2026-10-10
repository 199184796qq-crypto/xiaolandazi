import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir } from 'node:fs/promises'
import { createServer } from 'vite'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const fixture = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><link rel="icon" href="data:,"><div id="app"></div><script type="module">
import {createApp,h} from 'vue';import {createRouter,createMemoryHistory} from 'vue-router';
import View from '/src/views/LiveStrategyView.vue';import {applySession} from '/src/session.ts';import '/src/style.css';
const room={id:26,tenant_id:14,name:'测试直播间',status:'offline',platform:'douyin'};
const plan={id:4,tenant_id:14,name:'测试方案',room_ids:[26],room_count:1};
const records=[27,25,24,23].map(id=>({id,room_id:26,tenant_id:14,source_type:'live_uncertain',status:'editing',question:'待核对的直播问题'+id,original_reply:'待核对的原回答',updated_at:'2026-10-10T02:00:00Z',review:{kind:'missing_fact',reason:'需要核对保存的业务资料',plan_id:4}}));
window.fixture={calls:[],records,delayWorkspace:location.search.includes('delayed=1')};
window.fetch=async(url,init={})=>{const u=new URL(url,location.origin),p=u.pathname;window.fixture.calls.push({url:String(url),method:init.method||'GET'});if(init.method&&init.method!=='GET')throw Error('Unexpected write '+p);
if(p==='/api/v1/rooms')return Response.json({items:[room]});
if(p.endsWith('/ui-preferences'))return Response.json({selected_live_room_id:26});
if(p==='/api/v1/live-agent-plans'||p.endsWith('/live-agent-plans'))return Response.json({items:[plan]});
if(p.endsWith('/config-versions'))return Response.json([]);
if(p.endsWith('/content-mode'))return Response.json({content_mode:'static',allowed_modes:['static'],can_switch:false});
if(p.endsWith('/runtime'))return Response.json({room_id:26,agent_plan_id:4,agent_state:'stopped'});
if(p.endsWith('/interaction-learning')){let items=window.fixture.records.filter(r=>(u.searchParams.get('status')==='all'||r.status==='editing')&&(!u.searchParams.get('source')||r.source_type===u.searchParams.get('source')));return Response.json({items,total:items.length,pending_count:window.fixture.records.filter(r=>r.status==='editing').length});}
return Response.json({items:[],plugins:[],checks:[],groups:[],catalog:[],profiles:[],version:null,source:null,addressing:[],greeting:'你好'});};
applySession({actor:{user_id:14,tenant_id:14,role:'customer',username:'test'},tenants:[{id:14,name:'测试'}],environment:'test'});
const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:View}]});await router.push('/');await router.isReady();
createApp({render:()=>h(View)}).use(router).mount('#app');</script></html>`
const deferredWorkspace={name:'deferred-workspace-fixture',enforce:'pre',transform(code,id){
 if(!id.endsWith('/src/views/LiveStrategyView.vue'))return
 return code.replace("import InteractionRulesWorkspace from '../components/InteractionRulesWorkspace.vue'",`import {defineAsyncComponent} from 'vue'
const InteractionRulesWorkspace=defineAsyncComponent(async()=>{if(window.fixture.delayWorkspace)await new Promise(r=>window.fixture.releaseWorkspace=r);return (await import('../components/InteractionRulesWorkspace.vue')).default})`)
}}
const server = await createServer({plugins:[deferredWorkspace],server:{host:'127.0.0.1',port:0},appType:'custom'})
server.middlewares.use(async(req,res,next)=>{if(!req.url.startsWith('/__full_location'))return next();res.setHeader('Content-Type','text/html;charset=utf-8');res.end(await server.transformIndexHtml('/__full_location',fixture))})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try {
 const page=await browser.newPage({viewport:{width:1700,height:1000},reducedMotion:'reduce'}),errors=[]
 page.on('pageerror',e=>errors.push(e.message))
 await page.goto(server.resolvedUrls.local[0]+'__full_location')
 await page.locator('.interaction-pending-badge').waitFor()
 await page.locator('.workflow-step-memory').click()
 await page.waitForFunction(()=>document.activeElement?.getAttribute('data-interaction-review-id')==='27',{},{timeout:5000})
 assert.equal(await page.getByRole('tab',{name:'直播互动',exact:true}).getAttribute('aria-selected'),'true')
 assert.equal(await page.locator('.pending-question-dot').count(),4,'each editing record has a red marker')
 assert.equal(await page.locator('.is-review-located').count(),1)
 const box=await page.locator('[data-interaction-review-id="27"]').boundingBox();assert.ok(box.y>=0&&box.y<1000)
 await mkdir('../deploy/validation/interaction-location-fix',{recursive:true})
 await page.screenshot({path:'../deploy/validation/interaction-location-fix/full-view.png'})
 await page.locator('.workflow-step-memory').click();await page.waitForFunction(()=>document.activeElement?.getAttribute('data-interaction-review-id')==='27')
 await page.evaluate(()=>window.fixture.records[0].status='closed')
 await page.getByRole('tab',{name:'处理历史',exact:true}).click();await page.locator('[data-interaction-review-id="27"]').waitFor()
 assert.equal(await page.locator('[data-interaction-review-id="27"] .pending-question-dot').count(),0,'processed records have no red marker')
 assert.equal(await page.locator('.pending-question-dot').count(),3)
 await page.locator('.interaction-pending-badge').click()
 await page.waitForFunction(()=>document.activeElement?.getAttribute('data-interaction-review-id')==='25')
 await page.goto(server.resolvedUrls.local[0]+'__full_location?delayed=1')
 await page.locator('.interaction-pending-badge').waitFor();await page.locator('.interaction-pending-badge').click()
 await page.waitForFunction(()=>!!window.fixture.releaseWorkspace)
 await page.evaluate(()=>window.fixture.releaseWorkspace())
 await page.waitForFunction(()=>document.activeElement?.getAttribute('data-interaction-review-id')==='27',{},{timeout:5000})
 assert.equal(await page.locator('.pending-question-dot').count(),4,'navigation survives delayed component mounting')
 assert.ok((await page.evaluate(()=>window.fixture.calls)).every(c=>c.method==='GET'),'navigation has no writes or paid generation')
 assert.deepEqual(errors,[])
 console.log('PASS: full LiveStrategyView; badge and tab clicks; pending row dots; closed row has no dot; exact focus/scroll; delayed mount retains navigation; GET only')
} finally {await browser.close();await server.close()}
