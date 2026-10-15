import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { effectScope, reactive } from 'vue'

let server, useStrategies, inPlan
before(async () => {
  server = await createServer({ server: { middlewareMode:true, hmr:false }, appType:'custom' })
  ;({ usePlanAnswerStrategies:useStrategies, memoryInPlan:inPlan } = await server.ssrLoadModule('/src/planAnswerStrategies.ts'))
})
after(async () => { await server?.close() })
const deferred = () => { let resolve,reject;const promise=new Promise((a,b)=>{resolve=a;reject=b});return {promise,resolve,reject} }
const tick = () => new Promise(resolve=>setImmediate(resolve))
const memory=(id,plan=4,other={})=>({id,tenant_id:14,room_id:26,status:'active',memory_type:'semantic',memory_key:`plan:${plan}:rule`,current_version:{structured:{review_plan_id:plan}},...other})
function setup(load) {
  const scope=reactive({tenantId:14,roomId:26,planId:4,actorKey:'14:14'}), lifecycle=effectScope()
  const state=lifecycle.run(()=>useStrategies(()=>({...scope}),load))
  return {scope,state,close:()=>lifecycle.stop()}
}
test('列表只接收当前租户、直播间、方案的有效已绑定记忆',async()=>{
  const t=setup(async(room,plan)=>{assert.equal(room,26);assert.equal(plan,4);return {items:[memory(1),memory(2,5),memory(3,0),memory(4,4,{tenant_id:15}),memory(5,4,{room_id:27}),memory(6,4,{status:'inactive'}),memory(7,4,{memory_key:'plan:5:wrong'}),memory(8,4,{current_version:undefined})]}})
  await tick();assert.deepEqual(t.state.items.value.map(m=>m.id),[1]);t.close()
})
test('切换方案立即清空，切回同一方案也不能接受旧请求',async()=>{
  const pending=[];const t=setup(async()=>{const p=deferred();pending.push(p);return p.promise})
  t.scope.planId=5;assert.equal(t.state.items.value.length,0)
  t.scope.planId=4;assert.equal(pending.length,3)
  pending[2].resolve({items:[memory(3)]});await tick()
  pending[1].resolve({items:[memory(2,5)]});pending[0].resolve({items:[memory(1)]});await tick()
  assert.deepEqual(t.state.items.value.map(m=>m.id),[3]);t.close()
})
test('换租户、直播间、账号和取消方案均丢弃旧列表及迟到回执',async()=>{
  for(const [key,value] of Object.entries({tenantId:15,roomId:27,actorKey:'14:99',planId:0})) {
    const p=deferred();let calls=0;const t=setup(async()=>{calls++;return calls===1?p.promise:deferred().promise})
    t.scope[key]=value;assert.equal(t.state.items.value.length,0)
    p.resolve({items:[memory(1)]});await tick()
    assert.equal(t.state.items.value.length,0)
    if(key==='planId')assert.equal(calls,1)
    t.close()
  }
})
test('读取失败明确显示且可重试',async()=>{
  let first=true;const t=setup(async()=>{if(first)throw new Error('读取失败');return {items:[memory(1)]}})
  await tick();assert.equal(t.state.error.value,'读取失败');assert.equal(t.state.items.value.length,0)
  first=false;await t.state.refresh();assert.equal(t.state.error.value,'');assert.equal(t.state.items.value.length,1);t.close()
})
test('卸载后迟到响应不再改写状态',async()=>{
  const p=deferred();const t=setup(()=>p.promise);t.close();p.resolve({items:[memory(1)]});await tick();assert.equal(t.state.items.value.length,0)
})
