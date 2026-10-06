export type PreviewStreamEvent =
  | { type: 'progress'; message: string }
  | { type: 'segment'; text: string; segment_index: number }

// Parses real server frames across arbitrary UTF-8/network boundaries.
export async function readPreviewStream<T>(response: Response, onEvent: (event: PreviewStreamEvent) => void): Promise<T> {
  if (!response.body) throw new Error('服务器未返回输出流，请重试')
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let pending = ''
  try {
    while (true) {
      const chunk = await reader.read()
      pending += decoder.decode(chunk.value, { stream: !chunk.done })
      pending = pending.replace(/\r\n/g, '\n')
      let boundary: number
      while ((boundary = pending.indexOf('\n\n')) >= 0) {
        const frame = pending.slice(0, boundary)
        pending = pending.slice(boundary + 2)
        let type = ''
        const lines: string[] = []
        for (const line of frame.split('\n')) {
          if (line.startsWith('event:')) type = line.slice(6).trim()
          if (line.startsWith('data:')) lines.push(line.slice(5).trimStart())
        }
        if (!lines.length) continue
        const data = JSON.parse(lines.join('\n'))
        if (type === 'complete') return data as T
        if (type === 'error') throw new Error(typeof data.error === 'string' ? data.error : '生成未完成，已返回文字仍保留')
        if (type === 'progress' && typeof data.message === 'string') onEvent({ type, message: data.message })
        if (type === 'segment' && typeof data.text === 'string') onEvent({ type, text: data.text, segment_index: Number(data.segment_index) || 0 })
      }
      if (pending.length > 2_000_000) throw new Error('服务器返回内容过长')
      if (chunk.done) throw new Error('连接提前结束，已返回文字仍保留，请重试')
    }
  } finally {
    await reader.cancel().catch(() => {})
    reader.releaseLock()
  }
}
