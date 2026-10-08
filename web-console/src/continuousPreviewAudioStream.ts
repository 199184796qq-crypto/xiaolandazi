export interface ContinuousPreviewClip {
  id: string
  url: string
  durationMS: number
}

export interface ContinuousPreviewSnapshot {
  playing: boolean
  currentId: string
  currentMS: number
}

type AudioContextFactory = () => AudioContext
type AudioFetcher = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>

interface ScheduledClip {
  id: string
  startAt: number
  endAt: number
  durationMS: number
  source: AudioBufferSourceNode
}

// Every generated WAV is decoded and sample-accurately scheduled into one
// AudioContext output timeline. The browser never swaps <audio> URLs between
// clips, so the full head/tail guards remain in the continuous output stream.
export class ContinuousPreviewAudioStream {
  private readonly ended: (id: string, durationMS: number) => void
  private readonly contextFactory: AudioContextFactory
  private readonly fetchAudio: AudioFetcher
  private context: AudioContext | null = null
  private generation = 0
  private nextStartAt = 0
  private playbackLimitMS = 0
  private scheduledMS = 0
  private scheduled: ScheduledClip[] = []
  private known = new Set<string>()
  private controllers = new Set<AbortController>()
  private appendChain: Promise<void> = Promise.resolve()

  constructor(
    ended: (id: string, durationMS: number) => void,
    contextFactory: AudioContextFactory = () => new AudioContext(),
    fetchAudio: AudioFetcher = (input, init) => fetch(input, init),
  ) {
    this.ended = ended
    this.contextFactory = contextFactory
    this.fetchAudio = fetchAudio
  }

  async resume() {
    if (!this.context || this.context.state === 'closed') this.context = this.contextFactory()
    if (this.context.state === 'suspended') await this.context.resume()
    if (this.context.state !== 'running') throw new Error('浏览器音频输出流未进入运行状态')
  }

  async pause() {
    if (this.context?.state === 'running') await this.context.suspend()
  }

  setPlaybackLimit(limitMS: number) {
    this.playbackLimitMS = Number.isFinite(limitMS) ? Math.max(0, limitMS) : 0
  }

  append(clip: ContinuousPreviewClip) {
    if (this.known.has(clip.id)) return Promise.resolve()
    this.known.add(clip.id)
    const generation = this.generation
    const task = this.appendChain.then(() => this.appendOne(clip, generation))
    this.appendChain = task.catch(() => undefined)
    return task
  }

  snapshot(): ContinuousPreviewSnapshot {
    const context = this.context
    if (!context || context.state !== 'running') return { playing: false, currentId: '', currentMS: 0 }
    const now = context.currentTime
    const current = this.scheduled.find(item => now >= item.startAt && now < item.endAt)
    if (!current) return { playing: false, currentId: '', currentMS: 0 }
    return {
      playing: true,
      currentId: current.id,
      currentMS: Math.max(0, Math.min(current.durationMS, (now - current.startAt) * 1000)),
    }
  }

  stop() {
    this.generation += 1
    for (const controller of this.controllers) controller.abort()
    this.controllers.clear()
    for (const item of this.scheduled) {
      item.source.onended = null
      try { item.source.stop() } catch { /* Source already ended. */ }
      try { item.source.disconnect() } catch { /* Best-effort graph cleanup. */ }
    }
    this.scheduled = []
    this.known.clear()
    this.nextStartAt = 0
    this.scheduledMS = 0
    this.playbackLimitMS = 0
    this.appendChain = Promise.resolve()
    const context = this.context
    this.context = null
    if (context && context.state !== 'closed') void context.close().catch(() => undefined)
  }

  private async appendOne(clip: ContinuousPreviewClip, generation: number) {
    const context = this.context
    if (!context || context.state === 'closed') throw new Error('连续声音输出流尚未启用')
    const remainingMS = this.playbackLimitMS > 0 ? Math.max(0, this.playbackLimitMS - this.scheduledMS) : Infinity
    if (remainingMS === 0) return
    const controller = new AbortController()
    this.controllers.add(controller)
    try {
      const response = await this.fetchAudio(clip.url, { cache: 'no-store', credentials: 'same-origin', signal: controller.signal })
      if (!response.ok) {
        let detail = ''
        try { const body = await response.json(); if (typeof body?.error === 'string') detail = body.error } catch { /* Keep the HTTP status when the server has no JSON error. */ }
        throw new Error('声音流读取失败 HTTP ' + response.status + (detail ? '：' + detail : ''))
      }
      const encoded = await response.arrayBuffer()
      const decoded = await context.decodeAudioData(encoded.slice(0))
      if (generation !== this.generation || controller.signal.aborted || this.context !== context) return
      const source = context.createBufferSource()
      source.buffer = decoded
      source.connect(context.destination)
      const startAt = Math.max(this.nextStartAt, context.currentTime + 0.12)
      const durationMS = Math.min(decoded.duration * 1000, remainingMS)
      const endAt = startAt + durationMS / 1000
      const scheduled = { id: clip.id, startAt, endAt, durationMS, source }
      this.scheduled.push(scheduled)
      this.nextStartAt = endAt
      this.scheduledMS += durationMS
      source.onended = () => {
        if (generation !== this.generation) return
        this.scheduled = this.scheduled.filter(item => item !== scheduled)
        try { source.disconnect() } catch { /* Source cleanup only. */ }
        this.ended(clip.id, durationMS)
      }
      source.start(startAt, 0, durationMS / 1000)
    } finally {
      this.controllers.delete(controller)
    }
  }
}
