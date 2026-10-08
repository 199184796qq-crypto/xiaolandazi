import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

test('主播风格页只保留三个入口，不再显示测试与训练', async () => {
  const source = await readFile(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
  const nav = source.match(/<nav class="reusable-anchor-style-tabs"[\s\S]*?<\/nav>/)?.[0] || ''
  assert.equal((nav.match(/anchor-style-tab-card/g) || []).length, 3)
  assert.equal(nav.includes('测试与训练'), false)
  for (const label of ['风格样本', '风格分析结果', '个性化挂载']) assert.ok(nav.includes(label))
  assert.ok(source.includes('.reusable-anchor-style-tabs{display:grid;grid-template-columns:repeat(3,minmax(0,1fr))'))
})
