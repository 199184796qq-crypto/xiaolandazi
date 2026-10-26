import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import vm from 'node:vm'
import ts from 'typescript'

const room = await readFile(new URL('../src/views/RoomDetailView.vue', import.meta.url), 'utf8')
const script = room.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1]
const source = ts.createSourceFile('room.ts', script, ts.ScriptTarget.Latest, true)
const names = ['deviceVisualState', 'deviceStatusText', 'deviceStatusLabel']
const statements = source.statements.filter((node) => ts.isVariableStatement(node) && node.declarationList.declarations.some((d) => names.includes(d.name?.getText(source)))).map((n) => n.getText(source)).join('\n')
const control = source.statements.find((n) => ts.isFunctionDeclaration(n) && n.name?.text === 'controlBoundDevice').getText(source)
function fixture(device, running = false) {
  const state = {
    boundDevice: { value: device }, aiPaused: { value: false }, aiRunning: { value: running },
    computed: (read) => ({ get value() { return read() } }),
  }
  vm.createContext(state)
  vm.runInContext(ts.transpileModule(statements + '\nglobalThis.status = { deviceVisualState, deviceStatusText, deviceStatusLabel };', { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, state)
  return state
}
test('physical online and audio disabled are shown separately even during a live session', () => {
  const state = fixture({ sn: 'XL000007', connection_status: 'online', work_status: 'disconnected' }, true)
  assert.equal(state.status.deviceVisualState.value, 'disabled')
  assert.equal(state.status.deviceStatusLabel.value, '声音已停用')
  assert.match(state.status.deviceStatusText.value, /已连接，声音已停用/)
  assert.match(room, /v-else-if="deviceVisualState === 'disabled'"[\s\S]*?启用声音/)
})
test('transport offline and reconnecting take priority over old audio preferences', () => {
  for (const [connection_status, expected] of [['offline', 'offline'], ['connecting', 'reconnecting']]) {
    const state = fixture({ connection_status, work_status: 'disconnected' }, true)
    assert.equal(state.status.deviceVisualState.value, expected)
  }
  assert.equal(fixture(null).status.deviceVisualState.value, 'offline')
})
test('online pause remains distinct from standby and active playback', () => {
  assert.equal(fixture({ connection_status: 'online', work_status: 'paused' }, true).status.deviceVisualState.value, 'paused')
  assert.equal(fixture({ connection_status: 'online', work_status: 'idle' }).status.deviceVisualState.value, 'standby')
  assert.equal(fixture({ connection_status: 'online', work_status: 'working' }, true).status.deviceVisualState.value, 'working')
})
test('rejected connect refreshes real state and never paints a synthetic online device', async () => {
  const failure = new Error('尚未收到盒子的有效连接，请等待自动重连')
  const old = { connection_status: 'online', work_status: 'working' }
  let reads = 0
  const state = { Error, boundDevice: { value: old }, coreActionsAvailable: { value: true }, deviceControlBusy: { value: false }, deviceControlError: { value: '' }, runtimeSnapshot: { value: { device: old } }, roomId: 26,
    controlLiveDevice: async () => { throw failure }, refreshRuntime: async () => { reads++; state.runtimeSnapshot.value.device = { connection_status: 'offline', work_status: 'idle' } },
  }
  vm.createContext(state); vm.runInContext(ts.transpileModule(control, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, state)
  await state.controlBoundDevice('connect')
  assert.equal(reads, 1)
  assert.equal(state.runtimeSnapshot.value.device.connection_status, 'offline')
  assert.equal(state.deviceControlBusy.value, false)
  assert.equal(state.deviceControlError.value, failure.message)
  assert.doesNotMatch(control, /connection_status\s*:\s*['"]online['"]/)
})
test('successful action uses only the server-returned heartbeat and status', async () => {
  const returned = { connection_status: 'online', work_status: 'paused', last_heartbeat_at: '2026-10-26T11:37:33Z' }
  const state = { boundDevice: { value: { id: 7 } }, coreActionsAvailable: { value: true }, deviceControlBusy: { value: false }, deviceControlError: { value: '' }, runtimeSnapshot: { value: { device: {} } }, roomId: 26,
    controlLiveDevice: async () => returned, refreshRuntime: async () => { throw new Error('unexpected refresh') },
  }
  vm.createContext(state); vm.runInContext(ts.transpileModule(control, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, state)
  await state.controlBoundDevice('pause')
  assert.equal(state.runtimeSnapshot.value.device, returned)
  assert.equal(state.deviceControlBusy.value, false)
})
