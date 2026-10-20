import { onScopeDispose, ref, watch } from 'vue'
import { getPlanVoiceRate, normalizePlanVoiceRate, setPlanVoiceRate, type PlanVoiceRate } from './planVoiceRate'

export function usePlanVoiceRate(scope: () => { planId: number; tenantId: number; key: string; allowed?: boolean }) {
  const rate = ref(1), saved = ref(1), busy = ref(false), loading = ref(false), ready = ref(false), error = ref('')
  let epoch = 0, readSequence = 0, disposed = false
  const valid = () => { const s = scope(); return s.allowed !== false && s.planId > 0 && s.tenantId > 0 }
  function validate(result: PlanVoiceRate, planId: number, tenantId: number) {
    if (result.plan_id !== planId || result.tenant_id !== tenantId || !Number.isFinite(result.rate) || normalizePlanVoiceRate(result.rate) !== result.rate) throw new Error('方案语速回执不匹配，请重新读取')
  }
  function accept(result: PlanVoiceRate, planId: number, tenantId: number) {
    validate(result, planId, tenantId)
    rate.value = saved.value = result.rate
    ready.value = true
    error.value = ''
  }
  async function load() {
    if (!valid() || busy.value) return
    const s = scope(), token = epoch, sequence = ++readSequence
    const current = () => !disposed && epoch === token && readSequence === sequence
    loading.value = true
    error.value = ''
    try {
      const result = await getPlanVoiceRate(s.planId, s.tenantId)
      if (current()) accept(result, s.planId, s.tenantId)
    } catch (err) {
      if (current()) { ready.value = false; error.value = err instanceof Error ? err.message : '读取方案语速失败' }
    } finally { if (current()) loading.value = false }
  }
  async function save(value: number) {
    if (!valid() || !ready.value || loading.value || busy.value) return
    if (!Number.isFinite(value) || normalizePlanVoiceRate(value) !== value) { rate.value = saved.value; error.value = '请选择有效语速'; return }
    if (value === saved.value) { rate.value = saved.value; return }
    const s = scope(), token = epoch
    ++readSequence
    busy.value = true
    error.value = ''
    const current = () => !disposed && epoch === token
    try {
      const result = await setPlanVoiceRate(s.planId, s.tenantId, value)
      validate(result, s.planId, s.tenantId)
      if (current()) accept(result, s.planId, s.tenantId)
      window.dispatchEvent(new CustomEvent('live-plan-voice-rate-updated', { detail: result }))
    } catch (err) {
      if (current()) { rate.value = saved.value; error.value = err instanceof Error ? err.message : '语速保存失败，请重试' }
    } finally { if (current()) busy.value = false }
  }
  function updated(event: Event) {
    const detail = (event as CustomEvent<PlanVoiceRate>).detail, s = scope()
    if (detail?.plan_id === s.planId && detail.tenant_id === s.tenantId && !busy.value) void load()
  }
  watch(() => { const s = scope(); return [s.planId, s.tenantId, s.key, s.allowed] as const }, () => {
    ++epoch; ++readSequence
    rate.value = saved.value = 1
    busy.value = loading.value = ready.value = false
    error.value = ''
    void load()
  }, { immediate: true, flush: 'sync' })
  if (typeof window !== 'undefined') window.addEventListener('live-plan-voice-rate-updated', updated)
  onScopeDispose(() => { disposed = true; ++epoch; ++readSequence; if (typeof window !== 'undefined') window.removeEventListener('live-plan-voice-rate-updated', updated) })
  return { rate, saved, busy, loading, ready, error, load, save }
}
