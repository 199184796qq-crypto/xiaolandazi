import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
import {mkdir,writeFile} from 'node:fs/promises'
import {fileURLToPath} from 'node:url'
import {createServer} from 'vite'
import vue from '@vitejs/plugin-vue'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url)),{chromium}=require('playwright-core')
const root=fileURLToPath(new URL('..',import.meta.url))
const output=process.env.VOICE_LIBRARY_OUTPUT_DIR || fileURLToPath(new URL('../../deploy/validation/voice-model-libraries/',import.meta.url))
await mkdir(output,{recursive:true})
const plugin={name:'voice-libraries-fixture',configureServer(server){server.middlewares.use(async(req,res,next)=>{
 if(!req.url?.startsWith('/__voice-libraries'))return next()
 res.setHeader('Content-Type','text/html;charset=utf-8')
 res.end(await server.transformIndexHtml('/__voice-libraries',`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><body style="background:#eff3ff;padding:24px"><div id="app"></div><script type="module">
 import{createApp,ref,nextTick,h}from'vue';import Voice from'/src/components/LiveVoiceCenter.vue';import Feedback from'/src/components/GlobalFeedback.vue';import'/src/style.css';
 const qwen='qwen-audio-3.0-tts-plus',flash='cosyvoice-v3.5-flash',plus='cosyvoice-v3.5-plus';
 const profile=(id,name,model)=>({id,tenant_id:14,name,provider:model.startsWith('cosy')?'cosyvoice':'qwen_audio',voice_id:'voice-'+id,sample_asset_id:id+100,clone_status:'ready',config:{target_model:model},created_at:'2026-10-10T02:00:00Z'});
 const binding=p=>({id:p.id,profile_id:p.id,model:p.config.target_model,voice_id:p.voice_id,provider:p.provider,status:'ready',rate:1});
 window.fixture={writes:[],audios:[],fail:false,profiles:[profile(41,'海妹',qwen),profile(42,'小悠 Flash',flash),profile(43,'小悠 Plus',plus)],versions:[{id:9,lifecycle_status:'active',speech_config:{rooms:{26:{selected_voice:{source:'clone',profile_id:41,binding_id:41,name:'海妹',voice_id:'voice-41',model:qwen}},25:{selected_voice:{source:'official',voice_id:'longanlingxin',model:qwen}}}}}]};
 window.fixture.bindings=window.fixture.profiles.map(binding);
 class MockAudio extends EventTarget{constructor(src){super();this.duration=12;if(src)this.src=src;window.fixture.audios.push(this)}set src(value){this.value=value;if(value.startsWith('blob:'))queueMicrotask(()=>this.dispatchEvent(new Event('loadedmetadata')))}get src(){return this.value||''}async play(){this.dispatchEvent(new Event('playing'))}pause(){this.dispatchEvent(new Event('pause'))}}
 window.Audio=MockAudio;
 window.fetch=async(url,init={})=>{
  const path=new URL(url,location.origin).pathname;
  if(new URL(url,location.origin).origin!==location.origin)throw Error('Unexpected browser request to supplier');
  const payload=init.body&&typeof init.body==='string'?JSON.parse(init.body):{};
  if(init.method&&init.method!=='GET'){
   window.fixture.writes.push({path,payload,method:init.method});
   if(path.endsWith('/design')||path.endsWith('/clone')){
    if(window.fixture.fail)return Response.json({error:'模拟生成失败，请重试'},{status:502});
    const p=profile(50+window.fixture.profiles.length,payload.name,payload.model);window.fixture.profiles.push(p);window.fixture.bindings.push(binding(p));return Response.json(p,{status:201});
   }
   if(path.endsWith('/media-assets'))return Response.json({asset:{id:701}},{status:201});
   if(path.endsWith('/preview'))return Response.json({audio_url:'/test.wav'});
   if(path.endsWith('/config-versions')){window.fixture.draft={id:10,...payload};return Response.json(window.fixture.draft)}
   if(path.endsWith('/activate')){window.fixture.versions=[{...window.fixture.draft,lifecycle_status:'active'}];return Response.json({active_version_id:10})}
   if(path.endsWith('/voice-binding')){window.fixture.planVoice=window.fixture.bindings.find(b=>b.id===payload.binding_id);return Response.json({id:55,voice_identity:window.fixture.planVoice})}
   if(path.endsWith('/model-preference'))return Response.json(window.fixture.profiles.find(p=>p.id===Number(path.split('/')[6])));
   throw Error('Unexpected mutation: '+path);
  }
  if(path.endsWith('/official-voices'))return Response.json({items:[{id:'longanlingxin',name:'龙安灵心',gender:'女声',description:'知心温暖、自然亲切',tags:['温暖'],model:qwen},{id:'longanlufeng',name:'龙安鲁风',gender:'男声',description:'明亮开朗、自然有活力',tags:['明亮'],model:qwen}]});
  if(path.endsWith('/voice-profiles/quota'))return Response.json({limit:10,used:window.fixture.profiles.length,remaining:10-window.fixture.profiles.length});
  if(path.endsWith('/voice-profiles'))return Response.json(window.fixture.profiles);
  if(path.endsWith('/voice-model-bindings'))return Response.json(window.fixture.bindings);
  if(path.endsWith('/config-versions'))return Response.json(window.fixture.versions);
  return Response.json([]);
 };
 const visible=ref(true),room=ref(26);window.fixture.remount=async()=>{visible.value=false;await nextTick();visible.value=true};window.fixture.setRoom=id=>room.value=id;
 createApp({setup:()=>()=>[visible.value?h(Voice,{roomId:room.value,roomName:'绵都杨鸭子'}):null,h(Feedback)]}).mount('#app');
 </script></body></html>`))
})}}
const server=await createServer({root,configFile:false,plugins:[vue(),plugin],server:{host:'127.0.0.1',port:0},logLevel:'error'});await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try{
 const page=await browser.newPage({viewport:{width:1580,height:1050}}),errors=[];page.on('pageerror',e=>errors.push(e.message))
 await page.goto(server.resolvedUrls.local[0]+'__voice-libraries')
 const model=page.getByRole('combobox',{name:'声音大模型'})
 await page.getByText('龙安灵心',{exact:true}).waitFor()
 await page.screenshot({path:output+'/Qwen声音管理.png',fullPage:true})
 await page.getByRole('button',{name:/我的声音/}).click()
 assert.equal(await page.locator('.custom-voice-row').count(),1)
 assert.match(await page.locator('.custom-voice-row').innerText(),/海妹/)
 await model.selectOption('cosyvoice-v3.5-flash')
 assert.equal(await page.locator('.custom-voice-row').count(),1)
 assert.match(await page.locator('.custom-voice-row').innerText(),/小悠 Flash/)
 assert.equal(await page.locator('.model-picker select').isDisabled(),true)
 assert.equal(await page.evaluate(()=>window.fixture.writes.length),0,'browse model cannot apply or generate voices')
 await page.getByRole('button',{name:/官方声音/}).click()
 await page.getByText(/此模型没有预置官方音色/).waitFor()
 assert.equal(await page.locator('.voice-row').count(),0,'Qwen system voices must not leak into CosyVoice')
 await page.screenshot({path:output+'/CosyVoice官方声音区.png',fullPage:true})
 await page.getByRole('button',{name:'描述生成声音',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'描述生成声音'})
 await dialog.locator('input').fill('暖阳')
 await dialog.locator('textarea').fill('30岁左右的女性，声音温暖明亮，吐字清晰，适合自然的直播讲解。')
 await page.screenshot({path:output+'/CosyVoice声音设计.png',fullPage:true})
 await page.evaluate(()=>window.fixture.fail=true)
 await dialog.getByRole('button',{name:'确定并生成'}).click()
 await dialog.getByRole('alert').waitFor()
 assert.match(await dialog.getByRole('alert').innerText(),/模拟生成失败/)
 assert.equal(await dialog.locator('input').inputValue(),'暖阳','failed design keeps form for retry')
 await page.evaluate(()=>window.fixture.fail=false)
 await dialog.getByRole('button',{name:'确定并生成'}).click()
 await dialog.waitFor({state:'hidden'})
 assert.equal(await page.locator('.custom-voice-row').count(),2)
 let writes=await page.evaluate(()=>window.fixture.writes)
 assert.equal(writes.at(-1).payload.model,'cosyvoice-v3.5-flash')
 assert.equal(writes.at(-1).payload.description.includes('女性'),true)
 const generated=page.locator('.custom-voice-row').filter({hasText:'暖阳'})
 await generated.locator('.preview-button').click()
 await page.locator('.voice-preview-wave-panel.is-playing').waitFor()
 await page.locator('.voice-preview-wave-panel').getByRole('button',{name:'停止',exact:true}).click()
 assert.equal(await page.evaluate(()=>window.fixture.writes.filter(w=>w.path.endsWith('/voice-binding')).length),0,'audition cannot apply voice')
 await generated.getByRole('button',{name:'使用这个声音',exact:true}).click()
 await page.waitForFunction(()=>window.fixture.planVoice?.model==='cosyvoice-v3.5-flash')
 assert.equal(await page.evaluate(()=>window.fixture.versions[0].speech_config.rooms[25].selected_voice.voice_id),'longanlingxin','other room voice preserved')
 await model.selectOption('cosyvoice-v3.5-plus')
 assert.equal(await page.locator('.custom-voice-row').count(),1,'Flash voice does not leak into Plus')
 await page.locator('.upload-sample-button').click()
 const upload=page.getByRole('dialog',{name:'上传声音样本'})
 assert.match(await upload.locator('.dialog-model-label').innerText(),/Plus/)
 await upload.locator('input:not([type=file])').fill('海妹 Plus')
 await upload.locator('input[type=file]').setInputFiles({name:'sample.wav',mimeType:'audio/wav',buffer:Buffer.alloc(100)})
 await upload.getByRole('button',{name:'确定并生成'}).click()
 await upload.waitFor({state:'hidden'})
 writes=await page.evaluate(()=>window.fixture.writes)
 assert.equal(writes.at(-1).payload.model,'cosyvoice-v3.5-plus')
 assert.equal(writes.at(-1).payload.sample_asset_id,701)
 await page.screenshot({path:output+'/CosyVoice我的声音.png',fullPage:true})
 await page.evaluate(()=>window.fixture.remount())
 await model.waitFor()
 assert.equal(await model.inputValue(),'cosyvoice-v3.5-plus','library preference survives remount')
 await page.evaluate(()=>window.fixture.setRoom(25))
 await page.waitForFunction(()=>document.querySelector('.voice-model-bar select')?.value==='qwen_audio')
 assert.equal(await page.locator('.custom-voice-row').count(),0,'room switch resets to official view')
 await page.setViewportSize({width:760,height:1100})
 await model.selectOption('cosyvoice-v3.5-flash')
 const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth)
 assert.equal(overflow,false,'responsive design must fit viewport')
 assert.deepEqual(errors,[])
 await writeFile(output+'/声音模型接入验证.json',JSON.stringify({passed:true,checks:['Qwen兼容','模型浏览不修改当前声音','Flash/Plus声音库隔离','声音设计失败重试','设计/上传提交所选模型','试听不应用','应用模型绑定','其他房间保留','选择记忆','窄屏布局'],writes},null,2))
 console.log('PASS: model libraries, exact voice binding, design/retry, upload target, room preservation, preference, responsive layout')
}finally{await browser.close();await server.close()}
