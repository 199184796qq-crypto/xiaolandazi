import { normalizeCycleState, type CycleState, type CycleVoiceResponse } from './anchorCycleRun'

export interface BufferedVoice { id: string; url: string; durationMS: number }

export function bufferedVoiceMS(queue: BufferedVoice[], currentTimeSeconds: number): number {
  const total = queue.reduce((sum, item) => sum + Math.max(0, item.durationMS), 0)
  const played = queue.length ? Math.min(queue[0]!.durationMS, Math.max(0, currentTimeSeconds * 1000)) : 0
  return Math.max(0, total - played)
}

export function voiceBufferFull(queue: BufferedVoice[], currentTimeSeconds: number, limitSeconds: number): boolean {
  return limitSeconds > 0 && bufferedVoiceMS(queue, currentTimeSeconds) >= limitSeconds * 1000
}

export function pendingVoiceSegment(state: CycleState) {
  return normalizeCycleState(state).accepted.find(segment => segment.voice?.status !== 'ready')
}

function spokenUnits(text: string) { return [...text].filter(char => /[\p{L}\p{N}]/u.test(char)).length }

// Estimates only schedule production; subtitle timing always comes from PCM.
// Learn speaking speed from up to ten completed clips, otherwise use 4 units/s.
export function productionBuffer(state: CycleState | null, consumed: ReadonlySet<string>, currentId: string, playedSeconds: number) {
  const tts = state?.config.tts
  if (!state || !tts) return { totalMS: 0, pendingMS: 0, readyMS: 0, pendingCount: 0 }
  state = normalizeCycleState(state)
  const measured = state.accepted.filter(item => item.voice?.status === 'ready' && item.voice.bundle.audio_duration_ms > 0).slice(-10)
  const units = measured.reduce((sum, item) => sum + spokenUnits(item.voice!.bundle.text || item.selected.output_text || item.selected.text), 0)
  const seconds = measured.reduce((sum, item) => sum + item.voice!.bundle.audio_duration_ms / 1000, 0)
  const speed = seconds > 0 ? Math.max(2, Math.min(8, units / seconds)) : 4 * (tts.rate || 1)
  let pendingMS = 0, readyMS = 0, pendingCount = 0
  for (const item of state.accepted) {
    if (consumed.has(item.selected.id)) continue
    if (item.voice?.status === 'ready') {
      readyMS += Math.max(0, item.voice.bundle.audio_duration_ms - (item.selected.id === currentId ? Math.max(0, playedSeconds * 1000) : 0))
    } else {
      pendingCount++
      pendingMS += Math.ceil(spokenUnits(item.selected.output_text || item.selected.text) / speed * 1000)
    }
  }
  return { totalMS: pendingMS + readyMS, pendingMS, readyMS, pendingCount }
}

export function mergeVoiceResponse(state: CycleState, result: CycleVoiceResponse): CycleState {
  state = normalizeCycleState(state)
  if (result.run_id !== state.run_id || !result.voice) return state
  return { ...state, accepted: state.accepted.map(item => item.selected.id === result.candidate_id ? { ...item, voice: result.voice! } : item) }
}

// A reusable worker, independent of the text producer and playback adapter.
// It is sequential, never jumps over a failed clip, and doesn't retry paid work
// after an unknown network result. The caller exposes an explicit retry action.
export async function runVoiceQueue(options: {
  state: () => CycleState | null; stopped: () => boolean
  synthesize: (state: CycleState, id: string) => Promise<CycleVoiceResponse>
  inspect: (state: CycleState, id: string) => Promise<CycleVoiceResponse>
  receive: (result: CycleVoiceResponse) => void | Promise<void>
  wait: () => Promise<void>
}): Promise<'idle' | 'paused' | 'complete'> {
  while (!options.stopped()) {
    const state = options.state()
    if (!state?.config.tts) return 'idle'
    const next = pendingVoiceSegment(state)
    if (!next) return 'idle'
    let response = await options.synthesize(state, next.selected.id)
    if (options.state()?.run_id !== state.run_id) return 'paused'
    if (response.run_id !== state.run_id || response.candidate_id !== next.selected.id) throw new Error('声音响应与本场或采纳段不一致')
    if (response.preview_complete) return 'complete'
    let polls = 0
    while (response.voice?.status === 'generating' && !options.stopped()) {
      await options.receive(response)
      if (++polls > 150) throw new Error('上一声音请求结果尚未确认，声音队列已暂停；不会重复合成')
      await options.wait()
      if (options.state()?.run_id !== state.run_id) return 'paused'
      response = await options.inspect(state, next.selected.id)
      if (response.run_id !== state.run_id || response.candidate_id !== next.selected.id) throw new Error('声音状态与本场或采纳段不一致')
      if (response.preview_complete) return 'complete'
    }
    if (options.state()?.run_id !== state.run_id) return 'paused'
    await options.receive(response)
    if (response.voice?.status === 'generating') return 'paused'
    if (response.voice?.status !== 'ready') throw new Error(response.voice?.error || '声音资产尚未完成')
  }
  return 'paused'
}
