import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import ts from 'typescript'

const source = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8')
const start = source.indexOf('export async function request<')
const end = source.indexOf('\nexport function getPublicSystemConfig', start)
const javascript = ts.transpile(source.slice(start, end), { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS })

function fixture(status, message) {
  const calls = [], requests = []
  const response = new Response(JSON.stringify(status < 400 ? { actor: { user_id: 1, role: 'customer' } } : { error: message }), { status, headers: { 'Content-Type': 'application/json' } })
  const api = new Function('exports', 'fetch', 'showRequestError', 'clearRequestError', 'liveSupportRoomForRequest', javascript + '\nreturn exports')(
    {}, async (url, options) => { requests.push({ url, credentials: options.credentials }); return response },
    (...args) => calls.push(args), () => {}, () => null,
  )
  return { api, calls, requests }
}

test('anonymous and expired session checks reject to the route guard without an error dialog', async () => {
  for (const message of ['请先登录', '登录已过期，请重新登录']) {
    const f = fixture(401, message)
    await assert.rejects(f.api.getBootstrap(), new RegExp(message))
    assert.deepEqual(f.calls, [])
    assert.equal(f.requests[0].credentials, 'include')
  }
})

test('wrong credentials and captcha failures still show their actual error', async () => {
  for (const [status, message] of [[401, '账号或密码错误'], [400, '验证码错误']]) {
    const f = fixture(status, message)
    await assert.rejects(f.api.request('/api/v1/auth/login', { method: 'POST' }), new RegExp(message))
    assert.deepEqual(f.calls, [['/api/v1/auth/login', 'POST', message]])
  }
})

test('bootstrap server failures remain visible and authenticated checks succeed normally', async () => {
  const bad = fixture(503, '后台服务暂不可用，请稍后重试')
  await assert.rejects(bad.api.getBootstrap(), /后台服务/)
  assert.equal(bad.calls.length, 1)
  const good = fixture(200)
  assert.equal((await good.api.getBootstrap()).actor.user_id, 1)
  assert.deepEqual(good.calls, [])
})
