import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import ts from 'typescript'

const code = ts.transpileModule(readFileSync(process.env.CORE_STATUS_BASELINE || new URL('../src/coreRuntime.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
const results = []
function fixture() {
  let now = 1000, serial = 0
  const timers = new Map(), transitions = [], exports = {}
  let response = { available: true, core_boot_id: 'same-boot' }
  const context = { exports, Date: { now: () => now }, require: name => name === 'vue' ? { reactive: x => new Proxy(x, { set(o, k, v) { if (k === 'phase') transitions.push(v); o[k] = v; return true } }) } : { getCoreRuntimeStatus: async () => { if (typeof response === 'function') return response(); if (response instanceof Error) throw response; return response } }, window: { setInterval: () => ++serial, clearInterval() {}, setTimeout(fn, delay) { const id = ++serial; timers.set(id, { fn, at: now + delay }); return id }, clearTimeout(id) { timers.delete(id) } } }
  vm.runInNewContext(code, context)
  return { e: exports, transitions, set: x => response = x, async tick(ms) { now += ms; for (const [id, t] of timers) if (t.at <= now) { timers.delete(id); t.fn() } await Promise.resolve() } }
}
async function scenario(name, run) { await run(); results.push({ name, passed: true }) }
await scenario('isolated HTTP failure keeps healthy listener phase and boot identity', async () => {
  const f = fixture(); await f.e.refreshCoreRuntimeStatus()
  f.set(new Error('temporary network failure')); await f.e.refreshCoreRuntimeStatus()
  await f.tick(2000); f.set({ available: true, core_boot_id: 'same-boot' }); await f.e.refreshCoreRuntimeStatus()
  assert.equal(f.e.coreRuntime.phase, 'online'); assert.equal(f.e.coreRuntime.recoverySerial, 0); assert.ok(!f.transitions.includes('offline'))
})
await scenario('transient negative health receipt does not tear down PCM', async () => {
  const f = fixture(); await f.e.refreshCoreRuntimeStatus()
  f.set({ available: false, core_boot_id: '' }); await f.e.refreshCoreRuntimeStatus(); await f.tick(3000); await f.e.refreshCoreRuntimeStatus()
  assert.equal(f.e.coreRuntime.phase, 'online'); assert.equal(f.e.coreRuntime.bootId, 'same-boot')
})
await scenario('persistent failure becomes offline after grace and recovers exactly once', async () => {
  const f = fixture(); await f.e.refreshCoreRuntimeStatus(); f.set(new Error('unreachable')); await f.e.refreshCoreRuntimeStatus()
  await f.tick(5999); await f.e.refreshCoreRuntimeStatus(); assert.equal(f.e.coreRuntime.phase, 'online')
  await f.tick(1); await f.e.refreshCoreRuntimeStatus(); assert.equal(f.e.coreRuntime.phase, 'offline')
  f.set({ available: true, core_boot_id: 'same-boot' }); await f.e.refreshCoreRuntimeStatus(); assert.equal(f.e.coreRuntime.phase, 'recovering')
  await f.e.refreshCoreRuntimeStatus(); assert.equal(f.e.coreRuntime.recoverySerial, 1)
  await f.tick(3200); assert.equal(f.e.coreRuntime.phase, 'online')
})
await scenario('confirmed new Core boot still triggers recovery', async () => {
  const f = fixture(); await f.e.refreshCoreRuntimeStatus(); f.set({ available: true, core_boot_id: 'new-boot' }); await f.e.refreshCoreRuntimeStatus()
  assert.equal(f.e.coreRuntime.phase, 'recovering'); assert.equal(f.e.coreRuntime.recoverySerial, 1)
})
await scenario('stopped account watch ignores its late response and cannot unlock new poll', async () => {
  const f = fixture(); let releaseOld, releaseNew
  f.set(() => new Promise(resolve => releaseOld = resolve)); const old = f.e.refreshCoreRuntimeStatus()
  f.e.stopCoreRuntimeWatch(); f.set(() => new Promise(resolve => releaseNew = resolve)); const current = f.e.refreshCoreRuntimeStatus()
  releaseOld({ available: true, core_boot_id: 'old-account' }); await old
  assert.equal(f.e.coreRuntime.bootId, ''); assert.equal(f.e.coreRuntime.phase, 'checking')
  releaseNew({ available: true, core_boot_id: 'new-account' }); await current
  assert.equal(f.e.coreRuntime.bootId, 'new-account'); assert.equal(f.e.coreRuntime.phase, 'online')
})
console.log(JSON.stringify({ passed: true, results }))
