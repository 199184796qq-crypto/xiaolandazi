import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'

let server
let boundRoomIdForPlan

before(async () => {
  server = await createServer({ server: { middlewareMode: true }, appType: 'custom' })
  const mod = await server.ssrLoadModule('/src/tenantAgentContext.ts')
  boundRoomIdForPlan = mod.boundRoomIdForPlan
})

after(async () => { await server?.close() })

test('取 active 方案绑定的第一个房间', () => {
  const plans = [
    { id: 4, status: 'active', room_ids: [26, 29] },
    { id: 5, status: 'active', room_ids: [30] },
  ]
  assert.equal(boundRoomIdForPlan(plans, 4), 26)
})

test('停用方案不返回绑定房间', () => {
  assert.equal(boundRoomIdForPlan([{ id: 4, status: 'disabled', room_ids: [26] }], 4), 0)
})

test('未绑定房间返回 0', () => {
  assert.equal(boundRoomIdForPlan([{ id: 4, status: 'active' }], 4), 0)
})
