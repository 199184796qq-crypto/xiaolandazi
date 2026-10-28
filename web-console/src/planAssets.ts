import type { LiveAgentVoiceIdentity, OfficialVoice, VoiceModelBinding } from './types'

// Official voices come from the configured public catalogue; cloned voices must
// still point to the exact ready binding selected for this plan.
export function planVoiceChoices(bindings: VoiceModelBinding[], official: OfficialVoice[], configured: boolean): LiveAgentVoiceIdentity[] {
  return [
    ...bindings.filter(item => item.status === 'ready' && !!item.voice_id).map(item => ({
      name: `自定义声音 ${item.id}`, version: 'V1', source: 'clone', provider: item.provider,
      voice_id: item.voice_id, profile_id: item.profile_id, binding_id: item.id, model: item.model, rate: 1,
    })),
    ...(configured ? official.map(item => ({ name: item.name, version: 'V1', source: 'official', provider: item.provider || 'aliyun_qwen', voice_id: item.id, model: item.model, rate: 1 })) : []),
  ]
}

export function planVoiceUnavailable(identity: LiveAgentVoiceIdentity, bindings: VoiceModelBinding[], official: OfficialVoice[], configured: boolean): string {
  if (identity.source === 'official' && !identity.binding_id && !identity.profile_id) {
    return configured && official.some(item => item.id === identity.voice_id && (item.provider || 'aliyun_qwen') === identity.provider)
      ? '' : '该官方声音当前不可用，请重新选择声音后再保存'
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
