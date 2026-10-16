import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import vm from 'node:vm'
import ts from 'typescript'

// Run the actual view's save workflow, mocking I/O rather than reimplementing it.
const view = await readFile(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
const start = view.indexOf('async function saveFullShowVersion(')
const end = view.indexOf('async function saveFullShowVariantEdit(', start)
const source = view.slice(start, end)
const executable = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText
const plain = value => JSON.parse(JSON.stringify(value))

function workflow(options = {}) {
  const calls = []
  const old = { id: 115, tenant_id: 14, plan_id: 5, room_id: 26, version_no: 2 }
  const ctx = { JSON, Number, Math, String, Boolean, TextEncoder, Error, Promise, Date,
    setInterval: fn => setInterval(fn, 2), clearInterval }
  for (const match of source.matchAll(/\b([A-Za-z_$][\w$]*)\.value\b/g)) ctx[match[1]] ??= { value: null }
  Object.assign(ctx, {
    planVersionSaveEpoch: 1, contentModeDisposed: false,
    currentRoomPlanId: { value: 5 }, activeRoom: { value: { id: 26, tenant_id: 14, name: '测试房间' } },
    // Real list responses have room_count but omit room_ids.
    currentPlan: { value: { id: 5, tenant_id: 14, room_count: 1 } },
    contentModeAccess: { value: { content_mode: options.mode || 'ai_dynamic' } },
    latestSavedVersion: { value: old }, fullShowSavedVersion: { value: old }, fullShowSaveReceipt: { value: old },
    fullShowSaveBlockedReason: { value: options.blocked || '' }, fullShowVersionSaving: { value: false },
    fullShowVersionError: { value: '' }, fullShowNotice: { value: '已恢复历史版本V2' },
    fullShowResult: { value: { variants: [] } }, fullShowFormalVariantKeys: { value: [] },
    realtimeGenerationOptions: { value: { buffer_seconds: 120, body_min: 2, body_max: 4, max_chars: 300 } },
    fullShowDuration: { value: 90 }, fullShowRoundMinutes: { value: 7 }, currentVoiceIdentity: { value: {} },
    fullShowWorkspaceChecks: { value: [] }, fullShowWorkspaceInherited: { value: false },
    savePlanOneClickBusy: { value: false }, savePlanOneClickStage: { value: '' },
    poolStatus: { value: { state: options.state || 'ready' } }, poolError: { value: '' },
    compactOriginalSourceManifest: async () => { await options.compact?.(ctx); return null },
    validatedRealtimeOptions: value => ({ ...value }), cleanVersionVoiceIdentity: value => ({ ...value }),
    cloneVersionJSON: (value, fallback) => plain(value ?? fallback),
    cleanVersionTimeline: value => value || [], cleanVersionSafePoints: value => value || [],
    fullShowVoiceState: () => ({ asset_manifest: {}, timeline: [], safe_points: [] }),
    liveAgentVersionSafePayloadBytes: 60000, liveAgentVersionRequestLimitBytes: 65536,
    getRoomLiveAgentPlans: async room => {
      calls.push({ type: 'bindings', room })
      if (options.bindingError) throw Error(options.bindingError)
      await options.bindings?.(ctx)
      return { items: options.unbound ? [] : [{ id: 5, tenant_id: 14, room_count: 1 }] }
    },
    createLiveAgentPlanVersion: async (plan, payload) => {
      calls.push({ type: 'save', plan, payload: plain(payload) })
      if (options.saveError) throw Error(options.saveError)
      await options.response?.(ctx)
      return options.reply === 'old' ? old : { id: 116, tenant_id: 14, plan_id: 5, room_id: payload.room_id, version_no: 3, generation_context: {} }
    },
    realtimeOptionsFromVersion: () => ({ buffer_seconds: 120, body_min: 2, body_max: 4, max_chars: 300 }),
    refreshFullShowVersionHistory: async () => {}, refreshCurrentRoomPlan: async () => {},
    contentPools: {
      update: async () => {
        calls.push({ type: 'update' })
        if (options.updateError) ctx.poolError.value = options.updateError
        else ctx.poolStatus.value.state = 'ready'
      }, refresh: async () => {},
    },
  })
  vm.createContext(ctx)
  vm.runInContext(executable, ctx)
  return { ctx, calls, run: () => ctx.savePlanOneClick(), saves: () => calls.filter(c => c.type === 'save') }
}

test('bound dynamic plan saves without room_ids in the plan list', async () => {
  const w = workflow(); await w.run()
  assert.deepEqual(w.calls.map(c => c.type), ['bindings', 'save'])
  assert.equal(w.saves()[0].payload.room_id, 26)
  assert.equal(w.saves()[0].payload.generation_context.content_mode, 'ai_dynamic')
  assert.equal(w.ctx.fullShowSaveReceipt.value.version_no, 3)
  assert.equal(w.ctx.fullShowSavedVersion.value.version_no, 3)
  assert.equal(w.ctx.fullShowVersionError.value, '')
})

test('one click continues from content update to formal version creation', async () => {
  const w = workflow({ state: 'needs_update' }); await w.run()
  assert.deepEqual(w.calls.map(c => c.type), ['update', 'bindings', 'save'])
  assert.equal(w.ctx.fullShowSaveReceipt.value.version_no, 3)
  assert.equal(w.ctx.savePlanOneClickBusy.value, false)
})

for (const [name, option, error] of [
  ['binding read fails', 'bindingError', '绑定读取失败'],
  ['server rejects save', 'saveError', '事实已变化，请刷新'],
  ['content update fails', 'updateError', '更新请求失败'],
]) test(name + ' reports failure and clears the previous attempt receipt', async () => {
  const w = workflow({ [option]: error, state: option === 'updateError' ? 'needs_update' : 'ready' }); await w.run()
  assert.equal(w.ctx.fullShowSaveReceipt.value, null)
  assert.equal(w.ctx.fullShowSavedVersion.value.version_no, 2)
  assert.equal(w.ctx.fullShowVersionError.value, error)
  assert.equal(w.ctx.fullShowVersionSaving.value, false)
  assert.equal(w.ctx.savePlanOneClickBusy.value, false)
  if (option !== 'saveError') assert.equal(w.saves().length, 0)
})

test('old V2 response cannot produce a success receipt for this attempt', async () => {
  const w = workflow({ reply: 'old' }); await w.run()
  assert.equal(w.ctx.fullShowSaveReceipt.value, null)
  assert.match(w.ctx.fullShowVersionError.value, /新版本保存回执/)
})

test('blocked save makes its reason visible and does not call the server', async () => {
  const w = workflow({ blocked: '正在准备当前方案' }); await w.run()
  assert.equal(w.calls.length, 0)
  assert.equal(w.ctx.fullShowVersionError.value, '正在准备当前方案')
  assert.equal(w.ctx.fullShowSaveReceipt.value, null)
})

test('parameter change during preparation prevents a stale save', async () => {
  const w = workflow({ compact: ctx => { ctx.realtimeGenerationOptions.value.max_chars = 400 } }); await w.run()
  assert.equal(w.saves().length, 0)
  assert.match(w.ctx.fullShowVersionError.value, /参数.*变化/)
})

for (const change of ['A→B→A', 'account/room/mode', 'unmount']) test(change + ' cancels preparation before POST', async () => {
  const w = workflow({ compact: ctx => { if (change === 'unmount') ctx.contentModeDisposed = true; else ctx.planVersionSaveEpoch += 2 } }); await w.run()
  assert.equal(w.saves().length, 0)
  assert.equal(w.ctx.fullShowSaveReceipt.value, null)
  assert.equal(w.ctx.savePlanOneClickBusy.value, false)
})

test('unbound static plan still archives without binding or mode mismatch cancellation', async () => {
  const w = workflow({ unbound: true, mode: 'ai_pregenerated' })
  w.ctx.fullShowResult.value.variants = [{ variant_key: 'A', index: 1, text: '已确认文稿' }]
  w.ctx.fullShowFormalVariantKeys.value = ['A']
  await w.run()
  assert.equal(w.saves()[0].payload.room_id, 0)
  assert.equal(w.ctx.fullShowSaveReceipt.value.room_id, 0)
  assert.match(w.ctx.fullShowNotice.value, /已存档方案 V3/)
})

test('unbound save under a dynamic room receives explicit server validation errors', async () => {
  const w = workflow({ unbound: true, saveError: '存档方案至少需要一套正式文稿' }); await w.run()
  assert.equal(w.saves().length, 1)
  assert.equal(w.saves()[0].payload.room_id, 0)
  assert.match(w.ctx.fullShowVersionError.value, /至少需要/)
})

test('late successful response does not become the new page success state', async () => {
  const w = workflow({ response: ctx => { ctx.planVersionSaveEpoch++ } }); await w.run()
  assert.equal(w.saves().length, 1)
  assert.equal(w.ctx.fullShowSaveReceipt.value, null)
  assert.equal(w.ctx.fullShowSavedVersion.value.version_no, 2)
})

test('double click while bindings load sends one create request', async () => {
  let release
  const w = workflow({ bindings: () => new Promise(resolve => { release = resolve }) })
  const first = w.run(); await w.run()
  release(); await first
  assert.equal(w.saves().length, 1)
})
