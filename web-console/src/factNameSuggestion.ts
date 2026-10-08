import { onScopeDispose, ref, watch, type Ref } from 'vue'
import { request } from './api'

type FactDraft = { key: string; value: string; category: string }

// Separate UI state from the draft: suggestions never replace fact content.
export function useFactNameSuggestion(draft: Ref<FactDraft>, enabled: Ref<boolean>, scope: () => { planId: number | null; tenantId?: number }) {
  const loading = ref(false)
  const error = ref('')
  let manual = false, revision = 0, autoName = '', namedFor = ''
  let timer: ReturnType<typeof setTimeout> | undefined
  let controller: AbortController | undefined
  let pending: Promise<string> | undefined
  let pendingFor = ''
  const identity = () => JSON.stringify([scope().planId, scope().tenantId, draft.value.category, draft.value.value.trim()])
  function invalidate() {
    revision++
    clearTimeout(timer)
    controller?.abort()
    controller = undefined
    pending = undefined
    pendingFor = ''
    loading.value = false
    error.value = ''
  }
  function reset() {
    invalidate()
    manual = false
    autoName = ''
    namedFor = ''
  }
  function edited() {
    invalidate()
    manual = !!draft.value.key.trim()
    autoName = ''
    namedFor = ''
    if (!manual) schedule()
  }
  function schedule() {
    if (!enabled.value || manual || !draft.value.value.trim() || !scope().planId) return
    timer = setTimeout(() => { void ensure() }, 1000)
  }
  async function ensure(): Promise<string> {
    clearTimeout(timer)
    if (!enabled.value || !draft.value.value.trim() || !scope().planId) return ''
    if (manual || (draft.value.key.trim() && draft.value.key !== autoName)) return draft.value.key.trim()
    const key = identity()
    if (namedFor === key && draft.value.key.trim()) return draft.value.key.trim()
    if (pending && pendingFor === key) return pending
    invalidate()
    const token = revision, value = draft.value.value.trim(), category = draft.value.category, target = draft.value
    const { planId, tenantId } = scope()
    controller = new AbortController()
    const signal = controller.signal
    loading.value = true
    pendingFor = key
    pending = (async () => {
      try {
        const result = await request<{ name: string }>(`/api/v1/live-agent-plans/${planId}/fact-name-suggestion`, {
          method: 'POST', body: JSON.stringify({ tenant_id: tenantId, category, content: value }), signal,
        })
        if (token !== revision || !enabled.value || target !== draft.value || identity() !== key || manual) return ''
        if (typeof result.name !== 'string' || !result.name.trim() || [...result.name.trim()].length > 24) throw new Error('名称提炼失败，请手动填写')
        autoName = result.name.trim()
        namedFor = key
        draft.value.key = autoName
        return autoName
      } catch (err) {
        if (token === revision && !signal.aborted) error.value = err instanceof Error ? err.message : '名称提炼失败，请手动填写'
        return ''
      } finally {
        if (token === revision) { loading.value = false; pending = undefined; pendingFor = '' }
      }
    })()
    return pending
  }
  watch([() => draft.value, enabled, () => scope().planId, () => scope().tenantId], () => { reset(); schedule() }, { flush: 'sync' })
  watch([() => draft.value.value, () => draft.value.category], () => { invalidate(); schedule() }, { flush: 'sync' })
  onScopeDispose(invalidate)
  return { loading, error, ensure, edited, reset }
}
