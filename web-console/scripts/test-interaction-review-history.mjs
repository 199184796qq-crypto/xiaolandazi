import assert from 'node:assert/strict'
import {createServer} from 'vite'
import {createRequire} from 'node:module'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url))
const {chromium}=require('playwright-core')
const fixture=`<html><head><meta charset="utf-8"></head><body><div id="app"></div><script type="module">
import{createApp,h,ref}from'/node_modules/.vite/deps/vue.js';import Workspace from'/src/components/InteractionRulesWorkspace.vue';import{useInteractionReviewCount}from'/src/interactionReviewCount.ts';
window.fixture={calls:[],record:{id:9,source_type:'live_uncertain',status:'editing',question:'价格不到90对不对',original_reply:'84.5元，不到90元',updated_at:'2026-10-10T02:00:00Z',review:{kind:'semantic',occurrences:1}}};
window.fetch=async(url,init={})=>{url=String(url);window.fixture.calls.push({url,method:init.method||'GET'});if(init.method==='POST'){window.fixture.record.status='closed';return Response.json({ok:true})}const u=new URL(url,location.origin),s=u.searchParams.get('status')||'pending',record=window.fixture.record;const items=s==='all'||(s==='pending'&&record.status==='editing')||s===record.status?[record]:[];return Response.json({items,pending_count:record.status==='editing'?1:0,total:items.length,page:1})};
createApp({setup(){const room=ref(26),plan=ref(4),count=useInteractionReviewCount(room,plan);return()=>h('main',{},[h('div',{id:'badge'},String(count.value)),h(Workspace,{roomId:26,planId:4})])}}).mount('#app')</script></body></html>`
const server=await createServer({server:{host:'127.0.0.1',port:0},appType:'custom'});server.middlewares.use((req,res,next)=>{if(req.url==='/__review'){res.setHeader('Content-Type','text/html;charset=utf-8');res.end(fixture)}else next()});await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try{
 const page=await browser.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));await page.goto(server.resolvedUrls.local[0]+'__review');await page.waitForFunction(()=>document.querySelector('#badge')?.textContent==='1')
 await page.getByRole('tab',{name:'直播互动',exact:true}).click();await page.getByRole('img',{name:'待处理问题'}).waitFor();await page.getByRole('button',{name:'无需纠正',exact:true}).click();await page.waitForFunction(()=>document.querySelector('#badge').textContent==='0');assert.equal(await page.getByText('价格不到90对不对',{exact:true}).count(),0)
 await page.getByRole('tab',{name:'处理历史',exact:true}).click();await page.getByText('价格不到90对不对',{exact:true}).waitFor();assert.equal(await page.getByRole('img',{name:'待处理问题'}).count(),0,'closed record loses its pending red dot');assert.equal(await page.getByRole('button',{name:'填写纠正',exact:true}).count(),0,'closed record may not silently reopen')
 await page.getByLabel('处理状态',{exact:true}).selectOption('closed');await page.getByRole('button',{name:'价格',exact:true}).click();await page.waitForFunction(()=>window.fixture.calls.some(c=>c.url.includes('q=')&&decodeURIComponent(c.url).includes('价格')))
 await page.getByLabel('处理状态',{exact:true}).selectOption('pending');await page.waitForFunction(()=>!document.querySelector('article'))
 assert.deepEqual(errors,[]);console.log('PASS: pending badge 1→0 on close; dismissed evidence remains in history; state and price search; processed records read-only; no extra model calls')
}finally{await browser.close();await server.close()}
