import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'

let server, key, read, remember, restore
const scope = { userId:14,tenantId:14,roomId:26,supportSession:false }
before(async()=>{
  server=await createServer({server:{middlewareMode:true,hmr:false},appType:'custom'})
  const mod=await server.ssrLoadModule('/src/planSelectionPreference.ts')
  key=mod.planSelectionPreferenceKey;read=mod.readSelectedPlan;remember=mod.rememberSelectedPlan;restore=mod.restoredPlanSelection
})
after(async()=>{await server?.close()})
const storage=()=>{const data=new Map();return {getItem:k=>data.get(k)??null,setItem:(k,v)=>data.set(k,v)}}
test('选择方案5后刷新优先恢复5，临时清空不会抹掉记忆',()=>{
  const s=storage();remember(scope,5,s);remember(scope,null,s)
  assert.equal(read(scope,s),5)
  assert.equal(restore(new Set([4,5]),null,read(scope,s),4,[4]),5)
  assert.equal(restore(new Set([4,5]),4,5,5,[5]),4,'当前手动选择优先于运行方案和旧偏好')
})
test('账号、租户、直播间和授权协助各自隔离',()=>{
  const s=storage();remember(scope,5,s)
  for(const fields of [{userId:15},{tenantId:15},{roomId:27},{supportSession:true}]) {
    const other={...scope,...fields};assert.notEqual(key(other),key(scope));assert.equal(read(other,s),null);remember(other,4,s);assert.equal(read(scope,s),5)
  }
  assert.equal(key({...scope,userId:0}),'');remember({...scope,userId:0},7,s);assert.equal(read({...scope,userId:0},s),null)
})
test('删除、归档、跨租户或不可访问的方案不在有效集合时安全回退',()=>{
  assert.equal(restore(new Set([4]),null,5,4,[5,4]),4)
  assert.equal(restore(new Set([6]),null,5,4,[5,4,6]),6)
  assert.equal(restore(new Set(),null,5,4,[5,4]),null)
})
test('本地存储禁用、损坏或非法编号不阻断页面',()=>{
  const denied={getItem:()=>{throw Error('denied')},setItem:()=>{throw Error('denied')}}
  assert.equal(read(scope,denied),null);assert.doesNotThrow(()=>remember(scope,5,denied))
  for(const value of ['NaN','-1','0','1.5','9007199254740999'])assert.equal(read(scope,{getItem:()=>value}),null)
})
