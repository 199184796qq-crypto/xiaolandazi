import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
import {createServer} from 'vite'
import vue from '@vitejs/plugin-vue'
import {fileURLToPath} from 'node:url'
import {mkdir} from 'node:fs/promises'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url)),{chromium}=require('playwright-core')
const root=fileURLToPath(new URL('..',import.meta.url)),output=fileURLToPath(new URL('../../deploy/validation/voice-model-memory/',import.meta.url))
await mkdir(output,{recursive:true})
const plugin={name:'voice-model-fixture',configureServer(server){server.middlewares.use(async(req,res,next)=>{
 if(!req.url?.startsWith('/__voice-model'))return next()
 res.setHeader('Content-Type','text/html;charset=utf-8')
 res.end(await server.transformIndexHtml('/__voice-model',`<!doctype html><html><meta charset="utf-8"><body><div id="app"></div><script type="module">
 import{createApp,ref,nextTick,h}from'vue';import Voice from'/src/components/LiveVoiceCenter.vue';import Feedback from'/src/components/GlobalFeedback.vue';import'/src/style.css';
 const plus='qwen-audio-3.0-tts-plus',flash='qwen-audio-3.1-tts-flash';
 window.fixture={writes:[],fail:false,hold:false,previewHold:false,audios:[],profiles:[{id:41,tenant_id:14,name:'海妹',provider:'supplier',voice_id:'original-plus',sample_asset_id:9,clone_status:'ready',config:{target_model:plus},created_at:'2026-10-08T10:00:00Z'}],bindings:[{id:1,profile_id:41,model:plus,voice_id:'original-plus',provider:'supplier',status:'ready'},{id:2,profile_id:41,model:flash,voice_id:'selected-flash',provider:'supplier',status:'ready'}],versions:[{id:9,lifecycle_status:'active',speech_config:{rooms:{26:{selected_voice:{source:'clone',profile_id:41,binding_id:1,name:'海妹',voice_id:'original-plus',model:plus}},25:{selected_voice:{source:'official',voice_id:'official',model:plus}}}}}]};
 class MockAudio extends EventTarget{constructor(src){super();this.src=src;window.fixture.audios.push(this)}async play(){this.played=true}pause(){this.dispatchEvent(new Event('pause'))}}
 window.Audio=MockAudio;
 window.fetch=async(url,init={})=>{
  const path=new URL(url,location.origin).pathname,payload=init.body?JSON.parse(init.body):{};
  if(init.method&&init.method!=='GET'){
   window.fixture.writes.push({path,payload,method:init.method});
   if(path.endsWith('/model-preference')){
    if(window.fixture.hold)await new Promise(resolve=>window.fixture.release=resolve);
    if(window.fixture.fail)return Response.json({error:'模拟模型保存失败'},{status:500});
    const room=path.split('/')[4],profile=window.fixture.profiles[0];profile.config.ui_model_preferences||={};profile.config.ui_model_preferences[room]=payload.model;return Response.json(profile);
   }
   if(path.endsWith('/preview')){if(window.fixture.previewHold)await new Promise(resolve=>window.fixture.releasePreview=resolve);return Response.json({audio_url:'/test.wav'})}
   if(path.endsWith('/config-versions')){window.fixture.draft={id:10,...payload};return Response.json(window.fixture.draft)}
   if(path.endsWith('/activate')){window.fixture.versions=[{...window.fixture.draft,lifecycle_status:'active'}];return Response.json({active_version_id:10})}
   if(path.endsWith('/voice-binding')){window.fixture.planVoice=window.fixture.bindings.find(b=>b.id===payload.binding_id);return Response.json({id:55,voice_identity:window.fixture.planVoice})}
   throw Error('Unexpected mutation: '+path);
  }
  if(path.endsWith('/official-voices'))return Response.json({items:[]});
  if(path.endsWith('/voice-profiles/quota'))return Response.json({limit:3,used:1,remaining:2});
  if(path.endsWith('/voice-profiles'))return Response.json(window.fixture.profiles);
  if(path.endsWith('/voice-model-bindings'))return Response.json(window.fixture.bindings);
  if(path.endsWith('/config-versions'))return Response.json(window.fixture.versions);
  return Response.json([]);
 };
 const visible=ref(true),room=ref(26);window.fixture.remount=async()=>{visible.value=false;await nextTick();visible.value=true};window.fixture.setRoom=id=>room.value=id;window.fixture.hide=()=>visible.value=false;
 createApp({setup:()=>()=>[visible.value?h(Voice,{roomId:room.value,roomName:'测试房间'}):null,h(Feedback)]}).mount('#app');
 </script></body></html>`))
})}}
const server=await createServer({root,configFile:false,plugins:[vue(),plugin],server:{host:'127.0.0.1',port:0},logLevel:'error'});await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try{
 const page=await browser.newPage({viewport:{width:1560,height:1000}}),errors=[];page.on('pageerror',e=>errors.push(e.message))
 await page.goto(server.resolvedUrls.local[0]+'__voice-model');await page.getByRole('button',{name:'我的声音'}).click()
 const select=page.locator('.model-picker select'),panel=page.locator('.voice-preview-wave-panel')
 await select.selectOption('qwen-audio-3.1-tts-flash');await page.locator('.inline-notice').filter({hasText:'模型选择已记住'}).waitFor()
 assert.equal(await page.locator('.model-picker small').innerText(),'当前生效：通用模型（推荐）')
 let writes=await page.evaluate(()=>window.fixture.writes);assert.equal(writes.length,1);assert.ok(writes[0].path.endsWith('/model-preference'));assert.deepEqual(writes[0].payload,{model:'qwen-audio-3.1-tts-flash'})
 await page.evaluate(()=>window.fixture.remount());await page.getByRole('button',{name:'我的声音'}).click();await select.waitFor();assert.equal(await select.inputValue(),'qwen-audio-3.1-tts-flash','remember after page remount')
 await page.locator('.preview-button').click();await panel.waitFor();await page.waitForFunction(()=>window.fixture.audios.length===1)
 assert.match(await panel.innerText(),/正在准备/);assert.equal(await panel.evaluate(e=>e.classList.contains('is-playing')),false)
 await page.evaluate(()=>window.fixture.audios[0].dispatchEvent(new Event('playing')));await page.locator('.voice-preview-wave-panel.is-playing').waitFor();assert.equal(await panel.locator('i').count(),27)
 await page.screenshot({path:output+'/playing.png',fullPage:true})
 await page.evaluate(()=>window.fixture.audios[0].dispatchEvent(new Event('waiting')));await page.waitForFunction(()=>!document.querySelector('.voice-preview-wave-panel').classList.contains('is-playing'))
 await page.evaluate(()=>window.fixture.audios[0].dispatchEvent(new Event('playing')));await page.evaluate(()=>window.fixture.audios[0].dispatchEvent(new Event('ended')));await panel.waitFor({state:'hidden'})
 writes=await page.evaluate(()=>window.fixture.writes);assert.equal(writes.filter(w=>w.path.endsWith('/voice-binding')).length,0,'audition does not apply voice')
 await page.getByRole('button',{name:'使用这个模型',exact:true}).click();await page.waitForFunction(()=>window.fixture.planVoice?.model==='qwen-audio-3.1-tts-flash');await page.getByRole('button',{name:'当前使用',exact:true}).waitFor()
 assert.equal(await page.evaluate(()=>window.fixture.versions[0].speech_config.rooms[26].selected_voice.voice_id),'selected-flash')
 assert.equal(await page.evaluate(()=>window.fixture.planVoice.id),2)
 await page.evaluate(()=>window.fixture.setRoom(25));await select.waitFor();await page.waitForFunction(()=>document.querySelector('select')?.value==='qwen-audio-3.0-tts-plus');assert.equal(await select.inputValue(),'qwen-audio-3.0-tts-plus','room isolated')
 await page.evaluate(()=>window.fixture.fail=true);await select.selectOption('qwen-audio-3.1-tts-flash');await page.getByRole('alertdialog').filter({hasText:'模拟模型保存失败'}).waitFor();assert.equal(await select.inputValue(),'qwen-audio-3.0-tts-plus','failed save restores choice');await page.getByRole('button',{name:'知道了'}).click()
 await page.evaluate(()=>{window.fixture.fail=false;window.fixture.hold=true});await select.selectOption('qwen-audio-3.1-tts-flash');await page.waitForFunction(()=>!!window.fixture.release)
 await page.evaluate(()=>window.fixture.setRoom(26));await page.waitForFunction(()=>document.querySelector('select')?.value==='qwen-audio-3.1-tts-flash');await page.evaluate(()=>window.fixture.release());assert.equal(await select.inputValue(),'qwen-audio-3.1-tts-flash','stale other room save cannot reset selection')
 await page.evaluate(()=>window.fixture.previewHold=true);await page.locator('.preview-button').click();await page.waitForFunction(()=>!!window.fixture.releasePreview);await panel.getByRole('button',{name:'停止',exact:true}).click();await page.evaluate(()=>window.fixture.releasePreview());await panel.waitFor({state:'hidden'})
 assert.equal(await page.evaluate(()=>window.fixture.audios.length),1,'cancelled response cannot play audio')
 await page.evaluate(()=>{window.fixture.previewHold=false;window.fixture.releasePreview=null});await page.locator('.preview-button').click();await page.waitForFunction(()=>window.fixture.audios.length===2);await page.evaluate(()=>window.fixture.hide());await page.evaluate(()=>window.fixture.audios[1].dispatchEvent(new Event('playing')));assert.equal(await panel.count(),0,'unmounted audition cannot reappear')
 assert.deepEqual(errors,[])
 console.log('PASS: persistent per-room model choice, effective/selected distinction, exact plan binding, audition does not apply, playing-only waveform, waiting/ended/cancellation/unmount, save failure and stale room response')
}finally{await browser.close();await server.close()}
