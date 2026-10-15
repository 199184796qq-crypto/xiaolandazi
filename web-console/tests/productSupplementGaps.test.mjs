import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { effectScope, reactive, nextTick, createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'

let server, useGaps, component
before(async () => {
  server = await createServer({ server: { middlewareMode: true, hmr: false }, appType: 'custom' })
  useGaps = (await server.ssrLoadModule('/src/productSupplementGaps.ts')).useProductSupplementGaps
  component = (await server.ssrLoadModule('/src/components/ProductSupplementChat.vue')).default
})
after(async () => { await server?.close() })
const deferred = () => { let resolve, reject; const promise = new Promise((a,b) => { resolve=a; reject=b }); return { promise, resolve, reject } }
const gap = title => ({ 维度: title, 层级: 'missing', 待补事实: title, 填写提示: ['请提供已确认的资料'] })
function setup(check, fields={}) {
  const scope = reactive({ planId:4,tenantId:14,roomId:26,actorKey:'14:2',...fields })
  const lifecycle=effectScope(), state=lifecycle.run(() => useGaps(() => ({ ...scope }), check))
  return { scope,state,close:() => lifecycle.stop() }
}
test('初始页面同时显示左侧清单和右侧对话，无视图切换入口', async () => {
  const html=await renderToString(createSSRApp(component,{planId:0}))
  assert.ok(html.indexOf('aria-label="补全清单"') < html.indexOf('aria-label="商品资料补充对话"'))
  assert.ok(html.includes('aria-label="商品资料补充对话"'))
  assert.ok(html.includes('product-supplement-input'))
  assert.equal(html.includes('返回补全清单'),false)
  assert.equal(html.includes('进入对话'),false)
  assert.equal(html.includes('aria-label="待确认资料"'),false)
})
test('换方案、租户、直播间及账号丢弃旧清单与迟到响应', async () => {
  for(const [field,value] of Object.entries({planId:5,tenantId:15,roomId:27,actorKey:'14:3'})) {
    const first=deferred(), second=deferred(); let calls=0, oldSignal
    const testState=setup(async (_,__,signal) => { calls++; if(calls===1) {oldSignal=signal;return first.promise} return second.promise })
    testState.state.skipped.value=['旧问题']
    testState.scope[field]=value
    assert.equal(oldSignal.aborted,true)
    assert.deepEqual(testState.state.skipped.value,[])
    first.resolve({gaps:[gap('旧方案')],summary:''}); await new Promise(resolve => setImmediate(resolve))
    assert.equal(testState.state.gaps.value.length,0)
    second.resolve({gaps:[gap('新方案')],summary:''}); await new Promise(resolve => setImmediate(resolve))
    assert.equal(testState.state.gaps.value[0].维度,'新方案')
    assert.equal(calls,2); testState.close()
  }
})
test('失败清单可以单独刷新，刷新后恢复跳过项目', async () => {
  let calls=0
  const s=setup(async () => {if(++calls===1)throw Error('读取失败');return {gaps:[gap('配送')],summary:''}})
  await nextTick(); assert.equal(s.state.loading.value,false); assert.ok(s.state.error.value.includes('失败'))
  s.state.skipped.value=['配送']; await s.state.reload()
  assert.equal(s.state.error.value,'');assert.equal(s.state.gaps.value.length,1);assert.deepEqual(s.state.skipped.value,[])
  s.close()
})
test('未选择方案和已卸载组件不发起或接收清单请求', async () => {
  let calls=0
  const empty=setup(async () => {calls++;return {gaps:[],summary:''}}, {planId:0})
  await empty.state.reload();assert.equal(calls,0);empty.close()
  const pending=deferred();let signal
  const s=setup(async (_,__,inputSignal) => {signal=inputSignal;return pending.promise})
  s.close();assert.equal(signal.aborted,true)
  pending.resolve({gaps:[gap('迟到')],summary:''});await nextTick()
  assert.equal(s.state.gaps.value.length,0)
})

test('链接切换绑定商品ID并取消旧清单；全部省略商品范围', async () => {
  const first=deferred(), second=deferred(); const calls=[]
  const s=setup(async (plan,input,signal) => { calls.push({plan,input,signal}); return calls.length===1 ? first.promise : second.promise })
  s.scope.productLinkId=12
  assert.equal(calls[0].signal.aborted,true)
  assert.equal(calls[1].input.product_link_id,12)
  first.resolve({gaps:[gap('旧范围')],summary:''}); await new Promise(resolve => setImmediate(resolve))
  assert.equal(s.state.gaps.value.length,0)
  second.resolve({gaps:[gap('2号商品')],summary:''}); await new Promise(resolve => setImmediate(resolve))
  assert.equal(s.state.gaps.value[0].维度,'2号商品')
  s.scope.productLinkId=undefined
  assert.equal(calls.at(-1).input.product_link_id,undefined)
  s.close()
})
