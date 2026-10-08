import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

test('lexical hits receive semantic review; blocked P0 only edits or appeals', async () => {
  const source = await readFile(new URL('../src/components/FactRiskEvidencePanel.vue', import.meta.url), 'utf8')
  assert.ok(source.includes("item.severity === 'P0'"))
  assert.ok(source.includes("item.semantic_decision === 'pending'"))
  assert.ok(source.includes("item.semantic_decision === 'pass'"))
  assert.ok(source.includes("item.semantic_decision === 'evidence_required'"))
  assert.ok(source.includes('程序与语义复核均未通过，不能由商户直接放行'))
  assert.ok(source.includes('修改原始内容'))
  assert.ok(source.includes("action: 'appeal'"))
  assert.ok(source.includes('申请后台复核'))
  assert.ok(source.includes('needsEvidence'))
  assert.ok(source.includes('确认并备案'))
  assert.ok(source.includes("item.severity === 'P2'"))
  assert.ok(source.includes('确认可以使用并备案'))
  assert.ok(source.includes('处理原因：'))
  assert.ok(source.includes("session.bootstrap?.actor.role === 'customer'"))
})

test('risk UI is embedded as color annotations in the fact library', async () => {
  const source = await readFile(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
  assert.ok(source.includes('<FactRiskEvidencePanel ref="factRiskController" headless'))
  assert.ok(source.includes('strategy-fact-risk-badge'))
  assert.ok(source.includes('factRiskTone(item)'))
  assert.ok(source.includes('我已知晓并确认'))
  assert.ok(source.includes('用户确认该事实内容真实，并同意按当前内容用于本方案'))
  assert.ok(source.includes("pendingForArea(reviews, 'miner')"))
  assert.ok(source.includes("reviewsForArea(factRiskReviews.value, 'knowledge', item.id)"))
  assert.ok(source.includes("sourceArea(source) === 'knowledge' && source.entity_id === item.id"))
  assert.ok(source.includes('productRiskUnresolvedCount'))
  assert.ok(source.includes('benefitRiskUnresolvedCount'))
  assert.ok(source.includes('wheelRiskUnresolvedCount'))
  assert.ok(!source.includes('@count="factRiskUnresolvedCount = $event"'))
})

test('wheel risks stay with the speech miner instead of the fact tab', async () => {
  const source = await readFile(new URL('../src/components/SpeechMinerWorkbench.vue', import.meta.url), 'utf8')
  assert.ok(source.includes('riskForAngle'))
  assert.ok(source.includes('speech-miner-angle-risk'))
  assert.ok(source.includes("emit('confirmRisk', review)"))
  assert.ok(source.includes('我已知晓并确认'))
})
