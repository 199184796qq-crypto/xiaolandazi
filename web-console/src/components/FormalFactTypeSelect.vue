<script setup lang="ts">
import { computed, ref } from 'vue'
import type { LiveAgentPlanFact } from '../types'
import { factCategoryGroups, factCategoryState } from '../factCategories'
import type { FactTypeSuggestion } from '../api'

const props = defineProps<{
  fact: LiveAgentPlanFact
  disabled?: boolean
  save: (fact: LiveAgentPlanFact, category: string) => Promise<void>
  suggestion?: FactTypeSuggestion | null
}>()

const saving = ref(false)
const error = ref('')
const selected = computed(() => props.fact.category || '')
const definition = computed(() => factCategoryState.items.find(item => item.value === selected.value))
const selectedLabel = computed(() => definition.value?.label || (factCategoryState.loading ? '正在加载分类…' : factCategoryState.error ? '分类加载失败' : selected.value ? '未匹配分类' : '请选择类型'))
const categoryDisabled = computed(() => props.disabled || saving.value || factCategoryState.loading || !!factCategoryState.error || !factCategoryState.items.length)
const groups = computed(() => factCategoryGroups(props.fact.category).map(group => ({ ...group, options: group.options.map(option => ({ ...option, label: factCategoryState.items.some(item => item.value === option.value) ? option.label : '未匹配分类' })) })))

async function change(event: Event) {
  const control = event.target as HTMLSelectElement
  const value = control.value
  control.value = selected.value
  if (categoryDisabled.value || !value || value === selected.value) return
  saving.value = true
  error.value = ''
  try {
    await props.save(props.fact, value)
  } catch (e) {
    error.value = e instanceof Error ? e.message : '保存事实类型失败，请重试'
  } finally { saving.value = false }
}
</script>

<template>
  <div class="fact-type-select">
    <div class="fact-type-control">
    <span class="fact-type-selected" aria-hidden="true">{{ selectedLabel }}</span>
    <select :value="selected" :title="definition ? selectedLabel : selectedLabel + (selected ? '（原标识：' + selected + '）' : '')" :disabled="categoryDisabled" :aria-label="'修改“' + fact.key + '”的事实类型'" @change="change">
      <option v-if="!fact.category" value="" disabled>请选择类型</option>
      <optgroup v-for="group in groups" :key="group.label" :label="group.label">
        <option v-for="option in group.options" :key="option.value" :value="option.value">{{ option.label }}</option>
      </optgroup>
    </select>
    <i class="fact-type-caret" aria-hidden="true"></i>
    </div>
    <span v-if="saving" role="status">保存中…</span>
    <span v-if="error" class="error" role="alert">{{ error }}</span>
    <span v-else-if="suggestion && suggestion.low_conf" class="warn" role="alert">
      系统判断可能不准，请再确认：{{ suggestion.reason || '该内容可能属于多个类型' }}
    </span>
  </div>
</template>

<style scoped>
.fact-type-select { display:grid;gap:6px;min-width:0;font-size:16px;line-height:1.5; }
.fact-type-control { position:relative;min-width:0;background:#fff;border-radius:9px; }
.fact-type-select .fact-type-selected { display:block;box-sizing:border-box;min-height:42px;padding:8px 31px 8px 11px;color:#111;font-size:16px;line-height:24px;white-space:normal;overflow-wrap:anywhere; }
.fact-type-control select { position:absolute;inset:0;width:100%;height:100%;box-sizing:border-box;appearance:none;border:1px solid #ccd5e5;border-radius:9px;padding:7px 30px 7px 9px;background:transparent;color:transparent;font:inherit;font-size:16px;cursor:pointer; }
.fact-type-control select option,.fact-type-control select optgroup { background:#fff;color:#111;font-size:16px; }
.fact-type-control select:disabled { cursor:default; }
.fact-type-caret { position:absolute;right:13px;top:18px;width:7px;height:7px;border-right:2px solid #34405a;border-bottom:2px solid #34405a;transform:rotate(45deg);pointer-events:none; }
.fact-type-select select:focus-visible { outline:2px solid #6557ff;outline-offset:2px; }
.fact-type-select span { color:#526080;overflow-wrap:anywhere;font-size:16px; }
.fact-type-select .error { color:#c33; }
.fact-type-select .warn { color:#c0392b;font-size:16px;line-height:1.4; }
</style>
