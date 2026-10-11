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
  <div class="benefit-scope">
    <div class="scope-summary"><span>适用商品：{{ label }}</span><button v-if="!editing" type="button" :disabled="!scope" @click="edit">修改范围</button></div>
    <div v-if="editing" class="scope-options">
      <label class="scope-option scope-all" :class="{ selected: all }">
        <input v-model="all" type="checkbox" :disabled="busy" />
        <span><strong>全部商品</strong><small>包含以后新增的商品</small></span>
      </label>
      <div v-if="!all" class="scope-products">
        <label v-for="product in products" :key="product.id" class="scope-option" :class="{ selected: selected.includes(product.id) }">
          <input v-model="selected" type="checkbox" :value="product.id" :disabled="busy" />
          <span class="scope-product-copy"><b>{{ product.link }}号</b><span>{{ product.name }}</span></span>
        </label>
      </div>
      <div class="scope-actions">
        <small>{{ all ? '全部商品适用' : `已选 ${selected.length} 个商品` }}</small>
        <button type="button" :disabled="busy" @click="editing = false">取消</button>
        <button type="button" class="scope-save" :disabled="busy || (!all && !selected.length)" @click="save">{{ busy ? '保存中…' : '保存范围' }}</button>
      </div>
    </div>
    <p v-if="error" role="alert">{{ error }}</p>
  </div>
</template>
<style scoped>
.benefit-scope{width:100%;min-width:0;font-size:14px;line-height:1.7;overflow-wrap:anywhere}
.scope-summary{display:flex;align-items:center;flex-wrap:wrap;gap:8px}.scope-summary>span{min-width:0}
.benefit-scope button{flex:none;min-height:30px;padding:4px 10px;margin:0;border:1px solid #d8def4;border-radius:7px;background:#fff;color:#605ad0;font:inherit;font-size:12px;line-height:1.5;cursor:pointer}.benefit-scope button:disabled{opacity:.5;cursor:default}
.scope-options{display:grid;gap:8px;padding:10px;border:1px solid #e1e5f3;border-radius:10px;background:#f8f9fd;margin-top:10px}
.scope-option{display:flex;align-items:center;justify-content:flex-start;gap:9px;min-width:0;margin:0;padding:9px 10px;border:1px solid #e6e9f3;border-radius:8px;background:#fff;cursor:pointer;box-sizing:border-box}
.scope-option.selected{border-color:#c8cffb;background:#eef1ff}.scope-option:focus-within{outline:2px solid #98a4ee;outline-offset:1px}
.scope-option>input[type=checkbox]{display:block;flex:0 0 16px;width:16px!important;height:16px!important;min-width:16px;min-height:16px;max-width:16px;max-height:16px;padding:0!important;margin:0;accent-color:#6864df;cursor:pointer}
.scope-option>span{display:grid;gap:2px;min-width:0;flex:1;text-align:left}.scope-option strong{font-size:13px;font-weight:750;color:#3e4864}.scope-option small{font-size:11px;color:#8791a7;line-height:1.5}
.scope-products{display:grid;grid-template-columns:minmax(0,1fr);gap:6px;max-height:240px;overflow:auto;overscroll-behavior:contain}
.scope-option>.scope-product-copy{display:flex;align-items:baseline;justify-content:flex-start;gap:7px;color:#4c5974;font-size:13px;line-height:1.6}
.scope-product-copy>b{flex:none;color:#6864c8;font-size:12px;font-weight:750;white-space:nowrap}.scope-product-copy>span{min-width:0;overflow-wrap:anywhere}
.scope-actions{display:flex;align-items:center;justify-content:flex-end;flex-wrap:wrap;gap:7px;padding-top:8px;border-top:1px solid #e7eaf2}.scope-actions>small{margin-right:auto;color:#8b95a9;font-size:11px;line-height:1.5}
.scope-actions .scope-save{border-color:#6864df;background:#6864df;color:#fff}.benefit-scope button:focus-visible{outline:2px solid #98a4ee;outline-offset:2px}
.benefit-scope p{margin:8px 0 0;color:#aa3444;font-size:12px;line-height:1.6}
</style>
