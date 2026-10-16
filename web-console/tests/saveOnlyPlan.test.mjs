import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
const view = await readFile(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
const controls = await readFile(new URL('../src/components/AnchorControlBar.vue', import.meta.url), 'utf8')
test('plan UI has save but no separate publication or rollback action', () => {
  assert.match(controls, /保存方案/)
  assert.equal((view.match(/id="plan-save-button"/g) || []).length, 1)
  assert.match(view, /@click="savePlanOneClick"/)
  assert.match(view, /mode="initialize" compact/)
  // The former preview footer control has been removed; only the navigation's
  // initialization control remains alongside the single header save button.
  assert.equal((view.match(/<AnchorControlBar\b/g) || []).length, 1)
  assert.doesNotMatch(controls, /emit\('publish'\)|发布到直播间/)
  assert.doesNotMatch(view, /publishFullShowVersion|openCurrentPlanPublishing|@publish=/)
  assert.doesNotMatch(view, /S6 发布|待发布草稿|已绑定 · 未发布/)
  assert.match(view, /下一次开始直播直接使用本次保存内容/)
})
