import { computed, onBeforeUnmount, ref, watch, type Ref } from 'vue'
import { request } from './api'

export interface FinanceReviewPolicy { require_distinct_reviewer: boolean }
export const financeReviewSettingKey = 'finance_require_distinct_reviewer'

// This value controls display only. Every decision rechecks the stored policy on the server.
export function useFinanceReviewPolicy(enabled: Ref<boolean>) {
  const policy = ref<FinanceReviewPolicy | null>(null)
  const policyError = ref('')
  const policyLoading = ref(false)
  let controller: AbortController | undefined
  let timer: ReturnType<typeof setInterval> | undefined
  let disposed = false
  async function refreshPolicy() {
    if (!enabled.value || disposed) return
    controller?.abort()
    const current = new AbortController()
    controller = current
    policyLoading.value = true
    try {
      const result = await request<FinanceReviewPolicy>('/api/v1/staff/finance/review-policy', { signal: current.signal })
      if (current.signal.aborted || disposed) return
      if (typeof result.require_distinct_reviewer !== 'boolean') throw new Error('财务审核配置返回异常')
      policy.value = result
      policyError.value = ''
    } catch (e) {
      if (!current.signal.aborted && !disposed) {
        policy.value = null
        policyError.value = e instanceof Error ? e.message : '无法读取财务审核配置'
      }
    } finally {
      if (controller === current) policyLoading.value = false
    }
  }
  const visibleRefresh = () => { if (!document.hidden) void refreshPolicy() }
  watch(enabled, (active) => {
    controller?.abort()
    clearInterval(timer)
    policy.value = null
    policyError.value = ''
    if (active) {
      void refreshPolicy()
      timer = setInterval(visibleRefresh, 15000)
    }
  }, { immediate: true })
  window.addEventListener('focus', visibleRefresh)
  window.addEventListener('system-config-updated', visibleRefresh)
  onBeforeUnmount(() => {
    disposed = true
    controller?.abort()
    clearInterval(timer)
    window.removeEventListener('focus', visibleRefresh)
    window.removeEventListener('system-config-updated', visibleRefresh)
  })
  return {
    policy, policyError, policyLoading, refreshPolicy,
    policyReady: computed(() => policy.value !== null),
    requireDistinctReviewer: computed(() => policy.value?.require_distinct_reviewer !== false),
  }
}
