const SHARED_AUDIO_CONTEXT_KEY = '__livecompanion_shared_audio_context_v1'

type AudioRuntimeWindow = Record<string, AudioContext | undefined>

function runtimeWindow() {
  return window as unknown as AudioRuntimeWindow
}

export function getSharedAudioContext(create = false) {
  const AudioContextCtor = window.AudioContext
  if (!AudioContextCtor) return null
  const holder = runtimeWindow()
  let context = holder[SHARED_AUDIO_CONTEXT_KEY]
  if (context?.state === 'closed') {
    context = undefined
    holder[SHARED_AUDIO_CONTEXT_KEY] = undefined
  }
  if (!context && create) {
    context = new AudioContextCtor()
    holder[SHARED_AUDIO_CONTEXT_KEY] = context
  }
  return context ?? null
}

export async function unlockSharedAudioContext() {
  const context = getSharedAudioContext(true)
  if (!context) return null
  if (context.state === 'suspended') {
    await context.resume()
  }
  if (context.state !== 'running') {
    throw new Error('浏览器音频上下文未进入运行状态')
  }
  return context
}
