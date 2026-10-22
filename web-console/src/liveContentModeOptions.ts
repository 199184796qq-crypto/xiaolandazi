export type LiveContentMode = 'ai_pregenerated' | 'user_audio' | 'ai_dynamic'

export interface LiveContentModeAccess {
  content_mode: LiveContentMode
  available_modes: LiveContentMode[]
  dynamic_authorized: boolean
  authorization_source: 'default' | 'operator' | 'membership'
  auto_refresh_enabled: boolean
  refresh_interval_seconds: number
  replacement_percent: number
}

export const defaultLiveContentModes: LiveContentMode[] = ['user_audio']

export const liveContentModeLabels: Record<LiveContentMode, { title: string; description: string }> = {
  ai_pregenerated: { title: '固定生成', description: '先生成文案，确认后生成成品声音，直播时播放。' },
  user_audio: { title: '话术优先', description: '上传录音或直接粘贴话术；观众提问由 AI 实时回复。' },
  ai_dynamic: { title: '实时声音', description: '直播中按周期准备新内容与声音，并在安全边界切换。' },
}

export function visibleLiveContentModes(access: LiveContentModeAccess | null): LiveContentMode[] {
  const modes = [...defaultLiveContentModes]
  if (access?.available_modes.includes('ai_dynamic')) modes.push('ai_dynamic')
  return modes
}

export function canSelectLiveContentMode(access: LiveContentModeAccess | null, mode: LiveContentMode): boolean {
  return access !== null && access.available_modes.includes(mode) && visibleLiveContentModes(access).includes(mode)
}
