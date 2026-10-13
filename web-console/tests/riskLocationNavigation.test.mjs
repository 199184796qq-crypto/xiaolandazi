import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import vm from 'node:vm'
import ts from 'typescript'
import { reviewsForArea, riskSources, sourceArea } from './riskSources.test.mjs'

// Execute the actual view handlers, with only UI/async dependencies replaced.
const source = await readFile(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1]
const ast = ts.createSourceFile('view.ts', script, ts.ScriptTarget.Latest, true)
const handlers = ast.statements.filter(node => ts.isFunctionDeclaration(node) &&
  ['selectPlanConfigurationMode', 'openFactRiskLocation', 'openSourceRiskLocation', 'locatePendingRiskLocation'].includes(node.name?.text)).map(node => node.getText(ast)).join('\n')
const code = ts.transpileModule(handlers, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText
function fixture() {
  const calls = []
  const state = {
    reviewsForArea, riskSources, sourceArea,
    activeMode: { value: 'knowledge' }, pendingRiskLocation: { value: null }, riskLocationAttempt: 0,
    locatedFactRiskHash: { value: '' }, expandedFactRiskId: { value: null },
    formalFacts: { value: [] }, factRiskReviews: { value: [] },
    nextTick: async () => {}, editRiskSource: async payload => calls.push(payload),
    factRisksForFact: item => item.reviews,
    document: { querySelectorAll: () => [], querySelector: () => null }, window: { matchMedia: () => ({ matches: true }) },
    productCards: { value: { locateRisk: async (...args) => calls.push(args) } }, expandedFormalBenefitIds: {value:new Set()},
  }
  vm.createContext(state); vm.runInContext(code, state)
  return { state, calls }
}
test('task explanation opens directly even when old direction risks exist', async () => {
  const { state, calls } = fixture()
  state.factRiskReviews.value = [
    { claim_sha256: 'old', status: 'confirmed', source_key: 'wheel/0/name' },
    { claim_sha256: 'fact', status: 'pending', source_key: 'facts/0/0' },
    { claim_sha256: 'merged', status: 'pending', source_key: 'facts/1/0', sources: [{ source_key: 'wheel/4/angle/6' }] },
  ]
  state.selectPlanConfigurationMode('miner')
  assert.equal(state.activeMode.value, 'miner')
  assert.deepEqual(calls, [])
  assert.equal(state.pendingRiskLocation.value, null)
})
test('risk location respects leaving the page before next tick', async () => {
  const { state, calls } = fixture()
  const review = { claim_sha256: 'pending', status: 'pending', source_key: 'facts/2/0', sources: [{source_key:'facts/2/0',area:'knowledge',entity_id:20}] }
  state.factRiskReviews.value = [review]
  state.formalFacts.value = [{id:20,reviews:[review]}]
  state.nextTick = async () => { state.activeMode.value = 'products' }
  await state.openFactRiskLocation()
  assert.deepEqual(calls, [])
  assert.equal(state.activeMode.value, 'products')
})
test('fact badge expands and focuses exact review rather than confirmed rows', async () => {
  const { state } = fixture(), calls = []
  const review = { claim_sha256: 'pending', status: 'pending', source_key: 'facts/2/0', sources: [{source_key:'facts/2/0',text:'test',area:'knowledge',entity_id:20}] }
  state.factRiskReviews.value = [{ ...review, claim_sha256: 'old', status: 'confirmed' }, review]
  state.formalFacts.value = [{ id: 20, reviews: [review] }]
  state.document.querySelectorAll = () => [{ getAttribute: () => 'pending', getClientRects: () => [{}], scrollIntoView: () => calls.push('scroll'), focus: () => calls.push('focus') }]
  await state.openFactRiskLocation()
  assert.equal(state.expandedFactRiskId.value, 20)
  assert.equal(state.locatedFactRiskHash.value, 'pending')
  assert.deepEqual(calls, ['scroll', 'focus'])
})

test('product badge opens the exact product and field, not the fact library',async()=>{
 const {state,calls}=fixture()
 state.factRiskReviews.value=[{claim_sha256:'oleic',status:'pending',source_key:'facts/7/6',sources:[{source_key:'facts/7/6',area:'products',entity_id:21,field:'健康/营养型卖点'}]}]
 await state.openSourceRiskLocation('products')
 assert.equal(state.activeMode.value,'products')
 assert.equal(state.locatedFactRiskHash.value,'oleic')
 assert.deepEqual(calls,[[21,'健康/营养型卖点']])
 assert.equal(state.expandedFactRiskId.value,null)
})
