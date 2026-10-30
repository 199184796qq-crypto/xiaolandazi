// Complete real strategy view, local API/OSS fixtures, no paid calls or production writes.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'vite'
const { chromium } = createRequire(new URL('../../collector-worker/package.json', import.meta.url))('playwright-core')
const output = process.env.RECORDING_SAVE_TEST_OUTPUT || 'E:/直播伴播-local/plan-save-recording-20261030/browser'
await mkdir(output, { recursive: true })
let fixture = String.raw`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" href="data:,"><div id="app"></div><script type="module">
import {createApp,h} from 'vue';import {createRouter,createMemoryHistory} from 'vue-router';import View from '/src/views/LiveStrategyView.vue';import {applySession} from '/src/session.ts';import '/src/style.css';
const params=new URLSearchParams(location.search), plans=[7,8].map(id=>({id,tenant_id:14,name:'测试方案'+id,status:'active',room_count:0,term_count:0}));
const transcript='这是一段用于检查长录音保存完整性的测试话术。'.repeat(750);
const timeline=Array.from({length:251},(_,index)=>({segment_id:'s'+index,index:index+1,start_ms:index*9600,end_ms:(index+1)*9600,text:transcript.slice(index*60,index*60+60),safe_cut:true}));
const draft={audio_asset_id:26103,audio_url:'/api/v1/live/media-assets/26103/content',duration_ms:251*9600,transcript,timeline,srt:timeline.map((s,i)=>(i+1)+'\n00:00:00,000 --> 00:00:09,600\n'+s.text+'\n').join('\n'),safe_points:timeline.map(s=>({id:'p'+s.index,cut_ms:s.end_ms,score:95,grade:'A',kind:'SENTENCE',sentence_id:s.segment_id,left_preview:s.text,next_preview:'后文',topics:[]})),asset_manifest:{version:'v3',timeline_object_key:'fixture/timeline',safe_points_object_key:'fixture/points',subtitle_object_key:'fixture/srt'},clone_error:'模拟自动克隆失败，原始声音仍有效'};
const jobs=[{id:'done',status:'ready',result:draft}];
const official=[{id:'longanlingxin',name:'龙安灵心',provider:'aliyun_qwen',model:'qwen-audio-3.0-tts-plus',tags:['直播默认']}];
const bindings=[{id:21,profile_id:13,status:'ready',provider:'qwen_audio',model:'qwen-audio-3.1-tts-flash',voice_id:'fixture-clone-21'},{id:20,profile_id:12,status:'disabled',provider:'qwen_audio',model:'qwen-audio-3.1-tts-flash',voice_id:'fixture-clone-20'}];
window.fixture={calls:[],draft,official,bindings,mode:'user_audio',jobs:params.has('empty')?[]:jobs,holdWorkspace:params.has('delayed'),failSave:false,failRead:params.has('read-error'),saved:JSON.parse(sessionStorage.getItem('saved-recording')||'null')};
window.fetch=async(url,init={})=>{
 const f=window.fixture,u=new URL(url,location.origin),p=u.pathname,method=init.method||'GET',body=init.body?JSON.parse(init.body):null,plan=Number(p.match(/live-agent-plans\/(\d+)/)?.[1]||0);
 f.calls.push({path:p,method,body});
 if(method==='POST'&&p.endsWith('/fact-gap-check'))return Response.json({items:[],issues:[],gaps:[],checks:[]});
 if(method==='POST'&&p.endsWith('/uploads'))return Response.json({id:'new-upload',uploads:[{url:'https://fixture-oss.invalid/upload',fields:{key:'fixture'}}]});
 if(method==='POST'&&p.endsWith('/complete'))return Response.json({id:'new-upload',status:'ready',result:draft});
 if(method==='POST'&&p.endsWith('/versions')){
   f.payload=body;
   if(f.holdSave){f.holdSave=false;await new Promise(r=>f.releaseSave=r)}
   if(f.failSave){f.failSave=false;return Response.json({error:'模拟保存失败，请重试'},{status:503})}
   f.saved={...body,id:701,plan_id:plan,tenant_id:14,version_no:(f.saved?.version_no||0)+1,lifecycle_status:'published',created_at:new Date().toISOString(),updated_at:new Date().toISOString()};sessionStorage.setItem('saved-recording',JSON.stringify(f.saved));return Response.json(f.saved);
 }
 if(method!=='GET')throw Error('Unexpected mutation '+method+' '+p);
 if(p.endsWith('/workspace')){
   const response=f.saved&&plan===7?{version:f.saved,source:'published',recording_uploads:f.jobs}:{version:null,source:'',recording_uploads:plan===7?f.jobs:[]};
   if(f.holdWorkspace&&plan===7){f.holdWorkspace=false;await new Promise(r=>f.releaseWorkspace=r)}
   if(f.failRead)return Response.json({error:'读取已完成录音失败，请重试；无需重新上传'},{status:503});return Response.json(response);
 }
 if(p==='/api/v1/rooms')return Response.json({items:[]});
 if(p.endsWith('/ui-preferences'))return Response.json({});
 if(p==='/api/v1/live-agent-plans')return Response.json({items:plans});
 if(p.endsWith('/voice-rate'))return Response.json({tenant_id:14,plan_id:plan,rate:1.2});
 if(p.endsWith('/content-mode'))return Response.json({content_mode:f.mode,available_modes:['user_audio','ai_pregenerated','ai_dynamic'],dynamic_authorized:true});
 if(p.endsWith('/content-pools'))return Response.json({state:'synced',changes:[],products:[],benefits:[],pools:[],set_id:9,saved_set_id:9,job:{}});
 if(p.endsWith('/realtime-options'))return Response.json({buffer_seconds:60,body_min:2,body_max:4,max_chars:300});
 if(p.endsWith('/versions'))return Response.json({items:f.saved&&plan===7?[f.saved]:[]});
 if(p.endsWith('/official-voices'))return Response.json({configured:true,items:f.official});
 if(p.endsWith('/voice-model-bindings'))return Response.json(f.bindings);
 return Response.json({items:[],plugins:[],checks:[],groups:[],catalog:[],profiles:[],version:null,source:null,addressing:[],greeting:'你好'});
};
applySession({actor:{user_id:14,tenant_id:14,role:'customer',username:'test'},tenants:[{id:14,name:'测试'}],environment:'test'});
const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:View}]});await router.push('/');await router.isReady();createApp({render:()=>h(View)}).use(router).mount('#app');</script></html>`
if (process.env.RECORDING_SAVE_READY_RECEIPT) {
 const audit = JSON.parse(await readFile(process.env.RECORDING_SAVE_READY_RECEIPT, 'utf8'))
 const receipt = audit.recent_uploads.find(job => job.status === 'ready' && job.result)
 assert.ok(receipt, 'local read-only ready receipt required')
 fixture = fixture.replace(/const draft=\{[^\n]+\};/, () => 'const draft=' + JSON.stringify(receipt.result).replace(/</g, '\\u003c') + ';')
}
const useTencent = process.env.RECORDING_SAVE_OFFICIAL_LIBRARY === 'tencent-premium'
if (useTencent) fixture = fixture.replace(/const official=\[[^\n]+\];/, "const official=[{id:'101030',name:'智柯',provider:'tencent_tts',model:'tencent-premium',tags:['精品','通用']}];")
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom', plugins: [{
  name: 'recording-save-inspect', enforce: 'pre', transform(code,id) {
    if (!id.endsWith('/src/views/LiveStrategyView.vue')) return
    return code.replace('</script>', `window.fixture.view={
      state:()=>({plan:currentRoomPlanId.value,hint:fullShowSaveBlockedReason.value,tabs:recordingTabs.value,draft:customMainlineDraft.value,formal:fullShowResult.value,voice:currentVoiceIdentity.value,error:fullShowVersionError.value,loading:fullShowWorkspaceLoading.value,textLimit:recordingTabTextLimit.value,notice:fullShowNotice.value}),
      chooseVoice:handleLiveVoiceChanged,
      plan:id=>handlePlanSwitch({target:{value:String(id)}}),reload:loadFullShowWorkspace,remove:removeRecordingOriginal,
      mode:value=>recordingVoiceMode.value=value,
      upload:()=>{customMainlineFiles.value=[new File(['fixture'],'test.wav',{type:'audio/wav'})];customMainlineDurationMS.value=1000;return uploadCustomMainline()},
      edit:text=>{activeRecordingTab.value.text=text},
    };</script>`)
  },
}] })
server.middlewares.use(async(req,res,next)=>{
  if (!req.url.startsWith('/__recording-save')) return next()
  res.setHeader('Content-Type','text/html;charset=utf-8');res.end(await server.transformIndexHtml(req.url,fixture))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const checks=[],errors=[],external=[]
try {
 const context=await browser.newContext({viewport:{width:1600,height:1000},reducedMotion:'reduce'})
 await context.route('**/*',async route=>{
   const u=new URL(route.request().url());if(u.hostname==='127.0.0.1')return route.continue()
   if(u.hostname==='fixture-oss.invalid')return route.fulfill({status:204,headers:{'Access-Control-Allow-Origin':'*'}})
   external.push(u.hostname);return route.abort()
 })
 const page=await context.newPage();page.setDefaultTimeout(15000);page.on('pageerror',e=>errors.push(e.message))
 const base=server.resolvedUrls.local[0]+'__recording-save',save=page.locator('#plan-save-button')
 const ready=()=>page.waitForFunction(()=>window.fixture?.view&&!window.fixture.view.state().loading&&window.fixture.view.state().plan===7)
 const state=()=>page.evaluate(()=>window.fixture.view.state())
 const writes=()=>page.evaluate(()=>window.fixture.calls.filter(x=>x.method!=='GET'&&!x.path.endsWith('/fact-gap-check')))
 await page.goto(base);await ready();await page.waitForFunction(()=>document.querySelector('#plan-save-button')&&!document.querySelector('#plan-save-button').disabled)
 let s=await state();assert.equal(s.tabs.length,1);assert.equal(s.tabs[0].text.length,await page.evaluate(()=>window.fixture.draft.transcript.length));assert.equal(s.formal,null);assert.equal(s.textLimit,80000);assert.equal((await writes()).length,0)
 checks.push('完成录音可从无已保存版本的工作区恢复；长文/原音/SRT齐全、无生成预览也能保存，页面读取零付费/写入')
 await page.evaluate(()=>{window.fixture.failSave=true;window.fixture.holdSave=true});await save.click();await page.waitForFunction(()=>!!window.fixture.releaseSave)
 assert.equal(await save.isDisabled(),true);await save.evaluate(el=>el.click());assert.equal((await writes()).filter(x=>x.path.endsWith('/versions')).length,1)
 await page.evaluate(()=>window.fixture.releaseSave());await page.getByRole('alert').filter({hasText:'模拟保存失败'}).waitFor();assert.equal(await save.isEnabled(),true)
 assert.equal((await state()).tabs[0].audioAssetIds[0],26103)
 checks.push('显式保存先准备正式音轨；防重复、失败保留录音可重试，克隆失败不阻塞原音')
 await save.click();await page.getByText('本次保存成功 V1。',{exact:false}).waitFor()
 const payload=await page.evaluate(()=>window.fixture.payload)
 assert.equal(payload.room_id,0);assert.equal(payload.tenant_id,14);assert.equal(payload.generation_context.content_mode,'user_audio');assert.equal(payload.generation_context.recording_voice_mode,'original')
 assert.equal(payload.variants.length,1);assert.equal(payload.variants[0].variant_key,'CUSTOM');assert.equal(payload.variants[0].audio_asset_id,26103);assert.equal(payload.variants[0].timeline.length,251);assert.equal(payload.variants[0].safe_points.length,251);assert.equal(payload.voice_identity.voice_id,useTencent?'101030':'longanlingxin');assert.equal(payload.voice_identity.model,useTencent?'tencent-premium':'qwen-audio-3.0-tts-plus');assert.equal(payload.voice_identity.rate,1.2)
 assert.equal(payload.generation_context.recording_script.tabs[0].audio_asset_ids[0],26103);assert.equal(payload.generation_context.recording_original_sources.length,1)
 const bytes=Buffer.byteLength(JSON.stringify(payload));assert.ok(bytes>64*1024&&bytes<960*1024)
 await writeFile(output+'/payload.json',JSON.stringify(payload))
 await page.locator('.plan-configuration-header').screenshot({path:output+'/saved-desktop.png'}).catch(()=>page.screenshot({path:output+'/saved-desktop.png'}))
 checks.push('约40分钟/251句载荷超过旧64KB仍提交成功；真实序列化完整保留正文/原音/时间轴/切点/字幕，互动声音与1.20语速不被上传覆盖')
 await page.reload();await ready();await page.waitForFunction(()=>window.fixture.view.state().formal?.variants?.length===1)
 s=await state();assert.equal(s.tabs.length,1);assert.equal(s.tabs[0].audioAssetIds[0],26103);assert.equal((await writes()).length,0)
 await page.evaluate(()=>window.fixture.view.remove());assert.equal(await save.isDisabled(),true);assert.equal((await state()).draft,null)
 await page.evaluate(()=>window.fixture.view.mode('stream_clone'));await page.waitForFunction(()=>!document.querySelector('#plan-save-button').disabled);await save.click();await page.getByText('本次保存成功 V2。',{exact:false}).waitFor()
 await page.reload();await ready();s=await state();assert.equal(s.draft,null);assert.equal(s.tabs[0].audioAssetIds.length,0);assert.equal(s.tabs.length,1)
 checks.push('保存后刷新正确恢复；保存过的删除不因历史完成回执复活；AI文本模式保留原保存行为')
 await page.evaluate(()=>sessionStorage.removeItem('saved-recording'));await page.goto(base+'?empty');await ready();assert.equal(await save.isDisabled(),true)
 await page.evaluate(()=>window.fixture.view.upload());await page.waitForFunction(()=>!document.querySelector('#plan-save-button').disabled)
 s=await state();assert.equal(s.tabs.filter(x=>x.audioAssetIds.length).length,1);assert.equal(s.formal,null)
 await save.click();await page.getByText('本次保存成功 V1。',{exact:false}).waitFor();assert.equal((await writes()).filter(x=>x.path.endsWith('/versions')).length,1)
 checks.push('当前页面直接OSS上传并处理成功后立即可保存，无需先生成AI文稿或付费重做克隆')
 await page.evaluate(()=>sessionStorage.removeItem('saved-recording'));await page.goto(base+'?delayed');await page.waitForFunction(()=>!!window.fixture.releaseWorkspace)
 await page.evaluate(()=>window.fixture.view.plan(8));await page.waitForFunction(()=>window.fixture.view.state().plan===8&&!window.fixture.view.state().loading)
 await page.evaluate(()=>window.fixture.releaseWorkspace());await page.waitForTimeout(150)
 s=await state();assert.equal(s.plan,8);assert.equal(s.draft,null);assert.equal(s.tabs[0].audioAssetIds.length,0);assert.equal(await save.isDisabled(),true)
 checks.push('方案切换后旧录音恢复响应丢弃，禁止跨方案回显或保存')
 await page.evaluate(()=>localStorage.clear());await page.goto(base+'?read-error');await ready();await page.waitForFunction(()=>window.fixture.view.state().hint.includes('恢复'));assert.equal(await save.isDisabled(),true);assert.equal((await writes()).length,0)
 checks.push('读取失败明确阻止保存，不当成录音不存在、不自动上传或重试付费处理')
 await page.evaluate(()=>sessionStorage.removeItem('saved-recording'));await page.goto(base);await ready();await page.waitForFunction(()=>!document.querySelector('#plan-save-button').disabled)
 assert.equal((await state()).voice.voice_id,useTencent?'101030':'longanlingxin');assert.equal((await writes()).length,0)
 await page.evaluate(()=>window.fixture.view.chooseVoice({name:'已失效旧声音',version:'V1',source:'official',provider:'aliyun_qwen',model:'removed-model',voice_id:'removed-voice'}));await save.click()
 await page.waitForFunction(()=>window.fixture.view.state().error.includes('官方声音'))
 assert.equal((await state()).voice.voice_id,'removed-voice');assert.equal((await writes()).filter(x=>x.path.endsWith('/versions')).length,0)
 await page.evaluate(()=>{const b=window.fixture.bindings[0];window.fixture.view.chooseVoice({name:'新互动声音',version:'V1',source:'clone',provider:b.provider,model:b.model,voice_id:b.voice_id,binding_id:b.id,profile_id:b.profile_id})})
 assert.equal((await writes()).length,0);assert.match((await state()).notice,/点击.*保存方案/)
 await save.click();await page.getByText('本次保存成功 V1。',{exact:false}).waitFor()
 assert.equal(await page.evaluate(()=>window.fixture.payload.voice_identity.binding_id),21)
 assert.equal(await page.evaluate(()=>window.fixture.payload.variants[0].audio_asset_id),26103)
 await page.reload();await ready();assert.equal((await state()).voice.binding_id,21);assert.equal((await writes()).length,0)
 checks.push('默认声音跟随当前官方音库；失效的明确选择不被自动替换；无房间改选克隆声音不发房间发布请求，显式保存后刷新保持且主线原音不变')
 const helper=await page.evaluate(async()=>{
   const {planDefaultVoice,planVoiceUnavailable}=await import('/src/planAssets.ts'),f=window.fixture,v=f.official[0]
   return {noDefault:planDefaultVoice(f.official,false),mismatch:planVoiceUnavailable({name:v.name,source:'official',provider:v.provider,model:'wrong-model',voice_id:v.id},[],f.official,true),disabled:planVoiceUnavailable({name:'失效克隆',source:'clone',provider:f.bindings[1].provider,model:f.bindings[1].model,voice_id:f.bindings[1].voice_id,binding_id:20,profile_id:12},f.bindings,f.official,true)}
 })
 assert.equal(helper.noDefault,null);assert.match(helper.mismatch,/不可用/);assert.match(helper.disabled,/失效|停用/)
 checks.push('未配置官方库不伪造默认声音；同音色错误模型、已停用克隆均保持阻止保存')
 assert.deepEqual(errors,[]);assert.deepEqual(external,[])
 await writeFile(output+'/checks.json',JSON.stringify({passed:true,checks,payload_bytes:bytes,errors,external},null,2));console.log(JSON.stringify({passed:true,checks,payload_bytes:bytes}))
} finally {await browser.close();await server.close()}
