<script setup lang="ts">
import {onBeforeUnmount,onMounted,ref,watch} from 'vue'
import {businessAccounts} from '../customerBusinessApi'
import type {BusinessAccount} from '../customerBusinessApi'
import PaginationBar from './PaginationBar.vue'
const props=defineProps<{modelValue:number}>();const emit=defineEmits<{'update:modelValue':[number]}>()
const items=ref<BusinessAccount[]>([]),search=ref(''),page=ref(1),total=ref(0),pages=ref(1),loading=ref(false),error=ref('');let controller:AbortController|undefined,timer:ReturnType<typeof setTimeout>|undefined
async function load(){controller?.abort();const c=new AbortController();controller=c;loading.value=true;error.value='';try{const r=await businessAccounts({search:search.value,page:page.value,page_size:12},c.signal);if(!c.signal.aborted){items.value=r.items;total.value=r.total;pages.value=Math.max(1,r.total_pages)}}catch(e){if(!c.signal.aborted)error.value=e instanceof Error?e.message:'读取失败'}finally{if(controller===c)loading.value=false}}
watch(search,()=>{clearTimeout(timer);timer=setTimeout(()=>{page.value=1;void load()},300)});watch(page,load);onMounted(load);onBeforeUnmount(()=>{controller?.abort();clearTimeout(timer)})
</script>
<template><div class="cb-picker"><label>选择客户（仅可见范围）<input v-model="search" class="sb-search" placeholder="搜索名称或账号" /></label><p v-if="error" class="sb-error">{{error}} <button type="button" @click="load">重试</button></p><p v-else-if="loading">读取客户中…</p><div v-else class="sb-actions"><button v-for="v in items" :key="v.tenant_id" type="button" :class="{primary:props.modelValue===v.tenant_id}" :aria-pressed="props.modelValue===v.tenant_id" @click="emit('update:modelValue',v.tenant_id)">{{v.name}} · @{{v.username}}</button><span v-if="!items.length">无匹配客户</span></div><PaginationBar v-model:page="page" :total="total" :total-pages="pages" :page-size="12"/><small>当前选择：客户 #{{modelValue||'未选择'}}</small></div></template>
