<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { request } from '../api'
import type { BenefitScope, PoolProduct } from '../contentPools'
const props = defineProps<{ plan: number; room: number; tenant?: number; benefit: number; entityVersion: number; scope?: BenefitScope; products: PoolProduct[] }>()
const emit = defineEmits<{ saved: [scope: BenefitScope] }>()
const editing = ref(false), all = ref(false), selected = ref<number[]>([]), busy = ref(false), error = ref('')
let scopeVersion = 0
const label = computed(() => props.scope?.all ? '全部商品（含以后新增商品）' : props.products.filter(p => props.scope?.products?.includes(p.id)).map(p => `${p.link}号 ${p.name}`).join('、') || '正在读取适用范围…')
watch(() => [props.benefit, props.plan], () => { editing.value = false; error.value = '' })
function edit() { if (!props.scope) return; all.value = props.scope.all; selected.value = [...(props.scope.products || [])]; scopeVersion = props.scope.version_no || 0; editing.value = true; error.value = '' }
async function save() {
  if (busy.value || (!all.value && !selected.value.length)) return
  const owner = `${props.plan}:${props.benefit}`
  const query = new URLSearchParams({ room_id: String(props.room) }); if (props.tenant) query.set('tenant_id', String(props.tenant))
  busy.value = true; error.value = ''
  try {
    const saved = await request<BenefitScope>(`/api/v1/live-agent-plans/${props.plan}/benefits/${props.benefit}/scope?${query}`, { method: 'PUT', suppressErrorDialog: true, body: JSON.stringify({ all: all.value, products: selected.value, expected_version: props.entityVersion, expected_scope_version: scopeVersion }) })
    if (owner === `${props.plan}:${props.benefit}`) { editing.value = false; emit('saved', saved) }
  } catch (err) { if (owner === `${props.plan}:${props.benefit}`) error.value = err instanceof Error ? err.message : '保存范围失败' }
  finally { busy.value = false }
}
</script>
<template>
  <div class="benefit-scope"><span>适用商品：{{ label }}</span><button v-if="!editing" type="button" :disabled="!scope" @click="edit">修改范围</button>
    <div v-if="editing" class="scope-options"><label><input v-model="all" type="checkbox" :disabled="busy" />全部商品（含以后新增商品）</label><div v-if="!all" class="scope-products"><label v-for="product in products" :key="product.id"><input v-model="selected" type="checkbox" :value="product.id" :disabled="busy" />{{ product.link }}号 {{ product.name }}</label></div><div><button type="button" :disabled="busy || (!all && !selected.length)" @click="save">{{ busy ? '保存中…' : '保存范围' }}</button><button type="button" :disabled="busy" @click="editing = false">取消</button></div></div>
    <p v-if="error" role="alert">{{ error }}</p>
  </div>
</template>
<style scoped>
.benefit-scope{font-size:14px;line-height:1.7;overflow-wrap:anywhere}.benefit-scope button{padding:4px 10px;margin-left:8px;border:1px solid #c9cefa;border-radius:6px;background:white;color:#605ad0;cursor:pointer}.benefit-scope button:disabled{opacity:.5}.scope-options{padding:12px;background:#f5f6ff;border-radius:8px;margin:8px 0}.scope-options label{display:flex;align-items:center;gap:6px}.scope-products{max-height:240px;overflow:auto;margin:10px 0;display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:8px}.benefit-scope p{color:#aa3444}
</style>
