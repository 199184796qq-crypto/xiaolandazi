<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { previewLiveAgentPlanFactSpeech, type FactSpeechPreview } from '../api'
import type { LiveAgentPlanFact } from '../types'
import SpeechConstraintEditor from './SpeechConstraintEditor.vue'

const props = defineProps<{ fact: LiveAgentPlanFact; planId: number; roomId: number; tenantId?: number }>()
const emit = defineEmits<{ close: [] }>()
const dialog = ref<HTMLDialogElement | null>(null)
const loading = ref(false), error = ref(''), copied = ref(false)
const result = ref<FactSpeechPreview | null>(null)
const constraintBusy = ref(false), adoptedNotice = ref('')
let request: AbortController | null = null, sequence = 0
let previousFocus: HTMLElement | null = null, previousOverflow = ''

async function generate() {
  request?.abort()
  const controller = new AbortController(), ticket = ++sequence
  request = controller
  loading.value = true; error.value = ''; result.value = null; copied.value = false
  try {
    const output = await previewLiveAgentPlanFactSpeech(props.planId, props.fact.id, {
      room_id: props.roomId, tenant_id: props.tenantId, version_no: props.fact.version_no,
    }, controller.signal)
    if (ticket !== sequence || controller.signal.aborted) return
    if (output.fact_id !== props.fact.id || output.version_no !== props.fact.version_no) throw new Error('事实版本已变化，请刷新后预览')
    result.value = output
  } catch (cause) {
    if (ticket === sequence && !controller.signal.aborted) error.value = cause instanceof Error ? cause.message : '话术生成失败，请重试'
  } finally {
    if (ticket === sequence) loading.value = false
  }
}
async function copy() {
  try { await navigator.clipboard.writeText(result.value?.text || ''); copied.value = true }
  catch { error.value = '复制失败，请选中话术手动复制' }
}
function close() { request?.abort(); ++sequence; emit('close') }
function backdrop(event: MouseEvent) {
  if (event.target !== dialog.value || !dialog.value) return
  const rect = dialog.value.getBoundingClientRect()
  if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) close()
}
watch(() => [props.fact.id, props.fact.version_no, props.planId, props.roomId, props.tenantId], generate)
onMounted(() => {
  previousFocus = document.activeElement as HTMLElement | null
  previousOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  dialog.value?.showModal(); dialog.value?.focus()
  void generate()
})
onBeforeUnmount(() => {
  request?.abort(); ++sequence
  dialog.value?.close(); document.body.style.overflow = previousOverflow
  if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true })
})
</script>

<template>
  <Teleport to="body">
    <dialog ref="dialog" class="fact-speech-preview" tabindex="-1" aria-labelledby="fact-speech-preview-title" :aria-busy="loading" @cancel.prevent="close" @click="backdrop">
      <header><div><span>模拟直播间 · 讲品环节</span><h2 id="fact-speech-preview-title">话术预览</h2><p>根据这条事实，使用当前方案的主播风格和真实话术模型生成。</p></div><button type="button" class="close" aria-label="关闭话术预览" @click="close">×</button></header>
      <section class="source"><strong>{{ fact.key }}</strong><p>{{ fact.value }}</p><details v-if="fact.forbidden_wording || fact.safe_rewrite"><summary>查看表达要求</summary><p v-if="fact.forbidden_wording"><b>不要这样说：</b>{{ fact.forbidden_wording }}</p><p v-if="fact.safe_rewrite"><b>参考表达：</b>{{ fact.safe_rewrite }}</p></details></section>
      
      <div class="output" aria-live="polite">
        <div v-if="loading" class="waiting" role="status"><i aria-hidden="true"></i><strong>正在模拟直播讲品…</strong><span>正在规划讲解角度并生成话术。</span></div>
        <template v-else-if="result"><div class="model"><strong>直播话术</strong><span>{{ result.model_alias || result.model_name }} · {{ result.room_name }}</span></div><p class="speech">{{ result.text }}</p><p class="notice">{{ result.notice }}</p></template>
        <p v-else-if="!error" class="notice">点击“重新生成”获取话术。</p>
      </div>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <details v-if="result?.expression_constraints?.length" class="saved-constraints"><summary>已采用的表达约束 {{ result.expression_constraints.length }} 条</summary><p v-for="(rule, index) in result.expression_constraints" :key="index"><small>你的要求：{{ rule.feedback }}</small>{{ rule.constraint }}</p></details>
      <SpeechConstraintEditor v-if="result?.text" :plan-id="planId" :room-id="roomId" :tenant-id="tenantId" :fact-id="fact.id" :version-no="fact.version_no" :selected-text="result.text" :disabled="loading" @busy="constraintBusy = $event" @adopted="adoptedNotice = '约束已记录，可重新生成验证。后续新生成的直播话术和互动回答也会使用。'" />
      <p v-if="adoptedNotice" class="notice" role="status">{{ adoptedNotice }}</p>
      <footer><small>仅预览文字，可修改事实或采用新的表达约束。</small><div><button type="button" @click="close">关闭</button><button v-if="result?.text" type="button" @click="copy">{{ copied ? '已复制' : '复制话术' }}</button><button type="button" class="primary" :disabled="loading || constraintBusy" @click="generate">{{ loading ? '生成中…' : '重新生成' }}</button></div></footer>
    </dialog>
  </Teleport>
</template>

<style scoped>
.saved-constraints{margin-top:15px;padding:12px 16px;border:1px solid #dce5fa;border-radius:10px;background:#f4f8ff;line-height:1.7}.saved-constraints summary{cursor:pointer}.saved-constraints small{display:block;color:#7d8da9}
.review-suggestions{margin-top:14px;padding:14px 18px;border:1px solid #dce5f8;border-radius:10px;background:#f2f6ff;color:#526889;font-size:15px;line-height:1.7}.review-suggestions ul{padding-left:22px;margin:8px 0}.review-suggestions q{display:block}.review-suggestions small{font-size:13px;color:#75849e}
.fact-speech-preview{box-sizing:border-box;width:min(900px,calc(100vw - 36px));max-height:calc(100dvh - 36px);margin:auto;padding:26px;border:1px solid #dce4ff;border-radius:23px;background:linear-gradient(135deg,#fff,#f4f6ff);color:#283f65;font:inherit;box-shadow:0 28px 90px #20335440;overflow:auto}.fact-speech-preview::backdrop{background:#23314e70;backdrop-filter:blur(4px)}header{display:flex;justify-content:space-between;align-items:flex-start;gap:16px}header span{color:#7886ca;font-size:12px}h2{margin:5px 0;font-size:26px;color:#172a52}header p{margin:6px 0 20px;color:#71819c;font-size:14px;line-height:1.6}button{min-height:39px;padding:8px 15px;border:1px solid #d3dcf6;border-radius:10px;background:#fff;color:#5469b5;font:inherit;font-size:14px;font-weight:700;cursor:pointer}button:disabled{opacity:.6;cursor:wait}.close{flex-shrink:0;width:36px;min-height:36px;padding:0;font-size:25px}.source{padding:15px 18px;border:1px solid #e1e8fa;border-radius:14px;background:#edf3ff}.source p{margin:7px 0;line-height:1.7;white-space:pre-wrap;overflow-wrap:anywhere}.source details{color:#7183a5;font-size:13px}.source summary{cursor:pointer}.output{min-height:180px;margin-top:18px;padding:18px;border:1px solid #e2e7fa;border-radius:14px;background:#fff}.model{display:flex;justify-content:space-between;gap:12px;flex-wrap:wrap}.model span{color:#8895b0;font-size:12px}.speech{margin:15px 0;font-size:17px;line-height:1.9;white-space:pre-wrap;overflow-wrap:anywhere}.notice{margin:9px 0 0;color:#338e76;font-size:13px;line-height:1.6}.warning{padding:10px 12px;border-left:3px solid #e1a037;border-radius:6px;background:#fff7e9;color:#935b05;font-size:15px;font-weight:700}.is-review-incomplete{border-left-color:#7b99d3;background:#f0f5ff;color:#4f6e9f}.issues{display:grid;gap:8px;margin:12px 0;padding:12px 16px 12px 32px;border-radius:10px;background:#fff6e8;color:#815619;font-size:15px;font-weight:500;line-height:1.7}.issues q{display:block;color:#9b403e;font-weight:700}.waiting{display:grid;justify-items:center;gap:12px;padding:18px;text-align:center;color:#7183b8;font-size:14px}.waiting i{width:28px;height:28px;border:3px solid #e3e5fb;border-top-color:#6c6ae9;border-radius:50%;animation:spin 1s linear infinite}.error{padding:12px 15px;border-radius:10px;background:#fff0f3;color:#ba4867;line-height:1.6;overflow-wrap:anywhere}footer{display:flex;align-items:center;justify-content:space-between;gap:15px;margin-top:20px;padding-top:17px;border-top:1px solid #e3e9fa}footer small{color:#8593ad;line-height:1.6}footer>div{display:flex;gap:8px;flex-shrink:0}.primary{border-color:transparent;background:linear-gradient(120deg,#737cf3,#534be0);color:white}@keyframes spin{to{transform:rotate(360deg)}}@media(max-width:600px){.fact-speech-preview{padding:18px}footer{align-items:stretch;flex-direction:column}footer>div{justify-content:flex-end}.speech{font-size:16px}}@media(prefers-reduced-motion:reduce){.waiting i{animation:none}}
</style>
