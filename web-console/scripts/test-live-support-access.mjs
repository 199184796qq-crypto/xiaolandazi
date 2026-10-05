import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import ts from 'typescript'

const source = readFileSync(new URL('../src/liveSupportAccess.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } })
const { hasRoomStrategyAuthorization, canDeleteOperationsRoom, beginLiveSupportScope, liveSupportRoomForRequest, liveSupportMediaPlaybackURL } = await import('data:text/javascript;charset=utf-8,' + encodeURIComponent(outputText))

const grant = { staff_user_id: 7, room_id: 11, tenant_id: 5, status: 'active', capability: 'l3_policy' }
assert.equal(hasRoomStrategyAuthorization([grant], 7, 11, 5), true)
for (const changed of [{ staff_user_id: 8 }, { room_id: 12 }, { tenant_id: 6 }, { status: 'revoked' }, { capability: 'voice_clone' }]) {
  assert.equal(hasRoomStrategyAuthorization([{ ...grant, ...changed }], 7, 11, 5), false)
}
assert.equal(hasRoomStrategyAuthorization([], 7, 11), false)
assert.equal(canDeleteOperationsRoom({ actor: { role: 'staff' }, staff_access: { permissions: ['liveops.configure'] } }), true)
assert.equal(canDeleteOperationsRoom({ actor: { role: 'staff' }, staff_access: { permissions: ['liveops.view_all'] } }), false)
assert.equal(canDeleteOperationsRoom({ actor: { role: 'platform_admin' } }), true)
assert.equal(canDeleteOperationsRoom({ actor: { role: 'sales_staff' }, staff_access: { permissions: ['liveops.configure'] } }), false)

assert.equal(liveSupportRoomForRequest('/api/v1/live-agent-plans'), null)
const origin = 'https://console.example.test'
const mediaPath = '/api/v1/live/media-assets/42/content'
assert.equal(liveSupportMediaPlaybackURL(mediaPath, origin), mediaPath)
assert.throws(() => beginLiveSupportScope(0))
const finishFirst = beginLiveSupportScope(11)
assert.equal(liveSupportMediaPlaybackURL(mediaPath, origin), origin + mediaPath + '?support_room_id=11')
assert.equal(liveSupportMediaPlaybackURL(origin + mediaPath + '?download=1&support_room_id=9#start', origin), origin + mediaPath + '?download=1&support_room_id=11#start')
for (const url of ['https://assets.example.test' + mediaPath + '?signature=abc', 'blob:https://console.example.test/audio', 'data:audio/wav;base64,aA==', '/api/v1/live/voice-profiles/42', '/api/v1/live/media-assets/42/content/extra']) {
  assert.equal(liveSupportMediaPlaybackURL(url, origin), url, 'only same-origin media content playback URLs may be scoped')
}
for (const url of ['/api/v1/live-agent-plans?tenant_id=5', '/api/v1/live-agent-plans/42/facts', '/api/v1/rooms/11/runtime', '/api/v1/live/rooms/11/policy', '/api/v1/live/voice-profiles', '/api/v1/live/media-assets', '/api/v1/live/agent/config-versions']) {
  assert.equal(liveSupportRoomForRequest(url), 11)
}
for (const url of ['/api/v1/system/live-strategy-center', '/api/v1/live/policy/admin', '/api/v1/rooms', '/api/v1/tenants', '/api/v1/liveops/support-authorizations']) {
  assert.equal(liveSupportRoomForRequest(url), null)
}
const finishSecond = beginLiveSupportScope(12)
finishFirst()
assert.equal(liveSupportRoomForRequest('/api/v1/live-agent-plans'), 12, 'old component cleanup must not clear new room scope')
finishSecond()
assert.equal(liveSupportRoomForRequest('/api/v1/live-agent-plans'), null)
assert.equal(liveSupportMediaPlaybackURL(mediaPath, origin), mediaPath, 'ended support scope must not annotate later customer playback')
console.log('Live support access: exact grants, scoped headers/media playback, external URL isolation and cleanup passed')
