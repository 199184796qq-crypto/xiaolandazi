import { request } from './api'

export interface LiveContentPolicy {
  content_mode: 'ai_pregenerated' | 'user_audio' | 'ai_dynamic'
  auto_refresh_enabled: boolean
  mainline_ttl_seconds: number
  faq_ttl_seconds: number
  refresh_ahead_seconds: number
  replacement_percent: number
  faq_variant_count: number
  min_repeat_seconds: number
  minimum_buffer_seconds: number
}
export interface ContentPolicyRecord {
  policy: LiveContentPolicy
  overridden: boolean
  revision: number
  system_revision: number
  updated_at?: string
  dynamic_authorized: boolean
  refresh_status?: { id: number; status: string; publish_after: string; updated_at: string; last_error: string; attempts: number }
}
export const contentPolicyURL = (roomId?: number) => roomId
  ? `/api/v1/liveops/rooms/${roomId}/content-policy`
  : '/api/v1/system/live-content-policy'
export const getContentPolicy = (roomId?: number) => request<ContentPolicyRecord>(contentPolicyURL(roomId))
export const saveContentPolicy = (record: ContentPolicyRecord, roomId?: number, inherit = false, expectedMode?: LiveContentPolicy['content_mode']) => request<ContentPolicyRecord>(contentPolicyURL(roomId), {
  method: 'PUT', body: JSON.stringify({ policy: record.policy, expected_revision: record.revision, inherit, ...(roomId ? { dynamic_authorized: record.dynamic_authorized, expected_mode: expectedMode } : {}) }),
})
