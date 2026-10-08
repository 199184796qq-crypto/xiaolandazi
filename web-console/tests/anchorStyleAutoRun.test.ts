import test from 'node:test'
import assert from 'node:assert/strict'
import { wheelSlots, selectRememberedSlot, speechChars, runAutoLoop, encodeTurns, decodeTurns } from '../src/anchorStyleAutoRun.ts'

const slots = wheelSlots(JSON.stringify({ 卖点列表: ['A', 'B', 'C', 'D'].map(卖点名 => ({ 卖点名, 角度: ['角度1', '角度2'] })) }))
test('轮盘索引与后端角度交错顺序一致', () => assert.deepEqual(slots.map(x => x.key), ['A×角度1', 'B×角度1', 'C×角度1', 'D×角度1', 'A×角度2', 'B×角度2', 'C×角度2', 'D×角度2']))
test('卖点和组合记忆跨多圈，不把一圈结束当成清空', () => {
  const history: typeof slots = []
  for (let round = 0; round < 80; round++) {
    const next = selectRememberedSlot(slots, history, { pointGap: 2, combinationGap: 5 })
    assert.ok(!history.slice(-2).some(x => x.sellingPoint === next.sellingPoint))
    assert.ok(!history.slice(-5).some(x => x.key === next.key))
    history.push(next)
  }
  assert.equal(new Set(history.slice(0, 8).map(x => x.key)).size, 8)
  assert.deepEqual(selectRememberedSlot(slots, history, { pointGap: 2, combinationGap: 5 }), selectRememberedSlot(slots, JSON.parse(JSON.stringify(history)), { pointGap: 2, combinationGap: 5 }))
})
test('间隔无法满足时明确停下，不偷偷重选最近卖点', () => assert.throws(() => selectRememberedSlot(slots.slice(0, 2), slots.slice(0, 2), { pointGap: 2, combinationGap: 5 }), /无法满足/))
test('零间隔不会误把整个历史禁止，优先未用组合', () => assert.equal(selectRememberedSlot(slots, [slots[0]!], { pointGap: 0, combinationGap: 0 }).key, 'B×角度1'))
test('目标字数含标点、不含空白，Unicode字符不拆开', () => assert.equal(speechChars('啊\n哟 🙂！'), 4))

function fixture() {
  let chars = 0, rounds = 0, stopped = false
  const history: typeof slots = [], calls: string[] = [], budgets: number[] = []
  const options = {
    targetChars: 170, maxRounds: 10, segmentMaxChars: 100,
    stopped: () => stopped, generatedChars: () => chars, generatedRounds: () => rounds,
    assertContext: () => {}, select: () => selectRememberedSlot(slots, history, { pointGap: 2, combinationGap: 5 }),
    generate: async (slot: (typeof slots)[number], maxChars: number) => { calls.push('generate'); budgets.push(maxChars); return { slot, text: '啊'.repeat(100) } },
    receive: (turn: { slot: (typeof slots)[number]; text: string }) => { calls.push('receive'); chars += speechChars(turn.text); rounds++; history.push(turn.slot) },
    score: async () => { calls.push('score') }, feedback: () => { calls.push('feedback') },
  }
  return { options, calls, budgets, history, stop: () => { stopped = true } }
}
test('串行生成→保存→评分→回灌，到目标停止并保留完整末段', async () => {
  const f = fixture()
  assert.equal(await runAutoLoop(f.options), 'completed')
  assert.deepEqual(f.calls, ['generate', 'receive', 'score', 'feedback', 'generate', 'receive', 'score', 'feedback'])
  assert.deepEqual(f.budgets, [100, 80]); assert.equal(f.options.generatedChars(), 200)
})
test('本段回灌在下一段发送前生效，不覆盖当前已收到话术', async () => {
  const f = fixture(), sentFeedback: string[] = []
  let pending = ''
  f.options.generate = async slot => { sentFeedback.push(pending); return { slot, text: '啊'.repeat(100) } }
  f.options.feedback = () => { pending = '程序填好的单项回灌' }
  assert.equal(await runAutoLoop(f.options), 'completed')
  assert.deepEqual(sentFeedback, ['', '程序填好的单项回灌'])
  assert.equal(f.options.generatedChars(), 200)
})
test('暂停中途请求要收好原文、评分再停止，绝不发下一轮', async () => {
  const f = fixture(); f.options.generate = async slot => { f.calls.push('generate'); f.stop(); return { slot, text: '啊'.repeat(100) } }
  assert.equal(await runAutoLoop(f.options), 'paused')
  assert.deepEqual(f.calls, ['generate', 'receive', 'score', 'feedback']); assert.equal(f.history.length, 1)
})
test('评分失败保留原文和调度记忆，不发下一轮', async () => {
  const f = fixture(); f.options.score = async () => { throw new Error('评分失败') }
  await assert.rejects(runAutoLoop(f.options), /评分失败/); assert.equal(f.history.length, 1); assert.equal(f.options.generatedChars(), 100)
})
test('生成失败没有虚增记忆或字数，也不自动重试', async () => {
  const f = fixture(); f.options.generate = async () => { throw new Error('请求失败') }
  await assert.rejects(runAutoLoop(f.options), /请求失败/); assert.equal(f.history.length, 0); assert.equal(f.options.generatedChars(), 0)
})
test('无效目标、轮数保护、上下文变化会停止', async () => {
  const f = fixture(); f.options.targetChars = 0; await assert.rejects(runAutoLoop(f.options), /目标字数/)
  f.options.targetChars = 1000; f.options.maxRounds = 1; await assert.rejects(runAutoLoop(f.options), /安全轮数/)
  f.options.maxRounds = 10; f.options.assertContext = () => { throw new Error('资产改变') }; await assert.rejects(runAutoLoop(f.options), /资产改变/)
})
test('空白返回立即停止，不空转到安全轮数上限', async () => {
  const f = fixture(); f.options.generate = async slot => ({ slot, text: ' \n ' })
  await assert.rejects(runAutoLoop(f.options), /没有有效话术/)
  assert.equal(f.history.length, 1)
})
test('落盘去重基准，恢复评分原文和数据不变', () => {
  const original = [1, 2].map(id => ({ id, score: { baseline_json: '{"样本":1}', scorecard_asset_id: 4 } }))
  const saved = encodeTurns(original)
  assert.equal(Object.keys(saved.baselines).length, 1)
  assert.deepEqual(decodeTurns(saved.records, saved.baselines), original)
  assert.equal(original[0]!.score.baseline_json, '{"样本":1}')
})

test('等待快速模型回灌完成后才发下一段，不并发串段', async () => {
  const f = fixture(), events: string[] = []
  let correction = ''
  f.options.generate = async slot => { events.push('send:' + correction); return { slot, text: '啊'.repeat(100) } }
  f.options.feedback = async () => { events.push('feedback-start'); await Promise.resolve(); correction = '开头、风格、结尾'; events.push('feedback-end') }
  assert.equal(await runAutoLoop(f.options), 'completed')
  assert.deepEqual(events, ['send:', 'feedback-start', 'feedback-end', 'send:开头、风格、结尾', 'feedback-start', 'feedback-end'])
})

test('回灌模型失败保留收到的原文，停止下一次生成', async () => {
  const f = fixture()
  f.options.feedback = async () => { throw new Error('回灌模型失败') }
  await assert.rejects(runAutoLoop(f.options), /回灌模型失败/)
  assert.equal(f.history.length, 1)
  assert.equal(f.options.generatedChars(), 100)
})
