import test from 'node:test'
import assert from 'node:assert/strict'
import { copySpeechModel, type SpeechModelProfile } from '../src/speechModelCopy.ts'

const source: SpeechModelProfile = {
  id: 'original', name: 'Qwen38Max', protocol: 'openai_chat', base_url: 'https://example.com/v1',
  model: 'qwen-model', enabled: false, timeout_ms: 45000, max_tokens: 1234,
  system_prompt: 'custom prompt', chat_token_field: 'max_completion_tokens', has_api_key: true,
}

test('复制所有参数并产生独立新ID、名称+02，不修改原配置', () => {
  const before = JSON.stringify(source)
  const copy = copySpeechModel(source, [source], 'copy')
  assert.equal(copy.name, 'Qwen38Max02')
  assert.equal(copy.id, 'copy')
  assert.equal(copy.copy_from_id, 'original')
  assert.equal(copy.api_key, undefined)
  const { id, name, copy_from_id, ...same } = copy
  const { id: oldID, name: oldName, ...original } = source
  assert.deepEqual(same, original)
  assert.equal(JSON.stringify(source), before)
  copy.system_prompt = 'edited'
  assert.equal(source.system_prompt, 'custom prompt')
})

test('重名顺延03、04；复制未保存的副本仍引用已保存源ID', () => {
  const first = copySpeechModel(source, [source], 'first')
  const second = copySpeechModel(source, [source, first], 'second')
  assert.equal(second.name, 'Qwen38Max03')
  assert.equal(copySpeechModel(source, [source, first, second], 'third').name, 'Qwen38Max04')
  const child = copySpeechModel(first, [source, first, second], 'child')
  assert.equal(child.name, 'Qwen38Max0202')
  assert.equal(child.copy_from_id, source.id)
})

test('已输入新密钥随草稿复制，不复用旧密钥；无密钥草稿不伪造保存状态', () => {
  const changed = copySpeechModel({ ...source, api_key: 'fixture-only-key', copy_from_id: 'old' }, [source], 'copy')
  assert.equal(changed.api_key, 'fixture-only-key')
  assert.equal(changed.copy_from_id, undefined)
  const draft = copySpeechModel({ ...source, has_api_key: false }, [source], 'draft-copy')
  assert.equal(draft.has_api_key, false)
  assert.equal(draft.copy_from_id, undefined)
})

test('30项上限、重复ID和80字名称约束', () => {
  assert.throws(() => copySpeechModel(source, Array.from({ length: 30 }, () => source), 'copy'), /最多保存/)
  assert.throws(() => copySpeechModel(source, [source], source.id), /新的/)
  const long = copySpeechModel({ ...source, name: '🙂'.repeat(80) }, [source], 'copy')
  assert.equal(Array.from(long.name).length, 80)
  assert.ok(long.name.endsWith('02'))
})
