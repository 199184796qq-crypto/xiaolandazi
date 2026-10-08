import test from 'node:test'
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import {stripTypeScriptTypes} from 'node:module'
import vm from 'node:vm'
// Execute the actual view's orchestration functions with in-memory API mocks.
// This detects an accidental extra fusion call, without touching models or DB.
const view=await readFile(new URL('../src/views/LiveStrategyView.vue',import.meta.url),'utf8')
const first=view.indexOf('async function analyzeOneAnchorSample(')
const last=view.indexOf('async function deleteReusableAnchorStyleSample(',first)
assert.ok(first>=0&&last>first)
const functions=stripTypeScriptTypes(view.slice(first,last),{mode:'strip'})
function fixture({multi=false,sole=false,failFusion=false}={}) {
 const style={id:7,name:'主播',profile:{delivery_spec:{meta_prompt_md:'原总风格'}},sample_count:sole?1:2}
 const sample={id:71,title:'样本甲',analysis_status:'analyzed',analysis:{delivery_spec:{meta_prompt_md:'甲独立结果'}}}
 const second={id:72,title:'样本乙',analysis_status:'pending',analysis:{}}
 const ref=value=>({value})
 const context={
  selectedReusableAnchorStyle:ref(style),reusableAnchorStyleBusy:ref(false),
  reusableAnchorStyles:ref([style]),reusableAnchorStyleSamples:ref(sole?[sample]:[sample,second]),
  reusableAnchorStyleNotice:ref(''),reusableAnchorStyleError:ref(''),reusableAnchorStyleAnalysisStage:ref(''),reusableAnchorStyleAnalysisProgress:ref(0),
  reusableAnchorMultiAnalysis:ref(multi),reusableAnchorStyleSampleDraft:ref({title:'',raw_text:''}),
  reusableAnchorAnalysisPreviewId:ref(2),reusableAnchorAnalysisVersions:ref({active_version_id:2}),
  anchorAnalysisScope:()=>({tenant:14,current:()=>true}),anchorAnalysisProgress:()=>{},
  refreshAnchorAnalysisVersions:async()=>{},syncAnchorSampleCount:()=>{},
  calls:[],window:{},Error,listLiveAnchorStyleSamples:async()=>({items:context.reusableAnchorStyleSamples.value}),
  analyzeLiveAnchorStyleSample:async(_style,id)=>{
   context.calls.push(['single',id])
   const old=context.reusableAnchorStyleSamples.value.find(s=>s.id===id)
   const result={...old,analysis_status:'analyzed',analysis:{delivery_spec:{meta_prompt_md:'单份新分析'}}}
   const updated=sole?{...style,profile:{delivery_spec:{meta_prompt_md:'唯一样本总风格'}}}:style
   return {sample:result,style:updated,overall_updated:sole,notice:sole?'已更新总风格':'单份已保存，总风格未改变'}
  },
  fuseLiveAnchorAnalyses:async()=>{
   context.calls.push(['fusion'])
   if(failFusion) throw new Error('综合失败')
   return {style:{...style,profile:{delivery_spec:{meta_prompt_md:'综合新风格'}}}}
  },
 }
 vm.createContext(context);vm.runInContext(functions,context)
 return {context,sample}
}
test('individual sample action never invokes fusion, even if checkbox is checked',async()=>{
 const {context:c,sample}=fixture({multi:true})
 await c.analyzeReusableAnchorStyleSample(sample)
 assert.deepEqual(c.calls,[['single',71]])
 assert.equal(c.selectedReusableAnchorStyle.value.profile.delivery_spec.meta_prompt_md,'原总风格')
 assert.equal(c.reusableAnchorStyleSamples.value[0].analysis.delivery_spec.meta_prompt_md,'单份新分析')
})
test('unchecked header analyses only one sample and leaves overall version untouched',async()=>{
 const {context:c}=fixture()
 await c.startReusableAnchorAnalysis()
 assert.deepEqual(c.calls,[['single',72]])
 assert.equal(c.selectedReusableAnchorStyle.value.profile.delivery_spec.meta_prompt_md,'原总风格')
})
test('explicit checkbox first analyses pending results and then requests fusion',async()=>{
 const {context:c}=fixture({multi:true})
 await c.startReusableAnchorAnalysis()
 assert.deepEqual(c.calls,[['single',72],['fusion']])
 assert.equal(c.selectedReusableAnchorStyle.value.profile.delivery_spec.meta_prompt_md,'综合新风格')
})
test('sole sample accepts server auto-promotion without calling fusion',async()=>{
 const {context:c}=fixture({sole:true})
 await c.startReusableAnchorAnalysis()
 assert.deepEqual(c.calls,[['single',71]])
 assert.equal(c.selectedReusableAnchorStyle.value.profile.delivery_spec.meta_prompt_md,'唯一样本总风格')
})
test('failed explicit fusion keeps existing overall style and saved individual results',async()=>{
 const {context:c}=fixture({multi:true,failFusion:true})
 await c.startReusableAnchorAnalysis()
 assert.deepEqual(c.calls,[['single',72],['fusion']])
 assert.equal(c.selectedReusableAnchorStyle.value.profile.delivery_spec.meta_prompt_md,'原总风格')
 assert.equal(c.reusableAnchorStyleSamples.value[1].analysis_status,'analyzed')
 assert.equal(c.reusableAnchorStyleError.value,'综合失败')
 assert.equal(c.reusableAnchorStyleBusy.value,false)
})

test('legacy overall analysis edits at expected version zero and refreshes effective file mapping',async()=>{
 const {context:c}=fixture()
 c.reusableAnchorAnalysisVersions.value={active_version_id:0}
 c.reusableAnchorAnalysisPreviewId.value=0
 c.editLiveAnchorAnalysis=async(style,expected,sections,tenant)=>{
  c.calls.push(['edit',style,expected,tenant])
  const md=sections.map(s=>`【${s.title}】\n${s.points.join('\n')}`).join('\n')
  return {style:{...c.selectedReusableAnchorStyle.value,profile:{delivery_spec:{meta_prompt_md:md,rulebook:md,scoring_baseline_json:'new-score'}}}}
 }
 c.refreshAnchorAnalysisVersions=async()=>{c.reusableAnchorAnalysisVersions.value={active_version_id:3};c.reusableAnchorAnalysisPreviewId.value=3}
 await c.saveOverallAnchorAnalysis([{title:'口语风格',points:['称呼：各位朋友']}])
 assert.deepEqual(c.calls,[['edit',7,0,14]])
 assert.equal(c.selectedReusableAnchorStyle.value.profile.delivery_spec.meta_prompt_md,'【口语风格】\n称呼：各位朋友')
 assert.equal(c.reusableAnchorAnalysisPreviewId.value,3)
 assert.equal(c.reusableAnchorStyleBusy.value,false)
 c.reusableAnchorAnalysisPreviewId.value=2
 await c.saveOverallAnchorAnalysis([{title:'旧版本',points:['不应保存']}])
 assert.equal(c.calls.length,1,'historical preview remains read-only')
})
