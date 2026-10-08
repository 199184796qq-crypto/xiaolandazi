import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'
import { mkdir } from 'node:fs/promises'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url)),{chromium}=require('playwright-core')
const root=fileURLToPath(new URL('..',import.meta.url)),output=fileURLToPath(new URL('../../deploy/validation/voice-name-edit/',import.meta.url))
await mkdir(output,{recursive:true})
const plugin={name:'voice-name-fixture',configureServer(server){server.middlewares.use(async(req,res,next)=>{
 if(!req.url?.startsWith('/__voice-name'))return next()
 res.setHeader('Content-Type','text/html;charset=utf-8')
 res.end(await server.transformIndexHtml('/__voice-name',`<!doctype html><html><meta charset="utf-8"><body><div id="app"></div><script type="module">
 import{createApp}from'vue';import Voice from'/src/components/LiveVoiceCenter.vue';import'/src/style.css';
 window.fixture={writes:[],fail:false,hold:false,profiles:[{id:41,name:'小海',provider:'supplier',voice_id:'immutable',clone_status:'ready',config:{target_model:'qwen-audio-3.0-tts-plus'},created_at:'2026-10-08T10:00:00Z'},{id:42,name:'样本声音',provider:'supplier',voice_id:'immutable2',clone_status:'ready',config:{},created_at:'2026-10-01T10:00:00Z'}]};
 window.fetch=async(url,init={})=>{
  const path=new URL(url,location.origin).pathname;
  if(init.method==='PATCH'){
   const payload=JSON.parse(init.body),id=Number(path.split('/').at(-2));window.fixture.writes.push({path,payload});
   if(window.fixture.hold)await new Promise(resolve=>window.fixture.release=resolve);
   if(window.fixture.fail)return Response.json({error:'模拟保存失败'},{status:500});
   const profile=window.fixture.profiles.find(item=>item.id===id);profile.name=payload.name;return Response.json(profile);
  }
  if(init.method&&init.method!=='GET')throw Error('Unexpected mutation: '+path);
  if(path.endsWith('/official-voices'))return Response.json({items:[]});
  if(path.endsWith('/voice-profiles/quota'))return Response.json({limit:3,used:2,remaining:1});
  if(path.endsWith('/voice-profiles'))return Response.json(window.fixture.profiles);
  if(path.endsWith('/config-versions'))return Response.json([{id:9,lifecycle_status:'active',speech_config:{rooms:{26:{selected_voice:{source:'clone',profile_id:41,name:'小海',voice_id:'immutable'}}}}}]);
  return Response.json([]);
 };
 createApp(Voice,{roomId:26,roomName:'测试房间',supportSession:new URLSearchParams(location.search).has('support')}).mount('#app');
 </script></body></html>`))
})}}
const server=await createServer({root,configFile:false,plugins:[vue(),plugin],server:{host:'127.0.0.1',port:0},logLevel:'error'});await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try{
 const page=await browser.newPage({viewport:{width:1560,height:1000}}),errors=[];page.on('pageerror',e=>errors.push(e.message))
 await page.goto(server.resolvedUrls.local[0]+'__voice-name');await page.getByRole('button',{name:'我的声音'}).click()
 const rows=page.locator('.custom-voice-row'),input=page.getByRole('textbox',{name:'声音名称'})
 await rows.first().locator('.voice-name-edit').click();await input.fill('小海新名字');await input.press('Enter');await input.waitFor({state:'hidden'})
 assert.equal(await rows.first().locator('strong').textContent(),'小海新名字');assert.match(await page.locator('.room-voice-state').innerText(),/小海新名字/)
 await rows.nth(1).locator('.voice-name-edit').click();await input.fill('第二声音');await page.locator('.voice-title-line h3').click();await input.waitFor({state:'hidden'})
 assert.equal(await rows.nth(1).locator('strong').textContent(),'第二声音')
 let count=await page.evaluate(()=>window.fixture.writes.length)
 await rows.nth(1).locator('.voice-name-edit').click();await input.fill(' ');await input.press('Enter');await page.getByRole('alert').filter({hasText:'声音名称须为1～80字'}).waitFor()
 assert.equal(await page.evaluate(()=>window.fixture.writes.length),count)
 await input.fill('取消的名字');await input.press('Escape');await input.waitFor({state:'hidden'});assert.equal(await page.evaluate(()=>window.fixture.writes.length),count)
 await rows.nth(1).locator('.voice-name-edit').click();await input.press('Enter');await input.waitFor({state:'hidden'});assert.equal(await page.evaluate(()=>window.fixture.writes.length),count)
 await page.evaluate(()=>window.fixture.fail=true);await rows.first().locator('.voice-name-edit').click();await input.fill('保存失败草稿');await input.press('Enter');await page.getByRole('alert').filter({hasText:'模拟保存失败'}).waitFor()
 assert.equal(await input.inputValue(),'保存失败草稿');assert.match(await page.locator('.room-voice-state').innerText(),/小海新名字/)
 await page.evaluate(()=>{window.fixture.fail=false;window.fixture.hold=true});await input.fill('重试成功');await input.press('Enter');await page.waitForFunction(()=>!!window.fixture.release)
 count=await page.evaluate(()=>window.fixture.writes.length);await page.locator('.voice-title-line h3').click();assert.equal(await page.evaluate(()=>window.fixture.writes.length),count,'Enter+blur must not duplicate request')
 await page.evaluate(()=>window.fixture.release());await input.waitFor({state:'hidden'});assert.equal(await rows.first().locator('strong').textContent(),'重试成功')
 const writes=await page.evaluate(()=>window.fixture.writes);assert.ok(writes.every(item=>Object.keys(item.payload).sort().join(',')==='expected_name,name'))
 assert.deepEqual(errors,[]);await page.screenshot({path:output+'/inline-name-edit.png',fullPage:true})
 await page.goto(server.resolvedUrls.local[0]+'__voice-name?support');await page.getByRole('button',{name:'我的声音'}).click();await rows.first().waitFor();assert.equal(await page.locator('.voice-name-edit').count(),0,'staff cannot rename shared profiles')
 console.log('PASS: Enter/blur autosave, no duplicate request, blank/unchanged/Escape no writes, failure retains draft, selected name updates, metadata-only payload, support read-only')
}finally{await browser.close();await server.close()}
