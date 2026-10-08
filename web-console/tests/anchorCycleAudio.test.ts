import test from 'node:test'
import assert from 'node:assert/strict'
import { bufferedVoiceMS, voiceBufferFull, pendingVoiceSegment, productionBuffer, mergeVoiceResponse, runVoiceQueue } from '../src/anchorCycleAudio.ts'
import { runCycleLoop, cycleProgress } from '../src/anchorCycleRun.ts'
import type { CycleState, CycleVoiceResponse } from '../src/anchorCycleRun.ts'

test('缓存按实际音频时长扣除已播放部分，不按文字字数', () => {
  const queue=[{id:'a',url:'one',durationMS:30000},{id:'b',url:'two',durationMS:100000}]
  assert.equal(bufferedVoiceMS(queue,5),125000)
  assert.equal(voiceBufferFull(queue,5,120),true)
  assert.equal(voiceBufferFull(queue,11,120),false)
  assert.equal(bufferedVoiceMS([],999),0)
  assert.equal(bufferedVoiceMS(queue,999),100000)
})

test('只找未完成的采纳声音，候选不进TTS', () => {
  const state={accepted:[{selected:{id:'a'},voice:{status:'ready'}},{selected:{id:'b'},voice:{status:'failed'}}],attempts:[{id:'rejected'}]} as unknown as CycleState
  assert.equal(pendingVoiceSegment(state)?.selected.id,'b')
  state.accepted[1]!.voice!.status='ready'
  assert.equal(pendingVoiceSegment(state),undefined)
})

test('可选前置调度更新状态后，模型请求使用最新版本', async () => {
  let state={run_id:'one',revision:1,phase:'body',cursor:0,cycle_number:1,status:'ready',busy:false,uncertain:false,config:{max_retries:0},attempts:[],accepted:[]} as unknown as CycleState
  let calls=0
  const result=await runCycleLoop({state:()=>state,stopped:()=>false,beforeAdvance:async()=>{state={...state,revision:2}},advance:async current=>{assert.equal(current.revision,2);calls++;return{run:{...current,revision:3,phase:'ended'}}},receive:value=>{state=value}})
  assert.equal(result,'completed');assert.equal(calls,1)
})

test('缓存等待期间手动暂停，不多发一轮模型请求', async () => {
  const state={run_id:'one',revision:1,phase:'body',config:{max_retries:0},attempts:[],accepted:[]} as unknown as CycleState
  let stopped=false,calls=0
  const result=await runCycleLoop({state:()=>state,stopped:()=>stopped,beforeAdvance:async()=>{stopped=true},advance:async()=>{calls++;return{run:state}},receive:()=>{}})
  assert.equal(result,'paused');assert.equal(calls,0)
})

test('前置调度失败或内容承接卡死时不发模型请求（异步TTS不使用此阻塞钩子）', async () => {
  const state={run_id:'one',revision:1,phase:'body',status:'ready',config:{max_retries:0},attempts:[],accepted:[]} as unknown as CycleState
  let calls=0
  await assert.rejects(runCycleLoop({state:()=>state,stopped:()=>false,beforeAdvance:async()=>{throw new Error('前置调度失败')},advance:async()=>{calls++;return{run:state}},receive:()=>{}}),/前置调度失败/)
  state.status='content_blocked'
  await assert.rejects(runCycleLoop({state:()=>state,stopped:()=>false,advance:async()=>{calls++;return{run:state}},receive:()=>{}}),/塑价钩子/)
  assert.equal(calls,0)
})

test('可变段数与钩子显示服务器实际计划，不显示固定3段', () => {
  const state={phase:'body',body_target:6,config:{rolling:true,body_min:2,body_max:6},queue:[{key:'甲×乙'}],accepted:[],attempts:[],pending_price_hook:{promise:'两桶多少钱'}} as unknown as CycleState
  const result=cycleProgress(state)
  assert.equal(result.bodyTotal,6);assert.equal(result.total,8);assert.match(result.task,/两桶多少钱/)
})

function audioState(): CycleState {
  return { run_id:'one',revision:1,phase:'body',cursor:0,status:'ready',busy:false,uncertain:false,config:{max_retries:0,tts:{rate:1,buffer_seconds:120}},attempts:[],accepted:[{selected:{id:'a',text:'甲'.repeat(120)}}] } as unknown as CycleState
}
function readyVoice(id: string, durationMS=30000): CycleVoiceResponse {
  return {run_id:'one',candidate_id:id,voice:{status:'ready',latency_ms:500,bundle:{audio_url:'https://example.invalid/'+id,audio_asset_id:1,audio_duration_ms:durationMS,text:'甲'.repeat(120),srt:'1\n00:00:00,000 --> 00:00:30,000\n甲',timeline:[],safe_points:[],asset_manifest:{}}}}
}

test('缓存包括未合成、正在合成和失败文字，完成后用实际时长替代，不双算', () => {
  let state=audioState()
  assert.equal(productionBuffer(state,new Set(),' ',0).totalMS,30000)
  state=mergeVoiceResponse(state,readyVoice('a',40000))
  state.accepted.push({selected:{id:'b',text:'乙'.repeat(120)},voice:{status:'failed'}} as CycleState['accepted'][number])
  let result=productionBuffer(state,new Set(),'a',5)
  assert.equal(result.readyMS,35000);assert.equal(result.pendingMS,40000);assert.equal(result.totalMS,75000)
  state=mergeVoiceResponse(state,readyVoice('b',35000))
  result=productionBuffer(state,new Set(),'a',5)
  assert.equal(result.pendingMS,0);assert.equal(result.totalMS,70000)
  assert.equal(productionBuffer(state,new Set(['a']),'b',10).totalMS,25000)
})

test('TTS尚未返回，文字已继续生成两段；之后声音仍按采纳顺序', async () => {
  let state=audioState(), resolveFirst!: (value: CycleVoiceResponse)=>void
  const first=new Promise<CycleVoiceResponse>(resolve=>{resolveFirst=resolve})
  const audioCalls: string[]=[]
  const worker=runVoiceQueue({state:()=>state,stopped:()=>false,synthesize:async(_state,id)=>{audioCalls.push(id);return id==='a'?first:readyVoice(id)},inspect:async(_state,id)=>readyVoice(id),receive:value=>{state=mergeVoiceResponse(state,value)},wait:async()=>{}})
  let calls=0
  await runCycleLoop({state:()=>state,stopped:()=>false,advance:async current=>{calls++; const id=calls===1?'b':'c'; return {run:{...current,cursor:current.cursor+1,revision:current.revision+1,phase:calls===2?'ended':'body',accepted:[...current.accepted,{selected:{id,text:'乙'.repeat(60)}} as CycleState['accepted'][number]]}}},receive:value=>{state=value}})
  assert.equal(calls,2);assert.deepEqual(audioCalls,['a']);assert.equal(state.accepted.length,3)
  resolveFirst(readyVoice('a'))
  assert.equal(await worker,'idle');assert.deepEqual(audioCalls,['a','b','c'])
  assert.equal(state.revision,3);assert.equal(state.accepted.filter(item=>item.voice?.status==='ready').length,3)
})

test('声音失败只暂停声音队列，不跳过失败段；结束后的晚到声音不复活场次', async () => {
  let state: CycleState|null=audioState(), calls=0
  state.accepted.push({selected:{id:'b',text:'乙'}} as CycleState['accepted'][number])
  await assert.rejects(runVoiceQueue({state:()=>state,stopped:()=>false,synthesize:async()=>{calls++;return {run_id:'one',candidate_id:'a',voice:{status:'failed',error:'provider failed'}} as CycleVoiceResponse},inspect:async()=>readyVoice('a'),receive:value=>{state=mergeVoiceResponse(state!,value)},wait:async()=>{}}),/provider failed/)
  assert.equal(calls,1);assert.equal(state!.accepted[1]!.voice,undefined)
  let lateResolve!: (value:CycleVoiceResponse)=>void, received=false
  const late=new Promise<CycleVoiceResponse>(resolve=>{lateResolve=resolve})
  const worker=runVoiceQueue({state:()=>state,stopped:()=>false,synthesize:async()=>late,inspect:async()=>readyVoice('a'),receive:()=>{received=true},wait:async()=>{}})
  state=null;lateResolve(readyVoice('a'))
  assert.equal(await worker,'paused');assert.equal(received,false)
})

test('试听声音生成满额是正常完成，不作为播放错误', async () => {
  const state=audioState()
  const result=await runVoiceQueue({state:()=>state,stopped:()=>false,synthesize:async()=>({run_id:'one',candidate_id:'a',voice:null,preview_complete:true}),inspect:async()=>readyVoice('a'),receive:()=>{throw new Error('完成响应不应合并为空声音')},wait:async()=>{}})
  assert.equal(result,'complete')
})

test('已在生成的声音只查询状态，不盲目重新付费；错误响应不混入其他段', async () => {
  let state=audioState(), calls=0, polls=0
  await runVoiceQueue({state:()=>state,stopped:()=>false,synthesize:async()=>{calls++;return {run_id:'one',candidate_id:'a',voice:{status:'generating'}} as CycleVoiceResponse},inspect:async()=>{polls++;return readyVoice('a')},receive:value=>{state=mergeVoiceResponse(state,value)},wait:async()=>{}})
  assert.equal(calls,1);assert.equal(polls,1)
  state=audioState()
  await assert.rejects(runVoiceQueue({state:()=>state,stopped:()=>false,synthesize:async()=>readyVoice('wrong-id'),inspect:async()=>readyVoice('a'),receive:()=>{},wait:async()=>{}}),/不一致/)
})
