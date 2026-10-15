import { onScopeDispose, ref, watch } from 'vue'
import { getAgentMemories } from './api'
import type { AgentMemoryItem } from './types'

export type AnswerStrategyScope = { tenantId: number; roomId: number; planId: number; actorKey: string }

export function memoryInPlan(item: AgentMemoryItem, scope: AnswerStrategyScope) {
  return scope.tenantId > 0 && scope.roomId > 0 && scope.planId > 0 && !!scope.actorKey &&
    item.tenant_id === scope.tenantId && item.room_id === scope.roomId && item.status === 'active' &&
    item.current_version?.structured?.review_plan_id === scope.planId &&
    (!item.memory_key.startsWith('plan:') || item.memory_key.startsWith(`plan:${scope.planId}:`))
}

export function usePlanAnswerStrategies(scope: () => AnswerStrategyScope, load = getAgentMemories) {
  const items = ref<AgentMemoryItem[]>([]), loading = ref(false), error = ref('')
  let sequence = 0
  async function refresh() {
    const captured = { ...scope() }, token = ++sequence
    const current = () => token === sequence && JSON.stringify(captured) === JSON.stringify(scope())
    if (!captured.tenantId || !captured.roomId || !captured.planId || !captured.actorKey) {
      items.value = []; loading.value = false; return
    }
    loading.value = true; error.value = ''
    try {
      const result = await load(captured.roomId, captured.planId)
      if (current()) items.value = (result.items || []).filter(item => memoryInPlan(item, captured))
    } catch (cause) {
      if (current()) { items.value = []; error.value = cause instanceof Error ? cause.message : '读取当前方案回答策略失败，请重试。' }
    } finally { if (current()) loading.value = false }
  }
  watch(() => JSON.stringify(scope()), () => {
    items.value = []; error.value = ''; void refresh()
  }, { immediate: true, flush: 'sync' })
  onScopeDispose(() => { sequence++ })
  return { items, loading, error, refresh }
}
