export interface PreviewPCMFrame {
  type: 'audio'; candidate_id: string; sample_rate: number; channels: number
  pcm: string; clip_ms: number; stream_ms: number; caption: string
}
interface ScheduledFrame {
  source: AudioBufferSourceNode; start: number; durationMS: number; caption: string
}
// The server owns generation and file joining. The receiver holds only a short
// PCM jitter queue and counts samples actually sent to the audio output.
export class ContinuousPreviewPCMStream {
  private context: AudioContext | null = null
  private queue: ScheduledFrame[] = []
  private completedMS = 0
  private scheduledMS = 0
  private nextStart = 0
  private limitMS = 120000
  private finished = false
  private readonly limitReached: () => void
  private readonly factory: () => AudioContext
  constructor(limitReached: () => void, factory: () => AudioContext = () => new AudioContext()) {
    this.limitReached = limitReached
    this.factory = factory
  }
  async resume() {
    if (!this.context || this.context.state === 'closed') this.context = this.factory()
    await this.context.resume()
    if (this.context.state !== 'running') throw new Error('浏览器音频输出尚未开启')
  }
  setPlaybackLimit(ms: number) { this.limitMS = Math.max(0, ms) }
  snapshot() {
    const now = this.context?.currentTime || 0
    let playedMS = this.completedMS, caption = '', playing = false
    for (const frame of this.queue) {
      const elapsed = Math.max(0, Math.min(frame.durationMS, (now - frame.start) * 1000))
      playedMS += elapsed
      if (elapsed > 0 && elapsed < frame.durationMS) { playing = true; caption = frame.caption }
    }
    return { playedMS, playing, caption, queuedMS: Math.max(0, this.scheduledMS - playedMS) }
  }
  append(frame: PreviewPCMFrame) {
    if (this.finished) return
    const context = this.context
    if (!context || context.state !== 'running') throw new Error('连续声音输出流尚未开启')
    const rate = frame.sample_rate, channels = frame.channels
    const raw = atob(frame.pcm)
    if (!Number.isInteger(rate) || rate < 8000 || rate > 96000 || ![1, 2].includes(channels) || raw.length % (channels * 2)) throw new Error('声音流 PCM 格式错误')
    const remaining = this.limitMS > 0 ? this.limitMS - this.scheduledMS : Infinity
    const samples = Math.min(raw.length / (channels * 2), Math.floor(remaining * rate / 1000 + 0.000001))
    if (samples <= 0) return
    if (this.snapshot().queuedMS > 2000) throw new Error('声音接收缓冲超限，已暂停')
    const buffer = context.createBuffer(channels, samples, rate)
    for (let c = 0; c < channels; c++) {
      const values = buffer.getChannelData(c)
      for (let i = 0; i < samples; i++) {
        const offset = (i * channels + c) * 2
        let value = raw.charCodeAt(offset) | raw.charCodeAt(offset + 1) << 8
        if (value >= 32768) value -= 65536
        values[i] = value / 32768
      }
    }
    const source = context.createBufferSource(); source.buffer = buffer; source.connect(context.destination)
    const start = Math.max(this.nextStart, context.currentTime + 0.06), durationMS = samples * 1000 / rate
    const item = { source, start, durationMS, caption: frame.caption }
    this.queue.push(item); this.nextStart = start + durationMS / 1000; this.scheduledMS += durationMS
    source.onended = () => {
      this.queue = this.queue.filter(row => row !== item); this.completedMS += durationMS; source.disconnect()
      if (!this.finished && this.limitMS > 0 && this.completedMS >= this.limitMS - 0.1) {
        this.finished = true; this.limitReached()
      }
    }
    source.start(start, 0, durationMS / 1000)
  }
  pause() {
    const playedMS = this.snapshot().playedMS
    for (const row of this.queue) { row.source.onended = null; try { row.source.stop(); row.source.disconnect() } catch { /* Already ended. */ } }
    this.queue = []; this.completedMS = playedMS; this.scheduledMS = playedMS; this.nextStart = 0
    return playedMS
  }
  stop() {
    this.pause(); this.finished = true
    const context = this.context; this.context = null
    if (context && context.state !== 'closed') void context.close()
  }
}

export async function readPreviewPCM(response: Response, signal: AbortSignal, receive: (packet: Record<string, unknown>) => void) {
  if (!response.ok) {
    let detail = ''
    try { const body = await response.json(); detail = body.error || '' } catch { /* Use status. */ }
    throw new Error('声音流读取失败 HTTP ' + response.status + (detail ? '：' + detail : ''))
  }
  if (!response.body) throw new Error('浏览器不支持连续声音流')
  const reader = response.body.getReader(), decoder = new TextDecoder()
  let pending = ''
  try {
    while (!signal.aborted) {
      const result = await reader.read()
      pending += decoder.decode(result.value, { stream: !result.done })
      let end: number
      while ((end = pending.indexOf('\n')) >= 0) {
        const line = pending.slice(0, end); pending = pending.slice(end + 1)
        if (line.trim() && !signal.aborted) receive(JSON.parse(line))
      }
      if (pending.length > 4 * 1024 * 1024) throw new Error('声音流数据超限')
      if (result.done) { if (!signal.aborted) throw new Error('声音输出流已断开，请继续试听'); return }
    }
  } finally { await reader.cancel().catch(() => undefined); reader.releaseLock() }
}
