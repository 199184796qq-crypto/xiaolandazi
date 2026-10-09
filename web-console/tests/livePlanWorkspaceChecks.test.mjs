import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import ts from 'typescript'

const source = await readFile(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
const start = source.indexOf('async function loadFullShowWorkspace()')
const end = source.indexOf('\nfunction cloneVersionJSON', start)
const js = ts.transpile(source.slice(start, end), { target: ts.ScriptTarget.ES2022 })

test('workspace notices still restore the saved plan and do not become blocking errors', async () => {
  const checks = [{ code: 'plan_sources_changed', message: '当前来源与保存方案有差异' }]
  const version = { id: 57, version_no: 14, generation_context: {} }
  let restored = false
  const context = {
    currentRoomPlanId: { value: 4 }, activeRoom: { value: { id: 26, tenant_id: 14 } },
    fullShowWorkspaceLoadToken: 0, resetFullShowWorkspaceState() {},
    fullShowWorkspaceLoading: { value: false }, contentModeAccess: { value: { content_mode: 'ai_dynamic' } },
    getLiveAgentPlanWorkspace: async () => ({ version, source: 'published', checks }),
    fullShowWorkspaceChecks: { value: [] }, realtimeGenerationOptions: { value: null },
    realtimeOptionsFromVersion: () => null, loadRealtimeOptionsDefaults: async () => {},
    fullShowNotice: { value: '' }, fullShowWorkspaceInherited: { value: false },
    fullShowSavedVersion: { value: null }, fullShowError: { value: '' },
    restoreFullShowWorkspaceVersion: async (saved, status) => {
      assert.equal(saved, version)
      assert.equal(status, 'published')
      restored = true
    },
  }
  const load = new Function(...Object.keys(context), js + '\nreturn loadFullShowWorkspace')(...Object.values(context))
  await load()
  assert.equal(restored, true)
  assert.deepEqual(context.fullShowWorkspaceChecks.value, checks)
  assert.equal(context.fullShowError.value, '')
  assert.equal(context.fullShowWorkspaceLoading.value, false)
})
