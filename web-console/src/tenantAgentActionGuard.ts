export type TenantActionLike = {
  payload?: {
    plan_id?: unknown
    room_id?: unknown
  } | null
}

/**
 * 租户动作卡只能在生成它的同一方案、同一房间执行。
 * 任何一边被切换，旧卡都必须失效，避免把 A 方案的动作写到 B 方案。
 */
export function tenantActionContextMatches(
  action: TenantActionLike | undefined | null,
  currentPlanId: number,
  currentRoomId: number,
): boolean {
  const planId = Number(action?.payload?.plan_id || 0)
  const roomId = Number(action?.payload?.room_id || 0)
  if (!planId || !roomId) return false
  return planId === Number(currentPlanId || 0) && roomId === Number(currentRoomId || 0)
}
