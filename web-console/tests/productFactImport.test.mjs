import assert from 'node:assert/strict'
import { readFile, writeFile, mkdtemp, unlink, rmdir } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'
import ts from 'typescript'
const dir = await mkdtemp(join(tmpdir(), 'product-fact-import-test-'))
const files = ['productFactImport.mjs', 'benefitScopeDisplay.mjs']
try {
 for (const name of ['productFactImport', 'benefitScopeDisplay']) {
  const source = await readFile(new URL(`../src/${name}.ts`, import.meta.url), 'utf8')
  const js = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText.replace("'./benefitScopeDisplay'", "'./benefitScopeDisplay.mjs'")
  await writeFile(join(dir, name + '.mjs'), js)
 }
 const { buildProductFactImportRows: build, importProductFacts: run } = await import(pathToFileURL(join(dir, files[0])))
 const ctx = { plan: 4, tenant: 14 }
 const p = { id: 1, plan_id: 4, tenant_id: 14, link_key: '1号链接', product_name: '一号套餐', spec: '10人餐', daily_price: '337', field_labels: { daily_price: '套餐售价' }, quantity: '1', audience: '10人', status: 'active', version_no: 1, attributes: [{ id: 31, tenant_id: 14, plan_id: 4, product_link_id: 1, label: '内容', value: '22道菜', status: 'active' }, { id: 32, tenant_id: 99, plan_id: 4, product_link_id: 1, label: '外部', value: '不能导入', status: 'active' }] }
 const b = { id: 9, tenant_id: 14, plan_id: 4, key: '饮料赠礼', link_key: '已改名', gift: '饮料一份', activity_price: '300', validity_mode: 'scheduled', starts_at: '2026-10-01T00:00:00Z', ends_at: '2026-10-31T00:00:00Z', status: 'active' }
 const now = Date.parse('2026-10-17T00:00:00Z')
 const built = await build(ctx, [p, { ...p, id: 2, link_key: '2号链接', attributes: [] }, { ...p, id: 3, tenant_id: 99 }, { ...p, id: 4, status: 'disabled' }, { ...p, id: 5, plan_id: 5 }], [b, { ...b, id: 10, ends_at: '2026-10-02T00:00:00Z' }, { ...b, id: 11, starts_at: '2026-11-01T00:00:00Z' }], [{ benefit_id: 9, all: false, products: [2], version_no: 1 }], now)
 assert.equal(built.rows.length, 13); assert.equal(built.skippedBenefits, 2)
 assert.ok(built.rows.every(r => [1, 2].includes(r.product)))
 assert.ok(built.rows.some(r => r.value.includes('套餐售价：337')))
 assert.ok(built.rows.filter(r => r.source.startsWith('benefit:')).every(r => r.product === 2 && r.value.includes(b.ends_at) && r.value.includes(b.starts_at)))
 assert.ok(!built.rows.some(r => r.value.includes('不能导入')))
 let store = [], adopts = 0, scopes = 0, scopeFails = false, blocked = false, foreign = false, alive = true, switchOnAdopt = false, accepted = 0
 const io = {
  current: () => alive, progress() {}, accept() { accepted++ },
  async adopt(candidate) { adopts++; if (blocked) return { results: [{ status: 'blocked', message: '未通过整理' }] }; const saved = { ...candidate, value: '模型整理：' + candidate.value, id: store.length + 1, plan_id: foreign ? 5 : 4, tenant_id: 14, status: 'active', source_type: 'manual_create', version_no: 1 }; store.push(saved); if (switchOnAdopt) alive = false; return { results: [{ status: 'adopted', saved }] } },
  async scope(fact, id) { scopes++; if (scopeFails) throw Error('版本冲突'); const saved = { ...fact, version_no: fact.version_no + 1, scope: { kind: 'product', product_link_id: id } }; store = store.map(f => f.id === fact.id ? saved : f); return saved },
 }
 let result = await run(ctx, built.rows, [], io); assert.equal(result.imported, 13); assert.equal(result.failures.length, 0); assert.ok(store.every(f => f.scope?.kind === 'product'))
 result = await run(ctx, built.rows, store, io); assert.equal(result.existing, 13); assert.equal(adopts, 13); assert.equal(scopes, 13)
 store[0] = { ...store[0], source_type: 'manual_edit', value: '人工改写保留', scope: { kind: 'common' } }
 result = await run(ctx, built.rows, store, io); assert.equal(store[0].value, '人工改写保留'); assert.equal(store[0].scope.kind, 'common'); assert.equal(adopts, 13); assert.equal(scopes, 13)
 const changed = await build(ctx, [{ ...p, daily_price: '400' }], [], [], now)
 result = await run(ctx, changed.rows, store, io); assert.ok(result.failures.some(f => f.includes('资料已变化'))); assert.equal(adopts, 13)
 store = []; adopts = scopes = accepted = 0; scopeFails = true
 result = await run(ctx, built.rows.slice(0, 1), [], io); assert.equal(result.imported, 0); assert.equal(result.failures.length, 1); assert.equal(store.length, 1); assert.equal(store[0].scope, undefined); assert.equal(accepted, 1)
 scopeFails = false; result = await run(ctx, built.rows.slice(0, 1), store, io); assert.equal(result.existing, 1); assert.equal(adopts, 1); assert.equal(store[0].scope.product_link_id, 1)
 // A reactivated row may carry an old scope: new adoption must set the card's product.
 const reactivate = { ...io, async adopt(candidate) { const saved = { ...candidate, id: 71, plan_id: 4, tenant_id: 14, status: 'active', version_no: 8, scope: { kind: 'common' } }; store.push(saved); return { results: [{ status: 'adopted', saved }] } } }
 store = []; result = await run(ctx, built.rows.slice(0, 1), [], reactivate); assert.equal(result.imported, 1); assert.equal(store[0].scope.product_link_id, 1)
 store = []; adopts = scopes = accepted = 0; blocked = true
 result = await run(ctx, built.rows.slice(0, 1), [], io); assert.equal(result.failures.length, 1); assert.equal(scopes, 0); assert.equal(accepted, 0); blocked = false
 foreign = true; result = await run(ctx, built.rows.slice(0, 1), [], io); assert.equal(result.failures.length, 1); assert.equal(scopes, 0); assert.equal(accepted, 0); foreign = false
 store = []; adopts = scopes = accepted = 0; switchOnAdopt = true
 result = await run(ctx, built.rows, [], io); assert.equal(adopts, 1); assert.equal(scopes, 1); assert.equal(accepted, 0); assert.equal(result.stopped, true)
 switchOnAdopt = false; alive = true; store = []; adopts = scopes = 0
 const manual = { id: 99, status: 'active', tenant_id: 14, plan_id: 4, category: 'other', key: built.rows[0].key, value: '已有人工事实', version_no: 1, scope: { kind: 'common' } }
 result = await run(ctx, built.rows.slice(0, 1), [manual], io); assert.equal(result.failures.length, 1); assert.equal(adopts, 0); assert.equal(scopes, 0)
 console.log(JSON.stringify({ passed: true, isolation: true, stableBenefitScope: true, validity: true, originalNormalization: true, normalizedDedup: true, manualEditsPreserved: true, changedSourceNoOverwrite: true, scopeRetryNoDuplicate: true, blockedAndForeignRejected: true, planSwitchCompletesOnlyCurrentRow: true, sameNameConflictSafe: true, productionWrites: 0, paidCalls: 0 }))
} finally { for (const file of files) { try { await unlink(join(dir, file)) } catch (e) { if (e.code !== 'ENOENT') throw e } } await rmdir(dir) }
