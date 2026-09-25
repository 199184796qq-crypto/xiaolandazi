import type { InvitationRecord } from './types'
export interface FinanceInvitation extends InvitationRecord { confirmed_at?: string; confirmed_amount_cents: number; confirmation_receipt_id?: number }
export interface InvitationEarning { id:number; external_id:string; order_id:number; order_no:string; beneficiary_type:string; beneficiary_id:number; earning_type:string; amount_cents:number; currency:string; quota_seconds:number; status:string; program_version_id?:number; rule_id?:number; source_refund_id?:number; reversal_of_earning_id?:number; settlement_batch_no:string; settlement_status:string; created_at:string }
export interface FinanceInvitationPage<T> { items:T[]; total:number; page:number; page_size:number; total_pages:number }
async function read<T>(path:string,params:Record<string,string|number>,signal?:AbortSignal):Promise<FinanceInvitationPage<T>> {
 const query=new URLSearchParams(Object.entries(params).map(([k,v])=>[k,String(v)]))
 const response=await fetch(path+'?'+query,{credentials:'include',signal,headers:{Accept:'application/json'}})
 if(!response.ok){let message='读取邀请与推荐失败';try{const error=await response.json();if(typeof error.error==='string')message=error.error}catch{}throw new Error(message)}
 const data=await response.json()
 if(!Array.isArray(data.items)||!Number.isSafeInteger(data.total)||data.total<0||!Number.isSafeInteger(data.total_pages)||data.total_pages<1)throw new Error('邀请记录返回格式异常')
 return data
}
export function financeInvitations(params:Record<string,string|number>,signal?:AbortSignal){return read<FinanceInvitation>('/api/v1/staff/finance/invitations',params,signal)}
export function invitationEarnings(id:number,page:number,signal?:AbortSignal){return read<InvitationEarning>('/api/v1/staff/finance/invitations/'+id+'/earnings',{page,page_size:12},signal)}
