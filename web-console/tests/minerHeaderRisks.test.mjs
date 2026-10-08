import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import vm from 'node:vm'
import ts from 'typescript'

const source = await readFile(new URL('../src/components/SpeechMinerWorkbench.vue', import.meta.url), 'utf8')
const ast = ts.createSourceFile('miner.ts', source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1], ts.ScriptTarget.Latest, true)
const handlers = ast.statements.filter(node => ts.isFunctionDeclaration(node) && ['headerRisks', 'locatedHeaderRisk'].includes(node.name?.text)).map(node => node.getText(ast)).join('\n')
test('name, audience and pain have actionable risks, without repeating confirmed or angle risks', () => {
  const reviews = [
    { claim_sha256: 'audience', status: 'pending', source_key: 'wheel/5/audience' },
    { claim_sha256: 'merged', status: 'pending', source_key: 'facts/1/0', sources: [{ source_key: 'wheel/5/name' }] },
    { claim_sha256: 'confirmed', status: 'confirmed', source_key: 'wheel/5/pain' },
    { claim_sha256: 'angle', status: 'pending', source_key: 'wheel/5/angle/0' },
    { claim_sha256: 'other', status: 'pending', source_key: 'wheel/4/audience' },
  ]
  const state = { props: { riskReviews: reviews }, riskSources: review => review.sources?.length ? review.sources : [{source_key: review.source_key}], locatedRiskSource: { value: 'wheel/5/audience' } }
  vm.createContext(state)
  vm.runInContext(ts.transpileModule(handlers, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, state)
  assert.deepEqual(Array.from(state.headerRisks(5), item => item.claim_sha256), ['audience', 'merged'])
  assert.equal(state.locatedHeaderRisk(5), 'audience')
  assert.match(source, /SourceRiskDetails :reviews="headerRisks\(pointIndex\)"/)
  assert.match(source, /@confirm="emit\('confirmRisk', \$event\)"/)
})

const panel = await readFile(new URL('../src/components/FactRiskEvidencePanel.vue', import.meta.url), 'utf8')
test('confirmation invalidates old refresh and semantic responses', async () => {
  const script = panel.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1]
  const tree = ts.createSourceFile('panel.ts', script, ts.ScriptTarget.Latest, true)
  const funcs = tree.statements.filter(node => ts.isFunctionDeclaration(node) && ['reviewSemantics', 'confirm'].includes(node.name?.text)).map(node => node.getText(tree)).join('\n')
  let resolveReview
  const reviewPromise = new Promise(resolve => { resolveReview = resolve })
  const state = {
    epoch: 1, active: true, props: { planId: 4 }, customerCanConfirm: {value: true},
    semanticLoading: {value: false}, loading: {value: false}, items: {value: [{ severity:'P0', semantic_decision:'pending' }]},
    drafts: {value:{}}, error: {value:''}, notice: {value:''},
    url: suffix => suffix || '', emit: () => { throw new Error('stale semantic result emitted') },
    request: path => path === '/semantic-review' ? reviewPromise : Promise.resolve({}),
    load: async () => { state.items.value = [{status:'confirmed'}] },
  }
  vm.createContext(state)
  vm.runInContext(ts.transpileModule(funcs, {compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText, state)
  const pending = state.reviewSemantics()
  await state.confirm({ severity:'P0', semantic_decision:'pass', claim_sha256:'hash' })
  resolveReview({items:[{status:'pending'}], reviewed:1})
  await pending
  assert.equal(state.items.value[0].status, 'confirmed')
  assert.equal(state.epoch, 2)
})
