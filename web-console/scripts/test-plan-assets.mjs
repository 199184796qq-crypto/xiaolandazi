// Real strategy view + real voice centre; live-start fixture uses the actual
// RoomDetail handlers and selector. Every network request is intercepted locally.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
import ts from 'typescript'
const { chromium } = createRequire(new URL('../../collector-worker/package.json', import.meta.url))('playwright-core')
const output = process.env.PLAN_TEST_OUTPUT || 'E:/直播伴播-local/plan-account-assets-20261028/tests'
await mkdir(output, { recursive: true })
const strategy = await readFile(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
const room = await readFile(new URL('../src/views/RoomDetailView.vue', import.meta.url), 'utf8')
const agent = await readFile(new URL('../src/components/SystemAgentLayer.vue', import.meta.url), 'utf8')
const voice = await readFile(new URL('../src/components/LiveVoiceCenter.vue', import.meta.url), 'utf8')
for (const source of [strategy, room, agent]) {
  assert.ok(!source.includes('bindRoomLiveAgentPlan'), 'No plan binding mutations or imports')
  assert.ok(!source.includes('unbindRoomLiveAgentPlan'))
}
for (const removed of ['confirm_live_plan_bind', 'confirm_live_plan_unbind']) assert.ok(!agent.includes(removed))
for (const removed of ['getLiveAgentConfigVersions', 'createLiveAgentConfigDraft', 'activateLiveAgentConfigVersion', 'publishLiveAgentVoiceBinding']) assert.ok(!voice.includes(removed), 'Voice selection must not write room config')
function functions(source, names) {
  const script = source.slice(source.indexOf('<script setup lang="ts">') + 24, source.indexOf('</script>'))
  const ast = ts.createSourceFile('fixture.ts', script, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
  return names.map(name => {
    const node = ast.statements.find(n => ts.isFunctionDeclaration(n) && n.name?.text === name)
    assert.ok(node, name); return node.getText(ast)
  }).join('\n')
}
const liveHandlers = functions(room, ['loadLiveAgentPlansForRoom', 'changeLiveAgentPlan', 'startCompanionRuntime'])
const agentHandlers = functions(agent, ['resolveIntentTargetPlan', 'prepareLivePlanIntentAction'])
const selector = room.match(/<label v-if="canConfigureRoomStrategy" class="companion-plan-select"[\s\S]*?<\/label>/)[0]
const fixtures = new Map()
const roomID = fileURLToPath(new URL('../src/__plan-assets-live.vue', import.meta.url)).replaceAll('\\', '/')
fixtures.set(roomID, `<script setup lang="ts">
import {ref,computed,onMounted} from 'vue'
import {getRoomLiveAgentPlans,setLiveRuntimePlan,getRoomPublishedLiveAgentPlanVersion,startLiveRuntime,getLiveAgentPlans} from '/src/api.ts'
import {openPlanPicker} from '/src/planAssets.ts'
const roomId=26,room=ref({tenant_id:14}),canConfigureRoomStrategy=ref(true),coreActionsAvailable=ref(true),agentPlanBusy=ref(false),runtimeControlBusy=ref(false),agentPlanError=ref(''),agentPlanNotice=ref(''),livePlanPicker=ref(null),liveAgentPlans=ref([])
let pageUnmounted=false,accountPlanLoadEpoch=0
const accountPlanScope=()=>String(window.fixture.scope),selectedLiveAgentPlanId=ref(0),liveAgentPlanSelectValue=computed(()=>String(selectedLiveAgentPlanId.value||'')),selectedLiveAgentPlanName=computed(()=>window.fixture.plans.find(p=>p.id===selectedLiveAgentPlanId.value)?.name||'')
const firstVoiceStartPending=ref(false),aiActive=ref(false),aiRuntimeMode=ref('anchor'),runtimeError=ref(''),firstVoicePlaybackReceived=ref(false),initialVoiceStage=ref('opening_audio'),pendingStartAfterSessionDecision=ref(false),localAudioError=ref(''),sessionStats=ref({resume_pending:false}),publishedSpeechVersion=ref(null)
let firstVoicePlaybackEpoch=0
const startupMilestones={connecting:{percent:8},waiting_startup:{percent:12}}
function closeRuntimeActionError(){runtimeError.value=''}
function openRuntimeActionError(title,err,fallback){runtimeError.value=typeof err==='string'?err:fallback}
function flushCompositeAudioQueue(){}
async function ensureLocalAudioUnlocked(){window.fixture.audioCalls++;return true}
async function refreshSessionStats(){}
async function connectLocalAudioReceiver(){window.fixture.audioCalls++}
async function refreshRuntime(){selectedLiveAgentPlanId.value=window.fixture.selected[26]||0}
async function refreshAgentDecisions(){}
function logUserAction(){}
async function createDefaultLiveAgentPlan(){throw Error('Unused create entry')}
function intentString(value){return String(value||'').trim()}
${liveHandlers}
${agentHandlers}
window.fixture.live={select:changeLiveAgentPlan,start:startCompanionRuntime,load:loadLiveAgentPlansForRoom,selectID:id=>selectedLiveAgentPlanId.value=id,state:()=>({error:agentPlanError.value,runtimeError:runtimeError.value,notice:agentPlanNotice.value,selected:selectedLiveAgentPlanId.value,busy:runtimeControlBusy.value}),prepare:prepareLivePlanIntentAction,unmount:()=>pageUnmounted=true}
onMounted(()=>loadLiveAgentPlansForRoom())
</script><template><main>${selector}<p role="alert">{{agentPlanError}} {{runtimeError}}</p><p role="status">{{agentPlanNotice}}</p><button @click="startCompanionRuntime">开始直播</button></main></template>`)
const setup = String.raw`
import {createApp,h} from 'vue';import {createRouter,createMemoryHistory} from 'vue-router';
import {applySession} from '/src/session.ts';import '/src/style.css';
const plans=[4,5,6].map((id,i)=>({id,tenant_id:14,name:['方案甲','方案乙','未保存草稿'][i],description:'测试账号资产',status:'active',room_count:0,term_count:0}));
const rooms=(location.search.includes('empty=1')?[]:[26,29]).map(id=>({id,tenant_id:14,name:'测试房间'+id,status:'offline',platform:'douyin'}));
const bindings=[101,102].map((id,i)=>({id,tenant_id:14,profile_id:201+i,sample_asset_id:1,provider:'aliyun_qwen',model:'test-model',voice_id:'test-voice-'+id,rate:1,status:'ready',created_at:'2026-10-28T00:00:00Z',updated_at:'2026-10-28T00:00:00Z'}));
const official={id:'longanlingxin',name:'龙安灵心',provider:'aliyun_qwen',model:'qwen-audio-3.0-tts-plus',gender:'女',tags:[],description:'官方'};
function identity(id){const b=bindings[id===4?0:1];return {name:id===4?'声音甲':'声音乙',version:'V1',source:'clone',provider:b.provider,model:b.model,voice_id:b.voice_id,binding_id:b.id,profile_id:b.profile_id,rate:1,emotion_enabled:true,emotion:'亲切',style:{tone:'亲切'}}}
const versions=Object.fromEntries([4,5].map(id=>[id,{id:id*10,tenant_id:14,plan_id:id,room_id:26,version_no:1,lifecycle_status:'published',duration_minutes:90,round_minutes:7,voice_identity:identity(id),variants:[],generation_context:{content_mode:'ai_dynamic',anchor_generation_options:{buffer_seconds:120,body_min:2,body_max:4,max_chars:300}}}]));
window.fixture={scope:1,calls:[],plans,rooms,bindings,versions,selected:{26:4,29:0},audioCalls:0,hold:'',fail:'',mode:'ai_dynamic',savedOnly:[4,5],setSession:applySession};
window.fetch=async(url,init={})=>{
 const f=window.fixture,u=new URL(url,location.origin),p=u.pathname,method=init.method||'GET',body=init.body?JSON.parse(init.body):null;
 f.calls.push({path:p,query:u.search,method,body});
 if(f.hold&&p.endsWith(f.hold)){f.hold='';await new Promise(r=>f.release=r)}
 if(f.fail&&p.endsWith(f.fail))return Response.json({error:'模拟读取失败'},{status:503});
 const roomID=Number(p.match(/rooms\/(\d+)/)?.[1]||26),planID=Number(p.match(/live-agent-plans\/(\d+)/)?.[1]||0);
 if(method!=='GET'){
  if(p.endsWith('/question-library')&&planID)return Response.json({id:901,plan_id:planID,...body});
  if(p.endsWith('/reading-rules')&&planID)return Response.json({plan_id:planID,rules_text:body.rules_text});
  if(p==='/api/v1/live-agent-plans'&&method==='POST'){const v={id:plans.length+10,...body,status:'active',room_count:0,term_count:0};plans.push(v);return Response.json(v)}
  if(p.endsWith('/runtime/plan')){f.selected[roomID]=body.plan_id;return Response.json({room_id:roomID,agent_plan_id:body.plan_id})}
  if(p.endsWith('/runtime/start'))return Response.json({});
  if(p.endsWith('/versions')&&method==='POST'){const v={id:planID*10+1,plan_id:planID,version_no:(versions[planID]?.version_no||0)+1,lifecycle_status:'published',...body,generation_context:{...body.generation_context,anchor_generation:{buffer_seconds:body.generation_context.anchor_generation_options?.buffer_seconds,config:body.generation_context.anchor_generation_options}}};versions[planID]=v;f.savedOnly=[...new Set([...f.savedOnly,planID])];return Response.json(v)}
  throw Error('Unexpected local mutation '+method+' '+p)
 }
 if(p==='/api/v1/rooms')return Response.json({items:rooms});
 if(p.endsWith('/ui-preferences'))return Response.json({selected_live_room_id:26});
 if(p==='/api/v1/live-agent-plans')return Response.json({items:plans});
 if(p.endsWith('/live-agent-plans')){if(u.searchParams.get('published_only')!=='1')throw Error('Legacy binding list requested');return Response.json({items:plans.filter(p=>f.savedOnly.includes(p.id)).sort((a,b)=>(b.id===f.selected[roomID])-(a.id===f.selected[roomID]))})}
 if(p.endsWith('/runtime'))return Response.json({room_id:roomID,agent_plan_id:f.selected[roomID],agent_state:'stopped'});
 if(p.endsWith('/published-version'))return Response.json({version:versions[f.selected[roomID]]||null});
 if(p.endsWith('/config-versions'))return Response.json([]);
 if(p.endsWith('/content-mode'))return Response.json({content_mode:f.mode,available_modes:['user_audio','ai_pregenerated','ai_dynamic'],dynamic_authorized:true});
 if(p.endsWith('/voice-rate'))return Response.json({tenant_id:14,plan_id:planID,rate:1});
 if(p.endsWith('/workspace'))return Response.json({version:versions[planID]||null,source:versions[planID]?'published':null,checks:[]});
 if(p.endsWith('/versions'))return Response.json({items:versions[planID]?[versions[planID]]:[]});
 if(p.endsWith('/realtime-options'))return Response.json({buffer_seconds:120,body_min:2,body_max:4,max_chars:300});
 if(p.endsWith('/content-pools'))return Response.json({state:'synced',changes:[],products:[],benefits:[],pools:[],set_id:9,saved_set_id:9,job:{}});
 if(p.endsWith('/official-voices'))return Response.json({configured:true,items:[official]});
 if(p.endsWith('/voice-model-bindings'))return Response.json(bindings);
 if(p.endsWith('/voice-profiles/quota'))return Response.json({limit:10,used:2,remaining:8});
 if(p.endsWith('/voice-settings'))return Response.json({custom_model:'test-model',can_design:false});
 if(p.endsWith('/voice-profiles'))return Response.json([201,202].map((id,i)=>({id,tenant_id:14,name:i?'声音乙':'声音甲',clone_status:'ready',sample_asset_id:1,config:{},created_at:'2026-10-28T00:00:00Z'})));
 return Response.json({items:[],plugins:[],checks:[],groups:[],catalog:[],profiles:[],version:null,source:null,addressing:[],greeting:'你好'});
};
applySession({actor:{user_id:14,tenant_id:14,role:'customer',username:'test'},tenants:[{id:14,name:'测试'}],environment:'test'});
`
const server = await createServer({server:{host:'127.0.0.1',port:0},appType:'custom',plugins:[{
 name:'plan-assets-local',enforce:'pre',resolveId(id){if(id==='/__plan-assets-live.vue')return roomID},load(id){return fixtures.get(id)},
 transform(code,id){if(!id.endsWith('/src/views/LiveStrategyView.vue'))return;return code.replace('</script>',`defineExpose({currentRoomPlanId,activeRoomId,currentVoiceIdentity,fullShowSaveBlockedReason,fullShowWorkspaceLoading,fullShowWorkspaceHydrating,loadFullShowWorkspace,refreshLivePlans,refreshCurrentRoomPlan,handlePlanSwitch,activeMode,planBusy,saveFullShowVersion});</script>`)},
 configureServer(dev){dev.middlewares.use(async(req,res,next)=>{
  if(!req.url.startsWith('/__plan_assets'))return next();const live=req.url.includes('live=1');
  const html='<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" href="data:,"><div id="app"></div><script type="module">'+setup+`import View from '${live?'/__plan-assets-live.vue':'/src/views/LiveStrategyView.vue'}';const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:View}]});await router.push('/');await router.isReady();window.fixture.vm=createApp({render:()=>h(View,{ref:el=>window.fixture.view=el})}).use(router).mount('#app');</script></html>`;
  res.setHeader('Content-Type','text/html;charset=utf-8');res.end(await dev.transformIndexHtml(req.url,html));
 })}
}]})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const checks=[],errors=[]
try{
 const page=await browser.newPage({viewport:{width:1600,height:1000}});page.setDefaultTimeout(8000);page.on('pageerror',e=>errors.push(e.message))
 const base=server.resolvedUrls.local[0]+'__plan_assets';const select=page.getByRole('combobox',{name:'选择智能体方案',exact:true})
 const ready=async(id=4)=>page.waitForFunction(id=>window.fixture?.view?.currentRoomPlanId===id&&!window.fixture.view.fullShowWorkspaceLoading&&!window.fixture.view.fullShowWorkspaceHydrating,id)
 await page.goto(base);await ready();assert.ok((await select.locator('option').allTextContents()).some(t=>t.includes('未保存草稿')))
 assert.equal(await page.getByRole('button',{name:/绑定当前|解除房间|取消绑定/}).count(),0)
 checks.push('P1/P7：真实方案页展示全部账号方案和草稿，0绑定也可见；无绑定/解绑入口和旧列表请求')
 await select.selectOption('5');await ready(5);assert.equal(await page.evaluate(()=>window.fixture.selected[26]),4)
 await page.waitForFunction(()=>window.fixture.view.currentVoiceIdentity.binding_id===102)
 await select.selectOption('4');await ready(4);assert.equal(await page.evaluate(()=>window.fixture.view.currentVoiceIdentity.binding_id),101)
 checks.push('P2/P3：编辑器甲乙切换只加载账号方案，不修改任何房间选择，恢复各自声音')
 await page.evaluate(()=>window.fixture.view.activeRoomId=29);await ready(4)
 assert.equal(await page.evaluate(()=>window.fixture.view.currentVoiceIdentity.binding_id),101)
 assert.equal(await page.evaluate(()=>window.fixture.calls.some(c=>c.method!=='GET'&&c.path.includes('/runtime/plan'))),false)
 checks.push('改变可选场次上下文不改变方案声音，不向房间写入配置或选择关系')
 await page.evaluate(()=>window.fixture.view.activeRoomId=26);await ready(4)
 await page.evaluate(()=>window.fixture.view.activeMode='voice');await page.locator('.voice-library-nav button').filter({hasText:'我的声音'}).click()
 const use=page.locator('.use-voice-button').filter({hasText:'使用这个声音'});await use.first().waitFor();
 const writesBefore=await page.evaluate(()=>window.fixture.calls.filter(c=>c.method!=='GET').length)
 await use.first().click();await page.waitForFunction(()=>window.fixture.view.currentVoiceIdentity.binding_id===102)
 assert.equal(await page.evaluate(()=>window.fixture.calls.filter(c=>c.method!=='GET').length),writesBefore)
 checks.push('声音只选入当前方案草稿，选择不激活房间配置、不另行发布、不生成试听或TTS')
 const save=page.locator('#plan-save-button');await page.waitForFunction(()=>!document.querySelector('#plan-save-button').disabled)
 await save.click();await page.waitForFunction(()=>window.fixture.calls.some(c=>c.method==='POST'&&c.path.endsWith('/versions')))
 await page.waitForFunction(()=>!window.fixture.view.fullShowSaveBlockedReason)
 const payload=await page.evaluate(()=>window.fixture.calls.filter(c=>c.method==='POST'&&c.path.endsWith('/versions')).at(-1).body)
 assert.equal(payload.voice_identity.binding_id,102);assert.equal(payload.room_id,0);assert.equal(payload.generation_context.content_mode,'ai_dynamic')
 assert.equal(payload.tenant_id,14)
 checks.push('方案独立保存，room_id为0，实时模式与所选方案声音正确；保存不触发内容生成')
 // Re-saving a restored plan must keep emotion/style metadata as well as its voice binding.
 await select.selectOption('5');await ready(5);await save.click();await page.waitForFunction(()=>window.fixture.versions[5].version_no===2)
 const identity=await page.evaluate(()=>window.fixture.versions[5].voice_identity);assert.equal(identity.emotion_enabled,true);assert.deepEqual(identity.style,{tone:'亲切'})
 await select.selectOption('4');await ready(4)
 checks.push('恢复并重新保存方案保留声音情感、风格和语速配置')
 // A late voice validation read cannot save into the plan selected afterwards.
 const beforeLate=await page.evaluate(()=>window.fixture.calls.filter(c=>c.method==='POST'&&c.path.endsWith('/versions')).length)
 await page.evaluate(()=>{window.fixture.hold='/voice-model-bindings';window.fixture.release=null;window.fixture.view.saveFullShowVersion()});await page.waitForFunction(()=>!!window.fixture.release)
 await select.selectOption('5');await ready(5);await page.evaluate(()=>window.fixture.release());await page.waitForFunction(()=>!document.querySelector('#plan-save-button').disabled)
 assert.equal(await page.evaluate(()=>window.fixture.calls.filter(c=>c.method==='POST'&&c.path.endsWith('/versions')).length),beforeLate)
 assert.equal(await page.evaluate(()=>window.fixture.view.currentVoiceIdentity.binding_id),102)
 await select.selectOption('4');await ready(4)
 checks.push('保存校验中切换方案：旧声音响应不会创建版本，也不会覆盖新方案声音')
 await page.evaluate(()=>{window.fixture.bindings[1].status='disabled'})
 const saves=await page.evaluate(()=>window.fixture.calls.filter(c=>c.method==='POST'&&c.path.endsWith('/versions')).length)
 await save.click();await page.getByRole('combobox',{name:'改选可用方案声音'}).waitFor()
 assert.equal(await page.evaluate(()=>window.fixture.calls.filter(c=>c.method==='POST'&&c.path.endsWith('/versions')).length),saves)
 const alternatives=page.getByRole('combobox',{name:'改选可用方案声音'});assert.equal(await alternatives.locator('option').count(),3)
 await page.screenshot({path:output+'/voice-unavailable-desktop.png'})
 await page.setViewportSize({width:390,height:1000});await alternatives.scrollIntoViewIfNeeded();await page.screenshot({path:output+'/voice-unavailable-mobile.png'});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);assert.ok(await alternatives.evaluate(el=>parseFloat(getComputedStyle(el).fontSize)>=16));await page.setViewportSize({width:1600,height:1000})
 await alternatives.selectOption('0');await save.click();await page.waitForFunction(n=>window.fixture.calls.filter(c=>c.method==='POST'&&c.path.endsWith('/versions')).length===n+1,saves)
 checks.push('P6：每次保存重新读声音状态；已停用绑定禁止保存，保存位置给可用列表，改选后可重试')
 await select.selectOption('6');await ready(6);await page.evaluate(()=>window.fixture.view.activeMode='memory')
 await page.waitForFunction(()=>window.fixture.calls.some(c=>c.path==='/api/v1/live-agent-plans/6/question-library'))
 assert.equal(await page.getByText('请选择直播间',{exact:true}).count(),0)
 checks.push('未保存且未被任何房间选择的方案，通过方案API读取问题库，无房间绑定门槛')
 await page.locator('.question-library').getByRole('button',{name:'添加一行',exact:true}).click()
 await page.locator('.ql-table tbody tr td:nth-child(2) input').fill('可以开发票吗')
 await page.locator('.ql-table tbody tr').getByRole('button',{name:'保存',exact:true}).click()
 await page.waitForFunction(()=>window.fixture.calls.some(c=>c.method==='POST'&&c.path==='/api/v1/live-agent-plans/6/question-library'))
 checks.push('草稿的问题库保存直接写租户+方案，不写当前房间或另一方案')
 await page.evaluate(()=>window.fixture.view.activeMode='reading')
 await page.locator('.reading-rules textarea').fill('绸都 => chou2 du1')
 await page.locator('.reading-rules').getByRole('button',{name:'保存',exact:true}).click()
 await page.waitForFunction(()=>window.fixture.calls.some(c=>c.method==='PUT'&&c.path==='/api/v1/live-agent-plans/6/reading-rules'))
 checks.push('读写规则在未选用的方案直接保存，前后请求都按方案隔离')

 await page.evaluate(()=>window.fixture.view.activeMode='plan');await page.getByRole('button',{name:'＋ 新建方案',exact:true}).click()
 await page.getByRole('textbox',{name:'方案名称',exact:true}).fill('新建账号方案');await page.getByRole('button',{name:'创建方案',exact:true}).click()
 await page.waitForFunction(()=>window.fixture.plans.some(p=>p.name==='新建账号方案'))
 assert.equal(await page.evaluate(()=>window.fixture.calls.some(c=>c.path.endsWith('/room-bindings'))),false)
 checks.push('真实新建弹窗创建账号资产，创建不调用绑定，方案可直接编辑')
 // Failed and stale list reads must not reintroduce another room/account's selection.
 await page.evaluate(()=>{window.fixture.hold='/live-agent-plans';window.fixture.release=null;window.fixture.view.refreshLivePlans()});await page.waitForFunction(()=>!!window.fixture.release)
 await page.evaluate(()=>window.fixture.view.activeRoomId=29);await ready(await page.evaluate(()=>window.fixture.view.currentRoomPlanId));await page.evaluate(()=>window.fixture.release());assert.equal(await page.evaluate(()=>window.fixture.view.activeRoomId),29)
 checks.push('迟到账号方案列表不回写已切换房间的选择')
 await page.evaluate(()=>window.fixture.rooms.splice(0))
 await page.goto(base+'?empty=1');await ready(4)
 await page.waitForFunction(()=>window.fixture.view.currentVoiceIdentity.binding_id>0)
 await page.locator('#plan-save-button').click()
 await page.waitForFunction(()=>window.fixture.calls.some(c=>c.method==='POST'&&c.path.endsWith('/versions')))
 assert.equal(await page.evaluate(()=>window.fixture.calls.filter(c=>c.path.endsWith('/versions')&&c.method==='POST').at(-1).body.room_id),0)
 checks.push('账号没有任何直播间，仍能恢复声音、事实/风格工作区并保存方案版本')
 await page.goto(base+'?live=1');await page.getByRole('button',{name:'开始直播'}).waitFor();await page.evaluate(()=>window.fixture.live.selectID(0));await page.getByRole('button',{name:'开始直播'}).click()
 assert.equal(await page.getByRole('combobox').evaluate(el=>el===document.activeElement),true)
 assert.match((await page.evaluate(()=>window.fixture.live.state())).error,/请先选择一个方案/)
 assert.equal(await page.evaluate(()=>window.fixture.audioCalls),0)
 checks.push('未选方案点击开始：提示并聚焦/尝试展开原生下拉；无音频或开播请求')
 await page.evaluate(()=>{window.fixture.selected[26]=6;window.fixture.live.selectID(6)})
 await page.getByRole('button',{name:'开始直播'}).click();await page.waitForFunction(()=>!window.fixture.live.state().busy)
 assert.equal((await page.evaluate(()=>window.fixture.live.state())).error,'该方案还没有保存，请先保存')
 assert.equal(await page.evaluate(()=>window.fixture.audioCalls),0)
 checks.push('P5：已选但无保存版本，真实开播处理函数在音频连接和开播POST前拦截')
 await page.evaluate(()=>{window.fixture.selected[26]=4;window.fixture.live.selectID(4);window.fixture.fail='/published-version'})
 await page.getByRole('button',{name:'开始直播'}).click();await page.waitForFunction(()=>!window.fixture.live.state().busy)
 assert.equal(await page.evaluate(()=>window.fixture.audioCalls),0)
 checks.push('版本读取失败保持关闭，不把未知状态当作已保存')
 await page.evaluate(()=>{window.fixture.fail='';window.fixture.hold='/published-version';window.fixture.live.start()})
 await page.waitForFunction(()=>!!window.fixture.release);await page.evaluate(()=>{window.fixture.scope++;window.fixture.release()});await page.waitForFunction(()=>!window.fixture.live.state().busy)
 assert.equal(await page.evaluate(()=>window.fixture.calls.filter(c=>c.path.endsWith('/runtime/start')).length),0)
 checks.push('账号/方案校验期间变化，不继续连接音频或开播')
 await page.evaluate(()=>window.fixture.live.start());await page.waitForFunction(()=>!window.fixture.live.state().busy)
 assert.equal(await page.evaluate(()=>window.fixture.calls.filter(c=>c.path.endsWith('/runtime/start')).length),1)
 checks.push('有效已保存方案才进入原开播流程；仅本地桩记录，未启动真实直播')
 const prepared=await page.evaluate(()=>window.fixture.live.prepare(4,26,'使用方案乙',{intent:'plan.switch',target:{plan_id:5}}));assert.equal(prepared.action.type,'confirm_live_plan_switch')
 const draft=await page.evaluate(()=>window.fixture.live.prepare(4,26,'使用草稿',{intent:'plan.switch',target:{plan_id:6}}));assert.equal(draft.text,'该方案还没有保存，请先保存')
 for(const intent of ['plan.bind','plan.unbind'])assert.equal((await page.evaluate(intent=>window.fixture.live.prepare(4,26,'旧命令',{intent,target:{plan_id:5}}),intent)).action,undefined)
 checks.push('智能体使用账号已保存列表生成切换确认；草稿及旧绑定/解绑指令无写动作')
 assert.deepEqual(errors,[])
 await page.screenshot({path:output+'/live-guard.png'})
 await writeFile(output+'/results.json',JSON.stringify({checks,errors},null,2))
 console.log(JSON.stringify({passed:checks.length,checks},null,2))
}catch(e){const p=browser.contexts()[0]?.pages()[0];const state=await p?.evaluate(()=>({calls:window.fixture?.calls.slice(-22),blocked:window.fixture?.view?.fullShowSaveBlockedReason,text:document.querySelector('.plan-configuration-head')?.textContent||document.querySelector('.strategy-plan-hero')?.textContent,body:document.body.innerText.slice(0,1600)})).catch(()=>null);await p?.screenshot({path:output+'/failure.png'});await writeFile(output+'/failure.json',JSON.stringify({error:String(e),stack:e.stack,checks,errors,state},null,2));throw e}
finally{await browser.close();await server.close()}
