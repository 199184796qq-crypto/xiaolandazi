import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import vm from 'node:vm'
import ts from 'typescript'
const source = await readFile(new URL('../src/riskSources.ts',import.meta.url),'utf8')
const code = ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText
const scope = {exports:{}}; vm.createContext(scope); vm.runInContext(code,scope)
export const {reviewsForArea,pendingForArea,riskSources,sourceArea} = scope.exports
test('product-card high oleic is not a fact-library pending badge',()=>{
 const reviews=[{claim_sha256:'oleic',status:'pending',source_key:'facts/7/6',sources:[{source_key:'facts/7/6',text:'高油酸',area:'products',entity_id:21,field:'健康/营养型卖点'}]}]
 assert.equal(pendingForArea(reviews,'products'),1)
 assert.equal(pendingForArea(reviews,'knowledge'),0)
 assert.equal(reviewsForArea(reviews,'products',21).length,1)
 assert.equal(reviewsForArea(reviews,'products',22).length,0)
 reviews[0].status='confirmed'
 for(const area of ['products','benefits','knowledge','miner']) assert.equal(pendingForArea(reviews,area),0)
})
test('merged approvals are counted once per actual area and retained on their own cards',()=>{
 const review={status:'pending',sources:[{source_key:'facts/1/0',area:'knowledge',entity_id:11},{source_key:'facts/2/6',area:'products',entity_id:21},{source_key:'facts/2/7',area:'products',entity_id:21},{source_key:'wheel/0/angle/0',area:'miner'}]}
 assert.equal(pendingForArea([review],'products'),1)
 assert.equal(pendingForArea([review],'knowledge'),1)
 assert.equal(pendingForArea([review],'miner'),1)
 assert.equal(pendingForArea([review],'benefits'),0)
})
test('unknown legacy fact-package sources are not guessed to belong to facts',()=>{
 assert.equal(pendingForArea([{status:'pending',source_key:'facts/7/6'}],'knowledge'),0)
 assert.equal(pendingForArea([{status:'pending',source_key:'wheel/0/name'}],'miner'),1)
})
