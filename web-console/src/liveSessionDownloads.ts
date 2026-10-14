import { request } from './api'
import { liveSupportRoomForRequest } from './liveSupportAccess'

export interface SessionVoiceItem {
  id: number; session_id: number; kind: string; text: string; duration_ms: number
  played_at: string; expires_at?: string; available: boolean
}
export interface SessionVoicePage {
  session_id: number; started_at: string; ended_at?: string
  items: SessionVoiceItem[]; total: number; page: number; page_size: number
}
export function sessionDownloadURL(room: number, session?: number) {
  return `/api/v1/rooms/${room}/live-downloads` + (session ? `?session_id=${session}` : '')
}
export function getSessionVoices(room: number, session: number | undefined, from: string, to: string, page: number, signal?: AbortSignal) {
  const params = new URLSearchParams({ page: String(page) })
  if (session) params.set('session_id', String(session))
  if (from) params.set('from', new Date(from).toISOString())
  if (to) params.set('to', new Date(to).toISOString())
  return request<SessionVoicePage>(`/api/v1/rooms/${room}/live-downloads?${params}`, { signal, suppressErrorDialog: true })
}
export async function downloadSessionFile(url: string, filename: string, signal?: AbortSignal) {
  const headers = new Headers()
  const support = liveSupportRoomForRequest(url)
  if (support) headers.set('X-Live-Support-Room-ID', String(support))
  const response = await fetch(url, { credentials: 'include', headers, signal })
  if (!response.ok) {
    const error = await response.json().catch(() => ({})) as { error?: string }
	throw Object.assign(new Error(error.error || '下载失败，请重试'), { status: response.status })
  }
  const blob = await response.blob()
  if (signal?.aborted) return
  const objectURL = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = objectURL; link.download = filename
  document.body.append(link); link.click(); link.remove()
  window.setTimeout(() => URL.revokeObjectURL(objectURL), 60_000)
}
