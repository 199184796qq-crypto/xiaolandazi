import { request } from './api'
import type { Page, Query } from './salesBusinessApi'

export interface SalesCustomer {
  qualified: boolean
  collection_status: string
  confirmed_at?: string
  user_id: number
  tenant_id: number
  username: string
  display_name: string
  phone: string
  email: string
  province: string
  city: string
  district: string
  address: string
  status: string
  source_type: string
  created_at: string
}
export interface CustomerSummary {
  qualified_count: number
  unconfirmed_count: number
  pending_receipt_count: number
  total_count: number
  active_count: number
  disabled_count: number
}
export function getSalesCustomerPage(query: Query, signal?: AbortSignal) {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== '') params.set(key, String(value))
  }
  return request<Page<SalesCustomer> & { summary: CustomerSummary }>(
    '/api/v1/sales/customers/page?' + params.toString(), { signal },
  )
}
export const customerSourceLabels: Record<string, string> = {
  sales_lead: '意向顾客转入', sales_invite: '销售邀请注册',
  platform_invite: '系统邀请注册', referral: '客户推荐',
  direct: '自主注册', unknown: '未记录来源',
}
export function customerSourceLabel(source: string) {
  return customerSourceLabels[source] || '其他来源'
}
export function customerStatusLabel(status: string) {
  return ({ active: '启用', disabled: '已停用', pending: '待启用', suspended: '已暂停', locked: '已锁定' } as Record<string, string>)[status] || '其他状态'
}
