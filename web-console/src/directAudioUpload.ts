import type { LiveAgentCustomMainlineResponse } from './types'

export const MAX_ORIGINAL_AUDIO_BYTES = 2 * 1024 * 1024 * 1024
const audioExtensions = /\.(wav|mp3|m4a|aac|flac|ogg|webm)$/i

export function validateOriginalAudioFiles(files: File[]) {
  if (!files.length || files.length > 20) throw new Error('请选择1至20段录音。')
  if (files.some(file => file.size <= 0 || file.size > MAX_ORIGINAL_AUDIO_BYTES)) {
    throw new Error('每段录音必须大于0且不超过2GB。')
  }
  if (files.some(file => !audioExtensions.test(file.name))) {
    throw new Error('仅支持 WAV、MP3、M4A、AAC、FLAC、OGG、WEBM 音频。')
  }
}

type UploadForm = { url: string; fields: Record<string, string> }
type UploadJob = { id: string; status: string; error?: string; result?: LiveAgentCustomMainlineResponse }
type JSONRequest = <T>(path: string, options?: RequestInit & { suppressErrorDialog?: boolean }) => Promise<T>

function aborted() { return new DOMException('上传已取消', 'AbortError') }
function checkAbort(signal?: AbortSignal) { if (signal?.aborted) throw aborted() }

function uploadFile(form: UploadForm, file: File, progress: (fraction: number) => void, signal?: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    if (signal?.aborted) { reject(aborted()); return }
    const url = new URL(form.url)
    if (url.protocol !== 'https:' || url.username || url.password) { reject(new Error('OSS上传地址配置无效')); return }
    const data = new FormData()
    Object.entries(form.fields).forEach(([key, value]) => data.append(key, value))
    // Native File is streamed by the browser. Never decode or arrayBuffer a 2GB recording.
    data.append('file', file, file.name)
    const xhr = new XMLHttpRequest()
    const abort = () => xhr.abort()
    const finish = (error?: Error) => {
      signal?.removeEventListener('abort', abort)
      if (error) reject(error); else resolve()
    }
    xhr.open('POST', form.url)
    xhr.withCredentials = false
    xhr.upload.addEventListener('progress', event => {
      if (event.lengthComputable) progress(Math.min(1, event.loaded / event.total))
    })
    xhr.addEventListener('load', () => finish(xhr.status === 204 || xhr.status === 201 || xhr.status === 200
      ? undefined : new Error('OSS上传失败，请重新上传；若持续失败，请检查OSS跨域设置与上传授权。')))
    xhr.addEventListener('error', () => finish(new Error('无法直传OSS，请检查网络与OSS跨域设置后重试。')))
    xhr.addEventListener('abort', () => finish(aborted()))
    signal?.addEventListener('abort', abort, { once: true })
    xhr.send(data)
  })
}

function waitForStatus(signal?: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    if (signal?.aborted) { reject(aborted()); return }
    const abort = () => { clearTimeout(timer); reject(aborted()) }
    const timer = setTimeout(() => { signal?.removeEventListener('abort', abort); resolve() }, 1500)
    signal?.addEventListener('abort', abort, { once: true })
  })
}

export async function uploadOriginalAudio(
  request: JSONRequest, files: File[], durationMS: number,
  onProgress?: (loaded: number, total: number) => void, signal?: AbortSignal,
) {
  validateOriginalAudioFiles(files)
  checkAbort(signal)
  const init = await request<{ id: string; uploads: UploadForm[] }>('', {
    method: 'POST', signal,
    body: JSON.stringify({ files: files.map(file => ({ name: file.name, size: file.size })), duration_ms: Math.max(0, Math.round(durationMS || 0)) }),
  })
  const path = '/' + encodeURIComponent(init.id)
  let completed = false
  try {
    checkAbort(signal)
    if (init.uploads.length !== files.length) throw new Error('OSS上传授权数量不匹配，请重试。')
    const total = files.reduce((sum, file) => sum + file.size, 0)
    let loaded = 0
    for (const [index, file] of files.entries()) {
      checkAbort(signal)
      await uploadFile(init.uploads[index]!, file, fraction => {
        onProgress?.(Math.min(total * 0.99, loaded + file.size * fraction), total)
      }, signal)
      loaded += file.size
    }
    onProgress?.(total, total)
    let job = await request<UploadJob>(path + '/complete', { method: 'POST', signal })
    for (;;) {
      checkAbort(signal)
      if (job.status === 'ready' && job.result) { completed = true; return job.result }
      if (['failed', 'cancelled', 'expired'].includes(job.status)) throw new Error(job.error || '原始声音处理失败，请重新上传。')
      await waitForStatus(signal)
      job = await request<UploadJob>(path, { signal })
    }
  } finally {
    // Abort does not abandon a paid processing task silently. Cancellation is scoped
    // to this job; a late response can never alter the next plan or upload.
    if (!completed) await request(path, { method: 'DELETE', suppressErrorDialog: true, signal: AbortSignal.timeout(10000) }).catch(() => undefined)
  }
}
