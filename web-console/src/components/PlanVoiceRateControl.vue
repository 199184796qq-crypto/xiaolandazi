<script setup lang="ts">
import { planVoiceRateOptions } from '../planVoiceRate'
defineProps<{ value: number; busy: boolean; loading: boolean; ready: boolean; error: string }>()
const emit = defineEmits<{ change: [rate: number]; retry: [] }>()
</script>
<template>
  <section class="plan-voice-rate-setting" aria-label="方案声音语速">
    <label><span>声音语速</span><select aria-label="声音语速" :value="value" :disabled="busy || loading || !ready" @change="emit('change', Number(($event.target as HTMLSelectElement).value))"><option v-for="rate in planVoiceRateOptions" :key="rate" :value="rate">{{ rate.toFixed(2) }}</option></select></label>
    <span v-if="loading" role="status">读取中…</span><span v-else-if="busy" role="status">保存中…</span><span v-else-if="ready && !error" role="status">自动保存 · 下次开播使用</span>
    <span v-if="error" class="plan-voice-rate-error" role="alert">{{ error }}</span>
    <button v-if="error && !ready" type="button" :disabled="loading" @click="emit('retry')">重新读取</button>
  </section>
</template>
<style scoped>
.plan-voice-rate-setting{display:flex;align-items:center;flex-wrap:wrap;gap:12px;padding:18px 22px;margin-bottom:18px;border:1px solid #e0e5ff;border-radius:16px;background:#f8f9ff;color:#66748f;font-size:16px}
label{display:flex;align-items:center;gap:12px;font-size:16px;font-weight:600;color:#46557b}
select,button{min-height:42px;padding:8px 14px;border:1px solid #cfd8f3;border-radius:10px;background:white;color:#405075;font:inherit;font-size:16px}
select{min-width:100px}select:disabled,button:disabled{opacity:.55}.plan-voice-rate-error{color:#b53750;overflow-wrap:anywhere}
</style>
