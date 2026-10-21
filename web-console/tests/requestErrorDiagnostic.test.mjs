import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import ts from 'typescript'
const source=await readFile(new URL('../src/api.ts',import.meta.url),'utf8')
const helper=await readFile(new URL('../src/errorDiagnostic.ts',import.meta.url),'utf8')
const diagnostic=await import('data:text/javascript;charset=utf-8,'+encodeURIComponent(ts.transpile(helper,{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.ESNext})))
const part=source.slice(source.indexOf('export async function request<'),source.indexOf('\nexport function getBootstrap'))
const javascript=ts.transpile(part,{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS})
function fixture(fetcher){
 const notices=[],cleared=[]
 const exports={}
 new Function('exports','fetch','showRequestError','clearRequestError','liveSupportRoomForRequest','readPreviewStream',...Object.keys(diagnostic),javascript)(exports,fetcher,(...args)=>notices.push(args),(...args)=>cleared.push(args),()=>null,async()=>({streamed:true}),...Object.values(diagnostic))
 return {request:exports.request,notices,cleared}
}
test('real request keeps the Chinese JSON failure and operation without technical fields',async()=>{
 const f=fixture(async()=>new Response(JSON.stringify({error:{message:'声音已停用 api_key="private secret"',code:'voice_disabled',stage:'声音配置校验'}}),{status:422,headers:{'X-Request-ID':'req-real-389'}}))
 await assert.rejects(f.request('/api/v1/rooms/29/live-runtime?token=private-token',{method:'POST'}),error=>{
  for(const part of ['声音已停用','声音配置校验','发生操作：直播控制与状态读取'])assert.ok(error.message.includes(part),part)
  assert.doesNotMatch(error.message,/[A-Za-z]|422|private secret|private-token|\/api|错误码|请求编号/)
  assert.equal(error.message.match(/发生操作/g).length,1)
  return true
 })
 assert.equal(f.notices.length,1)
})
test('gateway and missing-cause failures are Chinese and never invent a voice failure',async()=>{
 const f=fixture(async()=>new Response('<html><title>502 Bad Gateway</title><body>internal secret details</body></html>',{status:502}))
 await assert.rejects(f.request('/api/v1/plans'),error=>{
  assert.match(error.message,/上游服务暂时无法正常响应/)
  assert.doesNotMatch(error.message,/[A-Za-z]|502|<html>|internal secret|声音/)
  return true
 })
 const empty=fixture(async()=>new Response('',{status:500}))
 await assert.rejects(empty.request('/api/v1/plans'),/尚未返回具体原因/)
})
test('network errors, abort, malformed success, 204 and preview remain distinct',async()=>{
 const network=fixture(async()=>{throw new TypeError('Failed to fetch')})
 await assert.rejects(network.request('/api/v1/plans'),/未收到服务响应/)
 const abort=fixture(async()=>{throw new DOMException('cancelled','AbortError')})
 await assert.rejects(abort.request('/api/v1/plans'),{name:'AbortError'});assert.equal(abort.notices.length,0)
 const malformed=fixture(async()=>new Response('<html>login proxy</html>',{status:200}))
 await assert.rejects(malformed.request('/api/v1/plans'),error=>{assert.match(error.message,/返回的数据格式异常/);assert.doesNotMatch(error.message,/[A-Za-z]|200|错误码|请求位置/);return true})
 const success=fixture(async()=>new Response(null,{status:204}))
 assert.equal(await success.request('/api/v1/plans'),undefined)
 const suppressed=fixture(async()=>new Response('{"error":"permission denied"}',{status:403}))
 await assert.rejects(suppressed.request('/api/v1/plans',{suppressErrorDialog:true}),/没有访问权限/);assert.equal(suppressed.notices.length,0)
 const preview=fixture(async()=>new Response('data: ready\n\n',{headers:{'Content-Type':'text/event-stream'}}))
 assert.deepEqual(await preview.request('/api/v1/preview',{},()=>{}),{streamed:true})
})
