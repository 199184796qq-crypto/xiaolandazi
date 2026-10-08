import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { readFile } from 'node:fs/promises'
let server, component
before(async () => { server = await createServer({ server: { middlewareMode: true }, appType: 'custom' }); component = (await server.ssrLoadModule('/src/components/RealtimeAudioPlayer.vue')).default })
after(async () => { await server?.close() })
const props = { preparing: true, playing: false, active: true, progress: 63, elapsedMS: 0, readySeconds: 0, unlimited: false, startDisabled: false, stopDisabled: false, failed: false }
test('gradient controls expose accessible labels, real percent, no backend step names', async () => {
  const html = await renderToString(createSSRApp(component, props))
  assert.equal((html.match(/<button\b/g) || []).length, 3)
  for (const label of ['开始或继续试听', '暂停试听', '结束试听', '正在准备', '63%', 'aria-valuenow="63"', '已缓冲 0 秒', '最多试听2分钟']) assert.ok(html.includes(label))
  for (const internal of ['TTS', 'OSS', '评分', '会话', '模型']) assert.equal(html.includes(internal), false)
})
test('once playing, progress switches to elapsed time and stopped buttons stay disabled', async () => {
  const html = await renderToString(createSSRApp(component, { ...props, preparing: false, playing: true, progress: 25, elapsedMS: 28000, unlimited: true }))
  assert.ok(html.includes('00:28')); assert.ok(html.includes('试听播放进度')); assert.ok(html.includes('授权运维不限时'))
  assert.equal(html.includes('正在准备'), false)
  const idle = await renderToString(createSSRApp(component, { ...props, active: false, stopDisabled: true }))
  assert.equal((idle.match(/<button\b[^>]*\bdisabled/g) || []).length, 2)
})
test('caption centered both ways; ready cache uses actual audio, no producer estimate', async () => {
  const source = await readFile(new URL('../src/components/RealtimeAnchorPreviewPanel.vue', import.meta.url), 'utf8')
  assert.ok(source.includes('align-items:center;justify-content:center;text-align:center;'))
  assert.ok(source.includes('Math.floor(buffered.readyMS / 1000)'))
  assert.ok(source.includes('Accept: \'text/event-stream\''))
  assert.ok(source.includes('new ContinuousPreviewAudioStream(streamEnded)'))
  assert.ok(source.includes('await audioStream.append'))
  assert.equal(source.includes('<audio ref="player"'), false)
})
test('only realtime cumulative speech has a fixed scrolling window', async () => {
  const source = await readFile(new URL('../src/components/RealtimeAnchorPreviewPanel.vue', import.meta.url), 'utf8')
  assert.ok(source.includes('.realtime-anchor-preview :deep(.risk-text.box){height:440px;min-height:0;max-height:440px;overflow-y:auto;overflow-x:hidden;scrollbar-gutter:stable}'))
  assert.ok(source.includes('height:320px;max-height:320px'))
  assert.ok(source.includes('box auto-scroll label="累计生成话术"'))
})
test('only realtime player is reduced to 50% and remains centered; speech and captions are not zoomed', async () => {
  const source = await readFile(new URL('../src/components/RealtimeAnchorPreviewPanel.vue', import.meta.url), 'utf8')
  assert.ok(source.includes('.realtime-anchor-preview :deep(.audio-player){width:50%;box-sizing:border-box;zoom:.5;justify-self:center}'))
  assert.equal(/(?:risk-text|playback-caption)[^\n]*zoom:/.test(source), false)
})
test('generation budget completion drains cached audio before finishing without a red error', async () => {
  const source = await readFile(new URL('../src/components/RealtimeAnchorPreviewPanel.vue', import.meta.url), 'utf8')
  assert.ok(source.includes("result === 'complete'"))
  assert.ok(source.includes('voiceComplete.value = true'))
  assert.ok(source.includes('if (!voiceComplete.value || current.value) return false'))
  assert.ok(source.includes("previewFinished.value ? '本场试听已播放完成'"))
  assert.equal(source.includes('本场试听已达到2分钟上限，请结束试听'), false)
})
