<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import type { LiveAgentPlanFact } from '../types'

const props = defineProps<{
  fact: LiveAgentPlanFact
  save: (fact: LiveAgentPlanFact, value: string) => Promise<LiveAgentPlanFact>
}>()
const emit = defineEmits<{ done: [] }>()
const input = ref<HTMLTextAreaElement | null>(null)
const draft = ref(props.fact.value)
const savedFact = ref(props.fact)
const saving = ref(false)
const error = ref('')
const saved = ref(false)
const dirty = computed(() => draft.value.trim() !== savedFact.value.value.trim())
let composing = false
let disposed = false

function changed() { error.value = ''; saved.value = false }
function compositionStart() { composing = true }
function compositionEnd() { composing = false }

// 只有点「保存」或按 Ctrl+Enter 才提交。边输入边保存会在改到一半时触发
// 事实审核（报「请补充具体的差异点」这类错），用户还以为是保存功能坏了。
async function submit() {
  if (disposed || saving.value || composing) return
  if (!dirty.value) { emit('done'); return }
  const value = draft.value.trim()
  if (!value) { error.value = '事实内容不能为空'; return }
  const submitted = draft.value
  saving.value = true
  error.value = ''
  try {
    const updated = await props.save(savedFact.value, value)
    if (disposed) return
    savedFact.value = updated
    if (draft.value === submitted) draft.value = updated.value
    saved.value = true
    emit('done')
  } catch (reason) {
    if (!disposed) error.value = reason instanceof Error ? reason.message : '保存失败，请重试'
  } finally {
    saving.value = false
  }
}
function cancel() {
  if (saving.value) return
  emit('done')
}
defineExpose({ submit, saving })
onMounted(async () => { await nextTick(); input.value?.focus({ preventScroll: true }); input.value?.scrollIntoView({ block: 'nearest', inline: 'nearest' }) })
onBeforeUnmount(() => { disposed = true })
</script>

<template>
  <div class="inline-formal-fact-editor">
    <textarea ref="input" v-model="draft" :aria-label="'修改“' + fact.key + '”的事实内容'" rows="3" maxlength="4000" :aria-invalid="!!error" :aria-describedby="'inline-fact-status-' + fact.id" @input="changed" @compositionstart="compositionStart" @compositionend="compositionEnd" @keydown.ctrl.enter.prevent="submit" @keydown.meta.enter.prevent="submit"></textarea>
    <div :id="'inline-fact-status-' + fact.id" class="inline-fact-status">
      <span v-if="error" class="error" role="alert">{{ error }}</span>
      <span v-else-if="saving" role="status">正在保存并审核…</span>
      <span v-else-if="saved && !dirty" role="status">已保存</span>
      <span v-else>改完点右侧「保存」，或按 Ctrl+Enter</span>
    </div>
    <div class="inline-fact-actions">
      <button type="button" class="inline-fact-cancel" :disabled="saving" @click="cancel">取消</button>
    </div>
  </div>
</template>

<style scoped>
.inline-formal-fact-editor { display:grid; gap:8px; min-width:0; }
.inline-formal-fact-editor textarea { box-sizing:border-box; width:100%; min-height:108px; padding:10px 12px; border:1px solid #7d99e5; border-radius:9px; background:#fff; color:#111; font:inherit; font-size:16px; line-height:1.7; resize:vertical; }
.inline-formal-fact-editor textarea:focus { outline:2px solid #cbd8ff; outline-offset:1px; }
.inline-fact-status,.inline-fact-status span { color:#637795; font-size:16px; line-height:1.5; overflow-wrap:anywhere; }
.inline-fact-status .error { color:#bf3150; }
.inline-fact-actions { display:flex; gap:8px; }
.inline-fact-cancel { border:1px solid #d7deec; border-radius:8px; padding:5px 14px; background:#fff; color:#4a5570; font:inherit; font-size:15px; cursor:pointer; }
.inline-fact-cancel:hover:not(:disabled) { border-color:#9aa5f0; color:#3b3f8f; }
.inline-fact-cancel:disabled { opacity:.5; cursor:default; }
</style>
