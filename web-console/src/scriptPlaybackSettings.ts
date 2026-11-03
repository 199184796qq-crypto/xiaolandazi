import type { LiveAgentVoiceIdentity } from './types'

export type ScriptPlaybackVoice = { voice_id: string; model: string; identity?: LiveAgentVoiceIdentity }
export type ScriptPlaybackSettings = {
  play_mode: 'full' | 'single'
  voices: ScriptPlaybackVoice[]
  version_cap?: number
  min_voice_block?: number
}
export const playbackVoiceKey = (voice: Pick<LiveAgentVoiceIdentity, 'voice_id' | 'model'>): string => JSON.stringify([voice.model, voice.voice_id])

export function readScriptPlaybackSettings(value: unknown): ScriptPlaybackSettings {
  const raw = (value && typeof value === 'object' ? value : {}) as Partial<ScriptPlaybackSettings>
  return {
    play_mode: raw.play_mode === 'single' ? 'single' : 'full',
    voices: Array.isArray(raw.voices) ? raw.voices.filter(voice => voice && typeof voice.voice_id === 'string').map(voice => ({
      voice_id: voice.voice_id, model: voice.model || '',
      ...(voice.identity ? { identity: { ...voice.identity } } : {}),
    })) : [],
    ...(typeof raw.version_cap === 'number' ? { version_cap: raw.version_cap } : {}),
    ...(typeof raw.min_voice_block === 'number' ? { min_voice_block: raw.min_voice_block } : {}),
  }
}

export function selectedScriptVoices(settings: ScriptPlaybackSettings, primary: LiveAgentVoiceIdentity): LiveAgentVoiceIdentity[] {
  if (!settings.voices.length) return primary.voice_id ? [{ ...primary }] : []
  return settings.voices.map(voice => voice.identity ? { ...voice.identity } :
    voice.voice_id === primary.voice_id && (!voice.model || voice.model === primary.model) ? { ...primary } : {
      version: 'V1', source: '', provider: primary.provider, tts_pool: primary.tts_pool,
      name: '待重新选择的声音', voice_id: voice.voice_id, model: voice.model,
    })
}

export function scriptPlaybackForSave(settings: ScriptPlaybackSettings, voices: readonly LiveAgentVoiceIdentity[]): ScriptPlaybackSettings {
  if (voices.length > 8) throw new Error('最多选择8个声音')
  if (!voices.length) throw new Error('请至少选择一个声音')
  if (new Set(voices.map(playbackVoiceKey)).size !== voices.length) throw new Error('不能重复选择同一个声音')
  return {
    ...settings,
    voices: voices.map(identity => ({ voice_id: identity.voice_id, model: identity.model, identity: { ...identity } })),
  }
}
