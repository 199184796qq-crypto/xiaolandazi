import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir } from 'node:fs/promises'
import { createServer } from 'vite'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const fixture = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><link rel="icon" href="data:,"><div id="app"></div><script type="module">
import {createApp,h,ref} from 'vue';import Panel from '/src/components/LectureTasksPanel.vue';import {applySession} from '/src/session.ts';import '/src/style.css';
const task={id:9,revision:1,title:'南充土麻鸭与地方风味',purpose:'让观众理解原料来源与地方风味',angles:[{title:'原料产地',goal:'讲清南充背景'},{title:'本地老卤',goal:'说明地方风味'}],facts:[{id:99,name:'原料产地',text:'四川南充',quote:'四川南充'}],links:[],enabled:true,valid:true,source:'土麻鸭产自四川南充，配上本地老卤。'};
let lib={tasks:[task],policy:{enabled:true,frequency:'normal'},fingerprint:'draft'};
const plan=ref(4);window.fixture={calls:[],setPlan:v=>plan.value=v,delay:false};
window.fetch=async(url,init={})=>{const p=new URL(url,location.origin).pathname;window.fixture.calls.push({url:String(url),method:init.method||'GET',body:init.body});
if(p.endsWith('/product-links'))return Response.json({items:[{link_key:'1',product_name:'整鸭',status:'active'}]});
if(p.endsWith('/lecture-tasks') && !init.method)return Response.json(p.includes('/5/')?{...lib,tasks:[],fingerprint:'other'}:lib);
if(p.endsWith('/ingest')){const data=JSON.parse(init.body);if(window.fixture.delay)await new Promise(r=>window.fixture.release=r);if(data.source.includes('保证'))return Response.json({saved:false,issues:[{quote:'保证',rule:'当前规则',reason:'不能承诺未经确认的效果',suggestion:'改成可核对的事实'}]});lib={...lib,fingerprint:'new'};return Response.json({saved:true,title:task.title,facts_added:2,angles:task.angles});}
if(p.endsWith('/preview'))return Response.json({text:'咱们这只土麻鸭产自四川南充，配上本地老卤，讲的就是这份地方风味。'});
if(p.endsWith('/policy')){lib={...lib,policy:JSON.parse(init.body),fingerprint:'policy'};return Response.json({ok:true});}
if(p.endsWith('/action')){const data=JSON.parse(init.body);lib={...lib,tasks:data.action==='delete'?[]:lib.tasks.map(t=>({...t,enabled:data.action==='enable',revision:t.revision+1})),fingerprint:'action'};return Response.json({ok:true});}
throw Error('unmocked '+p);};
applySession({actor:{user_id:14,tenant_id:14,role:'customer',username:'test'},tenants:[{id:14,name:'测试'}],environment:'test'});
createApp({render:()=>h(Panel,{planId:plan.value,tenantId:14,roomId:26,savedFingerprint:'saved'})}).mount('#app');</script></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom' })
server.middlewares.use(async (req,res,next) => { if(req.url!='/__lectures')return next();res.setHeader('Content-Type','text/html;charset=utf-8');res.end(await server.transformIndexHtml('/__lectures',fixture)) })
await server.listen()
const browser = await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try {
 const page=await browser.newPage({viewport:{width:1500,height:1100}}), errors=[]
 page.on('pageerror',e=>errors.push(e.message));await page.goto(server.resolvedUrls.local[0]+'__lectures')
 await page.getByRole('heading',{name:'南充土麻鸭与地方风味'}).waitFor()
 await mkdir('../deploy/validation/lecture-tasks',{recursive:true});await page.screenshot({path:'../deploy/validation/lecture-tasks/desktop.png',fullPage:true})
 await page.getByRole('button',{name:'预览讲解',exact:true}).click();await page.locator('.lecture-preview').waitFor()
 const source=page.getByRole('textbox',{name:'故事素材'});await source.fill('保证这个效果');await page.getByRole('button',{name:'发送',exact:true}).click();await page.locator('.lecture-issues').waitFor();assert.equal(await source.inputValue(),'保证这个效果')
 await source.fill('土麻鸭产自四川南充');await page.getByRole('checkbox',{name:/回车/}).check();await source.evaluate(el=>el.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',isComposing:true,bubbles:true})));await source.press('Shift+Enter')
 assert.equal(await page.evaluate(()=>window.fixture.calls.filter(c=>c.url.includes('/ingest')).length),1)
 await source.press('Enter');await page.waitForFunction(()=>!document.querySelector('textarea').value);assert.match(await page.locator('.lecture-notice').innerText(),/已收好/)
 await page.locator('.lecture-toolbar select').selectOption('less');await page.waitForFunction(()=>document.querySelector('.lecture-schedule').textContent.includes('6—8轮'))
 await page.getByRole('button',{name:'删除',exact:true}).click();await page.getByRole('button',{name:'确认删除',exact:true}).click();await page.waitForFunction(()=>!document.querySelector('.lecture-task'));assert.equal(await page.getByRole('button',{name:'保存智能体',exact:true}).isVisible(),true)
 await page.evaluate(()=>window.fixture.delay=true);await source.fill('另一段故事');await source.press('Enter');await page.waitForFunction(()=>!!window.fixture.release);await page.evaluate(()=>{window.fixture.setPlan(5);window.fixture.release()});await page.waitForFunction(()=>!document.querySelector('textarea').value);assert.equal(await page.locator('.lecture-notice').count(),0)
 await page.setViewportSize({width:390,height:900});await page.screenshot({path:'../deploy/validation/lecture-tasks/mobile.png',fullPage:true});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true)
 assert.deepEqual(errors,[]);console.log('PASS: review keeps source; IME/Shift+Enter; Enter send; preview; frequency; last-task deletion preserves save; stale scope response ignored; mobile layout')
} finally {await browser.close();await server.close()}
