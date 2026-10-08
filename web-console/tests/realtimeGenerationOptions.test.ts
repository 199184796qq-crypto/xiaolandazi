import test from 'node:test'
import assert from 'node:assert/strict'
import { realtimeGenerationOptionsError, realtimeOptionsFromVersion, validatedRealtimeOptions } from '../src/realtimeGenerationOptions.ts'

const options = { buffer_seconds: 120, body_min: 2, body_max: 6, max_chars: 300 }
test('只发送四个可编辑参数，不夹带后台模型及凭据', () => {
  const result = validatedRealtimeOptions({ ...options, credential_profile_id: 'private' } as typeof options)
  assert.deepEqual(result, options)
  result.max_chars = 400
  assert.equal(options.max_chars, 300)
})
test('已有方案按真实服务端版本快照恢复，不受默认值变化影响', () => {
  const context = { anchor_generation: { buffer_seconds: 45, config: { body_min: 3, body_max: 5, max_chars: 480 } }, anchor_generation_options: options }
  const restored = realtimeOptionsFromVersion(context)
  assert.deepEqual(restored, { buffer_seconds: 45, body_min: 3, body_max: 5, max_chars: 480 })
  restored!.max_chars = 200
  assert.equal(context.anchor_generation.config.max_chars, 480)
  assert.equal(realtimeOptionsFromVersion({}), null)
})
test('非法值、小数、空值和最少超过最多均禁止保存', () => {
  for (const invalid of [
    { ...options, buffer_seconds: 19 }, { ...options, buffer_seconds: 601 },
    { ...options, body_min: 1 }, { ...options, body_min: 5, body_max: 4 },
    { ...options, body_max: 7 }, { ...options, body_max: 2.5 },
    { ...options, max_chars: 79 }, { ...options, max_chars: 1001 }, { ...options, max_chars: NaN },
  ]) { assert.ok(realtimeGenerationOptionsError(invalid)); assert.throws(() => validatedRealtimeOptions(invalid)) }
  assert.ok(realtimeGenerationOptionsError(null))
  assert.equal(realtimeOptionsFromVersion({ anchor_generation: { buffer_seconds: 0, config: {} } }), null)
})
test('边界值可用，不把用户选择钳制回后台默认', () => {
  for (const valid of [{ buffer_seconds:20,body_min:2,body_max:2,max_chars:80 }, { buffer_seconds:600,body_min:6,body_max:6,max_chars:1000 }]) assert.deepEqual(validatedRealtimeOptions(valid), valid)
})
