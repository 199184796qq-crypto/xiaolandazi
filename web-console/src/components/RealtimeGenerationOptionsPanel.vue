<script setup lang="ts">
import { computed } from 'vue'
import { realtimeGenerationOptionsError, type RealtimeGenerationOptions } from '../realtimeGenerationOptions'
const props = defineProps<{ modelValue: RealtimeGenerationOptions | null; loading?: boolean; disabled?: boolean; loadError?: string; changed?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [RealtimeGenerationOptions]; retry: [] }>()
const error = computed(() => props.loadError || (props.modelValue ? realtimeGenerationOptionsError(props.modelValue) : ''))
function update(key: keyof RealtimeGenerationOptions, event: Event) {
  if (!props.modelValue) return
  emit('update:modelValue', { ...props.modelValue, [key]: (event.target as HTMLInputElement).valueAsNumber })
}
</script>

<template>
  <section class="realtime-options-panel" aria-label="实时声音生成参数">
    <fieldset class="realtime-options-fields" :disabled="loading || disabled || !modelValue">
      <label><strong>正式主线缓存</strong><span class="option-input"><input :value="modelValue?.buffer_seconds" @input="update('buffer_seconds', $event)" type="number" min="20" max="600" step="1" placeholder="读取中" aria-label="正式主线缓存秒数"><em>秒</em></span><small>按实际声音时长计算，达到上限后等待消耗；单次在途段可能略超。</small></label>
      <div class="body-count-field"><strong>中间讲解段数</strong><span class="option-input body-range"><input :value="modelValue?.body_min" @input="update('body_min', $event)" type="number" min="2" max="6" step="1" placeholder="最少" aria-label="中间讲解最少段数"><b>～</b><input :value="modelValue?.body_max" @input="update('body_max', $event)" type="number" min="2" max="6" step="1" placeholder="最多" aria-label="中间讲解最多段数"><em>段</em></span><small>范围为2～6段，最少不能超过最多；每轮按范围调度。</small></div>
      <label><strong>单段最大字数</strong><span class="option-input"><input :value="modelValue?.max_chars" @input="update('max_chars', $event)" type="number" min="80" max="1000" step="1" placeholder="读取中" aria-label="单段最大字数"><em>字</em></span><small>限制每段生成字数，不改变主播风格或模型选择。</small></label>
    </fieldset>
    <p v-if="error" class="options-error" role="alert">{{ error }} <button v-if="loadError" type="button" :disabled="loading" @click="emit('retry')">重新读取</button></p>
    <p class="options-hint">{{ loading ? '正在读取参数…' : changed ? '有未保存参数，请保存智能体方案；下次开播直接使用。' : '参数跟随当前方案保存，不影响其它方案。' }} 当前场次保持原参数，保存不会打断直播。</p>
  </section>
</template>

<style scoped>
.realtime-options-panel{padding:18px 20px;border-radius:18px;background:linear-gradient(125deg,#eff9f9,#f2f7ff);border:1px solid #e1edf1;color:#53637b}.realtime-options-fields{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:24px;border:0;padding:0;margin:0;min-width:0}.realtime-options-fields>label,.body-count-field{display:grid;align-content:start;gap:9px;min-width:0}.realtime-options-fields strong{font-size:14px;font-weight:800;line-height:1.6}.option-input{display:flex;align-items:center;gap:8px;max-width:270px}.option-input input{width:100%;min-width:0;padding:10px 12px;border:1px solid #cedce7;border-radius:10px;background:#fff;color:#2f425f;font:inherit;font-size:17px;font-weight:650}.option-input input:focus{outline:2px solid #89afef;outline-offset:1px}.option-input em{font-style:normal;font-size:13px;white-space:nowrap}.body-range b{font-size:15px}.realtime-options-fields small{color:#7c8aa1;font-size:12px;line-height:1.7}.options-hint{margin:13px 0 0;padding-top:12px;border-top:1px solid #e0e9ef;color:#75869e;font-size:12px;line-height:1.7}.options-error{margin:12px 0 0;color:#b82e4f;font-size:13px;line-height:1.7}.options-error button{margin-left:8px;padding:4px 9px;border:1px solid #d6e0e9;border-radius:7px;background:#fff;color:#52688c;cursor:pointer}.realtime-options-fields:disabled{opacity:.6}@media(max-width:900px){.realtime-options-fields{grid-template-columns:1fr;gap:18px}}
</style>
