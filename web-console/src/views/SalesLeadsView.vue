<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import ModulePageNav from '../components/ModulePageNav.vue'
import DataListControls from '../components/DataListControls.vue'
import PaginationBar from '../components/PaginationBar.vue'
import RegionSelect from '../components/RegionSelect.vue'
import CredentialResultModal from '../components/CredentialResultModal.vue'
import type { InitialCredential } from '../types'
import { listLeads, getLead, saveLead, listActivities, addActivity, loseLead, convertLead, stageLabels, activityLabels, localInputTime, isoInput, formatSalesTime as time } from '../salesBusinessApi'
import type { Lead, LeadInput, Activity, Conversion } from '../salesBusinessApi'
import '../salesBusiness.css'

const items=ref<Lead[]>([])
const loading=ref(false), error=ref(''), notice=ref('')
const search=ref(''), status=ref('open'), stage=ref('all'), sort=ref('updated-desc')
const page=ref(1), pageSize=ref(12), total=ref(0), pages=ref(1), view=ref<'card'|'table'>('table')
const statusOptions=[{label:'全部结果',value:'all'},{label:'跟进中',value:'open'},{label:'已开户·待财务确认',value:'registered'},{label:'已转入客户档案',value:'won'},{label:'未成交',value:'lost'}]
const stages=Object.entries(stageLabels).filter(([key])=>key!=='won'&&key!=='lost'&&key!=='registered')
const sorts=[{label:'最近更新',value:'updated-desc'},{label:'最新录入',value:'created-desc'},{label:'最早录入',value:'created-asc'},{label:'拜访时间优先',value:'visit-asc'},{label:'跟进时间优先',value:'followup-asc'},{label:'顾客名称',value:'name-asc'}]
let controller: AbortController|undefined, searchTimer: ReturnType<typeof setTimeout>|undefined
async function load(){
 controller?.abort();const current=new AbortController();controller=current;loading.value=true;error.value=''
 try{const r=await listLeads({search:search.value,status:status.value,stage:stage.value,sort:sort.value,page:page.value,page_size:pageSize.value},current.signal)
  if(current.signal.aborted)return
  items.value=r.items;total.value=r.total;pages.value=r.total_pages
  if(page.value>pages.value)page.value=pages.value
 }catch(e){if(!current.signal.aborted)error.value=e instanceof Error?e.message:'读取失败'}
 finally{if(controller===current)loading.value=false}
}
function resetPage(){if(page.value!==1)page.value=1;else void load()}
watch(page,load);watch([status,stage,sort,pageSize],resetPage)
watch(search,()=>{clearTimeout(searchTimer);searchTimer=setTimeout(resetPage,300)})
onMounted(load);onBeforeUnmount(()=>{controller?.abort();clearTimeout(searchTimer)})

const mode=ref<''|'edit'|'detail'|'activity'|'lost'|'convert'>('')
const target=ref<Lead|null>(null), saving=ref(false), formError=ref('')
const emptyForm=()=>({business_name:'',contact_name:'',phone:'',wechat:'',email:'',industry_name:'',province:'',city:'',district:'',address:'',source_type:'self_developed',stage:'new',planned_visit_at:'',next_followup_at:''})
const form=reactive(emptyForm())
const activity=reactive({activity_type:'visit',outcome:'',content:'',stage:'contacted',occurred_at:'',next_followup_at:''})
const conversion=reactive<Conversion>({username:'',display_name:'',phone:'',email:'',province:'',city:'',district:'',address:'',delivery_method:'copy',handoff_summary:''})
const confirmed=ref(false), lostReason=ref('')
const dialogTitle=computed(()=>mode.value==='edit'?(target.value?'编辑顾客与拜访计划':'新增意向顾客'):mode.value==='activity'?'记录拜访 / 跟进':mode.value==='lost'?'结束跟进 · 未成交':mode.value==='convert'?'预开户 · 等待财务确认':'顾客详情与跟进历史')
function close(){if(saving.value)return;mode.value='';formError.value=''}
function edit(item?:Lead){
 target.value=item||null
 const next=emptyForm()
 for(const key of Object.keys(next) as Array<keyof typeof next>) next[key]=item?.[key]??next[key]
 next.planned_visit_at=localInputTime(item?.planned_visit_at);next.next_followup_at=localInputTime(item?.next_followup_at)
 Object.assign(form,next);formError.value='';mode.value='edit'
}
async function save(){
 saving.value=true;formError.value=''
 try{const payload: LeadInput={...form,planned_visit_at:isoInput(form.planned_visit_at),next_followup_at:isoInput(form.next_followup_at)}
  const saved=await saveLead(payload,target.value?.id);notice.value='已保存：'+saved.business_name;mode.value='';await load()
 }catch(e){formError.value=e instanceof Error?e.message:'保存失败'}finally{saving.value=false}
}
function visit(item:Lead){target.value=item;Object.assign(activity,{activity_type:'visit',outcome:'',content:'',stage:item.stage==='new'||item.stage==='visit_planned'?'contacted':item.stage,occurred_at:localInputTime(new Date().toISOString()),next_followup_at:localInputTime(item.next_followup_at)});formError.value='';mode.value='activity'}
async function saveVisit(){if(!target.value)return;saving.value=true;formError.value=''
 try{await addActivity(target.value.id,{...activity,occurred_at:isoInput(activity.occurred_at),next_followup_at:isoInput(activity.next_followup_at)});notice.value='拜访记录已保存，下一次跟进时间已更新。';mode.value='';await load()}
 catch(e){formError.value=e instanceof Error?e.message:'保存失败'}finally{saving.value=false}
}
function fail(item:Lead){target.value=item;lostReason.value='';formError.value='';mode.value='lost'}
async function saveLost(){if(!target.value)return;saving.value=true;formError.value=''
 try{await loseLead(target.value.id,lostReason.value);notice.value='未成交原因已保存，历史拜访记录继续保留。';mode.value='';await load()}
 catch(e){formError.value=e instanceof Error?e.message:'保存失败'}finally{saving.value=false}
}
const credential=ref<InitialCredential|null>(null), credentialName=ref(''), credentialUsername=ref('')
function win(item:Lead){target.value=item;Object.assign(conversion,{username:'kh'+item.id,display_name:item.business_name.slice(0,128),phone:item.phone,email:item.email,province:item.province,city:item.city,district:item.district,address:item.address,delivery_method:'copy',handoff_summary:''});confirmed.value=false;formError.value='';mode.value='convert'}
async function saveConversion(){if(!target.value||!confirmed.value||saving.value)return;saving.value=true;formError.value=''
 try{const r=await convertLead(target.value.id,conversion);credential.value=r.credential;credentialName.value=r.customer.display_name;credentialUsername.value=r.customer.username;notice.value='账号已建立，等待收款与财务审核。未计真实客户，也未自动分派运维。';mode.value='';await load()}
 catch(e){formError.value=e instanceof Error?e.message:'转换失败'}finally{saving.value=false}
}
const history=ref<Activity[]>([]),historyPage=ref(1),historySize=ref(12),historyTotal=ref(0),historyPages=ref(1),historyLoading=ref(false),historyView=ref<'card'|'table'>('card')
let historyRequest=0
async function loadHistory(){if(!target.value)return;const n=++historyRequest;const id=target.value.id;historyLoading.value=true
 try{const r=await listActivities(id,{page:historyPage.value,page_size:historySize.value});if(n!==historyRequest||target.value?.id!==id)return;history.value=r.items;historyTotal.value=r.total;historyPages.value=r.total_pages}
 catch(e){if(n===historyRequest)formError.value=e instanceof Error?e.message:'读取历史失败'}finally{if(n===historyRequest)historyLoading.value=false}
}
async function detail(item:Lead){target.value=item;mode.value='detail';formError.value='';history.value=[];historyPage.value=1
 try{target.value=await getLead(item.id);await loadHistory()}catch(e){formError.value=e instanceof Error?e.message:'读取失败'}
}
watch(historyPage,()=>{if(mode.value==='detail')void loadHistory()});watch(historySize,()=>{if(historyPage.value!==1)historyPage.value=1;else void loadHistory()})
function overdue(value?:string){return Boolean(value&&new Date(value).getTime()<Date.now())}
</script>

<template>
 <div class="management-page sales-business">
  <ModulePageNav context="workspace-sales" active-title="意向顾客" />
  <header class="sb-toolbar"><div><h2>意向顾客</h2><p>先录入商家并跟进，合作后可预开户；财务审核入账才认定真实客户，未成交保留原因。</p></div><button class="primary" @click="edit()">＋ 新增意向顾客</button></header>
  <p v-if="notice" class="sb-success" role="status">{{notice}}</p>
  
  <section class="sb-panel">
   <DataListControls v-model:view-mode="view" v-model:search="search" v-model:status="status" v-model:sort="sort" v-model:page-size="pageSize" :status-options="statusOptions" :sort-options="sorts" search-placeholder="商家、联系人、电话、微信、行业或地址" />
   <div class="sb-toolbar"><label>意向阶段 <select v-model="stage"><option value="all">全部阶段</option><option v-for="[key,label] in stages" :key="key" :value="key">{{label}}</option></select></label><button :disabled="loading" @click="load">刷新</button></div>
   <p v-if="error" class="sb-error" role="alert">{{error}} <button @click="load">重试</button></p>
   <div v-else-if="loading" class="sb-empty" aria-live="polite">正在读取意向顾客…</div>
   <div v-else-if="!items.length" class="sb-empty">{{search||status!=='open'||stage!=='all'?'没有符合筛选条件的顾客。':'还没有意向顾客。点击“新增意向顾客”，录入你准备拜访的商家。'}}</div>
   <div v-else-if="view==='table'" class="sb-table"><table><thead><tr><th>顾客 / 联系人</th><th>联系方式</th><th>意向阶段</th><th>拜访 / 跟进时间</th><th>业务动作</th></tr></thead><tbody><tr v-for="item in items" :key="item.id"><td><strong>{{item.business_name}}</strong><small>{{item.contact_name||'未填联系人'}} · {{item.lead_no}}</small><small>{{[item.province,item.city,item.district,item.address].join(' ')}}</small></td><td>{{item.phone||'未留电话'}}<small v-if="item.wechat">微信：{{item.wechat}}</small></td><td><span class="sb-badge">{{stageLabels[item.stage]||item.stage}}</span><small v-if="item.status==='lost'">原因：{{item.lost_reason}}</small></td><td>拜访：{{time(item.planned_visit_at)}}<small :style="overdue(item.next_followup_at)&&item.status==='open'?{color:'#b54b37'}:{}">跟进：{{time(item.next_followup_at)}}</small></td><td><div class="sb-actions"><button @click="detail(item)">详情</button><template v-if="item.status==='open'"><button @click="edit(item)">编辑 / 改期</button><button @click="visit(item)">记拜访</button><button class="primary" @click="win(item)">预开户</button><button class="danger" @click="fail(item)">未成交</button></template></div></td></tr></tbody></table></div>
   <div v-else class="sb-grid"><article v-for="item in items" :key="item.id" class="sb-record"><header><h3>{{item.business_name}}</h3><span class="sb-badge">{{stageLabels[item.stage]}}</span></header><p>{{item.contact_name||'未填联系人'}} · {{item.phone||item.wechat}}</p><small>{{[item.province,item.city,item.district,item.address].join(' ')}}</small><p>计划拜访：{{time(item.planned_visit_at)}}<br />下次跟进：{{time(item.next_followup_at)}}</p><p v-if="item.status==='lost'">未成交原因：{{item.lost_reason}}</p><div class="sb-actions"><button @click="detail(item)">详情</button><template v-if="item.status==='open'"><button @click="edit(item)">编辑 / 改期</button><button @click="visit(item)">记拜访</button><button class="primary" @click="win(item)">预开户</button><button class="danger" @click="fail(item)">未成交</button></template></div></article></div>
   <PaginationBar v-model:page="page" :total-pages="pages" :total="total" :page-size="pageSize" />
  </section>

  <Teleport to="body"><div v-if="mode" class="sb-overlay" @keydown.esc="close"><section class="sb-dialog sales-business" role="dialog" aria-modal="true" aria-labelledby="lead-dialog-title">
   <header class="sb-toolbar"><div><h2 id="lead-dialog-title">{{dialogTitle}}</h2><p v-if="target">{{target.business_name}} · {{target.lead_no}}</p></div><button :disabled="saving" aria-label="关闭对话框" @click="close">关闭</button></header>
   <p v-if="formError" class="sb-error" role="alert">{{formError}}</p>
   <form v-if="mode==='edit'" class="sb-form" @submit.prevent="save">
    <label>商家 / 顾客名称 *<input v-model="form.business_name" maxlength="160" required /></label><label>联系人<input v-model="form.contact_name" maxlength="128" /></label>
    <label>电话<input v-model="form.phone" maxlength="64" inputmode="tel" /></label><label>微信<input v-model="form.wechat" maxlength="128" /></label>
    <label>行业<input v-model="form.industry_name" maxlength="128" /></label><label>邮箱<input v-model="form.email" maxlength="255" type="email" /></label>
    <RegionSelect v-model:province="form.province" v-model:city="form.city" v-model:district="form.district" class="sb-wide" :required="false" />
    <label class="sb-wide">详细拜访地址<input v-model="form.address" maxlength="255" /></label>
    <label>来源<select v-model="form.source_type"><option value="self_developed">自主开发</option><option value="referral">他人介绍</option><option value="walk_in">到店 / 地推</option><option value="other">其他</option></select></label>
    <label>当前阶段<select v-model="form.stage"><option v-for="[key,label] in stages" :key="key" :value="key">{{label}}</option></select></label>
    <label>计划拜访时间<input v-model="form.planned_visit_at" type="datetime-local" /></label><label>下次跟进时间<input v-model="form.next_followup_at" type="datetime-local" /></label>
    <p class="sb-wide sb-note">电话或微信至少填写一项。这里只建立意向档案，不会提前生成登录用户、订单或收费。</p><button type="submit" class="primary sb-wide" :disabled="saving">{{saving?'保存中…':'保存顾客与计划'}}</button>
   </form>
   <form v-else-if="mode==='activity'" class="sb-form" @submit.prevent="saveVisit">
    <label>沟通方式<select v-model="activity.activity_type"><option value="visit">到店拜访</option><option value="phone">电话</option><option value="wechat">微信</option><option value="message">消息</option><option value="note">备注</option></select></label><label>实际沟通时间<input v-model="activity.occurred_at" type="datetime-local" required /></label>
    <label class="sb-wide">拜访 / 沟通内容 *<textarea v-model="activity.content" maxlength="2000" required placeholder="记录需求、沟通情况、顾客顾虑与下一步安排" /></label>
    <label>本次结果<input v-model="activity.outcome" maxlength="64" placeholder="例如：愿意试用，等待老板确认" /></label><label>最新阶段<select v-model="activity.stage"><option v-for="[key,label] in stages" :key="key" :value="key">{{label}}</option></select></label><label class="sb-wide">下一次跟进<input v-model="activity.next_followup_at" type="datetime-local" /><small>留空表示本次不再安排下次跟进。完成拜访后会清除旧的待拜访计划。</small></label><button type="submit" class="primary sb-wide" :disabled="saving">{{saving?'保存中…':'保存拜访记录'}}</button>
   </form>
   <form v-else-if="mode==='lost'" class="sb-form" @submit.prevent="saveLost"><p class="sb-note sb-wide">未成交不会删除顾客及拜访历史，也不会创建正式用户。</p><label class="sb-wide">失败原因 *<textarea v-model="lostReason" required maxlength="1024" placeholder="例如：预算不足、暂不开展直播、选择其他服务，并写清具体原因" /></label><button type="submit" class="danger sb-wide" :disabled="saving">{{saving?'保存中…':'确认未成交并保留原因'}}</button></form>
   <form v-else-if="mode==='convert'" class="sb-form" @submit.prevent="saveConversion">
    <p class="sb-note sb-wide">此处只建立登录账号，保留销售归属；不代表已收款，不计为真实客户、不增加余额或时长，也不自动安排运维。真实客户认定由财务审核入账产生。</p>
    <label>登录账号 *<input v-model="conversion.username" maxlength="128" required autocomplete="off" /></label><label>客户名称 *<input v-model="conversion.display_name" maxlength="128" required /></label><label>客户电话 *<input v-model="conversion.phone" maxlength="64" required inputmode="tel" /></label><label>客户邮箱<input v-model="conversion.email" type="email" maxlength="255" /></label>
    <RegionSelect v-model:province="conversion.province" v-model:city="conversion.city" v-model:district="conversion.district" class="sb-wide" required />
    <label class="sb-wide">详细地址<input v-model="conversion.address" maxlength="255" /></label><label>初始凭证交付<select v-model="conversion.delivery_method"><option value="copy">复制交给客户</option><option value="email">邮件发送给客户</option></select></label><p class="sb-note">初始密码由系统生成，客户首次登录需修改。</p>
    <label class="sb-wide">开户说明（可选）<textarea v-model="conversion.handoff_summary" maxlength="2000" placeholder="开户需求与备注；需要运维时另行申请协助" /></label><label class="sb-wide"><span><input v-model="confirmed" type="checkbox" required style="width:auto" /> 我已核实客户同意开户，信息无误；理解开户不是财务确认成交。</span></label><button type="submit" class="primary sb-wide" :disabled="saving||!confirmed">{{saving?'正在预开户…':'确认预开户'}}</button>
   </form>
   <template v-else-if="mode==='detail'&&target">
    <section class="sb-panel"><h3>{{target.business_name}} <span class="sb-badge">{{stageLabels[target.stage]}}</span></h3><p>{{target.contact_name}} · {{target.phone}} <span v-if="target.wechat"> / 微信 {{target.wechat}}</span></p><p>{{[target.province,target.city,target.district,target.address].join(' ')}}</p><p v-if="target.status==='lost'" class="sb-error">失败原因：{{target.lost_reason}}</p><p v-if="target.converted_user_id" class="sb-success">已建立客户账号 #{{target.converted_user_id}}，转入时间：{{time(target.converted_at)}}。<RouterLink to="/sales/customers" @click="close">进入我的客户</RouterLink></p><div v-if="target.status==='open'" class="sb-actions"><button @click="visit(target)">新增拜访记录</button><button @click="edit(target)">编辑 / 重新安排</button></div></section>
    <header class="sb-toolbar"><h3>拜访与跟进历史</h3><div class="sb-actions"><button :aria-pressed="historyView==='card'" @click="historyView='card'">卡片</button><button :aria-pressed="historyView==='table'" @click="historyView='table'">表格</button><label>每页 <select v-model.number="historySize"><option :value="12">12</option><option :value="24">24</option><option :value="48">48</option></select></label></div></header>
    <p v-if="historyLoading" class="sb-empty">正在读取记录…</p><p v-else-if="!history.length" class="sb-empty">暂无记录。</p>
    <div v-else-if="historyView==='table'" class="sb-table"><table><thead><tr><th>时间 / 经办人</th><th>方式 / 结果</th><th>沟通记录</th><th>下次跟进</th></tr></thead><tbody><tr v-for="item in history" :key="item.id"><td>{{time(item.occurred_at)}}<small>{{item.sales_display_name}}</small></td><td>{{activityLabels[item.activity_type]||item.activity_type}}<small>{{item.outcome}}</small></td><td>{{item.content}}</td><td>{{time(item.next_followup_at)}}</td></tr></tbody></table></div>
    <div v-else class="sb-grid"><article v-for="item in history" :key="item.id" class="sb-record"><header><strong>{{activityLabels[item.activity_type]||item.activity_type}}</strong><small>{{time(item.occurred_at)}}</small></header><p>{{item.content}}</p><small>记录人：{{item.sales_display_name||'#'+item.sales_staff_id}} · {{item.outcome}}</small><p v-if="item.next_followup_at">下次跟进：{{time(item.next_followup_at)}}</p></article></div>
    <PaginationBar v-model:page="historyPage" :total-pages="historyPages" :total="historyTotal" :page-size="historySize" />
   </template>
  </section></div></Teleport>
  <CredentialResultModal :open="Boolean(credential)" title="客户已开户 · 待财务确认" :display-name="credentialName" :username="credentialUsername" :credential="credential" @close="credential=null" />
 </div>
</template>
