import { computed, onBeforeUnmount, ref, watch, type Ref } from 'vue'
import { request } from './api'

export interface PoolProduct { id: number; link: string; name: string }
export interface BenefitScope { benefit_id: number; all: boolean; products: number[]; version_no: number }
export interface PoolChange { id: string; area: string; label: string; products: number[]; before: string; after: string }
export interface PoolTask { id: string; disabled?: boolean; selling_point: string; angle: string; target_audience?: string; pain_point?: string; source_ids?: string[] }
export interface ContentCorrection { id: string; products: number[]; original: string; feedback: string; created_at: string; content: { 理解说明: string; 事实修正: string; 表达限制: string; 展开指导: string; 取消说明: string; 澄清问题: string } }
export interface PoolSourceRow { id: string; area: string; label: string; products: number[]; fields: Record<string, string> }
export interface PoolFact { id: string; text: string; sources: string[]; products: number[] }
export interface PoolSummary { key: string; label: string; owner: number; products: number[]; task_count: number; reused: boolean; tasks?: PoolTask[]; facts?: PoolFact[] }
export interface PoolStatus {
	corrections?: ContentCorrection[];
  state: 'needs_update' | 'updating' | 'failed' | 'ready' | 'synced'; changes: PoolChange[];
  products: PoolProduct[]; benefits: BenefitScope[]; pools: PoolSummary[]; set_id: number; saved_set_id: number;
  job: { status: string; stage: string; error?: string };
  history?: { id: number; created_at: string; changes: PoolChange[] }[];
  source_rows?: PoolSourceRow[]; current_source_rows?: PoolSourceRow[];
}
export function contentPoolURL(plan: number, room: number, tenant?: number) {
  const query = new URLSearchParams({ room_id: String(room) })
  if (tenant) query.set('tenant_id', String(tenant))
  return `/api/v1/live-agent-plans/${plan}/content-pools?${query}`
}
export function useContentPools(plan: Ref<number | null>, room: Ref<{ id: number; tenant_id?: number } | undefined>) {
  const status = ref<PoolStatus | null>(null), error = ref(''), submitting = ref(false), preview = ref(false)
  const url = computed(() => plan.value && room.value ? contentPoolURL(plan.value, room.value.id, room.value.tenant_id) : '')
  let epoch = 0, timer: ReturnType<typeof setTimeout> | undefined, controller: AbortController | undefined
  let busy = false, disposed = false
  function schedule() { if (!disposed && url.value) timer = setTimeout(() => void refresh(), 5000) }
  async function refresh(force = false) {
    if ((busy && !force) || !url.value || disposed) return
    if (timer) clearTimeout(timer)
    const owner = ++epoch, target = url.value
    controller?.abort(); controller = new AbortController(); busy = true
    try {
      const next = await request<PoolStatus>(target + (preview.value ? '&preview=1' : ''), { signal: controller.signal, suppressErrorDialog: true })
      if (owner === epoch && url.value === target) { status.value = next; error.value = '' }
    } catch (err) {
      if (owner === epoch && url.value === target && !(err instanceof Error && err.name === 'AbortError')) error.value = err instanceof Error ? err.message : '待更新内容读取失败'
    } finally { if (owner === epoch) { busy = false; schedule() } }
  }
  async function update() {
    if (!url.value || submitting.value || status.value?.state === 'updating') return
    const target = url.value, owner = epoch
    submitting.value = true; error.value = ''
    try {
      const next = await request<PoolStatus>(target.replace('/content-pools?', '/content-pools/update?'), { method: 'POST', suppressErrorDialog: true })
      if (url.value === target && owner === epoch) status.value = { ...(status.value || {}), ...next } as PoolStatus
    } catch (err) { if (url.value === target) error.value = err instanceof Error ? err.message : '更新未完成，请重试' }
    finally { await refresh(true); submitting.value = false }
  }
  watch(url, () => { epoch++; controller?.abort(); busy = false; status.value = null; error.value = ''; preview.value = false; if (timer) clearTimeout(timer); void refresh() }, { immediate: true })
  watch(preview, () => void refresh())
  onBeforeUnmount(() => { disposed = true; epoch++; controller?.abort(); if (timer) clearTimeout(timer) })
  function scopeSaved(scope: BenefitScope) { if (status.value) status.value.benefits = status.value.benefits.map(row => row.benefit_id === scope.benefit_id ? scope : row); void refresh(true) }
  function acceptEdited(next: PoolStatus) { epoch++; controller?.abort(); busy = false; status.value = next; error.value = ''; void refresh(true) }
  return { status, error, submitting, preview, refresh, update, scopeSaved, acceptEdited }
}
