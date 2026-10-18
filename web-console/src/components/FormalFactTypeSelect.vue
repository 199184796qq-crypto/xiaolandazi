<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from 'vue'
import type { LiveAgentPlanFact } from '../types'
import { factCategoryGroups, factCategoryState } from '../factCategories'
import type { FactTypeSuggestion } from '../api'
import { factTypeFeedbackText } from '../factTypeFeedback'

const props = defineProps<{
  fact: LiveAgentPlanFact
  disabled?: boolean
  save: (fact: LiveAgentPlanFact, category: string) => Promise<void>
  suggestion?: FactTypeSuggestion | null
}>()

const saving = ref(false)
const error = ref('')
const detailsDialog = ref<HTMLDialogElement | null>(null)
const feedbackId = useId()
const selected = computed(() => props.fact.category || '')
const definition = computed(() => factCategoryState.items.find(item => item.value === selected.value))
const selectedLabel = computed(() => definition.value?.label || (factCategoryState.loading ? '正在加载分类…' : factCategoryState.error ? '分类加载失败' : selected.value ? '未匹配分类' : '请选择类型'))
const categoryDisabled = computed(() => props.disabled || saving.value || factCategoryState.loading || !!factCategoryState.error || !factCategoryState.items.length)
const groups = computed(() => factCategoryGroups(props.fact.category).map(group => ({ ...group, options: group.options.map(option => ({ ...option, label: factCategoryState.items.some(item => item.value === option.value) ? option.label : '未匹配分类' })) })))
const hasFeedback = computed(() => !!error.value || !!(props.suggestion && (props.suggestion.low_conf || props.suggestion.error)))
const feedbackError = computed(() => !!error.value || !!props.suggestion?.error)
const feedbackLabel = computed(() => error.value ? '保存失败' : props.suggestion?.error ? '分类失败' : '待确认')
const feedbackTitle = computed(() => error.value ? '事实类型保存失败' : props.suggestion?.error ? '分类建议未完成' : '事实类型待确认')
const suggestionLabel = computed(() => {
  const suggestion = props.suggestion
  if (!suggestion || suggestion.error) return ''
  const label = factCategoryState.items.find(item => item.value === suggestion.category)?.label || suggestion.label
  return label && /[\u3400-\u9fff]/.test(label) ? label : '请手动选择'
})
const feedbackReason = computed(() => factTypeFeedbackText(error.value || props.suggestion?.reason,
  error.value ? '本次修改未保存，请重试；如有版本冲突，请刷新后重新选择。' : props.suggestion?.error ? '暂时无法判断，请在事实类型下拉框中手动选择。' : '这条事实涉及多个类型，请根据主要内容选择。', factCategoryState.items))

function openFeedback() { if (hasFeedback.value && !detailsDialog.value?.open) detailsDialog.value?.showModal() }
function closeFeedback() { detailsDialog.value?.close() }
function closeOnBackdrop(event: MouseEvent) { if (event.target === detailsDialog.value) closeFeedback() }
watch(hasFeedback, value => { if (!value) closeFeedback() })
onBeforeUnmount(closeFeedback)

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
    <span v-if="saving" class="fact-type-saving" role="status">保存中…</span>
    <div v-else-if="hasFeedback" class="fact-type-feedback" :role="feedbackError ? 'alert' : 'status'">
      <button type="button" class="fact-type-feedback-button" :class="{ 'is-error': feedbackError }" :aria-label="feedbackLabel + '，查看分类说明'" aria-haspopup="dialog" :aria-controls="feedbackId" @click="openFeedback">
        <svg viewBox="0 0 20 20" aria-hidden="true"><circle cx="10" cy="10" r="7.5"/><path d="M10 5.8v5M10 14h.01"/></svg>
        {{ feedbackLabel }}<span aria-hidden="true">›</span>
      </button>
    </div>
  </div>
  <Teleport to="body">
    <dialog :id="feedbackId" ref="detailsDialog" class="fact-type-feedback-dialog" :aria-labelledby="feedbackId + '-title'" :aria-describedby="feedbackId + '-reason'" @click="closeOnBackdrop">
      <section class="fact-type-feedback-panel">
        <header><h3 :id="feedbackId + '-title'">{{ feedbackTitle }}</h3><button type="button" class="fact-type-feedback-close" aria-label="关闭分类说明" @click="closeFeedback">×</button></header>
        <dl><div><dt>当前类型</dt><dd>{{ selectedLabel }}</dd></div><div v-if="suggestionLabel && !error"><dt>建议类型</dt><dd>{{ suggestionLabel }}</dd></div></dl>
        <p :id="feedbackId + '-reason'" class="fact-type-feedback-reason">{{ feedbackReason }}</p>
        <p class="fact-type-feedback-help">{{ error ? '原有类型已保留，请关闭说明后重试。' : '请关闭说明，在事实类型下拉框中选择合适的分类。' }}</p>
        <footer><button type="button" @click="closeFeedback">知道了</button></footer>
      </section>
    </dialog>
  </Teleport>
</template>

<style scoped>
.fact-type-select { display:grid;gap:6px;min-width:0;font-size:16px;line-height:1.5; }
.fact-type-control { position:relative;min-width:0;height:42px;background:var(--formal-scope-bg,#eef3ff);border-radius:13px; }
.fact-type-select .fact-type-selected { display:block;box-sizing:border-box;height:42px;padding:9px 32px 9px 12px;color:#111;font-size:16px;line-height:24px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis; }
.fact-type-control select { position:absolute;inset:0;width:100%;height:100%;box-sizing:border-box;appearance:none;border:1px solid transparent;border-radius:inherit;padding:7px 30px 7px 9px;background:transparent;color:transparent;font:inherit;font-size:16px;cursor:pointer; }
.fact-type-control select option,.fact-type-control select optgroup { background:#fff;color:#111;font-size:16px; }
.fact-type-control select:disabled { cursor:default; }
.fact-type-caret { position:absolute;right:14px;top:50%;width:7px;height:7px;border-right:2px solid var(--formal-fact-accent,#4879fb);border-bottom:2px solid var(--formal-fact-accent,#4879fb);transform:translateY(-65%) rotate(45deg);pointer-events:none; }
.fact-type-select select:focus-visible { outline:2px solid #6557ff;outline-offset:2px; }
.fact-type-select span { color:#526080;overflow-wrap:anywhere;font-size:16px; }
.fact-type-saving { white-space:nowrap; }
.fact-type-feedback { min-width:0; }
.fact-type-feedback-button { display:inline-flex;align-items:center;gap:5px;max-width:100%;min-height:30px;padding:2px 9px;border:1px solid #f0d5a4;border-radius:999px;background:#fff8e9;color:#805315;font:inherit;font-size:16px;line-height:24px;white-space:nowrap;cursor:pointer; }
.fact-type-feedback-button.is-error { border-color:#f0c7ca;background:#fff3f3;color:#ac3540; }
.fact-type-feedback-button svg { flex:none;width:17px;height:17px;fill:none;stroke:currentColor;stroke-width:1.7;stroke-linecap:round; }
.fact-type-feedback-button span { color:inherit;font-size:20px;line-height:20px; }
.fact-type-feedback-button:focus-visible,.fact-type-feedback-dialog button:focus-visible { outline:2px solid #6557ff;outline-offset:3px; }
.fact-type-feedback-dialog { box-sizing:border-box;width:min(520px,calc(100vw - 32px));max-height:calc(100dvh - 48px);margin:auto;padding:0;overflow:auto;border:1px solid #dde3f2;border-radius:18px;background:#fff;color:#243754;box-shadow:0 24px 80px #26345c33;font-size:16px;line-height:1.6; }
.fact-type-feedback-dialog::backdrop { background:#15223b55; }
.fact-type-feedback-panel { padding:24px; }
.fact-type-feedback-panel header { display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:20px; }
.fact-type-feedback-panel h3 { margin:0;font-size:18px;line-height:1.5;color:#192e50; }
.fact-type-feedback-close { flex:none;width:36px;height:36px;padding:0;border:0;border-radius:8px;background:#f1f4fb;color:#53627c;font-size:24px;line-height:1;cursor:pointer; }
.fact-type-feedback-panel dl { display:grid;gap:10px;margin:0 0 18px; }
.fact-type-feedback-panel dl div { display:grid;grid-template-columns:80px minmax(0,1fr);gap:12px; }
.fact-type-feedback-panel dt { color:#65748b; }
.fact-type-feedback-panel dd { margin:0;color:#1c304f;overflow-wrap:anywhere; }
.fact-type-feedback-reason { margin:0;padding:14px 16px;border-radius:10px;background:#f5f7fc;white-space:pre-wrap;overflow-wrap:anywhere; }
.fact-type-feedback-help { margin:16px 0;color:#65748b; }
.fact-type-feedback-panel footer { display:flex;justify-content:flex-end; }
.fact-type-feedback-panel footer button { padding:8px 20px;border:1px solid #6656f7;border-radius:9px;background:#6656f7;color:#fff;font:inherit;font-size:16px;cursor:pointer; }
@media(max-width:480px) { .fact-type-feedback-panel { padding:18px; }.fact-type-feedback-panel dl div { grid-template-columns:72px minmax(0,1fr);gap:8px; } }
</style>
