<script setup lang="ts">
import {computed,onBeforeUnmount,ref,watch} from 'vue'
import {RouterLink} from 'vue-router'
import {session} from '../session'
import {receiptTodos} from '../customerBusinessApi'
const actor=computed(()=>session.bootstrap?.actor),access=computed(()=>session.bootstrap?.staff_access)
const enabled=computed(()=>!!actor.value&&(actor.value.role==='sales_staff'||access.value?.is_super_admin||access.value?.permissions.includes('finance.dashboard.view')))
const finance=computed(()=>actor.value?.role==='platform_admin'||access.value?.is_super_admin||access.value?.permissions.includes('finance.dashboard.view')),counts=ref<Record<string,number>|null>(null),failed=ref(false);let timer:ReturnType<typeof setInterval>|undefined,c:AbortController|undefined
async function load(){if(!enabled.value||document.hidden)return;c?.abort();const a=new AbortController();c=a;try{const r=await receiptTodos(a.signal);if(!a.signal.aborted){counts.value=r;failed.value=false}}catch{if(!a.signal.aborted)failed.value=true}}
watch(()=>actor.value?.user_id,()=>{counts.value=null;c?.abort();clearInterval(timer);if(enabled.value){void load();timer=setInterval(load,15000)}},{immediate:true});onBeforeUnmount(()=>{c?.abort();clearInterval(timer)})
</script><template><div v-if="enabled" class="customer-business-reminder"><RouterLink :to="finance?'/staff/finance/receipts':'/sales/receipts'">{{finance?'财务收款待办':'我的收款进度'}}<span v-if="counts">待审 {{counts.pending||0}} · 异常 {{counts.posting_failed||0}} · 补件 {{counts.needs_info||0}}</span><span v-else>查看待办</span><small v-if="failed">状态更新暂不可用</small></RouterLink></div></template><style scoped>.customer-business-reminder{margin:12px 0 16px;padding:12px 18px;border:1px solid #cbdfff;border-radius:12px;background:#f3f8ff;font-size:16px}.customer-business-reminder a{display:flex;flex-wrap:wrap;gap:12px;align-items:center;color:#2c5ca7;text-decoration:none}.customer-business-reminder:hover{box-shadow:0 0 0 3px #3c82ee18,0 5px 18px #4285ff20}.customer-business-reminder small{font-size:14px;color:#a26139}</style>
