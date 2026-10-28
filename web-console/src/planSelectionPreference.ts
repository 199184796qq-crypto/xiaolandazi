export type PlanSelectionScope = { userId: number; tenantId: number; roomId: number; supportSession: boolean }
type PreferenceStorage = Pick<Storage, 'getItem' | 'setItem'>

export function planSelectionPreferenceKey(scope: PlanSelectionScope) {
  if (![scope.userId, scope.tenantId].every(id => Number.isSafeInteger(id) && id > 0)) return ''
  return `xiaolan-ui:${scope.userId}:live-strategy:selected-account-plan:${scope.supportSession ? 'support' : 'customer'}:${scope.tenantId}`
}

export function readSelectedPlan(scope: PlanSelectionScope, storage?: PreferenceStorage) {
  try {
    const key = planSelectionPreferenceKey(scope)
    const value = key ? Number((storage || window.localStorage).getItem(key)) : 0
    return Number.isSafeInteger(value) && value > 0 ? value : null
  } catch { return null }
}

export function rememberSelectedPlan(scope: PlanSelectionScope, planId: number | null, storage?: PreferenceStorage) {
  // Loading clears the selected ref temporarily; retain the previous preference.
  if (!planId || !Number.isSafeInteger(planId) || planId <= 0) return
  try {
    const key = planSelectionPreferenceKey(scope)
    if (key) (storage || window.localStorage).setItem(key, String(planId))
  } catch { /* Storage may be unavailable; selection still works for this visit. */ }
}

export function restoredPlanSelection(available: Set<number>, current: number | null, remembered: number | null, runtime: number, fallbacks: Array<number | undefined>, preserveCurrent = true) {
  return [preserveCurrent ? current : null, remembered, runtime, ...fallbacks].find(id => id != null && available.has(id)) || null
}
