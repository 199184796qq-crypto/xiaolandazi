import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import ts from 'typescript'

// Exercise the actual frontend helper without importing Vue or modifying build output.
const source = readFileSync(new URL('../src/livePolicyAccess.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
})
const policy = await import('data:text/javascript;charset=utf-8,' + encodeURIComponent(outputText))
let cases = 0
for (let mask = 0; mask < 8; mask++) {
  for (const superAdmin of [false, true]) {
    const permissions = ['livepolicy.manage_l1', 'livepolicy.manage_l2', 'livepolicy.manage_l3_authorized']
      .filter((_, index) => mask & (1 << index))
    const bootstrap = { actor: { role: 'staff' }, staff_access: { permissions, is_super_admin: superAdmin } }
    const l1 = superAdmin || Boolean(mask & 1)
    const l2 = l1 || Boolean(mask & 2)
    assert.equal(policy.canManageLivePolicyL1(bootstrap), l1)
    assert.equal(policy.canManageLivePolicyL2(bootstrap), l2)
    assert.equal(policy.canDelegateLivePolicyL3(bootstrap), !l1 && Boolean(mask & 2))
    cases++
  }
}
for (const bootstrap of [
  null,
  { actor: { role: 'customer' } },
  { actor: { role: 'platform_admin' } },
  { actor: { role: 'staff' }, staff_access: { permissions: ['*'] } },
  { actor: { role: 'staff' }, staff_access: { permissions: ['livepolicy.manage_l3_authorized'] } },
]) {
  assert.equal(policy.canDelegateLivePolicyL3(bootstrap), false)
  cases++
}
console.log(`PASS ${cases} frontend policy access cases (L1 excludes delegated L3; L1 includes L2)`)
