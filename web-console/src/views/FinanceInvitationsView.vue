<script setup lang="ts">
import {computed,onBeforeUnmount,onMounted,ref,watch} from 'vue'
import {RouterLink} from 'vue-router'
import {session} from '../session'
import ModulePageNav from '../components/ModulePageNav.vue'
import DataListControls from '../components/DataListControls.vue'
import PaginationBar from '../components/PaginationBar.vue'
import BusinessDialog from '../components/BusinessDialog.vue'
import {financeInvitations,invitationEarnings,type FinanceInvitation,type InvitationEarning} from '../financeInvitationsApi'
import {businessTime,money} from '../customerBusinessApi'
import '../salesBusiness.css'
const items=ref<FinanceInvitation[]>([]),loading=ref(false),error=ref(''),search=ref(''),status=ref('all'),page=ref(1),size=ref(12),pages=ref(1),total=ref(0),view=ref<'table'|'card'>('table')
const states=[{value:'all',label:'全部认定状态'},{value:'confirmed',label:'财务已确认'},{value:'unconfirmed',label:'待财务确认'}]
const access=computed(()=>session.bootstrap?.staff_access)
const has=(p:string)=>session.bootstrap?.actor.role==='platform_admin'||access.value?.is_super_admin||access.value?.permissions.includes(p)
const canReward=computed(()=>has('finance.reward.grant')||has('finance.reward.approve'))
let request:AbortController|undefined,delay:ReturnType<typeof setTimeout>|undefined,detailRequest:AbortController|undefined
const target=ref<FinanceInvitation|null>(null),earnings=ref<InvitationEarning[]>([]),earningPage=ref(1),earningPages=ref(1),earningTotal=ref(0),detailLoading=ref(false),detailError=ref('')
function source(v:string){return ({platform_invite:'系统邀请',sales_invite:'销售邀请',agent_invite:'代理邀请',referral:'终端推荐'} as Record<string,string>)[v]||v}
function earningType(v:string){return ({referral_reward:'推荐奖励',sales_commission:'销售提成',agent_settlement:'代理结算',reversal:'冲回'} as Record<string,string>)[v]||v}
function beneficiary(v:string){return ({sales_staff:'销售员工',agent:'代理',customer:'客户',user:'用户'} as Record<string,string>)[v]||v}
function state(v:string){return ({pending:'待生效',available:'可结算',settling:'结算中',paid:'已支付',reversed:'已冲回',cancelled:'已取消',reviewing:'待审核',approved:'已审核',rejected:'已驳回'} as Record<string,string>)[v]||v||'未进入结算批次'}
async function load(){request?.abort();const c=new AbortController();request=c;loading.value=true;error.value='';try{const r=await financeInvitations({search:search.value,status:status.value,page:page.value,page_size:size.value},c.signal);if(c.signal.aborted)return;items.value=r.items;total.value=r.total;pages.value=r.total_pages;if(page.value>pages.value)page.value=pages.value}catch(e){if(!c.signal.aborted)error.value=e instanceof Error?e.message:'读取失败'}finally{if(request===c)loading.value=false}}
function first(){if(page.value===1)void load();else page.value=1}
watch([status,size],first);watch(page,load);watch(search,()=>{clearTimeout(delay);delay=setTimeout(first,300)})
async function loadEvidence(){detailRequest?.abort();earnings.value=[];detailError.value='';if(!target.value)return;const c=new AbortController();detailRequest=c;detailLoading.value=true;try{const r=await invitationEarnings(target.value.id,earningPage.value,c.signal);if(c.signal.aborted)return;earnings.value=r.items;earningTotal.value=r.total;earningPages.value=r.total_pages;if(earningPage.value>r.total_pages)earningPage.value=r.total_pages}catch(e){if(!c.signal.aborted)detailError.value=e instanceof Error?e.message:'读取失败'}finally{if(detailRequest===c)detailLoading.value=false}}
function evidence(item:FinanceInvitation){target.value=item;earningPage.value=1;void loadEvidence()}
function close(){detailRequest?.abort();target.value=null;earnings.value=[];detailError.value=''}
watch(earningPage,loadEvidence);onMounted(load);onBeforeUnmount(()=>{request?.abort();detailRequest?.abort();clearTimeout(delay)})
</script>
<template>
<div class="management-page sales-business finance-invitations-page">
 <ModulePageNav hub="finance" active-title="邀请与推荐"/>
 <header class="sb-toolbar"><h2>邀请与推荐</h2><div class="sb-actions"><RouterLink v-if="canReward" to="/staff/finance/approvals?type=reward">奖励审批</RouterLink><RouterLink to="/staff/finance/settlements">收益结算</RouterLink><button :disabled="loading" @click="load">刷新</button></div></header>
 <section class="sb-panel">
  <DataListControls v-model:view-mode="view" v-model:search="search" v-model:status="status" v-model:page-size="size" :status-options="states" search-placeholder="搜索客户、邀请人或邀请码"/>
  <p v-if="error" class="sb-error" role="alert">{{error}} <button @click="load">重试</button></p><p v-else-if="loading" class="sb-empty">正在读取邀请记录…</p><p v-else-if="!items.length" class="sb-empty">暂无符合条件的邀请记录</p>
  <template v-else>
   <div v-if="view==='table'" class="sb-table"><table><thead><tr><th>被邀请客户</th><th>邀请人 / 来源</th><th>邀请码</th><th>财务认定</th><th>首次确认收款</th><th>注册时间</th><th>核对</th></tr></thead><tbody><tr v-for="item in items" :key="item.id"><td><strong>{{item.referred_display_name}}</strong><small>@{{item.referred_username}}</small></td><td><strong>{{item.inviter_display_name}}</strong><small>{{source(item.source_type)}} · @{{item.inviter_username}}</small></td><td>{{item.invite_code}}</td><td><span class="sb-badge">{{item.confirmed_at?'财务已确认':'待财务确认'}}</span><small v-if="item.confirmed_at">{{businessTime(item.confirmed_at)}}</small></td><td>{{item.confirmed_at?money(item.confirmed_amount_cents):'—'}}</td><td>{{businessTime(item.bound_at)}}</td><td><div class="sb-actions"><RouterLink :to="'/staff/finance/receipts?tenant='+item.referred_tenant_id">收款记录</RouterLink><RouterLink :to="'/staff/finance/customers/'+item.referred_tenant_id+'/money'">资金记录</RouterLink><button @click="evidence(item)">奖励依据</button></div></td></tr></tbody></table></div>
   <div v-else class="sb-grid"><article v-for="item in items" :key="item.id" class="sb-record"><header><h3>{{item.referred_display_name}}</h3><span class="sb-badge">{{item.confirmed_at?'财务已确认':'待财务确认'}}</span></header><p>邀请人：{{item.inviter_display_name}}</p><small>{{source(item.source_type)}} · {{item.invite_code}}</small><p>首次确认收款：{{item.confirmed_at?money(item.confirmed_amount_cents):'—'}}</p><small>{{businessTime(item.bound_at)}}</small><div class="sb-actions"><RouterLink :to="'/staff/finance/receipts?tenant='+item.referred_tenant_id">收款记录</RouterLink><RouterLink :to="'/staff/finance/customers/'+item.referred_tenant_id+'/money'">资金记录</RouterLink><button @click="evidence(item)">奖励依据</button></div></article></div>
  </template>
  <PaginationBar v-if="!error&&!loading" v-model:page="page" :total="total" :total-pages="pages" :page-size="size"/>
 </section>
 <BusinessDialog v-if="target" title="关联奖励与提成" @close="close"><div class="finance-referral-evidence"><h3>{{target.referred_display_name}}</h3><p>邀请人：{{target.inviter_display_name}} · {{source(target.source_type)}}</p><p class="muted">以下为该客户订单产生的已有收益记录，实际受益人以每笔记录为准。</p><p v-if="detailLoading">正在核对奖励依据…</p><p v-else-if="detailError" class="sb-error">{{detailError}} <button @click="loadEvidence">重试</button></p><p v-else-if="!earnings.length" class="sb-empty">该客户订单暂无已生成的奖励或提成记录。</p><div v-else class="sb-grid"><article v-for="e in earnings" :key="e.id" class="sb-record"><header><h3>{{earningType(e.earning_type)}}</h3><span class="sb-badge">{{state(e.status)}}</span></header><p>{{e.currency}} {{(e.amount_cents/100).toFixed(2)}}<span v-if="e.quota_seconds"> · {{e.quota_seconds}} 秒</span></p><small>{{beneficiary(e.beneficiary_type)}} #{{e.beneficiary_id}}</small><p>订单 {{e.order_no}}<br/>规则版本 {{e.program_version_id||'—'}} · 规则 {{e.rule_id||'—'}}</p><small v-if="e.source_refund_id">退款单 #{{e.source_refund_id}}</small><small v-if="e.reversal_of_earning_id">冲回原收益 #{{e.reversal_of_earning_id}}</small><small>{{e.settlement_batch_no||'尚无结算批次'}} · {{state(e.settlement_status)}}</small><small>{{businessTime(e.created_at)}}</small></article></div><PaginationBar v-if="!detailError&&!detailLoading" v-model:page="earningPage" :total="earningTotal" :total-pages="earningPages" :page-size="12"/></div></BusinessDialog>
</div>
</template>
<style scoped>
.finance-invitations-page{font-size:18px;--table-font-size:18px}.finance-invitations-page h2{font-size:24px}.finance-invitations-page h3{font-size:18px}.finance-invitations-page :deep(button),.finance-invitations-page :deep(select),.finance-invitations-page :deep(input){font-size:18px}.finance-invitations-page .sb-actions{margin-top:0;gap:8px}.finance-invitations-page .sb-actions a{display:inline-flex;align-items:center;min-height:40px;padding:6px 10px;border:1px solid #d5e1f5;border-radius:10px;color:#326bd8;background:#f8fbff;text-decoration:none;box-sizing:border-box}.finance-invitations-page .sb-actions a:hover,.finance-invitations-page .sb-actions a:focus-visible{outline:none;box-shadow:0 0 0 3px #4285ff22;border-color:#78aaff}.finance-invitations-page .sb-table td{vertical-align:top}.finance-invitations-page small{display:block;font-size:16px;line-height:1.5;margin-top:5px}.finance-referral-evidence{font-size:18px}.finance-referral-evidence .sb-record{display:flex;flex-direction:column;gap:6px}.finance-referral-evidence .sb-record p{margin:6px 0}.finance-referral-evidence .sb-record small:last-child{margin-top:auto;padding-top:12px}
</style>
