import type { LiveAgentVoiceIdentity, OfficialVoice, VoiceModelBinding, VoiceProfile } from './types'
import { poolVoiceError, voicePoolID } from './ttsPools'

function officialIdentity(item: OfficialVoice): LiveAgentVoiceIdentity {
  const identity: LiveAgentVoiceIdentity = { name: item.name, version: 'V1', source: 'official', provider: item.provider || 'aliyun_qwen', voice_id: item.id, model: item.model, rate: 1 }
  identity.tts_pool = voicePoolID(identity) || undefined
  return identity
}

// Only an implicit default follows the configured catalogue. An explicitly
// selected or saved voice must still be validated rather than silently changed.
export function planDefaultVoice(official: OfficialVoice[], configured: boolean): LiveAgentVoiceIdentity | null {
  if (!configured) return null
  const item = official.find(voice => voice.tags?.includes('直播默认')) || official[0]
  return item ? officialIdentity(item) : null
}

// Official voices come from the configured public catalogue; cloned voices must
// still point to the exact ready binding selected for this plan.
export function planVoiceChoices(bindings: VoiceModelBinding[], official: OfficialVoice[], configured: boolean, profiles: VoiceProfile[] = []): LiveAgentVoiceIdentity[] {
  return [
    ...bindings.filter(item => item.status === 'ready' && !!item.voice_id).map(item => ({
      name: `自定义声音 ${item.id}`, version: 'V1', source: 'clone', provider: item.provider,
      voice_id: item.voice_id, profile_id: item.profile_id, binding_id: item.id, model: item.model, rate: 1,
      tts_pool: voicePoolID(item) || undefined,
    })),
    ...profiles.filter(p => p.provider === 'volcengine' && p.clone_status === 'ready' && p.config?.target_model === 'seed-icl-2.0' && p.voice_id).map(p => ({ name: p.name, version: 'V1', source: 'clone', provider: p.provider, model: 'seed-icl-2.0', voice_id: p.voice_id, profile_id: p.id, rate: 1, tts_pool: 'high_fidelity' })),
    ...(configured ? official.map(officialIdentity) : []),
  ]
}

export function planVoiceUnavailable(identity: LiveAgentVoiceIdentity, bindings: VoiceModelBinding[], official: OfficialVoice[], configured: boolean, profiles: VoiceProfile[] = []): string {
  const mismatch = poolVoiceError(identity)
  if (mismatch) return mismatch
  if (identity.source === 'official' && !identity.binding_id && !identity.profile_id) {
    return configured && official.some(item => item.id === identity.voice_id && item.model === identity.model && (item.provider || 'aliyun_qwen') === identity.provider)
      ? '' : '该官方声音当前不可用，请重新选择声音后再保存'
  }
  if (!identity.binding_id) {
    const profile = profiles.find(p => p.id === identity.profile_id)
    return profile?.clone_status === 'ready' && profile.voice_id === identity.voice_id && profile.provider === identity.provider && (profile.config?.target_model || 'qwen-audio-3.0-tts-plus') === identity.model
      ? '' : '该声音已失效或已停用，请重新选择声音后再保存'
  }
  const binding = bindings.find(item => item.id === identity.binding_id)
  return binding?.status === 'ready' && !!binding.voice_id && binding.voice_id === identity.voice_id
    && binding.provider === identity.provider && binding.model === identity.model
    && (!identity.profile_id || binding.profile_id === identity.profile_id)
    ? '' : '该声音已失效或已停用，请重新选择声音后再保存'
}

export function openPlanPicker(select: HTMLSelectElement | null) {
  select?.focus({ preventScroll: true })
  try { select?.showPicker?.() } catch { /* Focus remains usable when the browser disallows a native picker. */ }
}
