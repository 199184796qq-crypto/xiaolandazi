import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

test('人群反推和保存位于两个挖掘入口后，输入区只保留发送', async () => {
  const source = await readFile(new URL('../src/components/SpeechMinerWorkbench.vue', import.meta.url), 'utf8')
  const start = source.indexOf('<div class="speech-miner-chat-start">')
  const end = source.indexOf('</div>', start)
  const row = source.slice(start, end)
  assert.equal(row.includes("handleMiningModeClick('audience_reverse')"), true)
  assert.equal(row.includes('@click="openReview"'), true)
  assert.ok(row.indexOf('speech-miner-new-point') < row.indexOf('speech-miner-fact-gaps'))
  assert.ok(row.indexOf('speech-miner-fact-gaps') < row.indexOf('speech-miner-audience-reverse'))
  assert.ok(row.indexOf('speech-miner-audience-reverse') < row.indexOf('speech-miner-save'))
  assert.equal(row.includes("? '保存退出' : '挖掘内容'"), true)
  assert.equal(row.includes('开始挖掘'), false)
  assert.equal((row.match(/speech-miner-excavator/g) || []).length, 2)
  assert.equal(row.includes('speech-miner-excavator is-lift'), true)
  assert.equal(row.includes('speech-miner-excavator is-dig'), true)
  const composerStart = source.indexOf('<form class="speech-miner-composer"')
  const composerEnd = source.indexOf('</form>', composerStart)
  const composer = source.slice(composerStart, composerEnd)
  assert.equal(composer.includes('openReview'), false)
  assert.equal(composer.includes('发送'), true)
  assert.ok(source.includes('grid-template-columns:repeat(4,minmax(0,1fr))'))
})
