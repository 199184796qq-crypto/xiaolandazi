import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import ts from 'typescript'
const source = readFileSync(new URL('../src/deviceActivation.ts',import.meta.url),'utf8')
const { outputText } = ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}})
const { deviceActivationState: state } = await import('data:text/javascript;charset=utf-8,'+encodeURIComponent(outputText))
const base = {hardware_mac:'1c:29:04:31:0e:b8',quality_status:'qualified',lifecycle_status:'IN_STOCK',claim_enabled:false}
assert.equal(state(base).activated,false);assert.equal(state(base).allowed,true)
assert.equal(state({...base,claim_enabled:true}).activated,true)
for (const lifecycle_status of ['SOLD','CUSTOMER_BOUND','ACTIVE']) assert.equal(state({...base,lifecycle_status}).activated,true)
for (const lifecycle_status of ['SCRAPPED','AFTER_SALES','REPAIRING','REPLACED','IN_TRANSIT']) {
  assert.equal(state({...base,lifecycle_status,claim_enabled:true}).activated,false)
  assert.equal(state({...base,lifecycle_status}).allowed,false)
}
for (const quality_status of ['defective','unknown']) {
  assert.equal(state({...base,quality_status,claim_enabled:true}).activated,false)
  assert.equal(state({...base,quality_status}).allowed,false)
}
assert.equal(state({...base,hardware_mac:''}).allowed,false)
const view = readFileSync(new URL('../src/views/InventoryLifecycleView.vue',import.meta.url),'utf8')
assert.ok(view.includes("true, '库存页面一键允许激活'"))
assert.ok(view.includes('device.claim_enabled = true'))
assert.ok(!view.includes('openHardware'))
assert.ok(!view.includes('hardwareForm'))
assert.ok(view.includes('入库即默认允许激活'))
console.log('Device activation: default enable, direct action, independent inventory status and blocked cases passed')
