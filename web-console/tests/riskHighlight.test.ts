import test from 'node:test'
import assert from 'node:assert/strict'
import { riskChunks } from '../src/riskHighlight.ts'
import type { RiskHit, RiskTier } from '../src/riskHighlight.ts'

function hit(tier: RiskTier, start: number, text: string): RiskHit {
  return { rule_id: 'fixture', severity: tier, category: '测试', matched_text: text, original_start: start, original_end: start + Array.from(text).length, condition: 'review' }
}
test('P1粉红与P2大红的具体词段保留原文和Unicode位置', () => {
  const text = '😀零胆固醇，买到就是赚到。'
  const chunks = riskChunks(text, [hit('P1', 1, '零胆固醇'), hit('P2', 6, '买到就是赚到')])
  assert.equal(chunks.map(c => c.text).join(''), text)
  assert.equal(chunks.find(c => c.tier === 'P1')?.text, '零胆固醇')
  assert.equal(chunks.find(c => c.tier === 'P2')?.text, '买到就是赚到')
})
test('重叠时P0优先，P1和P2不同色，不重复输出文字', () => {
  const text = '非转基因更安全，绝对放心'
  const chunks = riskChunks(text, [hit('P0', 0, '非转基因更安全'), hit('P1', 0, '非转基因'), hit('P0', 8, '绝对'), hit('P2', 8, '绝对放心')])
  assert.equal(chunks.map(c => c.text).join(''), text)
  assert.equal(chunks[0]?.tier, 'P0')
  assert.equal(chunks.at(-1)?.tier, 'P2')
  assert.equal(chunks.at(-1)?.text, '放心')
})
test('旧响应、坏范围、UTF16错位不能把其他文字标色', () => {
  const text = '😀正常文字'
  const malformed = [hit('P1', 2, '正常'), hit('P2', -1, '😀'), hit('P1', 1, '另一个词')]
  const chunks = riskChunks(text, malformed)
  assert.equal(chunks.map(c => c.text).join(''), text)
  assert.equal(chunks.some(c => c.tier), false)
})
test('模型HTML只作为纯文字返回，不拼接高亮HTML', () => {
  const text = '<img src=x onerror=alert(1)>买到就是赚到'
  const chunks = riskChunks(text, [hit('P2', Array.from('<img src=x onerror=alert(1)>').length, '买到就是赚到')])
  assert.equal(chunks.map(c => c.text).join(''), text)
  assert.equal(chunks[0]?.tier, undefined)
  assert.deepEqual(riskChunks('', []), [])
})
