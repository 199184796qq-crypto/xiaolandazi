import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import ts from 'typescript'
const source = await readFile(new URL('../src/livePlanVersionStatus.ts', import.meta.url), 'utf8')
const js = ts.transpile(source, { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext })
const { livePlanVersionStatus: status, latestSavedPlanVersion } = await import('data:text/javascript;charset=utf-8,' + encodeURIComponent(js))
const saved = { id: 80, plan_id: 4, version_no: 31 }
const runtime = { agent_plan_id: 4, agent_mode: 'anchor', agent_state: 'working', session: { id: 233, status: 'running' } }
const program = { running: true, version_id: 80, version_no: 31, task: { session_id: 'live-runtime-233' } }

test('new saved version does not replace the version actually loaded in Core', () => {
  assert.equal(status(4, saved, runtime, program).state, 'same')
  const next = status(4, { ...saved, id: 81, version_no: 32 }, runtime, program)
  assert.equal(next.latest.version_no, 32)
  assert.equal(next.loaded.version_no, 31)
  assert.equal(next.state, 'different')
  assert.equal(next.hint, '有新版本，下次开播加载')
  assert.equal(status(4, saved, { ...runtime, agent_state: 'paused', session: { id: 233, status: 'paused' } }, program).state, 'same')
})
test('old scenes, independent previews and stopped programs never become the current loaded version', () => {
  for (const candidate of [null, { ...program, running: false }, { ...program, task: null },
    { ...program, version_no: 0 }, { ...program, version_id: -1 },
    { ...program, task: { session_id: 'live-runtime-232' } }, { ...program, task: { session_id: 'preview-233' } }]) {
    assert.equal(status(4, saved, runtime, candidate).loaded, null)
  }
  assert.equal(status(4, saved, { ...runtime, agent_state: 'stopped', session: { id: 233, status: 'stopped' } }, program).loaded, null)
  assert.equal(status(4, saved, { ...runtime, agent_mode: 'control' }, program).loaded, null)
  assert.equal(status(5, saved, runtime, program).latest, null)
})
test('all rooms use the same saved identity; unsaved drafts and another plan cannot bump the saved badge', () => {
  const old = { ...saved, lifecycle_status: 'superseded' }
  const latest = { ...saved, id: 81, version_no: 32, lifecycle_status: 'published' }
  assert.equal(latestSavedPlanVersion(4, [null, old, latest, { ...latest, version_no: 99, lifecycle_status: 'draft' },
    { ...latest, plan_id: 5, version_no: 100 }]), latest)
  assert.equal(latestSavedPlanVersion(5, [old]), null)
})
