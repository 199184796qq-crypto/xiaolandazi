export type TenantPlanSummary = {
  id: number
  status: string
  room_ids?: number[]
}

/** 返回方案绑定的第一个直播间；只接受 active 方案。 */
export function boundRoomIdForPlan(plans: TenantPlanSummary[], planId: number): number {
  const plan = plans.find((item) => item.id === planId && item.status === 'active')
  return Number(plan?.room_ids?.[0] || 0)
}
