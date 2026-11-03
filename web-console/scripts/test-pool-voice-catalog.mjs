import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from '../node_modules/vite/dist/node/index.js'
import vue from '../node_modules/@vitejs/plugin-vue/dist/index.mjs'
const { chromium } = createRequire(new URL('../../collector-worker/package.json', import.meta.url))('playwright-core')
const out = process.env.POOL_CATALOG_OUTPUT || 'E:/直播伴播-local/pool-voice-catalog-20261101/browser'
await mkdir(out, { recursive: true })
const definitions = String.raw`
const official={id:'longan',name:'标准主播',provider:'aliyun_qwen',model:'qwen-audio-3.1-tts-flash',tags:['直播默认'],gender:'男',description:'本地标准声音'};
const fire={id:'S_pool_source',name:'晚晴',provider:'volcengine',model:'seed-icl-2.0',tags:['高仿真'],gender:'女声',description:'后台已配置复刻音色'};
const standard={name:official.name,version:'V1',source:'official',provider:official.provider,voice_id:official.id,model:official.model,rate:1.13,tts_pool:'standard'};
const high={name:fire.name,version:'V1',source:'official',provider:fire.provider,voice_id:fire.id,model:fire.model,rate:1.13,tts_pool:'high_fidelity'};
const profiles=[1,2,3].map(id=>({id,tenant_id:14,name:'标准私有声音'+id,provider:'aliyun_qwen_clone',voice_id:'standard'+id,clone_status:'ready',config:{target_model:'qwen-audio-3.1-tts-flash'},created_at:new Date().toISOString()}));
class FakeAudio extends EventTarget { constructor(src){super();this.src=src} pause(){} async play(){this.dispatchEvent(new Event('playing'))} }
window.Audio=FakeAudio;
`
const wrapper = `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><div id="app"></div><script type="module">
import{createApp,ref,h}from'vue';import View from'/src/components/LiveVoiceCenter.vue';import{session}from'/src/session.ts';import'/src/style.css';${definitions}
session.bootstrap={actor:{user_id:2,tenant_id:14,role:'customer'},staff_access:{is_super_admin:false}};
window.fixture={calls:[],empty:false,selected:[],profiles,slow:false};
window.fetch=async(url,init={})=>{const u=new URL(url,location.origin),p=u.pathname,f=window.fixture;const body=init.body?JSON.parse(init.body):null;f.calls.push({p,q:u.search,method:init.method||'GET',body});
if(p.endsWith('/official-voices')){if(u.searchParams.get('pool')==='high_fidelity'){if(f.slow)await new Promise(resolve=>f.release=resolve);return Response.json({configured:!f.empty,items:f.empty?[]:[{...fire,gender:f.neutral?'':fire.gender}]})}return Response.json({configured:true,items:[official]})}
if(p.endsWith('/voice-profiles/quota'))return Response.json({limit:3,used:3,remaining:0});
if(p.endsWith('/voice-profiles'))return Response.json(f.profiles);
if(p.endsWith('/voice-model-bindings'))return Response.json([]);
if(p.endsWith('/voice-settings'))return Response.json({custom_model:'qwen-audio-3.1-tts-flash',can_design:true});
if(p.endsWith('/preview'))return Response.json({audio_url:'/fixture.wav'});
throw Error('Unexpected API '+p)};
createApp({setup(){const selected=ref(standard),scope=ref('plan4');window.fixture.current=()=>selected.value;window.fixture.set=(v,s)=>{selected.value=v;scope.value=s};window.fixture.standard=standard;window.fixture.high=high;return()=>h(View,{planId:4,planName:'测试方案',scopeKey:scope.value,selectedVoice:selected.value,onPoolSelected:p=>selected.value={...selected.value,tts_pool:p},onSelected:v=>{selected.value=v;window.fixture.selected.push(v)}})}}).mount('#app');</script>`
const base = (await readFile(new URL('./test-plan-prompt-editor.mjs', import.meta.url), 'utf8')).replace(/\r\n/g,'\n')
let parent = base.split('const fixture=String.raw`')[1].split('`\nconst server=')[0]
const setup = String.raw`
import{emptyPlanPromptSections,compilePlanPrompts}from'/src/planPrompt.ts';
${definitions}
const previousFetch=window.fetch;
const versions={4:{id:40,tenant_id:14,plan_id:4,version_no:1,lifecycle_status:'published',duration_minutes:90,round_minutes:7,voice_identity:standard,variants:[],generation_context:{content_mode:'ai_dynamic',anchor_generation_options:{buffer_seconds:120,body_min:2,body_max:4,max_chars:300}}},5:{id:50,tenant_id:14,plan_id:5,version_no:1,lifecycle_status:'published',duration_minutes:90,round_minutes:7,voice_identity:standard,variants:[],generation_context:{content_mode:'ai_dynamic',anchor_generation_options:{buffer_seconds:120,body_min:2,body_max:4,max_chars:300}}}};
for(const v of Object.values(versions)) v.generation_context={content_mode:'user_audio',recording_voice_mode:'stream_clone',recording_script:{version:2,tabs:[{tab_id:'a',title:'测试组',entries:[{entry_id:'1',text:'测试话术。'.repeat(100)}]}]}};
window.fixture.versions=versions;window.fixture.catalogEmpty=false;
window.fetch=async(url,init={})=>{const u=new URL(url,location.origin),p=u.pathname,method=init.method||'GET',body=init.body?JSON.parse(init.body):null,id=Number(p.match(/live-agent-plans\/(\d+)/)?.[1]||0),f=window.fixture;
if(p.endsWith('/save-check'))return Response.json({editable:true});
if(p.endsWith('/content-mode'))return Response.json({content_mode:'user_audio',available_modes:['user_audio'],dynamic_authorized:false});
if(p.endsWith('/prompt')){const sections=body?.sections||emptyPlanPromptSections(),compiled=compilePlanPrompts(sections);return Response.json({sections,...compiled,prompt_md:compiled.interaction_prompt_md})}
if(p.endsWith('/voice-profiles'))return Response.json(profiles);
if(p.endsWith('/voice-profiles/quota'))return Response.json({limit:3,used:3,remaining:0});
if(p.endsWith('/voice-settings'))return Response.json({custom_model:'qwen-audio-3.1-tts-flash',can_design:true});
if(p.endsWith('/voice-model-bindings'))return Response.json([]);
if(p.endsWith('/official-voices'))return Response.json({configured:!f.catalogEmpty,items:u.searchParams.get('pool')==='high_fidelity'?(f.catalogEmpty?[]:[fire]):[official]});
if(p.endsWith('/workspace'))return Response.json({version:versions[id],source:'published',checks:[]});
if(p.endsWith('/versions')){if(method==='POST'){f.calls.push({path:p,method,body});versions[id]={...versions[id],...body,id:versions[id].id+1,version_no:versions[id].version_no+1,lifecycle_status:'published',generation_context:{...body.generation_context,anchor_generation:{config:body.generation_context.anchor_generation_options}}};return Response.json(versions[id])}return Response.json({items:[versions[id]]})}
return previousFetch(url,init)};
`
parent = parent.replace('applySession(',setup+'\napplySession(')
const server = await createServer({root:fileURLToPath(new URL('..',import.meta.url)),configFile:false,cacheDir:out+'/vite-cache',plugins:[{name:'expose-view',enforce:'pre',transform(code,id){if(!id.endsWith('/src/views/LiveStrategyView.vue'))return;return code.replace('</script>','window.fixture.view={plan:()=>currentRoomPlanId.value,setPlan:id=>handlePlanSwitch({target:{value:String(id)}}),busy:()=>savePlanOneClickBusy.value,blocked:()=>fullShowSaveBlockedReason.value,hydrating:()=>fullShowWorkspaceHydrating.value,voice:()=>currentVoiceIdentity.value};</script>')}},vue(),{name:'local-fixtures',configureServer(dev){dev.middlewares.use(async(req,res,next)=>{const value={'/__voice':wrapper,'/__parent':parent}[req.url?.split('?')[0]];if(!value)return next();res.setHeader('Content-Type','text/html;charset=utf-8');res.end(await dev.transformIndexHtml(req.url,value))})}}],server:{host:'127.0.0.1',port:0},logLevel:'error'})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const checks=[],errors=[],external=[];let page
try{
 const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'})
 await context.route('**/*',route=>{if(new URL(route.request().url()).hostname==='127.0.0.1')return route.continue();external.push(route.request().url());return route.abort()})
 page=await context.newPage();page.setDefaultTimeout(20000);page.on('pageerror',e=>errors.push(e.message))
 const url=`http://127.0.0.1:${server.httpServer.address().port}`
 await page.goto(url+'/__voice');const picker=page.locator('.voice-pool-picker select');await page.getByText('标准主播',{exact:true}).first().waitFor();await page.waitForFunction(()=>!document.querySelector('.voice-pool-picker select').disabled)
 await page.getByRole('button',{name:/我的声音/}).click();assert.match(await page.locator('.quota-chip').innerText(),/3\s*\/\s*3/)
 assert.equal(await page.locator('.custom-voice-row').count(),3)
 const callsBefore=await page.evaluate(()=>window.fixture.calls.length)
 await picker.selectOption('high_fidelity');await page.getByText('晚晴',{exact:true}).waitFor()
 const after=await page.evaluate(n=>window.fixture.calls.slice(n),callsBefore)
 assert.deepEqual(after.map(c=>c.p),['/api/v1/live/official-voices']);assert.equal(after[0].q,'?pool=high_fidelity')
 assert.equal(await page.locator('.quota-chip').count(),0);assert.equal(await page.getByRole('button',{name:/上传声音样本|描述生成声音|导入已有音色/}).count(),0)
 assert.equal(await page.getByRole('navigation',{name:'声音库'}).count(),0)
 assert.equal(await page.evaluate(()=>window.fixture.calls.some(c=>c.method!=='GET')),false)
 checks.push('标准池3/3名额不影响高仿真；切池仅拉取可用目录，不读取私有名额、不训练或自动试听')
 await page.getByRole('button',{name:'使用这个声音',exact:true}).click()
 const selected=await page.evaluate(()=>window.fixture.current());assert.equal(selected.source,'official');assert.equal(selected.provider,'volcengine');assert.equal(selected.tts_pool,'high_fidelity');assert.equal(selected.voice_id,'S_pool_source');assert.equal(selected.profile_id,undefined);assert.equal(selected.binding_id,undefined)
 assert.equal(await page.getByText(/已切换语音池，请从下方/).count(),0)
 await page.getByRole('button',{name:/^▶试听$/}).click();await page.locator('.voice-preview-wave-panel.is-playing').waitFor()
 const preview=await page.evaluate(()=>window.fixture.calls.find(c=>c.method==='POST'));assert.equal(preview.p,'/api/v1/live/official-voices/S_pool_source/preview')
 assert.equal(preview.body.text,'你好，我是你的主播，欢迎为你效劳。')
 checks.push('点击明确选择高仿真完整身份；仅手工试听走既有声音预览入口，无私有档案或克隆绑定')
 await page.getByRole('button',{name:'停止',exact:true}).click()
 await page.evaluate(()=>window.fixture.set({...window.fixture.high,name:'高仿真音色 pool_source'},'legacy-label'))
 await page.getByRole('button',{name:'当前使用',exact:true}).waitFor()
 assert.equal(await page.locator('.voice-avatar[aria-label="女声"]').count(),1)
 assert.equal(await page.getByText('高仿真音色 pool_source',{exact:true}).count(),0)
 assert.equal(await page.evaluate(()=>window.fixture.current().name),'高仿真音色 pool_source')
 assert.ok((await page.getByText('晚晴',{exact:true}).count())>=2)
 checks.push('旧方案ID拼接名称由最新目录覆盖显示；女声头像稳定，展示读取不会偷偷改方案声音快照')
 await page.evaluate(()=>{window.fixture.neutral=true;window.fixture.set(window.fixture.high,'unknown-gender')})
 await page.locator('.voice-avatar[aria-label="声音"]').waitFor()
 assert.equal(await page.locator('.voice-avatar[aria-label="男声"]').count(),0)
 assert.equal(await page.locator('.voice-avatar[aria-label="女声"]').count(),0)
 checks.push('未知性别使用中性麦克风图标，不默认男声、不按列表位置猜年龄')
 await page.evaluate(()=>{window.fixture.neutral=false;window.fixture.set(window.fixture.high,'female-again')})
 await page.locator('.voice-avatar[aria-label="女声"]').waitFor()
 await page.screenshot({path:out+'/high-desktop.png',fullPage:true});await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);await page.screenshot({path:out+'/high-mobile.png',fullPage:true})
 await page.evaluate(()=>{window.fixture.empty=true;window.fixture.set(window.fixture.standard,'new-standard')});await page.waitForFunction(()=>!document.querySelector('.voice-pool-picker select').disabled);await picker.selectOption('high_fidelity');await page.getByText('暂无可用高仿真音色，请联系后台配置。',{exact:true}).waitFor();assert.equal(await page.getByRole('button',{name:'使用这个声音',exact:true}).count(),0)
 checks.push('目录为空明确提示，不展示旧池音色、不自动换声音')
 await page.evaluate(()=>{window.fixture.empty=false;window.fixture.set(window.fixture.standard,'race-standard')});await page.waitForFunction(()=>!document.querySelector('.voice-pool-picker select').disabled);await page.evaluate(()=>window.fixture.slow=true);await picker.selectOption('high_fidelity');await page.waitForFunction(()=>typeof window.fixture.release==='function');await page.evaluate(()=>{window.fixture.set(window.fixture.standard,'race-new-plan');window.fixture.release()});await page.getByText('标准主播',{exact:true}).first().waitFor();assert.equal(await picker.inputValue(),'standard');assert.equal(await page.getByText('晚晴',{exact:true}).count(),0)
 checks.push('迟到的高仿真目录不会覆盖已切回的标准池或另一方案')
 await page.setViewportSize({width:1440,height:1000});await page.goto(url+'/__parent');await page.waitForFunction(()=>window.fixture?.view?.plan()===4&&!window.fixture.view.hydrating()&&!window.fixture.view.blocked());await page.getByRole('button',{name:'声音',exact:true}).click();await page.waitForFunction(()=>!document.querySelector('.voice-pool-picker select')?.disabled)
 await page.locator('.voice-pool-picker select').selectOption('high_fidelity');await page.getByText('晚晴',{exact:true}).waitFor();await page.getByRole('button',{name:'选择声音',exact:true}).click();await page.locator('#plan-save-button').click();await page.waitForFunction(()=>!window.fixture.view.busy()&&window.fixture.calls.some(c=>c.method==='POST'&&c.path.endsWith('/versions')))
 const saved=await page.evaluate(()=>window.fixture.calls.filter(c=>c.method==='POST'&&c.path.endsWith('/versions')).at(-1).body.voice_identity)
 assert.equal(saved.voice_id,'S_pool_source');assert.equal(saved.source,'official');assert.equal(saved.provider,'volcengine');assert.equal(saved.model,'seed-icl-2.0');assert.equal(saved.tts_pool,'high_fidelity');assert.equal(saved.profile_id,undefined);assert.equal(saved.binding_id,undefined)
 for(const id of [5,4]){await page.evaluate(id=>window.fixture.view.setPlan(id),id);await page.waitForFunction(id=>window.fixture.view.plan()===id&&!window.fixture.view.hydrating()&&window.fixture.view.voice().tts_pool===(id===4?'high_fidelity':'standard'),id)}
 assert.equal(await page.locator('.voice-pool-picker select').inputValue(),'high_fidelity');await page.getByRole('button',{name:'✓ 已选择',exact:true}).waitFor()
 checks.push('实际方案父页完整保存高仿真池身份；切换方案后原样回显，原标准方案不变')
 await page.evaluate(()=>window.fixture.catalogEmpty=true);const beforeSaves=await page.evaluate(()=>window.fixture.calls.filter(c=>c.method==='POST'&&c.path.endsWith('/versions')).length);await page.locator('#plan-save-button').click();await page.waitForFunction(()=>!window.fixture.view.busy());assert.equal(await page.evaluate(()=>window.fixture.calls.filter(c=>c.method==='POST'&&c.path.endsWith('/versions')).length),beforeSaves)
 checks.push('保存前重新读取目录，音色停用时拒绝新版本，保留原方案')
 assert.deepEqual(errors,[]);assert.deepEqual(external,[])
 await writeFile(out+'/result.json',JSON.stringify({passed:true,checks,errors,external},null,2));console.log(JSON.stringify({passed:true,checks}))
}catch(error){if(page){await page.screenshot({path:out+'/failure.png',fullPage:true});await writeFile(out+'/failure.txt',String(error)+'\n'+await page.locator('body').innerText()+'\n'+JSON.stringify(errors))}throw error}finally{await browser.close();await server.close()}
