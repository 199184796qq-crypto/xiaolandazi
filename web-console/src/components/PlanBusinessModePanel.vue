<script setup lang="ts">
import { planBusinessModes, type PlanBusinessMode, type PlanBusinessIndustryOption } from '../planBusinessMode'
defineProps<{
  modelValue: PlanBusinessMode | null
  industries: Record<PlanBusinessMode, string>
  industryOptions: Record<PlanBusinessMode, PlanBusinessIndustryOption[]>
  loading: boolean
  saving: boolean
  disabled: boolean
  error: string
}>()
const emit = defineEmits<{ select: [value: PlanBusinessMode]; selectIndustry: [mode: PlanBusinessMode, industry: string]; retry: [] }>()
function selectIndustry(mode: PlanBusinessMode, event: Event) {
  emit('selectIndustry', mode, (event.target as HTMLSelectElement).value)
}
</script>

<template>
  <section class="plan-business-mode-panel" aria-label="业务模式">
    <fieldset :disabled="disabled || loading || saving || !!error" :aria-busy="loading || saving">
      <legend>业务模式</legend>
      <p class="business-mode-intro">选择当前方案的业务模式，每次只能选择一项。</p>
      <div class="business-mode-options">
        <div v-for="option in planBusinessModes" :key="option.value" class="business-mode-option">
        <label class="business-mode-card" :class="{ selected: modelValue === option.value }">
          <input type="radio" name="plan-business-mode" :value="option.value" :checked="modelValue === option.value" @change="$emit('select', option.value)" />
          <svg class="business-mode-icon" viewBox="0 0 48 48" aria-hidden="true">
            <path v-if="option.value === 'ecommerce'" d="M6 8h5l5 23h22l4-16H13M20 39a2 2 0 1 1-4 0 2 2 0 0 1 4 0m18 0a2 2 0 1 1-4 0 2 2 0 0 1 4 0M24 9h12m-6-6v12" />
            <path v-else d="M7 21h34L37 9H11zM10 21v20h28V21M19 41V29h10v12M7 21a5 5 0 0 0 9 0 5 5 0 0 0 8 0 5 5 0 0 0 8 0 5 5 0 0 0 9 0" />
          </svg>
          <strong>{{ option.label }}</strong>
          <span>{{ option.description }}</span>
          <b aria-hidden="true">{{ modelValue === option.value ? '已选中' : '选择' }}</b>
        </label>
        <div class="business-industry-field">
          <label :for="'business-industry-' + option.value">{{ option.label }}行业</label>
          <select :id="'business-industry-' + option.value" :aria-label="option.label + '行业'" :value="industries[option.value]" @change="selectIndustry(option.value, $event)">
            <option v-if="!industries[option.value]" value="" disabled>{{ loading ? '正在读取…' : '请选择行业' }}</option>
            <option v-for="industry in industryOptions[option.value]" :key="industry.code" :value="industry.code">{{ industry.label }}</option>
          </select>
        </div>
        </div>
      </div>
    </fieldset>
    <div v-if="error" class="business-mode-error" role="alert">{{ error }}<button type="button" @click="$emit('retry')">重新读取</button></div>
    <p v-else class="business-mode-status" role="status" aria-live="polite">{{ disabled ? '请先选择智能体方案。' : loading ? '正在读取业务模式…' : saving ? '正在保存…' : '选择后自动保存到当前方案。' }}</p>
  </section>
</template>

<style scoped>
.plan-business-mode-panel{box-sizing:border-box;min-width:0;padding:28px;border:1px solid #e5e8fd;border-radius:22px;background:#fff;color:#253751;font-size:16px}
fieldset{min-width:0;margin:0;padding:0;border:0}legend{padding:0;font-size:24px;font-weight:750}.business-mode-intro{margin:12px 0 24px;color:#687797;font-size:16px;line-height:1.6}
.business-mode-options{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:24px}.business-mode-card{position:relative;display:grid;min-width:0;min-height:190px;align-content:center;justify-items:center;gap:12px;box-sizing:border-box;padding:28px 48px;border:2px solid #e5e8fa;border-radius:20px;background:#fbfcff;cursor:pointer;transition:border-color .15s,background .15s,box-shadow .15s}
.business-mode-card input{position:absolute;top:22px;right:22px;width:24px;height:24px;margin:0;accent-color:#584cff;cursor:pointer}.business-mode-icon{width:48px;height:48px;fill:none;stroke:currentColor;stroke-width:2.8;stroke-linecap:round;stroke-linejoin:round;color:#69789b}.business-mode-card strong{font-size:26px;line-height:1.4}.business-mode-card>span{font-size:16px;line-height:1.6;text-align:center;color:#7887a5}.business-mode-card b{font-size:16px;line-height:1.5;font-weight:650;color:#7e8ba8}.business-mode-card.selected{border-color:#665cff;background:#f0efff;box-shadow:0 8px 24px #6558ef14}.business-mode-card.selected strong,.business-mode-card.selected b,.business-mode-card.selected .business-mode-icon{color:#5549f4}.business-mode-card:focus-within{outline:3px solid #aaa5ff;outline-offset:3px}fieldset:disabled .business-mode-card{cursor:default;opacity:.65}fieldset:disabled input{cursor:default}
.business-mode-status{margin:20px 0 0;color:#6f7e9b;font-size:16px;line-height:1.6}.business-mode-error{margin-top:20px;padding:14px;border:1px solid #f0bcc7;border-radius:12px;background:#fff2f4;color:#b92e4a;font-size:16px;line-height:1.6;overflow-wrap:anywhere}.business-mode-error button{margin-left:12px;padding:6px 12px;border:1px solid #d5dafa;border-radius:8px;background:#fff;color:#5549d9;font:inherit;font-size:16px;cursor:pointer}
.business-mode-option{min-width:0}.business-industry-field{display:grid;gap:10px;margin-top:18px}.business-industry-field label{font-size:16px;font-weight:650;color:#495c80}.business-industry-field select{box-sizing:border-box;width:100%;min-width:0;min-height:50px;padding:12px 36px 12px 14px;border:1px solid #d5dcf4;border-radius:12px;background:#fff;color:#263751;font:inherit;font-size:16px;line-height:1.5}.business-industry-field select:focus-visible{outline:3px solid #aaa5ff;outline-offset:2px}.business-industry-field select:disabled{color:#75839c;background:#f5f7fc}
@media(max-width:700px){.plan-business-mode-panel{padding:20px 16px}.business-mode-options{grid-template-columns:minmax(0,1fr);gap:16px}.business-mode-card{min-height:180px;padding:24px 40px}.business-mode-card strong{font-size:24px}}
@media(prefers-reduced-motion:reduce){.business-mode-card{transition:none}}
</style>
