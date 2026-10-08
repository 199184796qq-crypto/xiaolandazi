import test from 'node:test'
import assert from 'node:assert/strict'
import { cycleProgress, cycleSpeech, normalizeCycleState, runCycleLoop } from '../src/anchorCycleRun.ts'
import type { CycleState } from '../src/anchorCycleRun.ts'

function state(): CycleState { return { run_id:'run-a',revision:0,phase:'opening',queue:[{}],config:{max_retries:3},attempts:[],accepted:[],busy:false,uncertain:false } as CycleState }
test('旧场次或异常响应的空数组字段为null时先归一化，不让页面崩溃', () => {
  const malformed = { ...state(), accepted:null, attempts:null, queue:null, recent_summaries:null, warnings:null } as unknown as CycleState
  const normalized = normalizeCycleState(malformed)
  assert.deepEqual(normalized.accepted, [])
  assert.deepEqual(normalized.attempts, [])
  assert.deepEqual(normalized.queue, [])
  assert.equal(cycleSpeech(malformed), '')
  assert.doesNotThrow(() => cycleProgress(malformed))
})
test('P0允许首次加3次重写，跳过推进后继续；不受风格0重试阻断', async () => {
  let s=state(), calls=0
  s.cursor=0;s.config.max_retries=0;s.p0_max_rewrites=3
  const result=await runCycleLoop({state:()=>s,stopped:()=>false,advance:async before=>{
    calls++
    return {run:{...before,revision:before.revision+1,cursor:calls>=4?1:0,phase:calls>=5?'ended':'opening'}}
  },receive:value=>{s=value}})
  assert.equal(calls,5);assert.equal(result,'completed')
})
test('P0暂停原因可见且不再调用模型', async () => {
  const s=state();s.status='compliance_blocked';s.error='可用组合耗尽';let calls=0
  assert.equal(cycleProgress(s).task,s.error)
  await assert.rejects(runCycleLoop({state:()=>s,stopped:()=>false,advance:async()=>{calls++;return{run:s}},receive:()=>{}}),/可用组合耗尽/)
  assert.equal(calls,0)
})
test('正文只显示采纳稿，不拼候选稿', () => {
  const s=state();s.attempts=[{text:'未采纳'}] as CycleState['attempts'];s.accepted=[{selected:{text:'采纳稿'}}] as CycleState['accepted']
  assert.equal(cycleSpeech(s),'采纳稿')
})
test('开场内容阻断展示真实原因，不误报塑价未兑现', () => {
  const s=state(); s.status='content_blocked'; s.error='开场钩子硬槽数字缺失'
  assert.equal(cycleProgress(s).task,s.error)
})
test('正文使用补标点输出，原文和原始评分仍保留', () => {
  const s=state()
  s.accepted=[{selected:{text:'都买好了不',raw_text:'  都买好了不  ',output_text:'都买好了不？',score:{overall_score:40}}}] as CycleState['accepted']
  assert.equal(cycleSpeech(s),'都买好了不？')
  assert.equal(s.accepted[0].selected.text,'都买好了不')
  assert.equal(s.accepted[0].selected.raw_text,'  都买好了不  ')
  assert.equal(s.accepted[0].selected.score.overall_score,40)
})
function progressState(): CycleState {
  return { ...state(), phase:'opening', cursor:0, queue:[{key:'老坊×产地'},{key:'新油×年份'},{key:'试吃×规则'}], config:{max_retries:3,threshold:40},accepted:[] } as CycleState
}
test('大循环进度在开始、候选重写、中间及收口按采纳结果更新', () => {
  const s=progressState()
  let p=cycleProgress(s)
  assert.equal(p.round,1);assert.equal(p.percent,0);assert.equal(p.task,'老坊×产地');assert.equal(p.bodyTotal,2)
  s.attempts=[{}] as CycleState['attempts']
  p=cycleProgress(s)
  assert.equal(p.nextAttempt,2);assert.equal(p.done,0);assert.equal(p.covered,0)
  s.accepted=[{phase:'opening',task:s.queue[0],selected:{score:{overall_score:74},generation_ms:11230},attempts:[{generation_ms:11230}]}] as CycleState['accepted']
  s.phase='body';s.cursor=1;s.attempts=[]
  p=cycleProgress(s)
  assert.equal(p.percent,25);assert.equal(p.openingDone,true);assert.equal(p.task,'新油×年份');assert.equal(p.lastScore,74);assert.equal(p.lastGenerationSeconds,11.23)
  s.accepted.push({phase:'body',task:s.queue[1],selected:{score:{overall_score:62},generation_ms:9020},attempts:[{generation_ms:9020},{generation_ms:7752}]} as CycleState['accepted'][number])
  s.phase='closing';s.cursor=3
  p=cycleProgress(s)
  assert.equal(p.bodyDone,1);assert.equal(p.covered,2);assert.match(p.task,/本轮收口/);assert.equal(p.lastTaskGenerationSeconds,16.772)
})
test('收口采纳后显示第1轮100%完成，不虚增第2轮或已讲卖点', () => {
  const s=progressState()
  s.accepted=s.queue.map((task,i)=>({phase:i===0?'opening':'body',task,selected:{score:{overall_score:60}},attempts:[]})) as CycleState['accepted']
  s.accepted.push({phase:'closing',task:{key:'本轮收口'},selected:{score:{overall_score:60}},attempts:[]} as CycleState['accepted'][number])
  s.phase='ended';s.cursor=3
  const p=cycleProgress(s)
  assert.equal(p.round,1);assert.equal(p.percent,100);assert.equal(p.done,4);assert.equal(p.bodyDone,2);assert.equal(p.covered,3);assert.equal(p.closingDone,true)
})
test('无卖点直接收口的进度不会出现负段数或除零', () => {
  const s=progressState();s.queue=[];s.phase='closing'
  const p=cycleProgress(s)
  assert.equal(p.total,1);assert.equal(p.bodyTotal,0);assert.equal(p.percent,0)
})
test('循环开场显示第2轮接新版、任务索引及本轮进度，不把覆盖数当指针', () => {
  const s=progressState()
  s.config.rolling=true;s.config.body_segments=3;s.config.max_cycles=2
  s.cycle_number=2;s.opening_count=1;s.cursor=8;s.task_index=1
  s.accepted=[{phase:'opening',task:s.queue[0],opening_version:'完整版',selected:{score:{overall_score:70}},attempts:[]},{phase:'closing',task:{key:'本轮收口'},selected:{score:{overall_score:70}},attempts:[]}] as CycleState['accepted']
  let p=cycleProgress(s)
  assert.equal(p.round,2);assert.equal(p.openingVersion,'接新版');assert.equal(p.total,5);assert.equal(p.percent,0);assert.equal(p.task,'新油×年份')
  s.accepted.push({phase:'opening',task:s.queue[1],opening_version:'接新版',selected:{score:{overall_score:70}},attempts:[]} as CycleState['accepted'][number])
  s.phase='body';s.opening_count=2
  p=cycleProgress(s)
  assert.equal(p.openingVersion,'接新版');assert.equal(p.percent,20);assert.equal(p.covered,2)
})
test('持续大循环不会到旧单轮请求上限就停止，仍能手动暂停', async () => {
  let s=progressState(),calls=0
  s.config.rolling=true;s.config.max_cycles=0;s.config.body_segments=2;s.config.max_retries=0
  const result=await runCycleLoop({state:()=>s,stopped:()=>calls>=25,advance:async before=>{calls++;return{run:{...before,cursor:before.cursor+1,revision:before.revision+1,cycle_number:Math.floor(calls/4)+1}}},receive:value=>{s=value}})
  assert.equal(result,'paused');assert.equal(calls,25)
})
test('间隔不满足时不再请求模型；修订号变化但任务永不推进也会停止', async () => {
  let s=progressState(),calls=0
  s.status='spacing_blocked';s.error='避重间隔不满足'
  await assert.rejects(runCycleLoop({state:()=>s,stopped:()=>false,advance:async before=>{calls++;return{run:before}},receive:()=>{}}),/间隔不满足/)
  assert.equal(calls,0)
  s=progressState();s.config.max_retries=0
  await assert.rejects(runCycleLoop({state:()=>s,stopped:()=>false,advance:async before=>{calls++;return{run:{...before,revision:before.revision+1}}},receive:value=>{s=value}}),/请求保护上限/)
  assert.equal(calls,2)
})
test('未采纳候选仍驱动当前任务，直到收口结束', async () => {
  let s=state(),calls=0
  const outcome=await runCycleLoop({state:()=>s,stopped:()=>false,advance:async before=>{calls++;return {run:{...before,revision:before.revision+1,phase:calls===4?'ended':before.phase}}},receive:value=>{s=value}})
  assert.equal(calls,4);assert.equal(outcome,'completed')
})
test('暂停不发下一轮', async () => {
  let s=state(),pause=false,calls=0
  const result=await runCycleLoop({state:()=>s,stopped:()=>pause,advance:async before=>{calls++;pause=true;return {run:{...before,revision:before.revision+1}}},receive:value=>{s=value}})
  assert.equal(result,'paused');assert.equal(calls,1)
})
test('结束后晚到结果不能复活本场', async () => {
  let s:CycleState|null=state(),received=false
  const result=await runCycleLoop({state:()=>s,stopped:()=>false,advance:async before=>{s=null;return {run:{...before,revision:1}}},receive:()=>{received=true}})
  assert.equal(result,'paused');assert.equal(received,false)
})
test('版本不变和不确定返回不能盲目重试', async () => {
  const s=state();let calls=0
  await assert.rejects(runCycleLoop({state:()=>s,stopped:()=>false,advance:async()=>{calls++;return {run:s}},receive:()=>{}}),/没有推进/)
  assert.equal(calls,1);s.uncertain=true
  await assert.rejects(runCycleLoop({state:()=>s,stopped:()=>false,advance:async()=>{calls++;return {run:s}},receive:()=>{}}),/不能盲目重发/)
  assert.equal(calls,1)
})
