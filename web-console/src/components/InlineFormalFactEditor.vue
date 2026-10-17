<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import type { LiveAgentPlanFact } from '../types'

const props = defineProps<{
  fact: LiveAgentPlanFact
  save: (fact: LiveAgentPlanFact, value: string) => Promise<LiveAgentPlanFact>
}>()
const emit = defineEmits<{ done: [] }>()
const root = ref<HTMLElement | null>(null)
const input = ref<HTMLTextAreaElement | null>(null)
const draft = ref(props.fact.value)
const savedFact = ref(props.fact)
const saving = ref(false)
const error = ref('')
const saved = ref(false)
const dirty = computed(() => draft.value.trim() !== savedFact.value.value.trim())
let timer: ReturnType<typeof setTimeout> | undefined
let composing = false
let closeRequested = false
let disposed = false

function clearTimer() { if (timer !== undefined) clearTimeout(timer); timer = undefined }
function schedule() {
  clearTimer()
  if (!disposed && !composing && dirty.value) timer = setTimeout(() => { void flush() }, 1200)
}
function changed() { error.value = ''; saved.value = false; schedule() }
function compositionStart() { composing = true; clearTimer() }
function compositionEnd() { composing = false; if (closeRequested) void flush(); else changed() }
function focus() { closeRequested = false }
function blur(event: FocusEvent) {
  if (event.relatedTarget instanceof Node && root.value?.contains(event.relatedTarget)) return
  closeRequested = true
  void flush()
}
async function flush() {
  clearTimer()
  if (disposed || composing || saving.value) return
  if (!dirty.value) { if (closeRequested) emit('done'); return }
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
  } catch (reason) {
    if (!disposed) error.value = reason instanceof Error ? reason.message : '保存失败，请重试'
  } finally {
    saving.value = false
    if (!disposed && !error.value) {
      if (dirty.value) { if (closeRequested) void flush(); else schedule() }
      else if (closeRequested) emit('done')
    }
  }
}
onMounted(async () => { await nextTick(); input.value?.focus({ preventScroll: true }); input.value?.scrollIntoView({ block: 'nearest', inline: 'nearest' }) })
onBeforeUnmount(() => { disposed = true; clearTimer() })
</script>

<template>
  <div ref="root" class="inline-formal-fact-editor" @focusout="blur">
    <textarea ref="input" v-model="draft" :aria-label="'修改“' + fact.key + '”的事实内容'" rows="3" maxlength="4000" :aria-invalid="!!error" :aria-describedby="'inline-fact-status-' + fact.id" @input="changed" @compositionstart="compositionStart" @compositionend="compositionEnd" @focus="focus" @keydown.ctrl.enter.prevent="flush" @keydown.meta.enter.prevent="flush"></textarea>
    <div :id="'inline-fact-status-' + fact.id" class="inline-fact-status">
      <span v-if="error" class="error" role="alert">{{ error }}<button type="button" @click="flush">重试</button></span>
      <span v-else-if="saving" role="status">正在保存并审核…</span>
      <span v-else-if="saved && !dirty" role="status">已保存</span>
      <span v-else>停止输入后自动保存</span>
    </div>
  </div>
</template>

<style scoped>
.inline-formal-fact-editor { display:grid; gap:8px; min-width:0; }
.inline-formal-fact-editor textarea { box-sizing:border-box; width:100%; min-height:108px; padding:10px 12px; border:1px solid #7d99e5; border-radius:9px; background:#fff; color:#111; font:inherit; font-size:16px; line-height:1.7; resize:vertical; }
.inline-formal-fact-editor textarea:focus { outline:2px solid #cbd8ff; outline-offset:1px; }
.inline-fact-status,.inline-fact-status span { color:#637795; font-size:16px; line-height:1.5; overflow-wrap:anywhere; }
.inline-fact-status .error { color:#bf3150; }
.inline-fact-status button { margin-left:8px; padding:2px 7px; border:1px solid #b8c9ed; border-radius:5px; background:#fff; color:#345cba; font:inherit; font-size:16px; cursor:pointer; }
</style>
