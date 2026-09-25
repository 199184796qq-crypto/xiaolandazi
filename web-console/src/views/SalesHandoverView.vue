<script setup lang="ts">
import {onMounted,onBeforeUnmount,ref,watch} from 'vue'
import {RouterLink} from 'vue-router'
import ModulePageNav from '../components/ModulePageNav.vue'
import DataListControls from '../components/DataListControls.vue'
import PaginationBar from '../components/PaginationBar.vue'
import {listHandoverPeople,getHandoverPreview,transferPortfolio,listHandoverHistory,formatSalesTime as time} from '../salesBusinessApi'
import type {SalesPerson,Handover,HandoverPreview} from '../salesBusinessApi'
import '../salesBusiness.css'
const items=ref<SalesPerson[]>([]),loading=ref(false),error=ref(''),notice=ref('')
const page=ref(1),size=ref(12),total=ref(0),pages=ref(1),view=ref<'card'|'table'>('table'),search=ref(''),status=ref('all')
const statuses=[{label:'全部账号（含已停用）',value:'all'},{label:'在职启用',value:'active'},{label:'已停用 / 离职待交接',value:'disabled'}]
let ctrl:AbortController|undefined,timer:ReturnType<typeof setTimeout>|undefined
async function load(){ctrl?.abort();const c=new AbortController();ctrl=c;loading.value=true;error.value=''
 try{const r=await listHandoverPeople({page:page.value,page_size:size.value,search:search.value,status:status.value},c.signal);if(c.signal.aborted)return;items.value=r.items;total.value=r.total;pages.value=r.total_pages;if(page.value>pages.value)page.value=pages.value}
 catch(e){if(!c.signal.aborted)error.value=e instanceof Error?e.message:'读取失败'}finally{if(c===ctrl)loading.value=false}
}
function reset(){if(page.value!==1)page.value=1;else void load()}
watch(page,load);watch([size,status],reset);watch(search,()=>{clearTimeout(timer);timer=setTimeout(reset,300)})
const source=ref<SalesPerson|null>(null),preview=ref<HandoverPreview|null>(null),recipient=ref<SalesPerson|null>(null),reason=ref(''),confirmed=ref(false),saving=ref(false),formError=ref('')
const recipients=ref<SalesPerson[]>([]),recipientSearch=ref(''),recipientPage=ref(1),recipientTotal=ref(0),recipientPages=ref(1),recipientLoading=ref(false)
let recipientSeq=0,recipientTimer:ReturnType<typeof setTimeout>|undefined
async function loadRecipients(){const n=++recipientSeq;recipientLoading.value=true
 try{const r=await listHandoverPeople({status:'active',search:recipientSearch.value,page:recipientPage.value,page_size:12});if(n!==recipientSeq)return;recipients.value=r.items;recipientTotal.value=r.total;recipientPages.value=r.total_pages}
 catch(e){if(n===recipientSeq)formError.value=e instanceof Error?e.message:'读取接手销售失败'}finally{if(n===recipientSeq)recipientLoading.value=false}
}
watch(recipientPage,loadRecipients);watch(recipientSearch,()=>{clearTimeout(recipientTimer);recipientTimer=setTimeout(()=>{if(recipientPage.value!==1)recipientPage.value=1;else void loadRecipients()},300)})
async function begin(item:SalesPerson){source.value=item;recipient.value=null;preview.value=null;formError.value='';reason.value='';confirmed.value=false;recipientSearch.value='';recipientPage.value=1
 try{preview.value=await getHandoverPreview(item.staff_id);await loadRecipients()}catch(e){formError.value=e instanceof Error?e.message:'读取交接清单失败'}
}
async function submit(){if(!source.value||!recipient.value||!preview.value||!confirmed.value||saving.value)return;saving.value=true;formError.value=''
 try{const r=await transferPortfolio(source.value.staff_id,recipient.value.staff_id,reason.value,preview.value);notice.value='交接单 '+r.handover_no+' 已完成：'+r.customer_count+' 个正式客户、'+r.lead_count+' 个意向顾客交给 '+r.to_display_name+'。';source.value=null;await load();await loadHistory()}
 catch(e){formError.value=e instanceof Error?e.message:'交接失败'}finally{saving.value=false}
}
const records=ref<Handover[]>([]),historyPage=ref(1),historySize=ref(12),historyTotal=ref(0),historyPages=ref(1),historyView=ref<'card'|'table'>('table'),historyLoading=ref(false),historyError=ref('')
async function loadHistory(){historyLoading.value=true;historyError.value=''
 try{const r=await listHandoverHistory({page:historyPage.value,page_size:historySize.value});records.value=r.items;historyTotal.value=r.total;historyPages.value=r.total_pages}
 catch(e){historyError.value=e instanceof Error?e.message:'读取交接历史失败'}finally{historyLoading.value=false}
}
watch(historyPage,loadHistory);watch(historySize,()=>{if(historyPage.value!==1)historyPage.value=1;else void loadHistory()})
onMounted(()=>{void load();void loadHistory()});onBeforeUnmount(()=>{ctrl?.abort();clearTimeout(timer);clearTimeout(recipientTimer);recipientSeq++})
</script>

<template>
 <div class="management-page sales-business">
  <ModulePageNav hub="sales" active-title="客户交接" />
  <header class="sb-toolbar"><div><h2>销售离职 / 调岗 · 客户交接</h2><p>由主管确认，将正式客户和仍在跟进的意向顾客整体交给在职销售。</p></div><RouterLink class="text-action" to="/staff/employees?group=sales">员工停用与权限管理</RouterLink></header>
  <p class="sb-note">只转交当前责任，不改原开发来源、历史记录作者、历史订单归属和已生成提成。已停用账号也能补办交接；离职账号不必为交接而重新启用。已结束的线索保留归档。</p>
  <p v-if="notice" class="sb-success" role="status">{{notice}}</p>
  <section class="sb-panel"><DataListControls v-model:view-mode="view" v-model:search="search" v-model:status="status" v-model:page-size="size" :status-options="statuses" search-placeholder="销售姓名、账号、员工编号" />
   <p v-if="error" class="sb-error" role="alert">{{error}} <button @click="load">重试</button></p><div v-else-if="loading" class="sb-empty">正在读取交接范围…</div><div v-else-if="!items.length" class="sb-empty">没有符合条件或在你管理范围内的销售。</div>
   <div v-else-if="view==='table'" class="sb-table"><table><thead><tr><th>销售</th><th>账号状态</th><th>正式客户</th><th>跟进中意向顾客</th><th>业务动作</th></tr></thead><tbody><tr v-for="item in items" :key="item.staff_id"><td><strong>{{item.display_name}}</strong><small>@{{item.username}}</small></td><td>{{item.status==='active'?'启用':'已停用'}}</td><td>{{item.customer_count}}</td><td>{{item.lead_count}}</td><td><button :disabled="!item.customer_count&&!item.lead_count" @click="begin(item)">预览并交接</button></td></tr></tbody></table></div>
   <div v-else class="sb-grid"><article v-for="item in items" :key="item.staff_id" class="sb-record"><header><h3>{{item.display_name}}</h3><span class="sb-badge">{{item.status==='active'?'启用':'已停用'}}</span></header><small>@{{item.username}}</small><p>正式客户 {{item.customer_count}} 个<br />意向顾客 {{item.lead_count}} 个</p><button :disabled="!item.customer_count&&!item.lead_count" @click="begin(item)">预览并交接</button></article></div>
   <PaginationBar v-model:page="page" :total-pages="pages" :total="total" :page-size="size" />
  </section>
  <section class="sb-panel"><header class="sb-toolbar"><h3>交接历史</h3><div class="sb-actions"><button @click="historyView='table'">表格</button><button @click="historyView='card'">卡片</button><label>每页 <select v-model.number="historySize"><option :value="12">12</option><option :value="24">24</option><option :value="48">48</option></select></label><button @click="loadHistory">刷新</button></div></header><p v-if="historyError" class="sb-error">{{historyError}}</p><div v-else-if="historyLoading" class="sb-empty">正在读取历史…</div><div v-else-if="!records.length" class="sb-empty">暂无交接历史。</div>
   <div v-else-if="historyView==='table'" class="sb-table"><table><thead><tr><th>交接单 / 时间</th><th>原销售 → 接手人</th><th>交接数量</th><th>原因 / 经办人</th></tr></thead><tbody><tr v-for="item in records" :key="item.id"><td>{{item.handover_no}}<small>{{time(item.created_at)}}</small></td><td>{{item.from_display_name}} → {{item.to_display_name}}</td><td>正式客户 {{item.customer_count}}<small>意向顾客 {{item.lead_count}}</small></td><td>{{item.reason}}<small>操作账号 #{{item.created_by_user_id}}</small></td></tr></tbody></table></div>
   <div v-else class="sb-grid"><article v-for="item in records" :key="item.id" class="sb-record"><h3>{{item.from_display_name}} → {{item.to_display_name}}</h3><small>{{item.handover_no}} · {{time(item.created_at)}}</small><p>正式客户 {{item.customer_count}} 个 / 意向顾客 {{item.lead_count}} 个</p><p>{{item.reason}}</p><small>操作账号 #{{item.created_by_user_id}}</small></article></div>
   <PaginationBar v-model:page="historyPage" :total-pages="historyPages" :total="historyTotal" :page-size="historySize" />
  </section>
  <Teleport to="body"><div v-if="source" class="sb-overlay"><section class="sb-dialog sales-business" role="dialog" aria-modal="true" aria-labelledby="sales-handover-title"><header class="sb-toolbar"><h2 id="sales-handover-title">交接 {{source.display_name}} 的客户</h2><button :disabled="saving" @click="source=null">关闭</button></header><p v-if="formError" class="sb-error" role="alert">{{formError}}</p><div v-if="!preview" class="sb-empty">正在核对交接数量…</div><template v-else>
   <p class="sb-note">本次交接范围：正式客户 {{preview.customer_count}} 位、跟进中意向顾客 {{preview.open_lead_count}} 位。请核对后确认。</p>
   <h3>选择接手销售</h3><label>搜索在职销售<input v-model="recipientSearch" class="sb-search" placeholder="姓名或账号" /></label><p v-if="recipientLoading">正在读取…</p><div v-else class="sb-grid"><button v-for="item in recipients" :key="item.staff_id" :disabled="item.staff_id===source.staff_id" :class="{primary:recipient?.staff_id===item.staff_id}" @click="recipient=item">{{item.display_name}} · @{{item.username}}{{item.staff_id===source.staff_id?'（本人不可选）':''}}</button></div><div v-if="!recipientLoading&&!recipients.length" class="sb-empty">未找到可接手人员。</div><PaginationBar v-model:page="recipientPage" :total-pages="recipientPages" :total="recipientTotal" :page-size="12" /><p class="sb-note">已选接手人：{{recipient?.display_name||'尚未选择'}}。待跟进时间与历史记录一起接续，不把旧记录改成新人的记录。</p>
   <form class="sb-form" @submit.prevent="submit"><label class="sb-wide">交接原因 *<textarea v-model="reason" maxlength="1000" required placeholder="离职、调岗或主管安排，以及需要接手人注意的事项" /></label><label class="sb-wide"><span><input v-model="confirmed" type="checkbox" required style="width:auto" /> 已核对交接范围，确认整体转给所选接手销售。</span></label><button type="submit" class="primary sb-wide" :disabled="saving||!recipient||!confirmed||(!preview.customer_count&&!preview.open_lead_count)">{{saving?'交接中，请勿重复提交…':'确认交接'}}</button></form>
  </template></section></div></Teleport>
 </div>
</template>
