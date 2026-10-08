import test from 'node:test'
import assert from 'node:assert/strict'
import { canFuseAnchorSamples, editableAnchorSections, limitAnchorSample } from '../src/anchorAnalysis.ts'
test('Unicode sample limit warns and keeps first 10000 characters', () => {
 const source = '播🙂'.repeat(5001), result = limitAnchorSample(source)
 assert.equal(result.truncated, true)
 assert.equal(Array.from(result.text).length, 10000)
 assert.ok(source.startsWith(result.text))
 assert.equal(limitAnchorSample('主播').truncated, false)
})
test('fusion requires multiple complete results, never an analyzed subset', () => {
 assert.equal(canFuseAnchorSamples([]), false)
 assert.equal(canFuseAnchorSamples([{analysis_status:'analyzed'}]), false)
 assert.equal(canFuseAnchorSamples([{analysis_status:'analyzed'},{analysis_status:'pending'}]), false)
 assert.equal(canFuseAnchorSamples([{analysis_status:'analyzed'},{analysis_status:'failed'}]), false)
 assert.equal(canFuseAnchorSamples([{analysis_status:'analyzed'},{analysis_status:'analyzed'}]), true)
})
test('editing preserves section order and full point text', () => {
 assert.deepEqual(editableAnchorSections('【口语风格】\n1. 称呼：**同学们**。\n补充。\n2. 句式：短句\n【情绪】\n基调：温暖'), [
  { title:'口语风格',points:['称呼：**同学们**。\n补充。','句式：短句'] },
  { title:'情绪',points:['基调：温暖'] },
 ])
})
