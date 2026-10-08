import test from 'node:test'
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import vm from 'node:vm'
import ts from 'typescript'
const room=await readFile(new URL('../src/views/RoomDetailView.vue',import.meta.url),'utf8')
const script=room.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1]
const ast=ts.createSourceFile('room.ts',script,ts.ScriptTarget.Latest,true)
const handlers=ast.statements.filter(n=>ts.isFunctionDeclaration(n)&&['pageIsPinnedToBottom','restorePageBottomAfterRender','updatePreservingPageBottom'].includes(n.name?.text)).map(n=>n.getText(ast)).join('\n')
function fixture(y=0,height=900){
 const ticks=[],frames=new Map(),calls=[]
 let id=0
 const state={pageBottomRestoreFrame:undefined,pageScrollUpdateEpoch:0,pageUnmounted:false,PAGE_BOTTOM_PIN_THRESHOLD:80,
  document:{documentElement:{scrollHeight:height},body:{scrollHeight:height}},
  window:{scrollY:y,scrollX:0,innerHeight:900,cancelAnimationFrame:id=>frames.delete(id),requestAnimationFrame:fn=>{frames.set(++id,fn);return id},scrollTo:value=>calls.push(value)},
  nextTick:fn=>ticks.push(fn),
 }
 vm.createContext(state);vm.runInContext(ts.transpileModule(handlers,{compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText,state)
 return {state,calls,flush(){while(ticks.length)ticks.shift()();const current=[...frames.values()];frames.clear();for(const fn of current)fn()}}
}
test('short initial page is not bottom-pinned as async dashboard grows',()=>{
 const f=fixture();assert.equal(f.state.pageIsPinnedToBottom(),false)
 f.state.updatePreservingPageBottom(()=>{f.state.document.documentElement.scrollHeight=3000})
 f.flush();assert.equal(f.calls.length,0)
 assert.match(room,/onMounted\(\(\) => \{\s*window\.scrollTo\(\{ top: 0, left: 0, behavior: 'instant' \}\)/)
})
test('top stays top during background updates while deliberate bottom reading is preserved',()=>{
 const top=fixture(0,2500);top.state.updatePreservingPageBottom(()=>{top.state.document.documentElement.scrollHeight=3000});top.flush();assert.equal(top.calls.length,0)
 const bottom=fixture(1600,2500);assert.equal(bottom.state.pageIsPinnedToBottom(),true)
 bottom.state.updatePreservingPageBottom(()=>{bottom.state.document.documentElement.scrollHeight=3000});bottom.flush();assert.equal(bottom.calls[0].top,2100)
})
test('pending render cannot pull a user back down or scroll after navigation',()=>{
 for(const y of [0,500]){const f=fixture(1600,2500);f.state.restorePageBottomAfterRender(true);f.state.window.scrollY=y;f.flush();assert.equal(f.calls.length,0)}
 const f=fixture(1600,2500);f.state.restorePageBottomAfterRender(true);f.state.pageUnmounted=true;f.flush();assert.equal(f.calls.length,0)
 const newer=fixture(1600,2500);newer.state.restorePageBottomAfterRender(true);newer.state.restorePageBottomAfterRender(false);newer.flush();assert.equal(newer.calls.length,0)
})
test('dashboard overrides back/forward saved position but other routes retain normal restoration',async()=>{
 const router=await readFile(new URL('../src/router.ts',import.meta.url),'utf8')
 const method=router.match(/scrollBehavior\([^)]*\) \{([\s\S]*?)\n  \},/)[0].replace(/,\s*$/,'')
 const state={};vm.createContext(state);vm.runInContext('function '+method,state)
 const saved={left:0,top:1900}
 assert.equal(state.scrollBehavior({name:'room-detail',hash:''},{},saved).top,0)
 assert.equal(state.scrollBehavior({name:'room-detail',hash:'#anything'},{},saved).top,0)
 assert.equal(state.scrollBehavior({name:'rooms',hash:''},{},saved),saved)
 assert.equal(state.scrollBehavior({name:'rooms',hash:'#target'},{},null).el,'#target')
})
