import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { effectScope, reactive } from 'vue'

let server, useChat
before(async () => {
  server = await createServer({ server: { middlewareMode: true, hmr: false }, appType: 'custom' })
  useChat = (await server.ssrLoadModule('/src/productSupplementChat.ts')).useProductSupplementChat
})
after(async () => { await server?.close() })
const deferred = () => { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b }); return { promise, resolve, reject } }
const fact = { category: 'fulfillment', key: '通常发货', value: '通常下单后第二天上午发货', source_quote: '通常下单后第二天上午发货' }
function setup(services) {
  const scope = reactive({ planId: 4, tenantId: 14, roomId: 26, actorKey: '14:2' }), updates = []
  const lifecycle = effectScope()
  const chat = lifecycle.run(() => useChat(() => ({ ...scope }), value => updates.push(value), services))
  return { chat, scope, updates, close: () => lifecycle.stop() }
}

test('一次发送绑定当前方案，完整传原话，提取仅用户本次明确提供的事实，不自动保存', async () => {
  let calls = 0, adopted = false
  const state = setup({ chat: async (plan, body) => {
    calls++; assert.equal(plan, 4); assert.equal(body.tenant_id, 14); assert.equal(body.message, fact.value)
    assert.deepEqual(body.history, []); assert.deepEqual(body.draft_facts, [])
    return { reply: '我记下发货安排了。', captured_facts: [fact, { ...fact, key: '猜测', value: '当天发', source_quote: '当天发' }], completed: true }
  }, adopt: async () => { adopted = true } })
  state.chat.input.value = fact.value
  await state.chat.send()
  assert.equal(calls, 1); assert.equal(adopted, false)
  assert.equal(state.chat.drafts.value.length, 1)
  assert.deepEqual(state.chat.messages.value.map(item => item.role), ['user', 'agent'])
  state.close()
})

test('请求中不能重复发送；失败保留输入，重试历史不带失败回合', async () => {
  const pending = deferred(); let calls = 0
  const state = setup({ chat: async (_, body) => { calls++; if (calls === 1) return pending.promise; assert.deepEqual(body.history, []); return { reply: '已收到', captured_facts: [] } }, adopt: async () => {} })
  state.chat.input.value = '商品为开袋即食'
  const sent = state.chat.send()
  await state.chat.send(); assert.equal(calls, 1)
  pending.reject(new Error('连接中断')); await sent
  assert.equal(state.chat.input.value, '商品为开袋即食'); assert.equal(state.chat.messages.value.length, 0)
  await state.chat.send(); assert.equal(calls, 2); assert.equal(state.chat.error.value, '')
  state.close()
})

test('换方案及账号取消旧请求，迟到回复不能污染新方案', async () => {
  for (const field of ['planId', 'roomId', 'tenantId', 'actorKey']) {
    const pending = deferred(); let signal
    const state = setup({ chat: async (_, __, value) => { signal = value; return pending.promise }, adopt: async () => {} })
    state.chat.input.value = fact.value; const sent = state.chat.send()
    state.scope[field] = field === 'actorKey' ? '15:3' : 99
    assert.equal(signal.aborted, true)
    pending.resolve({ reply: '旧方案回复', captured_facts: [fact] }); await sent
    assert.equal(state.chat.messages.value.length, 0); assert.equal(state.chat.drafts.value.length, 0); assert.equal(state.chat.busy.value, '')
    state.close()
  }
})

test('确认保存逐条处理：保存后的规范化名称不导致重复；冲突资料保留待处理', async () => {
  let calls = 0
  const second = { category: 'product', key: '食用方式', value: '只支持开袋即食', source_quote: '只支持开袋即食' }
  const state = setup({ chat: async () => ({ reply: '请核对资料', captured_facts: [fact, second] }), adopt: async (plan, candidates, tenant, source) => {
    calls++; assert.equal(plan, 4); assert.equal(tenant, 14); assert.ok(source.startsWith('product-supplement:'))
    assert.equal(candidates[0].status, 'confirmed'); assert.equal(candidates[0].source_quote, fact.source_quote)
    return { adopted: 1, skipped: 0, conflicts: 1, blocked: 0, results: [{ status: 'adopted', candidate: { ...candidates[0], key: '发货安排（规范化）' } }, { status: 'conflict', candidate: candidates[1], message: '与现有资料冲突' }] }
  } })
  state.chat.input.value = `${fact.value}，${second.value}`; await state.chat.send()
  await state.chat.save()
  assert.equal(calls, 1); assert.equal(state.chat.drafts.value.length, 1); assert.equal(state.chat.drafts.value[0].key, second.key)
  assert.ok(state.chat.error.value.includes('与现有资料冲突')); assert.equal(state.updates.length, 1)
  state.close()
})

test('取消候选不写数据库；换类目保留条件与对象关系', async () => {
  let writes = 0
  const material = { category: 'product', key: '配件兼容', value: '这个配件只适配A型号，B型号不适配', source_quote: '这个配件只适配A型号，B型号不适配' }
  const state = setup({ chat: async () => ({ reply: '已整理配件兼容信息', captured_facts: [material] }), adopt: async () => { writes++ } })
  state.chat.input.value = material.value; await state.chat.send()
  assert.equal(state.chat.drafts.value[0].value, material.value)
  state.chat.discard(); assert.equal(writes, 0); assert.equal(state.chat.drafts.value.length, 0)
  assert.ok(state.chat.messages.value.at(-1).text.includes('尚未保存'))
  state.close()
})

test('问清单问题直接传焦点给对话，不发送或清空输入框内未提交的资料', async () => {
  const pending = deferred(); let request
  const state = setup({ chat: async (_, body) => { request = body; return pending.promise }, adopt: async () => { throw Error('不可自动保存') } })
  state.chat.input.value = '文件预填：包装采用独立包装。'
  const focus = JSON.stringify({ 维度: '物流', 待补事实: '外省配送范围' })
  const asking = state.chat.askGap(focus, '我想补充外省配送范围')
  assert.equal(request.focus_gap, focus)
  assert.equal(request.message, '我想补充外省配送范围')
  assert.equal(request.history.length, 0)
  assert.equal(state.chat.input.value, '文件预填：包装采用独立包装。')
  pending.resolve({ reply: '外省配送有什么已确认的要求？', captured_facts: [] })
  await asking
  assert.equal(state.chat.input.value, '文件预填：包装采用独立包装。')
  assert.equal(state.chat.messages.value.at(-1).role, 'agent')
  assert.equal(state.chat.drafts.value.length, 0)
  state.close()
})

test('清单问答失败也保留已有输入和历史，可继续自由对话', async () => {
  const state = setup({ chat: async () => { throw Error('暂时未响应') }, adopt: async () => { throw Error('不可保存') } })
  state.chat.input.value = '尚未发送的资料'
  await state.chat.askGap('{}', '我想补充产地')
  assert.equal(state.chat.input.value, '尚未发送的资料')
  assert.equal(state.chat.messages.value.length, 0)
  assert.equal(state.chat.busy.value, '')
  assert.ok(state.chat.error.value.includes('未响应'))
  state.close()
})
