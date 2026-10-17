<script setup lang="ts">
import { computed, ref } from 'vue'
import type { LiveAgentPlanFact, LiveAgentPlanProductLink } from '../types'
const props = defineProps<{
  fact: LiveAgentPlanFact
  products: LiveAgentPlanProductLink[]
  disabled?: boolean
  save: (fact: LiveAgentPlanFact, scope: NonNullable<LiveAgentPlanFact['scope']>) => Promise<void>
}>()
const saving = ref(false)
const error = ref('')
const selected = computed(() => props.fact.scope?.kind === 'common' ? 'common' : props.fact.scope?.kind === 'product' ? String(props.fact.scope.product_link_id) : '')
const products = computed(() => props.products.filter(p => p.tenant_id === props.fact.tenant_id && p.plan_id === props.fact.plan_id && p.status === 'active'))
const unavailable = computed(() => props.fact.scope?.kind === 'product' && !products.value.some(p => p.id === props.fact.scope?.product_link_id))
function linkLabel(key: string) { return /^\d+$/.test(key) ? key + '号链接' : key }
async function change(event: Event) {
  const control = event.target as HTMLSelectElement
  const value = control.value
  control.value = selected.value
  if (saving.value || props.disabled || value === selected.value || !value) return
  saving.value = true
  error.value = ''
  try {
    await props.save(props.fact, value === 'common' ? { kind: 'common' } : { kind: 'product', product_link_id: Number(value) })
  } catch (e) {
    error.value = e instanceof Error ? e.message : '保存归属失败，请重试'
  } finally { saving.value = false }
}
</script>

<template>
  <div class="fact-scope-select">
    <select :value="selected" :disabled="disabled || saving" :aria-label="'修改“' + fact.key + '”的归属范围'" @change="change">
      <option v-if="!fact.scope" value="" disabled>请选择归属</option>
      <option value="common">公共</option>
      <option v-if="unavailable" :value="String(fact.scope?.product_link_id)" disabled>原关联商品已不可用</option>
      <option v-for="p in products" :key="p.id" :value="String(p.id)">{{ linkLabel(p.link_key) }}</option>
    </select>
    <span v-if="saving" role="status">保存中…</span>
    <span v-if="error" class="error" role="alert">{{ error }}</span>
  </div>
</template>

<style scoped>
.fact-scope-select { display:grid;gap:6px;min-width:0;font-size:16px;line-height:1.5; }
.fact-scope-select select { width:100%;box-sizing:border-box;min-height:42px;border:1px solid #ccd5e5;border-radius:9px;padding:7px 9px;background:#fff;color:#111;font:inherit;font-size:16px; }
.fact-scope-select select:focus-visible { outline:2px solid #6557ff;outline-offset:2px; }
.fact-scope-select span { color:#526080;overflow-wrap:anywhere;font-size:16px; }
.fact-scope-select .error { color:#c33; }
</style>
