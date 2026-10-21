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
const products = computed(() => props.products.filter(p => p.tenant_id === props.fact.tenant_id && p.plan_id === props.fact.plan_id && p.status === 'active'))
// 归属有两种：公共，或一个/多个商品链接。多链接表示「这几个品都适用」，
// 也是「这两个品可以对比」的唯一依据——事实里没写明，模型就不许比较。
const selectedIDs = computed(() => {
  const scope = props.fact.scope
  if (!scope || scope.kind !== 'product') return [] as number[]
  if (scope.product_link_ids?.length) return scope.product_link_ids.filter(id => id > 0)
  return scope.product_link_id ? [scope.product_link_id] : []
})
const isCommon = computed(() => props.fact.scope?.kind === 'common')
const unavailable = computed(() => selectedIDs.value.filter(id => !products.value.some(p => p.id === id)))
function linkLabel(key: string) { return /^\d+$/.test(key) ? key + '链接' : key }
function scopeSummary() {
  if (isCommon.value) return '公共（所有商品都适用）'
  if (!props.fact.scope) return '未设置'
  const labels = products.value.filter(p => selectedIDs.value.includes(p.id)).map(p => linkLabel(p.link_key))
  if (!labels.length) return '原关联商品已不可用'
  return labels.join(' ＋ ')
}
async function apply(scope: NonNullable<LiveAgentPlanFact['scope']>) {
  if (saving.value || props.disabled) return
  saving.value = true
  error.value = ''
  try {
    await props.save(props.fact, scope)
  } catch (e) {
    error.value = e instanceof Error ? e.message : '保存归属失败，请重试'
  } finally { saving.value = false }
}
function chooseCommon() {
  if (isCommon.value) return
  apply({ kind: 'common' })
}
function toggle(id: number, checked: boolean) {
  const next = selectedIDs.value.filter(item => item !== id)
  if (checked) next.push(id)
  if (!next.length) return
  apply({ kind: 'product', product_link_id: next.length === 1 ? next[0] : 0, product_link_ids: next })
}
</script>

<template>
  <div class="fact-scope-select">
    <details class="scope-picker">
      <summary :aria-label="'修改“' + fact.key + '”的归属范围'">
        <span class="scope-summary-text">{{ scopeSummary() }}</span>
        <span v-if="saving" role="status">保存中…</span>
        <span v-else class="scope-hint">修改</span>
      </summary>
      <div class="scope-options">
        <label class="scope-option" :class="{ checked: isCommon }">
          <input type="radio" :checked="isCommon" :disabled="disabled || saving" @change="chooseCommon" />
          <span>公共（所有商品都适用）</span>
        </label>
        <label v-for="p in products" :key="p.id" class="scope-option" :class="{ checked: selectedIDs.includes(p.id) }">
          <input type="checkbox" :checked="selectedIDs.includes(p.id)" :disabled="disabled || saving" @change="toggle(p.id, ($event.target as HTMLInputElement).checked)" />
          <span>{{ linkLabel(p.link_key) }}</span>
        </label>
        <label v-for="id in unavailable" :key="'gone-' + id" class="scope-option is-gone">
          <input type="checkbox" checked disabled />
          <span>原关联商品已不可用</span>
        </label>
        <p class="scope-tip">勾选两个及以上表示「这几个品都适用」，也代表允许它们互相对比。</p>
      </div>
    </details>
    <span v-if="error" class="error" role="alert">{{ error }}</span>
  </div>
</template>

<style scoped>
.fact-scope-select { display:grid;gap:6px;min-width:0;font-size:16px;line-height:1.5; }
.scope-picker { position:relative; }
.scope-picker>summary { display:flex;align-items:center;justify-content:space-between;gap:8px;min-height:42px;box-sizing:border-box;border:1px solid #ccd5e5;border-radius:9px;padding:7px 9px;background:#fff;color:#111;cursor:pointer;list-style:none; }
.scope-picker>summary::-webkit-details-marker { display:none; }
.scope-picker>summary:focus-visible { outline:2px solid #6557ff;outline-offset:2px; }
.scope-summary-text { min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap; }
.scope-hint { flex:none;color:#6557ff;font-size:14px;font-weight:700; }
.scope-options { display:grid;gap:4px;margin-top:6px;padding:8px;border:1px solid #dbe2f1;border-radius:10px;background:#f8faff; }
.scope-option { display:flex;align-items:center;gap:8px;padding:6px 8px;border-radius:8px;cursor:pointer;color:#2a3550; }
.scope-option:hover { background:#eef1ff; }
.scope-option.checked { background:#e9ecff;font-weight:700; }
.scope-option.is-gone { color:#a05; }
.scope-option input { width:18px;height:18px;accent-color:#6557ff; }
.scope-tip { margin:4px 0 0;color:#7a869b;font-size:13px;line-height:1.5; }
.fact-scope-select span { color:#526080;overflow-wrap:anywhere;font-size:16px; }
.fact-scope-select .error { color:#c33; }
</style>
