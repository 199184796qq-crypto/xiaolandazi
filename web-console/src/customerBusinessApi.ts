import { request } from './api'
import type { Page, Query } from './salesBusinessApi'
export function queryString(q: Query) { const p=new URLSearchParams();for(const [k,v] of Object.entries(q))if(v!==undefined&&v!=='')p.set(k,String(v));return '?'+p }
export interface BusinessAccount {tenant_id:number;name:string;username:string;qualified:boolean;created_at:string}
export interface Recognition {tenant_id:number;qualified:boolean;collection_status:string;confirmed_at?:string;confirmed_amount_cents:number;confirmation_receipt_id?:number;pending_count:number}
export interface ReceiptInput {tenant_id:number;channel:string;purpose:string;amount_cents:number;order_id?:number;verified_payment_id?:number;existing_recharge_id?:number;payer_name:string;receiving_account:string;external_trade_no:string;evidence:string;occurred_at:string;idempotency_key:string}
export interface Receipt extends ReceiptInput {id:number;receipt_no:string;customer_name:string;status:string;requester_user_id:number;last_submitter_user_id:number;review_note:string;posted_at?:string;reviewed_at?:string;version:number;created_at:string}
export interface MoneyEntry {entry_key:string;kind:string;reference_no:string;amount_cents:number;direction:string;channel:string;status:string;occurred_at:string;posted_at?:string;note:string}
export interface TicketInput {tenant_id:number;category:string;title:string;description:string;contact_name:string;contact_phone:string;preferred_at?:string;idempotency_key:string}
export interface Ticket extends TicketInput {id:number;ticket_no:string;customer_name:string;requester_user_id:number;requester_role:string;assigned_user_id?:number;assigned_name:string;status:string;resolution:string;confirmed_at?:string;version:number;created_at:string;updated_at:string}
export interface BusinessEvent {id:number;actor_name:string;action:string;note:string;created_at:string}
const base='/api/v1/customer-business'
export const businessAccounts=(q:Query,signal?:AbortSignal)=>request<Page<BusinessAccount>>(base+'/accounts'+queryString(q),{signal})
export const receiptList=(q:Query,signal?:AbortSignal)=>request<Page<Receipt>>(base+'/receipts'+queryString(q),{signal})
export const submitReceipt=(p:ReceiptInput)=>request<Receipt>(base+'/receipts',{method:'POST',body:JSON.stringify(p)})
export const reviewReceipt=(r:Receipt,action:string,note:string)=>request<Receipt>(base+'/receipts/'+r.id+'/review',{method:'POST',body:JSON.stringify({version:r.version,action,note,confirmed:true})})
export const resubmitReceipt=(r:Receipt,evidence:string)=>request<Receipt>(base+'/receipts/'+r.id+'/resubmit',{method:'POST',body:JSON.stringify({version:r.version,evidence})})
export const receiptTodos=(signal?:AbortSignal)=>request<Record<string,number>>(base+'/todos',{signal})
export const recognition=(tenant:number,signal?:AbortSignal)=>request<Recognition>(base+'/customers/'+tenant+'/recognition',{signal})
export const moneyList=(tenant:number,q:Query,signal?:AbortSignal)=>request<Page<MoneyEntry>>(base+'/customers/'+tenant+'/money'+queryString(q),{signal})
export const ticketList=(q:Query,signal?:AbortSignal)=>request<Page<Ticket>>('/api/v1/service/tickets'+queryString(q),{signal})
export const createTicket=(p:TicketInput)=>request<Ticket>('/api/v1/service/tickets',{method:'POST',body:JSON.stringify(p)})
export const ticketAction=(r:Ticket,action:string,note:string)=>request<Ticket>('/api/v1/service/tickets/'+r.id+'/actions',{method:'POST',body:JSON.stringify({version:r.version,action,note})})
export const businessEvents=(kind:'receipt'|'ticket',id:number,q:Query,signal?:AbortSignal)=>request<Page<BusinessEvent>>((kind==='receipt'?base+'/receipts/':'/api/v1/service/tickets/')+id+'/events'+queryString(q),{signal})
export const receiptLabels:Record<string,string>={awaiting_payment:'待收款确认',pending:'待财务审核',needs_info:'待补资料',rejected:'审核未通过',posting_failed:'入账异常',posted:'财务已入账',historical_posted:'原单已入账·待核实认定',simulated:'模拟支付·不计真实收款',paid:'已支付·认定以财务为准',failed:'失败',completed:'已完成',approved:'已审核',pending_approval:'待审核',cancelled:'已取消'}
export const channelLabels:Record<string,string>={bank_transfer:'转账',offline:'线下收款',platform:'平台支付',wallet:'钱包',sandbox:'模拟支付',manual:'人工记录',alipay:'支付宝',wechat:'微信支付',wxpay:'微信支付'}
export const purposeLabels:Record<string,string>={recharge:'充值入账',order:'订单收款',platform_payment:'已核验平台支付',existing_recharge:'关联既有充值（不重复加余额）'}
export const ticketLabels:Record<string,string>={pending:'待受理',accepted:'已接单',in_progress:'处理中',waiting_customer:'待客户反馈',awaiting_confirmation:'待确认完成',completed:'已完成',cancelled:'已取消'}
export const categoryLabels:Record<string,string>={onboarding:'开户 / 安装咨询',equipment:'设备问题',live_config:'直播配置',policy:'策略协助',training:'主播训练',voice:'声音协助',other:'其他问题'}
export const actionLabels:Record<string,string>={review_policy:'审核规则留痕',accept:'接单',start:'开始处理',wait_customer:'请求补充信息',resolve:'提交处理结果',reply:'补充回复',confirm:'确认已解决',cancel:'取消申请',reopen:'未解决 / 重新打开',created:'提交申请',submitted:'提交收款',resubmitted:'补充收款资料',posted:'审核入账',rejected:'审核未通过',needs_info:'要求补件'}
export const moneyKinds:Record<string,string>={receipt_recharge:'收款·充值',receipt_order:'收款·订单',receipt_platform_payment:'收款·平台支付',receipt_existing_recharge:'既有充值核实',recharge:'充值原单',order_payment:'订单支付',refund:'退款',wallet_order_payment:'钱包消费',wallet_reward:'奖励',wallet_refund:'钱包退款'}
export const money=(n:number)=>'¥'+(n/100).toLocaleString('zh-CN',{minimumFractionDigits:2,maximumFractionDigits:2})
export const businessTime=(s?:string)=>s?new Date(s).toLocaleString('zh-CN',{hour12:false}):'—'
export function parseMoney(value:string):number {if(!/^\d{1,9}(\.\d{1,2})?$/.test(value.trim()))throw new Error('金额最多两位小数，且必须大于零');const [a,b='']=value.trim().split('.');const n=Number(a)*100+Number(b.padEnd(2,'0'));if(!Number.isSafeInteger(n)||n<=0)throw new Error('金额必须大于零');return n}
