import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import ts from 'typescript'

const require = createRequire(import.meta.url)
async function load(name, dependencies = {}) {
  const source = await readFile(new URL(`../src/${name}.ts`, import.meta.url), 'utf8')
  const js = ts.transpile(source, { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS })
  const module = { exports: {} }
  new Function('require', 'module', 'exports', js)(id => dependencies[id] || require(id), module, module.exports)
  return module.exports
}
const diagnostic = await load('errorDiagnostic')
const runtime = await load('liveRuntimeFailure', { './errorDiagnostic': diagnostic })

test('live voice failure retains named cause, stage, code, action and matching session', () => {
  const snapshot = { session: { id: 389 }, agent_state: 'stopped', failure: {
    session_id: 389, code: 'voice_disabled', stage: '声音配置校验', message: '“小芳”声音已停用，无法使用该声音。',
    action: '重新选择可用声音并保存方案后再开始直播。', http_status: 403, request_id: 'req-actual-389',
  } }
  const text = runtime.liveRuntimeFailureMessage(snapshot)
  for (const part of ['小芳', '已停用', '声音配置校验', 'voice_disabled', '389', '重新选择', '403', 'req-actual-389']) assert.ok(text.includes(part), part)
  assert.equal(runtime.liveRuntimeFailureMessage({ ...snapshot, session: { id: 390 } }), '', 'late error cannot attach to replacement run')
  const unknown = runtime.liveRuntimeFailureMessage({ session: { id: 389 }, agent_state: 'stopped', agent_stop_reason: 'system_error' })
  assert.match(unknown, /未返回具体失败详情/)
  assert.doesNotMatch(unknown, /声音生成发生错误/)
})

test('unknown causes remain diagnostic, secrets and signed URLs are removed', () => {
  const input = 'provider http 400 code=data_inspection_failed request_id=req-real-123 api_key="two words secret" Authorization: Bearer abcdefghijklmnopqrstuvwxyz https://example.test/a.wav?Signature=secret-signature'
  const text = diagnostic.safeDiagnosticText(input)
  for (const part of ['data_inspection_failed', 'http 400', 'req-real-123']) assert.ok(text.includes(part))
  for (const secret of ['two words secret', 'abcdefghijklmnopqrstuvwxyz', 'example.test', 'secret-signature']) assert.ok(!text.includes(secret), secret)
  assert.equal(diagnostic.safeDiagnosticText('unexpected worker exit: channel closed'), 'unexpected worker exit: channel closed')
})

test('request diagnostics keep room/plan route and strip query credentials', () => {
  const text = diagnostic.requestFailureDiagnostic('/api/v1/rooms/29/live-runtime?token=private&room_id=29', 'post', '选定声音已停用')
  assert.match(text, /POST \/api\/v1\/rooms\/29\/live-runtime/)
  assert.match(text, /选定声音已停用/)
  assert.doesNotMatch(text, /private|token=/)
})

test('GET failures retain actual reason in history and toast without a modal flood', async () => {
  const original = globalThis.window
  globalThis.window = { setTimeout: () => 0 }
  try {
    const feedback = await load('uiFeedback', { './errorDiagnostic': diagnostic })
    feedback.showRequestError('/api/v1/rooms/29/live-runtime', 'GET', 'Core连接被拒绝')
    feedback.showRequestError('/api/v1/rooms/29/live-runtime', 'GET', 'Core连接被拒绝')
    assert.equal(feedback.feedbackState.alertOpen, false)
    assert.equal(feedback.feedbackState.errors.length, 1)
    assert.match(feedback.feedbackState.errors[0].message, /Core连接被拒绝[\s\S]*GET \/api\/v1\/rooms\/29/)
    assert.match(feedback.feedbackState.toasts[0].message, /Core连接被拒绝/)
    feedback.showRequestError('/api/v1/live-agent-plans/5', 'PUT', '事实引用了快照之外的资料')
    assert.equal(feedback.feedbackState.alertOpen, true)
    assert.match(feedback.feedbackState.alertMessage, /事实引用了快照之外的资料[\s\S]*PUT \/api\/v1\/live-agent-plans\/5/)
    feedback.clearErrorHistory()
    assert.equal(feedback.feedbackState.errors.length, 0)
  } finally { globalThis.window = original }
})

test('component and promise failures show original error and location; abort stays quiet', async () => {
  const listeners = {}, alerts = []
  const original = globalThis.window
  globalThis.window = { addEventListener: (k, f) => { listeners[k] = f }, removeEventListener: k => { delete listeners[k] } }
  try {
    const unhandled = await load('unhandledErrorFeedback', { './errorDiagnostic': diagnostic, './uiFeedback': { showAlert: (message, title) => alerts.push({ message, title }) } })
    const stop = unhandled.installUnhandledErrorFeedback()
    listeners.error({ message: 'Cannot read properties of undefined', filename: 'https://web.test/assets/page.js?token=private', lineno: 53, colno: 7 })
    assert.match(alerts[0].message, /Cannot read properties of undefined[\s\S]*\/assets\/page.js:53:7/)
    assert.doesNotMatch(alerts[0].message, /private/)
    listeners.unhandledrejection({ reason: new Error('音频解码失败') })
    assert.match(alerts[1].message, /音频解码失败/)
    listeners.unhandledrejection({ reason: new DOMException('cancelled', 'AbortError') })
    assert.equal(alerts.length, 2)
    stop()
    assert.deepEqual(listeners, {})
  } finally { globalThis.window = original }
})
