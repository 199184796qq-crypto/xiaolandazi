import { request } from './api'
import type { LiveAnchorStyle } from './types'

export interface RecommendedAnchor { id: number; name: string; introduction: string; summary: string; avatar_type?: string; avatar_url?: string; status: string; version_no: number; source_tenant_id?: number; source_style_id?: number }
export interface AnchorSource { id: number; tenant_id: number; tenant_name: string; name: string; description: string; avatar_type?: string; avatar_url?: string; has_style: boolean }
export interface RecommendationDraft { tenant_id: number; style_id: number; name: string; introduction: string; summary: string; avatar_type?: string; avatar_url?: string; source_sha256: string; provider: string; model: string; raw_text: string; prompt_source: string; validation_error?: string }
function tenantQuery(tenant?: number) { return tenant ? '?tenant_id=' + tenant : '' }
export function getRecommendedAnchors() { return request<{ items: RecommendedAnchor[] }>('/api/v1/live/anchor-recommendations', { cache: 'no-store', suppressErrorDialog: true }) }
export function importRecommendedAnchors(ids: number[], tenant?: number) { return request<{ items: LiveAnchorStyle[] }>('/api/v1/live/anchor-recommendations/import' + tenantQuery(tenant), { method: 'POST', body: JSON.stringify({ ids }), suppressErrorDialog: true }) }
export function getCurrentAnchor(plan: number, tenant?: number) { return request<{ style_id: number }>(`/api/v1/live-anchor-styles/current?plan_id=${plan}${tenant ? '&tenant_id=' + tenant : ''}`, { cache: 'no-store' }) }
export function getRecommendationSources(search = '') { return request<{ items: AnchorSource[] }>('/api/v1/system/anchor-recommendations/sources?search=' + encodeURIComponent(search), { cache: 'no-store', suppressErrorDialog: true }) }
export function getRecommendationAdmin() { return request<{ items: RecommendedAnchor[] }>('/api/v1/system/anchor-recommendations', { cache: 'no-store', suppressErrorDialog: true }) }
export function previewRecommendation(source: AnchorSource) { return request<RecommendationDraft>('/api/v1/system/anchor-recommendations/preview', { method: 'POST', body: JSON.stringify({ tenant_id: source.tenant_id, style_id: source.id }), suppressErrorDialog: true }) }
export function publishRecommendation(draft: RecommendationDraft) {
  return request<RecommendedAnchor>('/api/v1/system/anchor-recommendations', { method: 'POST', body: JSON.stringify({ tenant_id: draft.tenant_id, style_id: draft.style_id, name: draft.name, introduction: draft.introduction, summary: draft.summary, source_sha256: draft.source_sha256, generation: { provider: draft.provider, model: draft.model, raw_text: draft.raw_text, prompt_source: draft.prompt_source } }), suppressErrorDialog: true })
}
export function withdrawRecommendation(item: RecommendedAnchor) { return request(`/api/v1/system/anchor-recommendations/${item.id}/withdraw`, { method: 'POST', body: JSON.stringify({ version_no: item.version_no }), suppressErrorDialog: true }) }
export async function uploadAnchorAvatar(style: number, file: File, tenant?: number) {
  if (!['image/png', 'image/jpeg'].includes(file.type) || file.size > 10 * 1024 * 1024) throw new Error('请选择10MB以内的JPG或PNG头像')
  const bitmap = await createImageBitmap(file)
  let blob: Blob
  try {
    const scale = Math.min(1, 512 / Math.max(bitmap.width, bitmap.height)), canvas = document.createElement('canvas')
    canvas.width = Math.max(1, Math.round(bitmap.width * scale)); canvas.height = Math.max(1, Math.round(bitmap.height * scale))
    const ctx = canvas.getContext('2d'); if (!ctx) throw new Error('浏览器暂时无法处理头像，请重试')
    ctx.fillStyle = '#fff'; ctx.fillRect(0, 0, canvas.width, canvas.height); ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height)
    blob = await new Promise<Blob>((resolve, reject) => canvas.toBlob(value => value ? resolve(value) : reject(new Error('头像处理失败')), 'image/jpeg', 0.85))
  } finally { bitmap.close() }
  const form = new FormData(); form.append('avatar', blob, 'avatar.jpg')
  return request<LiveAnchorStyle>(`/api/v1/live-anchor-styles/${style}/avatar-image` + tenantQuery(tenant), { method: 'POST', body: form, suppressErrorDialog: true })
}
