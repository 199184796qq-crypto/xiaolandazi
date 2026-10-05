import { request } from './api'
import type { LiveContentMode, LiveContentModeAccess } from './liveContentModeOptions'

export function getLiveContentMode(roomId: number) {
  return request<LiveContentModeAccess>(`/api/v1/live/rooms/${roomId}/content-mode`)
}

export function setLiveContentMode(roomId: number, contentMode: LiveContentMode) {
  return request<LiveContentModeAccess>(`/api/v1/live/rooms/${roomId}/content-mode`, {
    method: 'PUT', body: JSON.stringify({ content_mode: contentMode }),
  })
}
