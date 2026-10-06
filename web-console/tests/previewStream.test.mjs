import test from 'node:test'
import assert from 'node:assert/strict'
import { readPreviewStream } from '../src/agent/previewStream.ts'

function response(chunks) {
  return new Response(new ReadableStream({ start(controller) { for (const chunk of chunks) controller.enqueue(chunk); controller.close() } }))
}
test('arbitrary UTF-8 chunks deliver progress and segments before completion', async () => {
  const raw = 'event: progress\r\ndata: {"message":"安排中"}\r\n\r\nevent: segment\r\ndata: {"text":"第一段","segment_index":1}\r\n\r\nevent: segment\r\ndata: {"text":"第一段\\n第二段","segment_index":2}\r\n\r\nevent: complete\r\ndata: {"text":"第一段\\n第二段"}\r\n\r\n'
  const bytes = new TextEncoder().encode(raw)
  const events = []
  const result = await readPreviewStream(response([...bytes].map((value) => Uint8Array.of(value))), (event) => events.push(event))
  assert.equal(result.text, '第一段\n第二段')
  assert.deepEqual(events.map((event) => event.type), ['progress', 'segment', 'segment'])
  assert.equal(events[1].text, '第一段')
})
test('terminal error and dropped connections do not remove already delivered text', async () => {
  for (const tail of ['', 'event: error\ndata: {"error":"后续失败"}\n\n']) {
    const events = []
    const raw = 'event: segment\ndata: {"text":"保留正文","segment_index":1}\n\n' + tail
    await assert.rejects(readPreviewStream(response([new TextEncoder().encode(raw)]), (event) => events.push(event)))
    assert.equal(events[0].text, '保留正文')
  }
})
