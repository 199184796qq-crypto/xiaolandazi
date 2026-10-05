export type LiveContentMode = 'ai_pregenerated' | 'user_audio' | 'ai_dynamic'

export interface LiveContentModeAccess {
  content_mode: LiveContentMode
  available_modes: LiveContentMode[]
  dynamic_authorized: boolean
  authorization_source: 'default' | 'operator' | 'membership'
}

export const defaultLiveContentModes: LiveContentMode[] = ['ai_pregenerated', 'user_audio']

export const liveContentModeLabels: Record<LiveContentMode, { title: string; description: string }> = {
  ai_pregenerated: { title: 'AI话术 + AI预生成 + AI实时互动', description: '先生成文案，确认后生成成品声音，直播时播放。' },
  user_audio: { title: '原始录音 + AI实时互动', description: '上传你的原始录音作为主线，观众提问由 AI 实时回复。' },
  ai_dynamic: { title: 'AI话术 + AI动态生成 + AI实时互动', description: '开播后按后台周期逐批更新文稿与声音，就绪后在音轨边界切换；失败保留原成品。' },
}

export function visibleLiveContentModes(access: LiveContentModeAccess | null): LiveContentMode[] {
  const modes = [...defaultLiveContentModes]
  if (access?.dynamic_authorized === true && access.available_modes.includes('ai_dynamic')) modes.push('ai_dynamic')
  return modes
}

export function canSelectLiveContentMode(access: LiveContentModeAccess | null, mode: LiveContentMode): boolean {
  return access !== null && access.available_modes.includes(mode) && visibleLiveContentModes(access).includes(mode)
}
