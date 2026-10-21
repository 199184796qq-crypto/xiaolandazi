<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import type { LiveAgentPlanFact, LiveAgentPlanProductLink } from '../types'
const props = defineProps<{
  fact: LiveAgentPlanFact
  products: LiveAgentPlanProductLink[]
  disabled?: boolean
  save: (fact: LiveAgentPlanFact, scope: NonNullable<LiveAgentPlanFact['scope']>) => Promise<void>
}>()
const saving = ref(false)
const error = ref('')
const open = ref(false)
const trigger = ref<HTMLButtonElement | null>(null)
const menu = ref<HTMLDivElement | null>(null)
const menuStyle = ref<Record<string, string>>({})
const products = computed(() => props.products.filter(p => p.tenant_id === props.fact.tenant_id && p.plan_id === props.fact.plan_id && p.status === 'active'))
const selectedIDs = computed(() => {
  const scope = props.fact.scope
  if (!scope || scope.kind !== 'product') return [] as number[]
  if (scope.product_link_ids?.length) return scope.product_link_ids.filter(id => id > 0)
  return scope.product_link_id ? [scope.product_link_id] : []
})
const isCommon = computed(() => props.fact.scope?.kind === 'common')
const unavailable = computed(() => selectedIDs.value.filter(id => !products.value.some(p => p.id === id)))
function linkLabel(key: string) { return /^\d+$/.test(key) ? key + '链接' : key }
// 折叠态只显示最简归属：公共 / 具体链接 / 多链。
function scopeSummary() {
  if (isCommon.value) return '公共'
  if (!props.fact.scope) return '未设置'
  if (selectedIDs.value.length > 1) return '多链'
  const linked = products.value.find(p => p.id === selectedIDs.value[0])
  return linked ? linkLabel(linked.link_key) : '已失效'
}
function close() { open.value = false }
function toggleOpen() {
  if (props.disabled) return
  if (open.value) { close(); return }
  const rect = trigger.value?.getBoundingClientRect()
  if (rect) {
    const estimated = 44 + products.value.length * 30
    const below = window.innerHeight - rect.bottom > estimated + 12
    menuStyle.value = {
      left: Math.round(rect.left) + 'px',
      top: Math.round(below ? rect.bottom + 4 : Math.max(8, rect.top - estimated - 4)) + 'px',
      minWidth: Math.max(150, Math.round(rect.width)) + 'px',
    }
  }
  open.value = true
}
async function apply(scope: NonNullable<LiveAgentPlanFact['scope']>) {
  if (saving.value || props.disabled) return
  saving.value = true
  error.value = ''
  try {
    await props.save(props.fact, scope)
    close()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '保存归属失败，请重试'
  } finally { saving.value = false }
}
function chooseCommon() {
  if (isCommon.value) { close(); return }
  apply({ kind: 'common' })
}
function toggle(id: number, checked: boolean) {
  const next = selectedIDs.value.filter(item => item !== id)
  if (checked) next.push(id)
  if (!next.length) return
  apply({ kind: 'product', product_link_id: next.length === 1 ? next[0] : 0, product_link_ids: next })
}
// 点到外面、滚动或缩放窗口都收起，避免浮层停在半空。
function onPointerDown(event: PointerEvent) {
  if (!open.value) return
  const target = event.target as Node
  if (menu.value?.contains(target) || trigger.value?.contains(target)) return
  close()
}
function onViewportChange() { if (open.value) close() }
onMounted(() => {
  document.addEventListener('pointerdown', onPointerDown, true)
  window.addEventListener('scroll', onViewportChange, true)
  window.addEventListener('resize', onViewportChange)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onPointerDown, true)
  window.removeEventListener('scroll', onViewportChange, true)
  window.removeEventListener('resize', onViewportChange)
})
</script>

<template>
  <div class="fact-scope-select">
    <button ref="trigger" type="button" class="scope-trigger" :class="{ open, disabled }" :disabled="disabled" :aria-expanded="open" :aria-label="'修改“' + fact.key + '”的归属范围'" @click.stop="toggleOpen">
      <span class="scope-summary-text">{{ scopeSummary() }}</span>
      <span v-if="saving" class="scope-saving" role="status">保存中</span>
    </button>
    <Teleport to="body">
      <div v-if="open" ref="menu" class="scope-menu" :style="menuStyle" role="listbox" @pointerdown.stop>
        <button type="button" class="scope-option" :class="{ checked: isCommon }" :disabled="saving" @click="chooseCommon">
          <i class="scope-mark" :class="{ on: isCommon, radio: true }"></i><span>公共</span>
        </button>
        <button v-for="p in products" :key="p.id" type="button" class="scope-option" :class="{ checked: selectedIDs.includes(p.id) }" :disabled="saving" @click="toggle(p.id, !selectedIDs.includes(p.id))">
          <i class="scope-mark" :class="{ on: selectedIDs.includes(p.id) }"></i><span>{{ linkLabel(p.link_key) }}</span>
        </button>
        <div v-for="id in unavailable" :key="'gone-' + id" class="scope-option is-gone">
          <i class="scope-mark on"></i><span>原关联商品已不可用</span>
        </div>
      </div>
    </Teleport>
    <span v-if="error" class="error" role="alert">{{ error }}</span>
  </div>
</template>

<style scoped>
.fact-scope-select { display:grid;gap:3px;min-width:0;font-size:15px;line-height:1.4; }
.scope-trigger { display:inline-flex;align-items:center;gap:6px;max-width:100%;box-sizing:border-box;min-height:32px;border:1px solid #d7deec;border-radius:8px;padding:3px 10px;background:#fff;color:#233;font:inherit;cursor:pointer; }
.scope-trigger:hover:not(:disabled) { border-color:#9aa5f0;background:#fafbff; }
.scope-trigger.open { border-color:#6557ff; }
.scope-trigger:focus-visible { outline:2px solid #6557ff;outline-offset:2px; }
.scope-trigger:disabled { opacity:.6;cursor:default; }
.scope-summary-text { min-width:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis; }
.scope-saving { flex:none;color:#6557ff;font-size:12px; }
.fact-scope-select .error { color:#c33;font-size:13px; }
</style>

<style scoped>
.scope-menu { position:fixed;z-index:1200;display:grid;gap:1px;padding:6px;border:1px solid #dbe2f1;border-radius:10px;background:#fff;box-shadow:0 10px 26px #5b6ba526; }
.scope-menu .scope-option { display:flex;align-items:center;gap:8px;width:100%;box-sizing:border-box;padding:6px 9px;border:0;border-radius:7px;background:transparent;color:#2a3550;font:inherit;text-align:left;white-space:nowrap;cursor:pointer; }
.scope-menu .scope-option:hover:not(:disabled) { background:#f2f4ff; }
.scope-menu .scope-option.checked { background:#e9ecff;font-weight:700; }
.scope-menu .scope-option.is-gone { color:#a05;cursor:default; }
.scope-menu .scope-option:disabled { opacity:.6;cursor:default; }
.scope-mark { flex:none;width:14px;height:14px;border:1px solid #b9c3d8;border-radius:3px;background:#fff; }
.scope-mark.radio { border-radius:50%; }
.scope-mark.on { border-color:#6557ff;background:#6557ff;box-shadow:inset 0 0 0 3px #fff; }
</style>
