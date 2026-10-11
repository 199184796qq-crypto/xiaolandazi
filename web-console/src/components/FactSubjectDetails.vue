<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { getLectures, type LectureTask } from '../lectureTasks'
const props=defineProps<{planId:number;tenantId?:number;factId:number}>()
const opened=ref(false),loading=ref(false),error=ref(''),tasks=ref<LectureTask[]>([])
let epoch=0
watch(()=>[props.planId,props.tenantId,props.factId],()=>{epoch++;opened.value=false;tasks.value=[];error.value='';loading.value=false})
onBeforeUnmount(()=>{epoch++})
async function toggle(){opened.value=!opened.value;if(!opened.value)return;const owner=epoch;loading.value=true;error.value='';try{const lib=await getLectures(props.planId,props.tenantId);if(owner===epoch)tasks.value=lib.tasks.filter(t=>t.root_fact?.id===props.factId)}catch(e){if(owner===epoch)error.value=e instanceof Error?e.message:'读取二级资料失败'}finally{if(owner===epoch)loading.value=false}}
</script>
<template>
 <div class="fact-subject-details"><button type="button" :aria-expanded="opened" @click="toggle">{{opened?'收起二级事实依据':'查看二级事实依据'}}</button>
 <div v-if="opened" class="subject-content"><p>仅用于重新生成对应对象的讲解，约束含义与边界；不是固定话术。</p><p v-if="loading">读取中…</p><p v-if="error" role="alert">{{error}}</p><section v-for="task in tasks" :key="task.id"><strong>{{task.subject||task.title}}</strong><small v-if="!task.valid">一级事实已变化，需重新整理后再讲解。</small><ul><li v-for="f in task.facts" :key="f.id"><b>{{f.name}}：</b>{{f.text}}</li></ul></section><p v-if="!loading&&!error&&!tasks.length">暂无已关联的二级资料。</p></div>
 </div>
</template>
<style scoped>
.fact-subject-details{margin-top:12px;font-size:14px}.fact-subject-details button{padding:6px 10px;border:1px solid #d7def4;border-radius:8px;color:#6960ce;background:#f8f7ff;font:inherit;cursor:pointer}.subject-content{margin-top:10px;padding:14px;border:1px solid #e4e3f4;border-radius:12px;background:#fafaff;white-space:normal;line-height:1.7}.subject-content p{font-size:13px;color:#7c86a5}.subject-content ul{margin:8px 0;padding-left:19px}.subject-content li+li{margin-top:8px}.subject-content small{display:block;color:#ab5770}
</style>
