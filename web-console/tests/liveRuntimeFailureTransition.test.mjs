import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import ts from 'typescript'

const source = await readFile(new URL('../src/liveRuntimeFailure.ts', import.meta.url), 'utf8')
const diagnosticSource = await readFile(new URL('../src/errorDiagnostic.ts', import.meta.url), 'utf8')
const diagnosticURL = 'data:text/javascript;charset=utf-8,' + encodeURIComponent(ts.transpile(diagnosticSource, { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext }))
const js = ts.transpile(source, { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext }).replace("'./errorDiagnostic'", JSON.stringify(diagnosticURL))
const { isNewLiveRuntimeFailure, liveRuntimeFailureMessage } = await import('data:text/javascript;charset=utf-8,' + encodeURIComponent(js))
const failed = (id, code = 'formal_mainline_failed') => ({ session: { id, status: 'stopped' }, agent_state: 'stopped', agent_stop_reason: 'system_error', failure: { session_id: id, code, message: '声音已停止' } })

test('refreshing a page or changing an old failure explanation never becomes a new stop', () => {
  assert.equal(isNewLiveRuntimeFailure(null, failed(221)), false)
  assert.equal(isNewLiveRuntimeFailure(failed(221), failed(221)), false)
  assert.equal(isNewLiveRuntimeFailure(failed(221, 'plan_sources_changed'), failed(221)), false)
  assert.match(liveRuntimeFailureMessage(failed(221)), /声音已停止[\s\S]*场次编号：221/, 'retain the history diagnosis and session')
})

test('a running session that fails and a newly started session that fails are still reported', () => {
  const running = { session: { id: 222, status: 'running' }, agent_state: 'working' }
  assert.equal(isNewLiveRuntimeFailure(running, failed(222)), true)
  assert.equal(isNewLiveRuntimeFailure(failed(221), failed(222)), true)
  assert.equal(isNewLiveRuntimeFailure(failed(222), running), false)
})

test('actual room refresh stays quiet for history, alerts on the next failed session exactly once', async () => {
  const page = await readFile(new URL('../src/views/RoomDetailView.vue', import.meta.url), 'utf8')
  const start = page.indexOf('async function refreshRuntime()')
  const end = page.indexOf('\nconst localVideoViewerActive', start)
  const fn = ts.transpile(page.slice(start, end), { target: ts.ScriptTarget.ES2022 })
  let next = failed(221)
  const alerts = []
  const context = {
    roomId: 26, getLiveRuntime: async () => next, getLiveRuntimeEvents: async () => [],
    getRoom: async () => ({ status: 'live' }), updatePreservingPageBottom: callback => callback(),
    runtimeSnapshot: { value: null }, room: { value: null }, runtimeError: { value: '' },
    observedRuntimeFailureSessions: new Set(), notifiedRuntimeFailures: new Set(),
    firstVoiceStartPending: { value: false }, firstVoicePlaybackEpoch: 0,
    isNewLiveRuntimeFailure, liveRuntimeFailureMessage, showAlert: message => alerts.push(message),
    agentTimeline: { value: [] }, formatTime: value => value,
  }
  const refresh = new Function(...Object.keys(context), fn + '\nreturn refreshRuntime')(...Object.values(context))
  await refresh()
  assert.deepEqual(alerts, [])
  assert.equal(context.runtimeError.value, '')
  next = failed(222)
  await refresh()
  await refresh()
  assert.equal(alerts.length, 1)
  assert.match(context.runtimeError.value, /声音已停止[\s\S]*场次编号：222/)
  next = failed(222, 'generation_blocked')
  await refresh()
  assert.equal(alerts.length, 1)
})
