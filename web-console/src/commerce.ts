import { request } from './api'
import type { BeneficiaryWalletDashboard, WithdrawalRequest } from './types'
export interface CommerceRule {
 id?: number; head_id?: number; name: string; channel: 'sales'|'referral'; scope_type: 'default'|'category'|'product'|'campaign';
 product_type: string; target_id: number; mode: 'off'|'fixed'|'percent'|'tier'; amount_cents: number; rate_bps: number;
 minimum_paid_cents: number; cap_cents: number; pending_days: number; schedule_mode: 'immediate'|'monthly';
 release_day: number; month_lag: number; window_end_day: number; tiers: {minimum_paid_cents:number;rate_bps:number}[];
 status?: string; created_at?: string
}
export interface CommerceTarget { id: number; name: string; kind: string }
export interface CommerceEarning { id:number; order_no:string; product_name:string; rule_name:string; rule_version_id:number; amount_cents:number; status:string; available_at?:string; created_at:string }
export const getCommerceRules = () => request<{items:CommerceRule[];targets:CommerceTarget[]}>('/api/v1/finance/commerce-rules')
export const saveCommerceDraft = (value:CommerceRule) => request<{id:number}>('/api/v1/finance/commerce-rules/drafts',{method:'POST',body:JSON.stringify(value)})
export const publishCommerceRule = (id:number) => request('/api/v1/finance/commerce-rules/'+id+'/publish',{method:'POST',body:'{}'})
export const getSalesCommissionWallet = (period='') => request<BeneficiaryWalletDashboard & {earnings:CommerceEarning[]}>('/api/v1/sales/commission-wallet?period='+encodeURIComponent(period))
export const createSalesCommissionWithdrawal = (amount:number) => request<WithdrawalRequest>('/api/v1/sales/commission-withdrawals',{method:'POST',body:JSON.stringify({amount_cents:amount})})
export const getSalesWithdrawals = () => request<{items:WithdrawalRequest[]}>('/api/v1/finance/sales-withdrawals')
export const reviewSalesWithdrawal = (id:number,action:'approve'|'reject'|'pay',reason='') => request('/api/v1/finance/sales-withdrawals/'+id+'/'+action,{method:'POST',body:JSON.stringify({reason})})
export const money = (v:number) => '¥'+(v/100).toFixed(2)
export const commerceStatus = (v:string) => ({draft:'草稿',published:'已发布',pending:'待解冻',available:'可提现',reviewing:'待审核',approved:'待打款',paid:'已打款',rejected:'已驳回',reversed:'已冲回'}[v] || v)
export function rewardText(r:CommerceRule) {
 if(r.mode==='off') return '不参与'
 if(r.mode==='fixed') return '每个成交商品包 '+money(r.amount_cents)
 if(r.mode==='percent') return '成交实付 × '+(r.rate_bps/100)+'%'
 return r.tiers.map(t=>money(t.minimum_paid_cents)+'起 '+(t.rate_bps/100)+'%').join('；')
}
export const claimFreeMarketingOrder = (id:number) => request<{order:import('./types').CustomerShopOrder}>('/api/v1/shop/orders/'+id+'/free-claim',{method:'POST',body:'{}'})
