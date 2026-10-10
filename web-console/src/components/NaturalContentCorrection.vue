<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { request } from '../api'
import { contentPoolURL, type PoolStatus } from '../contentPools'
const props = defineProps<{ planId: number; roomId: number; tenantId?: number; setId?: number; poolKey?: string; taskId?: string; selectedText: string; productScope?: number[]; disabled?: boolean }>()
const emit = defineEmits<{ saved: []; busy: [value: boolean] }>()
const opened=ref(false), feedback=ref(''), clarification=ref(''), notice=ref(''), error=ref(''), busy=ref(false), products=ref<PoolStatus['products']>([]), selectedProducts=ref<number[]>([])
let epoch=0, timer:ReturnType<typeof setTimeout>|undefined
watch(()=>[props.planId,props.roomId,props.taskId,props.selectedText],()=>{epoch++;if(timer)clearTimeout(timer);busy.value=false;emit('busy',false);opened.value=false;feedback.value='';clarification.value='';notice.value='';error.value='';products.value=[];selectedProducts.value=[]})
onBeforeUnmount(()=>{epoch++;if(timer)clearTimeout(timer)})
async function open(){opened.value=!opened.value;if(!opened.value||props.taskId)return;const token=epoch;try{const s=await request<PoolStatus>(contentPoolURL(props.planId,props.roomId,props.tenantId),{suppressErrorDialog:true});if(token!==epoch)return;products.value=s.products;if(props.productScope?.length)selectedProducts.value=[...props.productScope];else if(s.products.length===1)selectedProducts.value=[s.products[0].id]}catch(e){if(token===epoch)error.value=e instanceof Error?e.message:'读取商品范围失败'}}
async function poll(owner:number, initial:number, correctionId:string){
 if(owner!==epoch)return
 try{const status=await request<PoolStatus>(contentPoolURL(props.planId,props.roomId,props.tenantId),{suppressErrorDialog:true});if(owner!==epoch)return
  if(status.state==='updating'){notice.value=status.job.stage||'正在修订关联内容…';timer=setTimeout(()=>{timer=undefined;void poll(owner,initial,correctionId)},3000);return}
  if(status.set_id>initial&&status.corrections?.some(item=>item.id===correctionId)&&['ready','synced'].includes(status.state)){notice.value='纠正和关联内容已保存。保存智能体方案后，下次开播使用新版本。';clarification.value='';emit('saved')}
  else error.value=status.job.error||'纠正尚未完整保存，请重试；原方案保持可用。'
 }catch(e){if(owner===epoch)error.value=e instanceof Error?e.message:'读取更新结果失败，请刷新查看'}
 finally{if(owner===epoch&&!timer){busy.value=false;emit('busy',false)}}
}
async function submit(){
 if(busy.value||!feedback.value.trim())return
 const owner=epoch;busy.value=true;emit('busy',true);error.value='';notice.value='正在理解你的纠正要求…'
 try{const status=await request<PoolStatus>(contentPoolURL(props.planId,props.roomId,props.tenantId),{suppressErrorDialog:true});if(owner!==epoch)return
  const result=await request<{state:string;summary:string;question?:string;correction?:{id:string}}>(contentPoolURL(props.planId,props.roomId,props.tenantId).replace('/content-pools?','/content-corrections?'),{method:'POST',body:JSON.stringify({expected_set_id:props.setId||status.set_id,pool_key:props.poolKey||'',task_id:props.taskId||'',selected_text:props.selectedText,products:selectedProducts.value,feedback:feedback.value.trim()}),suppressErrorDialog:true});if(owner!==epoch)return
  notice.value=result.summary
  if(result.state==='clarify'){clarification.value=result.question||'请补充这次纠正的具体要求。';return}
  timer=setTimeout(()=>{timer=undefined;void poll(owner,status.set_id,result.correction?.id||'')},1500)
 }catch(e){if(owner===epoch){error.value=e instanceof Error?e.message:'纠正未保存，请重试';notice.value=''}}
 finally{if(owner===epoch&&!timer){busy.value=false;emit('busy',false)}}
}
</script>
<template>
 <div class="natural-correction">
  <button type="button" :disabled="disabled||busy" @click="open">说哪里不对</button>
  <form v-if="opened" @submit.prevent="submit">
   <p class="selected">{{ selectedText }}</p>
   <fieldset v-if="!taskId&&products.length>1"><legend>这次纠正适用的商品</legend><label v-for="p in products" :key="p.id"><input v-model="selectedProducts" type="checkbox" :value="p.id" :disabled="busy"/>{{ p.link }}号 · {{ p.name }}</label></fieldset>
   <label>直接说哪里不对<textarea v-model="feedback" rows="3" maxlength="5000" :disabled="busy" placeholder="这段不要；或者说明真实情况、以后应该怎么说。无需填写固定格式。"/></label>
   <p v-if="clarification" class="clarify">{{ clarification }} 请在上面补充后再次提交。</p>
   <button type="submit" :disabled="busy||!feedback.trim()||(!taskId&&products.length>1&&!selectedProducts.length)">{{ busy?'正在处理…':'按我的要求处理' }}</button>
   <p v-if="notice" class="notice" role="status">{{ notice }}</p><p v-if="error" class="error" role="alert">{{ error }}</p>
  </form>
 </div>
</template>
<style scoped>
.natural-correction{margin-top:10px}.natural-correction button{padding:8px 12px;border:1px solid #d7def4;border-radius:9px;background:#fff;color:#5a65cc;font:inherit;cursor:pointer}.natural-correction button:disabled{opacity:.55;cursor:not-allowed}.natural-correction form{padding:14px;margin:10px 0;border:1px solid #dce4f6;border-radius:12px;background:#f8faff}.selected{font-size:13px;color:#7485a6;white-space:pre-wrap}.natural-correction textarea{box-sizing:border-box;display:block;width:100%;margin:8px 0;padding:10px;border:1px solid #d7def4;border-radius:8px;color:#344360;background:#fff;font:inherit;resize:vertical}.natural-correction fieldset{border:1px solid #d7def4;margin:10px 0;border-radius:8px}.natural-correction fieldset label{display:block;padding:4px}.notice{color:#26816a}.error,.clarify{color:#aa4659}
</style>
