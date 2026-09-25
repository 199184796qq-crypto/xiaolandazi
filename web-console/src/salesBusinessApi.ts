import { request } from './api'
import type { InitialCredential } from './types'

export interface Page<T> { items: T[]; total: number; page: number; page_size: number; total_pages: number }
export interface LeadInput {
 business_name: string; contact_name: string; phone: string; wechat: string; email: string;
 industry_name: string; province: string; city: string; district: string; address: string;
 source_type: string; stage: string; planned_visit_at?: string; next_followup_at?: string;
}
export interface Lead extends LeadInput {
 id: number; lead_no: string; owner_sales_staff_id: number; status: 'open'|'won'|'lost'|'registered';
 lost_reason: string; converted_tenant_id?: number; converted_user_id?: number;
 converted_at?: string; latest_activity_at?: string; created_at: string; updated_at: string;
}
export interface LeadSummary { total_count: number; open_count: number; visit_due_count: number; followup_due_count: number; won_count: number; lost_count: number }
export interface ActivityInput {activity_type: string; outcome: string; content: string; stage: string; occurred_at?: string; next_followup_at?: string}
export interface Activity extends ActivityInput { id: number; lead_id: number; sales_staff_id: number; sales_display_name: string; occurred_at: string; created_at: string }
export interface Conversion { username: string; display_name: string; phone: string; email: string; province: string; city: string; district: string; address: string; delivery_method: 'copy'|'email'; handoff_summary: string }
export interface Handoff {id: number; handoff_no: string; lead_id?: number; tenant_id: number; customer_user_id: number; customer_username: string; customer_name: string; customer_phone: string; sales_display_name: string; status: string; summary: string; accepted_by_user_id?: number; accepted_by_name: string; accepted_at?: string; completed_by_name: string; completed_at?: string; created_at: string}
export interface HandoffEvent {id: number; operator_name: string; status: string; content: string; created_at: string}
export interface SalesPerson {staff_id: number; user_id: number; display_name: string; username: string; status: string; customer_count: number; lead_count: number}
export interface HandoverPreview {from_sales_staff_id: number; from_display_name: string; customer_count: number; open_lead_count: number}
export interface Handover {id: number; handover_no: string; from_display_name: string; to_display_name: string; customer_count: number; lead_count: number; reason: string; status: string; created_by_user_id: number; created_at: string}
export type Query = Record<string, string|number|undefined>
function qs(q: Query) { const p = new URLSearchParams(); for (const [k,v] of Object.entries(q)) if (v !== undefined && v !== '') p.set(k,String(v)); return '?' + p.toString() }
export const listLeads = (q: Query, signal?: AbortSignal) => request<Page<Lead> & {summary: LeadSummary}>('/api/v1/sales/leads'+qs(q), {signal})
export const getLead = (id: number) => request<Lead>('/api/v1/sales/leads/'+id)
export const saveLead = (p: LeadInput, id?: number) => request<Lead>('/api/v1/sales/leads'+(id ? '/'+id : ''), {method: id ? 'PUT':'POST', body: JSON.stringify(p)})
export const listActivities = (id: number, q: Query, signal?: AbortSignal) => request<Page<Activity>>('/api/v1/sales/leads/'+id+'/activities'+qs(q),{signal})
export const addActivity = (id: number,p: ActivityInput) => request<Activity>('/api/v1/sales/leads/'+id+'/activities',{method:'POST',body:JSON.stringify(p)})
export const loseLead = (id: number,reason: string) => request<Lead>('/api/v1/sales/leads/'+id+'/lost',{method:'POST',body:JSON.stringify({reason})})
export const convertLead = (id: number,p: Conversion) => request<{lead: Lead; customer: {username: string; display_name: string}; credential: InitialCredential; handoff: Handoff|null}>('/api/v1/sales/leads/'+id+'/convert',{method:'POST',body:JSON.stringify(p)})
export const listHandoffs = (q: Query,signal?: AbortSignal) => request<Page<Handoff> & {manager_view: boolean}>('/api/v1/liveops/customer-handoffs'+qs(q),{signal})
export const updateHandoff = (id: number,status: string,note: string) => request<Handoff>('/api/v1/liveops/customer-handoffs/'+id,{method:'PATCH',body:JSON.stringify({status,note})})
export const listHandoffEvents = (id: number,q: Query) => request<Page<HandoffEvent>>('/api/v1/liveops/customer-handoffs/'+id+'/events'+qs(q))
export const listHandoverPeople = (q: Query,signal?: AbortSignal) => request<Page<SalesPerson>>('/api/v1/admin/sales/handover-people'+qs(q),{signal})
export const getHandoverPreview = (id: number) => request<HandoverPreview>('/api/v1/admin/sales/'+id+'/handover-preview')
export const transferPortfolio = (id: number,to: number,reason: string,preview: HandoverPreview) => request<Handover>('/api/v1/admin/sales/'+id+'/handover',{method:'POST',body:JSON.stringify({to_sales_staff_id:to,reason,expected_customer_count:preview.customer_count,expected_lead_count:preview.open_lead_count})})
export const listHandoverHistory = (q: Query) => request<Page<Handover>>('/api/v1/admin/sales/handovers'+qs(q))
export const stageLabels: Record<string,string> = {new:'新顾客',visit_planned:'待拜访',contacted:'已联系',interested:'有意向',quoted:'已报价',trial:'试用中',negotiation:'洽谈中',registered:'已开户·待财务确认',won:'已开户（认定看财务）',lost:'未成交'}
export const activityLabels: Record<string,string> = {phone:'电话',wechat:'微信',visit:'拜访',message:'消息',note:'备注',created:'建立顾客',updated:'修改资料',close:'结束跟进',conversion:'转正式客户',handover:'责任人交接'}
export const handoffLabels: Record<string,string> = {pending:'待接单',accepted:'已接单',in_progress:'处理中',completed:'已完成'}
export function formatSalesTime(v?: string) {return v ? new Date(v).toLocaleString('zh-CN',{hour12:false}) : '未安排'}
export function localInputTime(v?: string) {if (!v) return ''; const d=new Date(v); if (!Number.isFinite(d.getTime())) return ''; return new Date(d.getTime()-d.getTimezoneOffset()*60000).toISOString().slice(0,16)}
export function isoInput(v: string) {return v ? new Date(v).toISOString() : undefined}
