<script setup lang="ts">
import {onBeforeUnmount,ref,watch} from 'vue'
import {businessEvents,businessTime,actionLabels} from '../customerBusinessApi'
import type {BusinessEvent} from '../customerBusinessApi'
import PaginationBar from './PaginationBar.vue'
import { historyDisplayNote } from '../businessHistoryUi'
const p=defineProps<{kind:'receipt'|'ticket';id:number}>();const items=ref<BusinessEvent[]>([]),page=ref(1),size=ref(12),total=ref(0),pages=ref(1),error=ref(''),loading=ref(false),view=ref('card');let c:AbortController|undefined
async function load(){c?.abort();const a=new AbortController();c=a;loading.value=true;error.value='';try{const r=await businessEvents(p.kind,p.id,{page:page.value,page_size:size.value},a.signal);if(!a.signal.aborted){items.value=r.items;total.value=r.total;pages.value=Math.max(1,r.total_pages)}}catch(e){if(!a.signal.aborted)error.value=e instanceof Error?e.message:'读取失败'}finally{if(c===a)loading.value=false}}
watch(()=>[p.kind,p.id],()=>{page.value=1;void load()},{immediate:true});watch(page,load);watch(size,()=>{page.value=1;void load()});onBeforeUnmount(()=>c?.abort())
</script><template><section class="business-history"><header class="sb-toolbar"><h3>处理历史</h3><div class="sb-actions"><button type="button" @click="view='card'">卡片</button><button type="button" @click="view='table'">表格</button><select v-model.number="size" aria-label="历史每页数量"><option :value="12">12</option><option :value="24">24</option><option :value="48">48</option></select></div></header><p v-if="error" class="sb-error">{{error}} <button @click="load">重试</button></p><p v-else-if="loading">读取历史中…</p><p v-else-if="!items.length" class="sb-empty">暂无记录</p><div v-else-if="view==='table'" class="sb-table"><table><thead><tr><th>时间</th><th>经办人</th><th>操作</th><th>说明</th></tr></thead><tbody><tr v-for="v in items" :key="v.id"><td>{{businessTime(v.created_at)}}</td><td>{{v.actor_name}}</td><td>{{actionLabels[v.action]||v.action}}</td><td>{{historyDisplayNote(v)}}</td></tr></tbody></table></div><div v-else class="sb-grid"><article v-for="v in items" :key="v.id" class="sb-record history-card"><strong>{{actionLabels[v.action]||v.action}}</strong><p>{{historyDisplayNote(v)}}</p><small class="history-meta">{{v.actor_name}} · {{businessTime(v.created_at)}}</small></article></div><PaginationBar v-model:page="page" :total="total" :total-pages="pages" :page-size="size"/></section></template>
<style scoped>
.business-history .history-card{display:grid;grid-template-rows:auto 1fr auto;align-content:stretch;min-width:0;height:100%;box-sizing:border-box;padding:20px}
.business-history .history-card>strong{font-size:18px;line-height:1.5;align-self:start}
.business-history .history-card>p{font-size:18px;line-height:1.6;margin:12px 0 20px;overflow-wrap:anywhere}
.business-history .history-card>.history-meta{font-size:16px;line-height:1.5;margin:0;align-self:end;color:#687e99}
.business-history .sb-table td{vertical-align:top}
</style>

