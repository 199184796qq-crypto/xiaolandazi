<script setup lang="ts">
import {onMounted,onBeforeUnmount,ref,watch} from 'vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import DataListControls from '../components/DataListControls.vue'
import PaginationBar from '../components/PaginationBar.vue'
import {listHandoffs,updateHandoff,listHandoffEvents,handoffLabels,formatSalesTime as time} from '../salesBusinessApi'
import type {Handoff,HandoffEvent} from '../salesBusinessApi'
import '../salesBusiness.css'
const items=ref<Handoff[]>([]),loading=ref(false),error=ref(''),notice=ref('')
const search=ref(''),status=ref('pending'),sort=ref('created-asc'),page=ref(1),size=ref(12),total=ref(0),pages=ref(1),view=ref<'card'|'table'>('table')
const statuses=[{label:'全部状态',value:'all'},...Object.entries(handoffLabels).map(([value,label])=>({value,label}))]
const sorts=[{label:'最早交接优先',value:'created-asc'},{label:'最新交接优先',value:'created-desc'},{label:'客户名称',value:'customer-asc'},{label:'交接状态',value:'status-asc'}]
let ctrl:AbortController|undefined,timer:ReturnType<typeof setTimeout>|undefined
async function load(){ctrl?.abort();const c=new AbortController();ctrl=c;loading.value=true;error.value=''
 try{const r=await listHandoffs({page:page.value,page_size:size.value,status:status.value,search:search.value,sort:sort.value},c.signal);if(c.signal.aborted)return;items.value=r.items;total.value=r.total;pages.value=r.total_pages;if(page.value>pages.value)page.value=pages.value}
 catch(e){if(!c.signal.aborted)error.value=e instanceof Error?e.message:'读取交接失败'}finally{if(ctrl===c)loading.value=false}
}
function reset(){if(page.value!==1)page.value=1;else void load()}
watch(page,load);watch([status,sort,size],reset);watch(search,()=>{clearTimeout(timer);timer=setTimeout(reset,300)})
onMounted(load);onBeforeUnmount(()=>{ctrl?.abort();clearTimeout(timer)})
const target=ref<Handoff|null>(null),action=ref(''),note=ref(''),saving=ref(false),formError=ref('')
function nextState(item:Handoff){return ({pending:'accepted',accepted:'in_progress',in_progress:'completed'} as Record<string,string>)[item.status]||''}
function actionLabel(item:Handoff){return ({pending:'接单',accepted:'开始处理',in_progress:'完成交接'} as Record<string,string>)[item.status]||''}
function begin(item:Handoff){target.value=item;action.value=nextState(item);note.value='';formError.value=''}
async function submit(){if(!target.value||saving.value)return;saving.value=true;formError.value=''
 try{await updateHandoff(target.value.id,action.value,note.value);notice.value='交接单 '+target.value.handoff_no+' 已更新为“'+handoffLabels[action.value]+'”。';target.value=null;await load()}
 catch(e){formError.value=e instanceof Error?e.message:'处理失败'}finally{saving.value=false}
}
const eventTarget=ref<Handoff|null>(null),events=ref<HandoffEvent[]>([]),eventPage=ref(1),eventSize=ref(12),eventTotal=ref(0),eventPages=ref(1),eventLoading=ref(false),eventError=ref(''),eventView=ref<'card'|'table'>('card')
let eventSeq=0
async function loadEvents(){if(!eventTarget.value)return;const id=eventTarget.value.id,n=++eventSeq;eventLoading.value=true;eventError.value=''
 try{const r=await listHandoffEvents(id,{page:eventPage.value,page_size:eventSize.value});if(n!==eventSeq||eventTarget.value?.id!==id)return;events.value=r.items;eventTotal.value=r.total;eventPages.value=r.total_pages}
 catch(e){if(n===eventSeq)eventError.value=e instanceof Error?e.message:'读取历史失败'}finally{if(n===eventSeq)eventLoading.value=false}
}
function history(item:Handoff){eventTarget.value=item;events.value=[];eventPage.value=1;void loadEvents()}
watch(eventPage,loadEvents);watch(eventSize,()=>{if(eventPage.value!==1)eventPage.value=1;else void loadEvents()})
</script>

<template>
 <div class="management-page sales-business">
  <ModulePageNav context="live" active-title="客户交接" />
  <header class="sb-toolbar"><div><h2>客户交接</h2><p>接收销售新转入的正式客户，明确接手人、处理过程和完成结果。</p></div><button :disabled="loading" @click="load">刷新</button></header>
  <p class="sb-note">普通运维可查看待接单和自己接手的任务；负责人按权限管理。接单不自动授予客户直播策略、主播训练或声音复刻的操作授权。</p>
  <p v-if="notice" class="sb-success" role="status">{{notice}}</p>
  <section class="sb-panel">
   <DataListControls v-model:view-mode="view" v-model:search="search" v-model:status="status" v-model:sort="sort" v-model:page-size="size" :status-options="statuses" :sort-options="sorts" search-placeholder="客户、交接编号、电话、销售或交接说明" />
   <p v-if="error" class="sb-error" role="alert">{{error}} <button @click="load">重试</button></p><div v-else-if="loading" class="sb-empty">正在读取交接任务…</div><div v-else-if="!items.length" class="sb-empty">当前筛选范围内没有客户交接任务。</div>
   <div v-else-if="view==='table'" class="sb-table"><table><thead><tr><th>客户 / 交接单</th><th>销售交接说明</th><th>责任人 / 状态</th><th>业务动作</th></tr></thead><tbody><tr v-for="item in items" :key="item.id"><td><strong>{{item.customer_name}}</strong>{{item.customer_phone}}<small>{{item.handoff_no}}<br />{{time(item.created_at)}}</small></td><td>{{item.summary}}<small>交出销售：{{item.sales_display_name}}</small></td><td><span class="sb-badge">{{handoffLabels[item.status]}}</span><small>接手：{{item.accepted_by_name||'待认领'}}</small><small v-if="item.completed_at">完成：{{item.completed_by_name}}<br />{{time(item.completed_at)}}</small></td><td><div class="sb-actions"><button v-if="nextState(item)" class="primary" @click="begin(item)">{{actionLabel(item)}}</button><button @click="history(item)">处理历史</button></div></td></tr></tbody></table></div>
   <div v-else class="sb-grid"><article v-for="item in items" :key="item.id" class="sb-record"><header><h3>{{item.customer_name}}</h3><span class="sb-badge">{{handoffLabels[item.status]}}</span></header><small>{{item.handoff_no}} · {{time(item.created_at)}}</small><p>{{item.customer_phone}} · 销售 {{item.sales_display_name}}</p><p>{{item.summary}}</p><small>接手人：{{item.accepted_by_name||'待认领'}}</small><div class="sb-actions"><button v-if="nextState(item)" class="primary" @click="begin(item)">{{actionLabel(item)}}</button><button @click="history(item)">处理历史</button></div></article></div>
   <PaginationBar v-model:page="page" :total-pages="pages" :total="total" :page-size="size" />
  </section>
  <Teleport to="body">
   <div v-if="target" class="sb-overlay"><section class="sb-dialog sales-business" role="dialog" aria-modal="true" aria-labelledby="handoff-action-title"><header class="sb-toolbar"><h2 id="handoff-action-title">{{actionLabel(target)}} · {{target.customer_name}}</h2><button :disabled="saving" @click="target=null">关闭</button></header><p>{{target.summary}}</p><p v-if="formError" class="sb-error" role="alert">{{formError}}</p><form class="sb-form" @submit.prevent="submit"><label class="sb-wide">{{action==='completed'?'完成说明 *':'处理说明'}}<textarea v-model="note" maxlength="2000" :required="action==='completed'" placeholder="本次处理内容、已完成项目与需要后续关注的事项" /></label><button type="submit" class="primary sb-wide" :disabled="saving">{{saving?'保存中…':'确认'+actionLabel(target)}}</button></form></section></div>
   <div v-if="eventTarget" class="sb-overlay"><section class="sb-dialog sales-business" role="dialog" aria-modal="true" aria-labelledby="handoff-history-title"><header class="sb-toolbar"><h2 id="handoff-history-title">处理历史 · {{eventTarget.customer_name}}</h2><button @click="eventTarget=null">关闭</button></header><p class="sb-note">原始交接说明：{{eventTarget.summary}}</p><div class="sb-toolbar"><div class="sb-actions"><button @click="eventView='card'">卡片</button><button @click="eventView='table'">表格</button></div><label>每页 <select v-model.number="eventSize"><option :value="12">12</option><option :value="24">24</option><option :value="48">48</option></select></label></div><p v-if="eventError" class="sb-error">{{eventError}}</p><div v-else-if="eventLoading" class="sb-empty">读取中…</div><div v-else-if="!events.length" class="sb-empty">还没有处理记录。</div><div v-else-if="eventView==='table'" class="sb-table"><table><thead><tr><th>时间</th><th>操作人</th><th>状态</th><th>处理说明</th></tr></thead><tbody><tr v-for="item in events" :key="item.id"><td>{{time(item.created_at)}}</td><td>{{item.operator_name}}</td><td>{{handoffLabels[item.status]}}</td><td>{{item.content||'未填写说明'}}</td></tr></tbody></table></div><div v-else class="sb-grid"><article v-for="item in events" :key="item.id" class="sb-record"><header><strong>{{handoffLabels[item.status]}}</strong><small>{{time(item.created_at)}}</small></header><p>{{item.content||'未填写说明'}}</p><small>处理人：{{item.operator_name}}</small></article></div><PaginationBar v-model:page="eventPage" :total-pages="eventPages" :total="eventTotal" :page-size="eventSize" /></section></div>
  </Teleport>
 </div>
</template>
