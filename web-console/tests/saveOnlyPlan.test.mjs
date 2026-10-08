import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
const view = await readFile(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
const controls = await readFile(new URL('../src/components/AnchorControlBar.vue', import.meta.url), 'utf8')
test('plan UI has save but no separate publication or rollback action', () => {
  assert.match(controls, /保存智能体方案/)
  assert.doesNotMatch(controls, /emit\('publish'\)|发布到直播间/)
  assert.doesNotMatch(view, /publishFullShowVersion|openCurrentPlanPublishing|@publish=/)
  assert.doesNotMatch(view, /S6 发布|待发布草稿|已绑定 · 未发布/)
  assert.match(view, /下一次开始直播直接使用本次保存内容/)
})
