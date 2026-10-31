import { request, getLiveOfficialVoices } from './api'
import type { LiveAgentVoiceIdentity, OfficialVoice } from './types'

export type TTSPoolID = 'standard' | 'high_fidelity'
export const ttsPoolOptions: { id: TTSPoolID; name: string }[] = [{ id: 'standard', name: '标准' }, { id: 'high_fidelity', name: '高仿真' }]
export function voicePoolID(voice: Pick<LiveAgentVoiceIdentity, 'provider' | 'model'>): TTSPoolID | '' {
  if (voice.provider === 'volcengine' && voice.model === 'seed-icl-2.0') return 'high_fidelity'
  if (['aliyun', 'aliyun_qwen', 'aliyun_qwen_clone', 'qwen_audio', 'qwen', 'dashscope', 'cosyvoice', 'aliyun_cosyvoice'].includes(voice.provider) && voice.model !== 'seed-icl-2.0' && voice.model !== 'tencent-premium') return 'standard'
  return ''
}
export function poolVoiceError(voice: LiveAgentVoiceIdentity) {
  return voice.tts_pool && voice.tts_pool !== voicePoolID(voice) ? '所选声音不属于当前语音池，请选择该池内的声音后保存' : ''
}
export function getPoolOfficialVoices(pool: string | undefined) {
  return pool ? request<{ items: OfficialVoice[]; configured?: boolean }>(`/api/v1/live/official-voices?pool=${encodeURIComponent(pool)}`) : getLiveOfficialVoices()
}
export interface TTSRoomPolicy { tenant_id: number; room_id: number; allowed_pools: TTSPoolID[]; revision: number; updated_at?: string }
export function getTTSRoomPolicy(tenant: number, room: number) { return request<TTSRoomPolicy>(`/api/v1/system/tts-room-pools?tenant_id=${tenant}&room_id=${room}`, { suppressErrorDialog: true }) }
export function setTTSRoomPolicy(input: TTSRoomPolicy) { return request<TTSRoomPolicy>('/api/v1/system/tts-room-pools', { method: 'PUT', body: JSON.stringify(input), suppressErrorDialog: true }) }
