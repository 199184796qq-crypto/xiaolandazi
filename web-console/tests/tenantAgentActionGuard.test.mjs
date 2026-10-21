import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'

let server
let tenantActionContextMatches

before(async () => {
  server = await createServer({ server: { middlewareMode: true }, appType: 'custom' })
  const mod = await server.ssrLoadModule('/src/tenantAgentActionGuard.ts')
  tenantActionContextMatches = mod.tenantActionContextMatches
})

after(async () => { await server?.close() })

const action = { payload: { plan_id: 4, room_id: 26 } }

test('同一方案同一房间允许执行', () => {
  assert.equal(tenantActionContextMatches(action, 4, 26), true)
})

test('切换方案后旧卡失效', () => {
  assert.equal(tenantActionContextMatches(action, 5, 26), false)
})

test('切换房间后旧卡失效', () => {
  assert.equal(tenantActionContextMatches(action, 4, 27), false)
})

test('缺少方案或房间的动作不允许执行', () => {
  assert.equal(tenantActionContextMatches({ payload: { plan_id: 4 } }, 4, 26), false)
  assert.equal(tenantActionContextMatches({ payload: { room_id: 26 } }, 4, 26), false)
  assert.equal(tenantActionContextMatches(undefined, 4, 26), false)
})
