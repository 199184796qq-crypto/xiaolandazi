import { request } from './api'

export const planVoiceRateOptions = Array.from({ length: 61 }, (_, index) => (80 + index) / 100)
export type PlanVoiceRate = { tenant_id: number; plan_id: number; rate: number }
export function normalizePlanVoiceRate(value: unknown) {
  const rate = Number(value)
  return planVoiceRateOptions.some(option => Math.abs(option - rate) <= 1e-9)
    ? planVoiceRateOptions.find(option => Math.abs(option - rate) <= 1e-9)!
    : 1
}
export function getPlanVoiceRate(planId: number, tenantId: number) {
  return request<PlanVoiceRate>(`/api/v1/live-agent-plans/${planId}/voice-rate?tenant_id=${tenantId}`, { cache: 'no-store', suppressErrorDialog: true })
}
export function setPlanVoiceRate(planId: number, tenantId: number, rate: number) {
  return request<PlanVoiceRate>(`/api/v1/live-agent-plans/${planId}/voice-rate?tenant_id=${tenantId}`, {
    method: 'PUT', body: JSON.stringify({ rate }), suppressErrorDialog: true,
  })
}
