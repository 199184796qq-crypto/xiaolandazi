import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import ts from 'typescript'

const source = await readFile(new URL('../src/components/SystemAgentLayer.vue', import.meta.url), 'utf8')
const start = source.indexOf('async function prepareLiveFactIntentAction(')
const end = source.indexOf('\nfunction liveScriptExecutionModeFromText', start)
assert.ok(start > 0 && end > start)
const js = ts.transpile(source.slice(start, end), { target: ts.ScriptTarget.ES2022 })
let facts = [], reads = 0
const catalog = { error: '', items: [{ value: 'trade', label: '交易 / 售后', family: 'trade' }] }
const prepare = new Function('loadFactCategories', 'factCategoryState', 'intentString', 'getLiveAgentPlanFacts', js + '\nreturn prepareLiveFactIntentAction;')(
  async () => {}, catalog, value => String(value ?? '').trim(), async () => { reads++; return { items: facts } },
)

test('new category and two wording fields survive the real confirmation builder', async () => {
  facts = []
  const result = await prepare(202, 303, '用户明确要求新增', {
    intent: 'fact.add', target: { fact_key: '价格算法' }, changes: {
      new_fact_category: { label: '算账口径', family: 'product' }, fact_value: '按实际价格计算', forbidden_wording: '不能虚构差价', safe_rewrite: '按现价算账',
    },
  })
  assert.equal(result.action.type, 'add_live_fact')
  assert.deepEqual(result.action.payload.new_fact_category, { label: '算账口径', family: 'product' })
  assert.equal(result.action.payload.forbidden_wording, '不能虚构差价')
  assert.equal(result.action.payload.safe_rewrite, '按现价算账')
  assert.equal(result.action.payload.fact_value, '按实际价格计算')
  assert.equal(result.action.requires_confirmation, true)
  assert.deepEqual(facts, [], 'preparation must not persist anything')
})

test('wording-only edits preserve fact and untouched fields, and explicit clear is visible', async () => {
  facts = [{ id: 7, category: 'trade', key: '售后', value: '真实售后事实', forbidden_wording: '原禁说', safe_rewrite: '原替代', version_no: 3 }]
  let result = await prepare(202, 303, '修改售后说法', { intent: 'fact.update', target: { fact_key: '售后' }, changes: { safe_rewrite: '新替代' } })
  assert.equal(result.action.type, 'confirm_live_fact_update')
  assert.equal(result.action.payload.fact_value, '真实售后事实')
  assert.equal(result.action.payload.safe_rewrite, '新替代')
  assert.equal(result.action.payload.forbidden_wording, undefined)
  assert.equal(result.action.payload.current_safe_rewrite, '原替代')
  assert.equal(result.action.payload.current_version_no, 3)
  result = await prepare(202, 303, '清空替代说法', { intent: 'fact.update', target: { fact_key: '售后' }, changes: { safe_rewrite: '' } })
  assert.equal(result.action.payload.safe_rewrite, '')
  result = await prepare(202, 303, '清空事实', { intent: 'fact.update', target: { fact_key: '售后' }, changes: { fact_value: '' } })
  assert.equal(result.action, undefined, 'empty actual fact must not be saved')
})

test('unchanged facts can still change boundaries, existing category is reused, failures do not write', async () => {
  let result = await prepare(202, 303, '修改禁说', { intent: 'fact.update', target: { fact_key: '售后' }, changes: { fact_value: '真实售后事实', forbidden_wording: '新禁说' } })
  assert.ok(result.action)
  facts = []
  result = await prepare(202, 303, '新增', { intent: 'fact.add', target: { fact_key: '另一条' }, changes: { new_fact_category: { label: '交易 / 售后' }, fact_value: '真实内容' } })
  assert.equal(result.action.payload.fact_category, 'trade')
  assert.equal(result.action.payload.new_fact_category, undefined)
  catalog.error = '模拟读取失败'
  result = await prepare(202, 303, '新增', { intent: 'fact.add', changes: { fact_value: '真实内容' } })
  assert.equal(result.action, undefined)
  catalog.error = ''
  assert.ok(reads > 0)
  assert.ok(source.includes('<dt>不能这么说</dt>') && source.includes('<dt>可以这么说</dt>'))
  assert.ok(source.includes("'add_live_fact_category'"))
})
