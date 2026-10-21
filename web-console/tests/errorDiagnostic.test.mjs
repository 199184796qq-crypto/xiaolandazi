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

test('live voice failure retains Chinese cause, stage, action and session without internal codes', () => {
  const snapshot = { session: { id: 389 }, agent_state: 'stopped', failure: {
    session_id: 389, code: 'voice_disabled', stage: '声音配置校验', message: '“小芳”声音已停用，无法使用该声音。',
    action: '重新选择可用声音并保存方案后再开始直播。', http_status: 403, request_id: 'req-actual-389',
  } }
  const text = runtime.liveRuntimeFailureMessage(snapshot)
  for (const part of ['小芳', '已停用', '声音配置校验', '389', '重新选择']) assert.ok(text.includes(part), part)
  assert.doesNotMatch(text, /[A-Za-z]|voice_disabled|403|req-actual|错误码|请求编号/)
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

test('request diagnostics show the Chinese operation without routes or query credentials', () => {
  const text = diagnostic.requestFailureDiagnostic('/api/v1/rooms/29/live-runtime?token=private&room_id=29', 'post', '选定声音已停用')
  assert.match(text, /发生操作：直播控制与状态读取/)
  assert.match(text, /选定声音已停用/)
  assert.doesNotMatch(text, /[A-Za-z]|private|token=|\/api|29/)
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
    assert.match(feedback.feedbackState.errors[0].message, /音频播放服务连接被拒绝[\s\S]*直播控制与状态读取/)
    assert.match(feedback.feedbackState.toasts[0].message, /音频播放服务连接被拒绝/)
    assert.doesNotMatch(feedback.feedbackState.toasts[0].message, /[A-Za-z]|\/api/)
    feedback.showRequestError('/api/v1/live-agent-plans/5', 'PUT', '事实引用了快照之外的资料')
    assert.equal(feedback.feedbackState.alertOpen, true)
    assert.match(feedback.feedbackState.alertMessage, /事实引用了快照之外的资料[\s\S]*直播方案处理/)
    feedback.clearErrorHistory()
    assert.equal(feedback.feedbackState.errors.length, 0)
  } finally { globalThis.window = original }
})

test('component and promise failures use Chinese causes without source files; abort stays quiet', async () => {
  const listeners = {}, alerts = []
  const original = globalThis.window
  globalThis.window = { addEventListener: (k, f) => { listeners[k] = f }, removeEventListener: k => { delete listeners[k] } }
  try {
    const unhandled = await load('unhandledErrorFeedback', { './errorDiagnostic': diagnostic, './uiFeedback': { showAlert: (message, title) => alerts.push({ message, title }) } })
    const stop = unhandled.installUnhandledErrorFeedback()
    listeners.error({ message: 'Cannot read properties of undefined', filename: 'https://web.test/assets/page.js?token=private', lineno: 53, colno: 7 })
    assert.match(alerts[0].message, /页面读取或处理数据时发生异常/)
    assert.doesNotMatch(alerts[0].message, /[A-Za-z]|private|53:7|发生位置/)
    listeners.unhandledrejection({ reason: new Error('音频解码失败') })
    assert.match(alerts[1].message, /音频解码失败/)
    listeners.unhandledrejection({ reason: new DOMException('cancelled', 'AbortError') })
    assert.equal(alerts.length, 2)
    stop()
    assert.deepEqual(listeners, {})
  } finally { globalThis.window = original }
})

test('Chinese boundary preserves business reasons and translates known faults without publishing technical data', () => {
  const cases = [
    ['事实引用了快照之外的资料，请检查商品归属。', /事实引用了快照之外的资料/],
    ['provider http 400 code=data_inspection_failed request_id=req-real-123 api_key="private secret"', /上游模型服务拒绝了本次内容/],
    ['context deadline exceeded at worker.go:93', /响应超时/],
    ['dial tcp 127.0.0.1:8081: connection refused', /服务连接失败/],
    ['insufficient_quota account=internal-account', /可用额度不足/],
    ['sql: SELECT password FROM internal_users WHERE id=14', /后台数据处理失败/],
    ['unexpected worker exit: channel closed', /尚未提供可显示的中文原因/],
    ['错误码：runtime_error\n请求位置：POST /api/v1/rooms/29\n at internal.js:11:2', /尚未提供可显示的中文原因/],
    ['ＦＡＴＡＬ：ｉｎｔｅｒｎａｌ＿ｓｔａｔｅ', /尚未提供可显示的中文原因/],
    ['错误：неизвестная ошибка', /尚未提供可显示的中文原因/],
    ['“Alice”声音已停用，无法使用该声音。', /声音已停用/],
  ]
  for (const [raw, expected] of cases) {
    const visible = diagnostic.userFacingErrorText(raw)
    assert.match(visible, expected, raw)
    assert.doesNotMatch(visible, /[A-Za-z]|[а-я]|\/api|127\.0|8081|:93|private|错误码|请求位置/, raw)
  }
})

test('direct alerts, inline errors and warning records all use the Chinese boundary', async () => {
  const original = globalThis.window;globalThis.window = { setTimeout: () => 0 }
  try {
    const feedback = await load('uiFeedback', { './errorDiagnostic': diagnostic })
    feedback.showAlert('connection refused at /opt/private/app.go:42', 'Request failed')
    assert.match(feedback.feedbackState.alertMessage, /服务连接失败/)
    const inline = feedback.useFeedbackErrorRef('permission denied')
    assert.match(inline.value, /没有访问权限/)
    inline.value = '业务字段 request_id=req-secret 无效'
    assert.doesNotMatch(inline.value, /[A-Za-z]|req-secret/)
    feedback.showToast('Load failed', 'Core连接被拒绝', 'warning')
    for (const item of feedback.feedbackState.toasts) assert.doesNotMatch(item.title + item.message, /[A-Za-z]|\/opt/)
    assert.doesNotMatch(feedback.feedbackState.alertTitle + feedback.feedbackState.alertMessage, /[A-Za-z]|\/opt|:42/)
  } finally { globalThis.window = original }
})
