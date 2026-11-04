import { request } from './api'

export interface VoiceCleanupScope { tenant_id: number; plan_id: number; category: 'mainline' | 'interaction'; voice_key?: string }
export interface VoiceCleanupStamp { scope: VoiceCleanupScope; plan_epoch: number; voice_epoch: number }
export interface VoiceCleanupPreview {
  scope: VoiceCleanupScope; expected: VoiceCleanupStamp; plan_name: string; room_ids: number[]
  counts: Record<string, number>; physical_inventory_complete: boolean; applies_to: string; playback_policy: string
  bucket_version_state?: string; backup_state?:string
}
export interface VoiceCleanupJob {
  id: string; mode: string; scope: VoiceCleanupScope; state: string; scan_done: boolean; created_at: string; counts: Record<string, number>
}
export function listCleanupPlans(tenant: number) { return request<{ items: { id: number; name: string }[] }>('/api/v1/liveops/voice-assets/plans?tenant_id=' + tenant, { cache: 'no-store' }) }
export function previewVoiceCleanup(scope: VoiceCleanupScope,mode:'history'|'local_only'='history') { return request<VoiceCleanupPreview>('/api/v1/liveops/voice-assets/preview', { method: 'POST', body: JSON.stringify({...scope,mode}) }) }
export function clearVoiceCleanup(preview: VoiceCleanupPreview, id: string, mode: 'history' | 'local_only' = 'history') {
  return request<{ job: VoiceCleanupJob }>('/api/v1/liveops/voice-assets/clear', { method: 'POST', body: JSON.stringify({ scope: preview.scope, expected: preview.expected, request_id: id, mode }) })
}
export function listVoiceCleanupJobs(scope: VoiceCleanupScope) { return request<{ items: VoiceCleanupJob[] }>('/api/v1/liveops/voice-assets/jobs?tenant_id=' + scope.tenant_id + '&plan_id=' + scope.plan_id, { cache: 'no-store' }) }
export function voiceCleanupRequestID() { return Array.from(crypto.getRandomValues(new Uint8Array(32)), n => n.toString(16).padStart(2, '0')).join('') }

export function listCleanupVoices(scope: VoiceCleanupScope) { return request<{items:{key:string;label:string}[]}>('/api/v1/liveops/voice-assets/voices',{method:'POST',body:JSON.stringify(scope)}) }
export function scanVoicePolicy(scope: VoiceCleanupScope) { return request<{dry_run:boolean;counts:Record<string,number>}>('/api/v1/liveops/voice-assets/scan',{method:'POST',body:JSON.stringify(scope)}) }
