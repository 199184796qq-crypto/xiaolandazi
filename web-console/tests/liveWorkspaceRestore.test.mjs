import test from 'node:test'
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import ts from 'typescript'
const source=await readFile(new URL('../src/views/LiveStrategyView.vue',import.meta.url),'utf8')
const start=source.indexOf('async function restoreFullShowWorkspaceVersion('),end=source.indexOf('\nasync function loadFullShowWorkspace',start)
const fn=source.slice(start,end),js=ts.transpile(fn,{target:ts.ScriptTarget.ES2022})
const normalizerJS=ts.transpile(await readFile(new URL('../src/liveAgentPlanVersion.ts',import.meta.url),'utf8'),{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.ESNext})
const {normalizeLiveAgentPlanVersion}=await import('data:text/javascript;charset=utf-8,'+encodeURIComponent(normalizerJS))
function fixture(){
 const context={}
 for(const match of fn.matchAll(/\b([A-Za-z_$][\w$]*)\.value\b/g))context[match[1]]={value:null}
 context.currentPlan.value={name:'测试方案',description:''}
 Object.assign(context,{normalizeLiveAgentPlanVersion,defaultFullShowVoiceState:()=>({}),realtimeOptionsFromVersion:()=>null,fullShowVoiceIdentityKey:identity=>String(identity.profile_id),cloneVersionJSON:(value,fallback)=>structuredClone(value??fallback),asRecord:value=>value&&typeof value==='object'?value:{},cleanVersionTimeline:value=>value||[],cleanVersionSafePoints:value=>value||[],expandOriginalSourceManifest:async value=>value})
 const restore=new Function(...Object.keys(context),js+'\nreturn restoreFullShowWorkspaceVersion')(...Object.values(context))
 return {context,restore}
}
test('actual workspace restoration accepts legacy null real-time manuscripts',async()=>{
 const f=fixture(),version={id:37,plan_id:4,room_id:26,version_no:5,duration_minutes:90,round_minutes:7,voice_identity:{profile_id:8},generation_context:{content_mode:'ai_dynamic'},variants:null}
 await f.restore(version,'published')
 assert.equal(f.context.fullShowWorkspaceHydrating.value,false)
 assert.deepEqual(f.context.fullShowResult.value.variants,[])
 assert.equal(f.context.fullShowSavedVersion.value.id,37)
 assert.equal(version.variants,null,'do not rewrite the saved version')
})
test('actual workspace restoration retains fixed manuscript and voice resources',async()=>{
 const f=fixture(),version={id:38,plan_id:4,room_id:26,version_no:6,duration_minutes:90,round_minutes:7,voice_identity:{profile_id:8},generation_context:{content_mode:'ai_pregenerated'},variants:[{variant_key:'A',text:'真实文稿',is_formal:true,audio_asset_id:77,audio_url:'/audio/77',timeline:[],safe_points:[],asset_manifest:{},audio_duration_ms:1000}]}
 await f.restore(version,'draft')
 assert.equal(f.context.fullShowResult.value.variants[0].text,'真实文稿')
 assert.equal(f.context.fullShowVoiceStates.value.A.audio_asset_id,77)
 assert.deepEqual(f.context.fullShowFormalVariantKeys.value,['A'])
})
