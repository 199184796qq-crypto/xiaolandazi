import { onScopeDispose, ref, watch } from 'vue'
import { factGapCheck, type FactGapItem } from './api'
import type { ProductSupplementScope } from './productSupplementChat'

export function useProductSupplementGaps(scope: () => ProductSupplementScope & { productLinkId?: number }, check = factGapCheck) {
  const gaps = ref<FactGapItem[]>([])
  const loading = ref(false)
  const error = ref('')
  const skipped = ref<string[]>([])
  let epoch = 0
  let controller: AbortController | undefined
  async function reload() {
    controller?.abort()
    const token = ++epoch, captured = { ...scope() }
    if (captured.planId <= 0 || !captured.actorKey) { loading.value = false; return }
    const current = () => token === epoch && JSON.stringify(captured) === JSON.stringify(scope())
    controller = new AbortController()
    loading.value = true; error.value = ''
    try {
      const result = await check(captured.planId, { tenant_id: captured.tenantId, product_link_id: captured.productLinkId }, controller.signal)
      if (current()) { gaps.value = result.gaps || []; skipped.value = [] }
    } catch (cause) {
      if (current()) error.value = cause instanceof Error ? cause.message : '读取补全清单失败'
    } finally { if (current()) loading.value = false }
  }
  watch(() => JSON.stringify(scope()), () => {
    gaps.value = []; skipped.value = []; error.value = ''
    void reload()
  }, { immediate: true, flush: 'sync' })
  onScopeDispose(() => { epoch++; controller?.abort() })
  return { gaps, loading, error, skipped, reload }
}
