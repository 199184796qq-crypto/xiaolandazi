import test from 'node:test'
import assert from 'node:assert/strict'
import { preparationProgress, playbackPercent, playbackTime } from '../src/realtimePlayerProgress.ts'

test('real milestones are monotonic; concurrent text work cannot regress voice progress', () => {
  let progress = preparationProgress(0, 'session', 100)
  assert.equal(progress, 15)
  progress = preparationProgress(progress, 'text', 100)
  assert.equal(progress, 40)
  progress = preparationProgress(progress, 'voice', 70)
  assert.equal(progress, 78.5)
  assert.equal(preparationProgress(progress, 'text', 20), progress)
  assert.equal(preparationProgress(progress, 'voice', NaN), progress)
})
test('even all server and network steps cannot claim playback has started', () => {
  assert.equal(preparationProgress(95, 'download', 100), 99)
  assert.equal(preparationProgress(95, 'download', Infinity), 95)
  assert.equal(preparationProgress(0, 'voice', 200), 95)
})
test('playback uses actual audio time, with customer limit and ops queue duration', () => {
  assert.equal(playbackPercent(28000, 120000), 28000 / 120000 * 100)
  assert.equal(playbackPercent(1000, 0), 0)
  assert.equal(playbackPercent(130000, 120000), 100)
  assert.equal(playbackTime(28000), '00:28')
  assert.equal(playbackTime(121000), '02:01')
})
