<script setup lang="ts">
import {computed,onBeforeUnmount,ref,watch} from 'vue'
import {RouterLink} from 'vue-router'
import {inbox,inboxCategories,refreshInbox,getInboxItems,type InboxPage} from '../workInbox'
import TodoBadge from './TodoBadge.vue'
const props=withDefaults(defineProps<{request?:string;initialGroup?:string;initialCategory?:string;compact?:boolean}>(),{request:'',initialGroup:'',initialCategory:'',compact:false})
const category=ref(props.initialCategory),department=ref(''),selected=ref(props.initialGroup),page=ref(1),onlyDue=ref(false)
const detail=ref<InboxPage|null>(null),error=ref(''),loading=ref(false)
let controller:AbortController|undefined,serial=0
const valid=computed(()=>!!inbox.snapshot&&!inbox.error&&!inbox.snapshot.stale)
const allGroups=computed(()=>valid.value?inbox.snapshot!.groups:[])
const departments=computed(()=>[...new Set([...allGroups.value.filter(g=>g.count>0).map(g=>g.department),...(department.value?[department.value]:[])])])
const groups=computed(()=>allGroups.value.filter(g=>g.count>0&&(!category.value||g.category===category.value)&&(!department.value||g.department===department.value)))
const activeGroup=computed(()=>allGroups.value.find(g=>g.key===selected.value))
function categoryCount(key:string){return allGroups.value.filter(g=>g.category===key&&(!department.value||g.department===department.value)).reduce((sum,g)=>sum+(onlyDue.value?g.due_count:g.count),0)}
function pickCategory(key:string){category.value=key;selected.value='';detail.value=null;page.value=1}
function pickGroup(key:string){selected.value=key;page.value=1}
async function loadDetail(){controller?.abort();const s=++serial;detail.value=null;error.value='';if(!selected.value||!valid.value)return;const c=new AbortController();controller=c;loading.value=true;try{const result=await getInboxItems(selected.value,page.value,onlyDue.value,c.signal);if(s===serial){detail.value=result;if(page.value>result.total_pages)page.value=result.total_pages}}catch(e){if(!c.signal.aborted&&s===serial)error.value=e instanceof Error?e.message:'读取失败'}finally{if(s===serial)loading.value=false}}
watch([selected,page,onlyDue,()=>inbox.snapshot?.version,valid],loadDetail,{immediate:true})
watch(department,()=>{selected.value='';detail.value=null})
watch(()=>props.request,text=>{if(!text)return;const c=inboxCategories.find(c=>text.includes(c.label));category.value=c?.key||'';department.value=/维修|售后|库存|仓库|仓储/.test(text)?'仓储售后':/物流|发货/.test(text)?'仓储物流':/运维/.test(text)?'营销运维':/财务/.test(text)?'财务':/销售|回访|拜访/.test(text)?'客资销售':'';selected.value='';detail.value=null;onlyDue.value=/超时|逾期|已到期/.test(text)},{immediate:true})
onBeforeUnmount(()=>{serial++;controller?.abort()})
function time(value?:string){return value?new Date(value).toLocaleString('zh-CN',{hour12:false}):'—'}
</script>
<template>
<section class="work-inbox-panel" :class="{compact}">
 <header><div><h3>我的待办</h3><p>只列当前权限内、轮到你处理的事项。</p></div><button type="button" :disabled="inbox.loading" @click="refreshInbox">刷新</button></header>
 <p v-if="inbox.error" class="inbox-error" role="alert">{{inbox.error}} 未同步不代表没有待办。</p>
 <p v-else-if="!inbox.snapshot">正在同步待办…</p>
 <template v-else>
  <nav class="inbox-categories" aria-label="待办分类"><button type="button" :class="{selected:!category}" @click="pickCategory('')">全部</button><button v-for="c in inboxCategories" :key="c.key" type="button" :class="{selected:category===c.key}" @click="pickCategory(c.key)">{{c.label}}<TodoBadge :count="categoryCount(c.key)"/></button></nav>
  <div class="inbox-filters"><label class="inbox-business-filter"><span>业务</span><select v-model="department"><option value="">全部业务</option><option v-for="d in departments" :key="d">{{d}}</option></select></label><label class="inbox-due-filter"><input v-model="onlyDue" type="checkbox"/><span>仅已到处理时间</span></label></div>
  <p v-if="onlyDue" class="inbox-help">仅用于有明确计划时间的跟进与回访；未配置办理期限的维修、审核不虚构超时。</p>
  <div class="inbox-groups"><button v-for="g in groups.filter(g=>!onlyDue||g.due_count>0)" :key="g.key" type="button" :class="{selected:selected===g.key}" @click="pickGroup(g.key)"><span>{{g.title}}</span><small>{{g.department}} · {{g.shared_queue?'岗位共享待办':'当前可处理'}}</small><TodoBadge :count="onlyDue?g.due_count:g.count"/></button></div>
  <div v-if="valid&&!groups.some(g=>!onlyDue||g.due_count>0)" class="inbox-empty" role="status"><span class="inbox-empty-icon" aria-hidden="true"><svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><rect x="5" y="4" width="14" height="17" rx="3"/><path d="M9 4V2h6v2M8.5 12l2.5 2.5 4.5-5"/></svg></span><strong>暂无符合条件的待办</strong><p>当前已接入的业务中，没有符合条件的待办。</p></div>
  <section v-if="selected&&valid" class="inbox-details"><header><h4>{{activeGroup?.title||'待办明细'}}</h4><button type="button" @click="selected=''">收起明细</button></header><p v-if="loading">正在读取单据…</p><p v-else-if="error" class="inbox-error">{{error}} <button @click="loadDetail">重试</button></p><template v-else-if="detail"><p v-if="!detail.items.length">当前分类暂无待办，可能已被其他人员处理。</p><article v-for="item in detail.items" :key="item.key"><div><strong>{{item.title}}</strong><small>单据 {{item.reference}} · {{time(item.created_at)}}</small><small v-if="item.due_at">计划处理：{{time(item.due_at)}}</small></div><RouterLink :to="item.to">打开业务页</RouterLink></article><footer><span>共 {{detail.total}} 条 · 第 {{page}} / {{detail.total_pages}} 页</span><button :disabled="page<=1" @click="page--">上一页</button><button :disabled="page>=detail.total_pages" @click="page++">下一页</button></footer></template></section>
  
 </template>
</section>
</template>
<style scoped src="../workInboxPanel.css"></style>
