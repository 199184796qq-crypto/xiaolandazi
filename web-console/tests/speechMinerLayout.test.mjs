import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

test('speech miner chat does not stretch to the result-list height', async () => {
  const source = await readFile(new URL('../src/components/SpeechMinerWorkbench.vue', import.meta.url), 'utf8')
  assert.ok(source.includes('align-items: start;'))
  assert.ok(source.includes('height: fit-content;'))
  assert.ok(source.includes('grid-template-rows: auto auto auto auto auto;'))
  assert.ok(source.includes('max-height: 900px;'))
  assert.ok(!source.includes('grid-template-rows: auto minmax(320px,1fr) auto auto auto;'))
})
