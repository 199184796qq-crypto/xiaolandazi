import test from 'node:test'
import assert from 'node:assert/strict'
import { mainlineLiveTypeLabel, mainlineIndustryLabel, mainlineRoleSequenceLabel } from '../src/agent/mainlineDisplay.ts'

test('translate the commerce strategy without changing order or repeated actions', () => {
  assert.equal(mainlineLiveTypeLabel('commerce'), '电商带货')
  assert.equal(mainlineIndustryLabel('general'), '通用行业')
  assert.equal(mainlineRoleSequenceLabel(['orient', 'value', 'scenario', 'evidence', 'difference', 'action', 'decision', 'recap_bridge', 'action'], 'commerce'),
    '引入话题 → 讲清价值 → 展开使用场景 → 提供可信依据 → 解释差异 → 引导下单 → 帮助选择 → 回顾重点并自然过渡 → 引导下单')
})

test('all supported live types and industries have Chinese labels', () => {
  for (const code of ['commerce', 'ecommerce', 'education', 'performance', 'conversation', 'chat']) assert.match(mainlineLiveTypeLabel(code), /\p{Script=Han}/u)
  for (const code of ['general', 'apparel', 'food', 'durable_goods']) assert.match(mainlineIndustryLabel(code), /\p{Script=Han}/u)
  assert.equal(mainlineIndustryLabel('apparel'), '服装鞋帽')
  assert.equal(mainlineRoleSequenceLabel(['scenario', 'action', 'decision'], 'chat'), '展开具体情境 → 引导参与 → 帮助判断')
})

test('unknown identifiers are not exposed, while Chinese custom names are retained', () => {
  assert.equal(mainlineIndustryLabel('custom_package_v2'), '自定义行业')
  assert.equal(mainlineLiveTypeLabel('custom_live_v2'), '自定义直播')
  assert.equal(mainlineIndustryLabel('美妆护理'), '美妆护理')
  assert.equal(mainlineRoleSequenceLabel(['future_role'], 'commerce'), '继续展开话题')
})
