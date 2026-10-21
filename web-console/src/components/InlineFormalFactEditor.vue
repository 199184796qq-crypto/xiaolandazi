<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import type { LiveAgentPlanFact } from '../types'
import type { FactDiffAnalysis } from '../api'

const props = defineProps<{
  fact: LiveAgentPlanFact
  save: (fact: LiveAgentPlanFact, value: string) => Promise<LiveAgentPlanFact>
  analyze?: (fact: LiveAgentPlanFact) => Promise<FactDiffAnalysis>
}>()
const emit = defineEmits<{ done: [] }>()
const input = ref<HTMLTextAreaElement | null>(null)
const draft = ref(props.fact.value)
const savedFact = ref(props.fact)
const saving = ref(false)
const error = ref('')
const saved = ref(false)
const analyzing = ref(false)
const analysis = ref<FactDiffAnalysis | null>(null)
const analysisError = ref('')
const dirty = computed(() => draft.value.trim() !== savedFact.value.value.trim())
let composing = false
let disposed = false

function changed() { error.value = ''; saved.value = false; analysis.value = null; analysisError.value = '' }
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
// 让模型先从方案已有事实里找这两个链接能对上的差异：找得到直接给一句能存的说法，
// 找不到就说明两边各缺什么，再给一条带占位符的示例，用户照着补就行。
async function runAnalyze() {
  if (!props.analyze || analyzing.value || saving.value) return
  analyzing.value = true
  analysisError.value = ''
  analysis.value = null
  try {
    const result = await props.analyze(props.fact)
    if (!disposed) analysis.value = result
  } catch (reason) {
    if (!disposed) analysisError.value = reason instanceof Error ? reason.message : '分析失败，请重试'
  } finally {
    if (!disposed) analyzing.value = false
  }
}
function applyAnalysis(text: string) {
  if (!text) return
  draft.value = text
  analysis.value = null
  error.value = ''
  saved.value = false
  void nextTick(() => input.value?.focus({ preventScroll: true }))
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
      <button v-if="analyze" type="button" class="inline-fact-analyze" :disabled="analyzing || saving" @click="runAnalyze">
        {{ analyzing ? '正在找差异…' : '让模型找差异' }}
      </button>
      <button type="button" class="inline-fact-cancel" :disabled="saving" @click="cancel">取消</button>
    </div>
    <div v-if="analysisError" class="inline-fact-analysis is-error" role="alert">{{ analysisError }}</div>
    <div v-else-if="analysis" class="inline-fact-analysis">
      <template v-if="analysis.found">
        <p class="inline-fact-analysis-line"><b>模型从已有事实里找到的差异：</b>{{ analysis.value }}</p>
        <p v-if="analysis.digits?.length" class="inline-fact-analysis-hint">用到的数字：{{ analysis.digits.join('、') }}（都能在事实里查到）</p>
        <button type="button" class="inline-fact-apply" @click="applyAnalysis(analysis.value || '')">用这条</button>
      </template>
      <template v-else>
        <p class="inline-fact-analysis-line"><b>事实不足：</b>{{ analysis.reason }}</p>
        <p v-if="analysis.example" class="inline-fact-analysis-hint">照着这个格式补：{{ analysis.example }}</p>
        <button v-if="analysis.example" type="button" class="inline-fact-apply" @click="applyAnalysis(analysis.example || '')">用这个格式</button>
      </template>
    </div>
  </div>
</template>

<style scoped>
.inline-formal-fact-editor { display:grid; gap:8px; min-width:0; }
.inline-formal-fact-editor textarea { box-sizing:border-box; width:100%; min-height:108px; padding:10px 12px; border:1px solid #7d99e5; border-radius:9px; background:#fff; color:#111; font:inherit; font-size:16px; line-height:1.7; resize:vertical; }
.inline-formal-fact-editor textarea:focus { outline:2px solid #cbd8ff; outline-offset:1px; }
.inline-fact-status,.inline-fact-status span { color:#637795; font-size:16px; line-height:1.5; overflow-wrap:anywhere; }
.inline-fact-status .error { color:#bf3150; }
.inline-fact-actions { display:flex; gap:8px; flex-wrap:wrap; }
.inline-fact-analyze { border:1px solid #c7d2f5; border-radius:8px; padding:5px 14px; background:#f4f6ff; color:#42509b; font:inherit; font-size:15px; cursor:pointer; }
.inline-fact-analyze:hover:not(:disabled) { border-color:#9aa5f0; }
.inline-fact-analyze:disabled { opacity:.6; cursor:default; }
.inline-fact-cancel { border:1px solid #d7deec; border-radius:8px; padding:5px 14px; background:#fff; color:#4a5570; font:inherit; font-size:15px; cursor:pointer; }
.inline-fact-cancel:hover:not(:disabled) { border-color:#9aa5f0; color:#3b3f8f; }
.inline-fact-cancel:disabled { opacity:.5; cursor:default; }
.inline-fact-analysis { display:grid; gap:6px; justify-items:start; padding:10px 12px; border:1px solid #dbe6f3; border-radius:10px; background:#f8fbff; }
.inline-fact-analysis.is-error { border-color:#f1bdc7; background:#fff2f4; color:#bf3150; }
.inline-fact-analysis-line { margin:0; color:#2a3550; font-size:15px; line-height:1.6; overflow-wrap:anywhere; }
.inline-fact-analysis-hint { margin:0; color:#6b7a93; font-size:14px; line-height:1.6; overflow-wrap:anywhere; }
.inline-fact-apply { border:1px solid #7767f0; border-radius:8px; padding:5px 14px; background:#fff; color:#4b3fb8; font:inherit; font-size:15px; font-weight:700; cursor:pointer; }
.inline-fact-apply:hover { background:#f2f0ff; }
</style>
