<script setup lang="ts">
import { computed, ref } from 'vue'
import type { LiveAgentPlanFact } from '../types'
import { factCategoryGroups } from '../factCategories'
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
const groups = computed(() => factCategoryGroups(props.fact.category))

async function change(event: Event) {
  const control = event.target as HTMLSelectElement
  const value = control.value
  control.value = selected.value
  if (saving.value || props.disabled || !value || value === selected.value) return
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
    <select :value="selected" :disabled="disabled || saving" :aria-label="'修改“' + fact.key + '”的事实类型'" @change="change">
      <option v-if="!fact.category" value="" disabled>请选择类型</option>
      <optgroup v-for="group in groups" :key="group.label" :label="group.label">
        <option v-for="option in group.options" :key="option.value" :value="option.value">{{ option.label }}</option>
      </optgroup>
    </select>
    <span v-if="saving" role="status">保存中…</span>
    <span v-if="error" class="error" role="alert">{{ error }}</span>
    <span v-else-if="suggestion && suggestion.low_conf" class="warn" role="alert">
      系统判断可能不准，请再确认：{{ suggestion.reason || '该内容可能属于多个类型' }}
    </span>
  </div>
</template>

<style scoped>
.fact-type-select { display:grid;gap:6px;min-width:0;font-size:16px;line-height:1.5; }
.fact-type-select select { width:100%;box-sizing:border-box;min-height:42px;border:1px solid #ccd5e5;border-radius:9px;padding:7px 9px;background:#fff;color:#111;font:inherit;font-size:16px; }
.fact-type-select select:focus-visible { outline:2px solid #6557ff;outline-offset:2px; }
.fact-type-select span { color:#526080;overflow-wrap:anywhere;font-size:16px; }
.fact-type-select .error { color:#c33; }
.fact-type-select .warn { color:#c0392b;font-size:14px;line-height:1.4; }
</style>
