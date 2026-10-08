import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
import {createServer} from 'vite'
import vue from '@vitejs/plugin-vue'
import {fileURLToPath} from 'node:url'
import {mkdir} from 'node:fs/promises'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url)),{chromium}=require('playwright-core')
const root=fileURLToPath(new URL('..',import.meta.url)),output=fileURLToPath(new URL('../../deploy/validation/device-inline-name/',import.meta.url))
await mkdir(output,{recursive:true})
const plugin={name:'device-inline-name-fixture',configureServer(server){server.middlewares.use(async(req,res,next)=>{
 if(!req.url?.startsWith('/__device-inline-name'))return next()
 res.setHeader('Content-Type','text/html;charset=utf-8')
 res.end(await server.transformIndexHtml('/__device-inline-name',`<!doctype html><html><meta charset="utf-8"><body><div id="app"></div><script type="module">
 import{createApp,h}from'vue';import{createRouter,createMemoryHistory}from'vue-router';import Device from'/src/views/DeviceBindingView.vue';import Feedback from'/src/components/GlobalFeedback.vue';import{session}from'/src/session';import'/src/style.css';
 session.bootstrap={actor:{role:new URLSearchParams(location.search).has('readonly')?'admin':'customer',tenant_id:14,user_id:28}};
 window.fixture={writes:[],fail:false,hold:false,device:{id:7,tenant_id:14,device_name:'小蓝直播助手',sku_code:'xldzplus',connection_status:'offline',work_status:'stopped',display_status:'离线',stop_reason:'device_offline',room_id:23,binding_role:'primary',last_heartbeat_at:new Date().toISOString()}};
 window.fetch=async(url,init={})=>{
  const path=new URL(url,location.origin).pathname;
  if(init.method==='PATCH'){
    const payload=JSON.parse(init.body);window.fixture.writes.push({path,payload});if(window.fixture.hold)await new Promise(resolve=>window.fixture.release=resolve);
    if(window.fixture.fail)return Response.json({error:'模拟改名失败'},{status:500});window.fixture.device.device_name=payload.device_name;return Response.json(window.fixture.device);
  }
  if(init.method&&init.method!=='GET')throw Error('Unexpected mutation '+path);
  if(path.endsWith('/live/devices'))return Response.json([window.fixture.device]);
  if(path.endsWith('/rooms'))return Response.json({items:[{id:23,tenant_id:14,name:'菜籽油',status:'offline',external_room_id:'fixture'}]});
  if(path.endsWith('/addressing'))return Response.json({mode:'auto'});
  return Response.json({});
 };
 const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:{render:()=>null}},{path:'/rooms/:id',name:'room-detail',component:{render:()=>null}}]});
 createApp({render:()=>[h(Device),h(Feedback)]}).use(router).mount('#app');
 </script></body></html>`))
})}}
const server=await createServer({root,configFile:false,plugins:[vue(),plugin],server:{host:'127.0.0.1',port:0},logLevel:'error'});await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try{
 const page=await browser.newPage({viewport:{width:1500,height:1050}}),errors=[];page.on('pageerror',e=>errors.push(e.message))
 await page.goto(server.resolvedUrls.local[0]+'__device-inline-name')
 const card=page.locator('[data-device-id="7"]'),input=card.getByRole('textbox',{name:'设备名称',exact:true}),edit=card.getByRole('button',{name:'编辑设备名称',exact:true})
 await edit.waitFor();assert.equal(await input.count(),0);assert.equal(await page.getByRole('button',{name:'保存名称',exact:true}).count(),0)
 await edit.click();await input.waitFor();assert.equal(await input.evaluate(el=>el===document.activeElement),true);assert.equal(await input.inputValue(),'小蓝直播助手')
 await input.fill('一号助手');await page.getByRole('heading',{name:'设备绑定',exact:true}).click();await input.waitFor({state:'hidden'});assert.equal(await card.locator('h3').innerText(),'一号助手')
 assert.deepEqual(await page.evaluate(()=>window.fixture.writes),[{path:'/api/v1/live/devices/7',payload:{device_name:'一号助手'}}])
 await edit.click();await input.press('Enter');await input.waitFor({state:'hidden'});assert.equal(await page.evaluate(()=>window.fixture.writes.length),1,'unchanged name needs no request')
 await edit.click();await input.fill('取消草稿');await input.press('Escape');await input.waitFor({state:'hidden'});assert.equal(await card.locator('h3').innerText(),'一号助手');assert.equal(await page.evaluate(()=>window.fixture.writes.length),1)
 await edit.click();await input.fill(' ');await page.getByRole('heading',{name:'设备绑定',exact:true}).click();await card.getByRole('alert').filter({hasText:'不能为空'}).waitFor();assert.equal(await page.evaluate(()=>window.fixture.writes.length),1)
 await page.evaluate(()=>window.fixture.fail=true);await input.fill('失败草稿');await input.press('Enter');await page.getByRole('alertdialog').filter({hasText:'模拟改名失败'}).waitFor();assert.equal(await input.inputValue(),'失败草稿');assert.equal(await page.evaluate(()=>window.fixture.device.device_name),'一号助手');await page.getByRole('button',{name:'知道了'}).click()
 await page.evaluate(()=>{window.fixture.fail=false;window.fixture.hold=true});await input.fill('重试成功');await input.press('Enter');await page.waitForFunction(()=>!!window.fixture.release);let count=await page.evaluate(()=>window.fixture.writes.length);await page.getByRole('heading',{name:'设备绑定',exact:true}).click();assert.equal(await page.evaluate(()=>window.fixture.writes.length),count,'Enter+blur submits once');await page.evaluate(()=>window.fixture.release());await input.waitFor({state:'hidden'});assert.equal(await card.locator('h3').innerText(),'重试成功')
 await page.screenshot({path:output+'/saved.png',fullPage:true});await page.setViewportSize({width:430,height:1050});await edit.click();await input.waitFor();await card.screenshot({path:output+'/mobile-editing.png'});assert.ok(await input.evaluate(el=>el.getBoundingClientRect().right<=el.closest('article').getBoundingClientRect().right),'mobile input stays in card');await input.press('Escape')
 await page.reload();await edit.waitFor();assert.equal(await card.locator('h3').innerText(),'小蓝直播助手','fixture reload resets fake server; does not use local unsaved draft')
 await page.goto(server.resolvedUrls.local[0]+'__device-inline-name?readonly');await card.waitFor();assert.equal(await edit.count(),0,'non-customer remains read-only');assert.equal(await input.count(),0);assert.deepEqual(errors,[])
 console.log('PASS: title pencil editing, blur autosave, Enter+blur one request, unchanged/Escape/blank protection, failed draft retained and retry, exact name-only PATCH, removed lower row, mobile layout and customer-only edit; no real devices mutated')
}finally{await browser.close();await server.close()}
