<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import type { PlanPromptPage } from '../api'
import type { PromptTextTemplate } from '../planPromptTemplateCatalog'
import PlanPromptTemplatePicker from './PlanPromptTemplatePicker.vue'
import { compilePlanPrompts, planPromptTabs, planPromptValidation, type PlanPromptSections } from '../planPrompt'
import StructuredPromptEditor from './StructuredPromptEditor.vue'

const content = defineModel<PlanPromptSections>({ required: true })
const rewritePlanning = defineModel<string>('rewritePlanning', { default: '' })
const props = defineProps<{ planId: number | null; loaded: boolean; loading: boolean; saving: boolean; error: string; notice: string; legacyPrompt?: string }>()
const emit = defineEmits<{ retry: []; clearFeedback: [] }>()
const generationError = ref('')
const generationNotice = ref('')
const templateButton = ref<HTMLButtonElement | null>(null)
const selection = ref<{ planId: number; page: PlanPromptPage; original: string } | null>(null)
const selectionHandlers = computed(() => {
  const context = selection.value
  return {
    cancel: () => { if (context && selection.value === context) cancelTemplates() },
    apply: (template: PromptTextTemplate) => { if (context) applyTemplate(context, template) },
  }
})
const panel = ref<HTMLElement | null>(null)
const tabs = [...planPromptTabs, { key: 'rewrite_planning_md', label: '话术规划' }] as const
const activeTab = ref<PlanPromptPage>('persona_md')
const pageLabel = (page: PlanPromptPage) => tabs.find(tab => tab.key === page)!.label
const pageContent = (page: PlanPromptPage) => page === 'rewrite_planning_md' ? rewritePlanning.value : content.value[page]
const activeContent = computed(() => pageContent(activeTab.value))
const pageRevisions = ref(Object.fromEntries(tabs.map(tab => [tab.key, 0])) as Record<PlanPromptPage, number>)
const personaMetadata = computed(() => content.value.persona_editor_json || '')
const pageChange = computed(() => {
  const page = activeTab.value
  return (value: string, metadata: string) => updatePage(page, value, metadata)
})
function updatePage(page: PlanPromptPage, value: string, metadata: string) {
  // Cached pages bind their own key; stale blur/IME events cannot write another page.
  if (unavailable.value || page !== activeTab.value) return
  if (page === 'rewrite_planning_md') { rewritePlanning.value = value; return }
  const next = { ...content.value, [page]: value }
  if (page === 'persona_md') {
    if (metadata) next.persona_editor_json = metadata
    else delete next.persona_editor_json
  }
  content.value = next
}
const compiled = computed(() => compilePlanPrompts(content.value))
const validation = computed(() => planPromptValidation(content.value, rewritePlanning.value))
const busy = computed(() => props.loading || props.saving || !!selection.value)
const unavailable = computed(() => !props.planId || !props.loaded || busy.value)
let mounted = true
function invalidateSelection() { selection.value = null }
watch(() => [props.planId, props.loaded, props.loading, props.saving] as const, (next, previous) => {
  if (next[0] !== previous[0] || !next[1] || next[2] || next[3]) { invalidateSelection(); generationError.value = ''; generationNotice.value = '' }
}, { flush: 'sync' })
watch(activeTab, invalidateSelection, { flush: 'sync' })
onBeforeUnmount(() => { mounted = false; invalidateSelection() })
function edited() { generationError.value = ''; generationNotice.value = ''; emit('clearFeedback') }
function requestTemplates() {
  if (unavailable.value) return
  edited()
  selection.value = { planId: props.planId!, page: activeTab.value, original: pageContent(activeTab.value) }
}
function cancelTemplates() { invalidateSelection(); void nextTick(() => templateButton.value?.focus({ preventScroll: true })) }
function applyTemplate(context: NonNullable<typeof selection.value>, template: PromptTextTemplate) {
  if (!mounted || selection.value !== context || props.planId !== context.planId || activeTab.value !== context.page || !props.loaded || props.loading || props.saving) return
  if (pageContent(context.page) !== context.original) { generationError.value = '当前页已变化，模板未覆盖草稿，请重新选择'; cancelTemplates(); return }
  const next = context.page === 'rewrite_planning_md' ? { ...content.value } : { ...content.value, [context.page]: template.content }
  const planning = context.page === 'rewrite_planning_md' ? template.content : rewritePlanning.value
  const invalid = planPromptValidation(next, planning)
  if (invalid) { generationError.value = invalid; cancelTemplates(); return }
  if (context.page === 'rewrite_planning_md') rewritePlanning.value = template.content
  else { if (context.page === 'persona_md') delete next.persona_editor_json; content.value = next }
  pageRevisions.value[context.page]++
  generationNotice.value = '已使用「' + template.name + '」替换「' + pageLabel(context.page) + '」；检查后请点击顶部“保存方案”。'
  invalidateSelection()
  void nextTick(() => panel.value?.querySelector<HTMLElement>('textarea:not([readonly])')?.focus({ preventScroll: true }))
}
function moveTab(event: KeyboardEvent, index: number) {
  const keys = ['ArrowLeft', 'ArrowRight', 'Home', 'End']
  if (!keys.includes(event.key)) return
  event.preventDefault()
  const last = tabs.length - 1
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? last : (index + (event.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length
  activeTab.value = tabs[next]!.key
  ;(event.currentTarget as HTMLElement).parentElement?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus()
}
</script>

<template>
  <section class="plan-prompt-editor" aria-label="方案提示词" :aria-busy="busy">

    <div v-if="error" class="plan-prompt-error" role="alert">
      <strong>{{ loaded ? '保存未完成' : '读取未完成' }}</strong>
      <p>{{ error }}</p>
      <button v-if="!loaded" type="button" :disabled="busy" @click="emit('retry')">重新读取</button>
    </div>
    <div v-if="generationError" class="plan-prompt-error" role="alert"><strong>模板未应用</strong><p>{{ generationError }}</p><p>当前内容已保留，可重新选择。</p></div>
    <p v-if="generationNotice || notice" class="plan-prompt-notice" role="status">{{ generationNotice || notice }}</p>
    <div v-if="loading" class="plan-prompt-skeleton" role="status">
      <p>正在读取当前方案提示词…</p>
      <span v-for="index in 6" :key="index" aria-hidden="true"></span>
    </div>
    <div v-else class="plan-prompt-content">
      <details v-if="legacyPrompt" class="plan-prompt-preview">
        <summary>查看旧版完整提示词</summary>
        <p>旧版没有保存四页原文，因此保留整份内容供你查看，不自动拆分到四页。填写四页并保存后使用新组合。</p>
        <textarea :value="legacyPrompt" readonly rows="8" aria-label="旧版完整提示词"></textarea>
      </details>
      <div class="plan-prompt-tab-toolbar">
      <div class="plan-prompt-tabs" role="tablist" aria-label="方案提示词分区">
        <button v-for="(tab, index) in tabs" :id="'plan-prompt-tab-' + tab.key" :key="tab.key" type="button" role="tab" :aria-selected="activeTab === tab.key" aria-controls="plan-prompt-panel" :tabindex="activeTab === tab.key ? 0 : -1" @click="activeTab = tab.key" @keydown="moveTab($event, index)">{{ tab.label }}</button>
      </div>
      <button ref="templateButton" type="button" class="plan-prompt-template-button" aria-haspopup="dialog" :disabled="unavailable" @click="requestTemplates">
        <svg class="template-button-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><path d="M14 15h7m-7 5h7" stroke-linecap="round"/></svg>
        <span>模板选择</span>
        <svg class="template-button-arrow" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><path d="m5 7.5 5 5 5-5" stroke-linecap="round" stroke-linejoin="round"/></svg>
      </button>
      </div>
      <div class="plan-prompt-content-heading"><span>{{ Array.from(activeContent).length }} 字</span></div>
      <div id="plan-prompt-panel" ref="panel" role="tabpanel" :aria-labelledby="'plan-prompt-tab-' + activeTab">
        <KeepAlive :key="planId ?? 'none'" :max="6">
          <StructuredPromptEditor :key="activeTab" :label="pageLabel(activeTab)" :model-value="activeContent" :metadata="activeTab === 'persona_md' ? personaMetadata : undefined" :reset-key="pageRevisions[activeTab]" input-id="plan-prompt-content" :readonly="unavailable" @change="pageChange" @input="edited" />
        </KeepAlive>
      </div>
      <p v-if="validation" class="plan-prompt-error" role="alert">{{ validation }}</p>
      <details class="plan-prompt-preview">
        <summary>查看两份元提示词合成预览</summary>
        <p>点击顶部“保存方案”后生效；每份最多20000字。</p>
        <label>主播互动元提示词 · {{ Array.from(compiled.interaction_prompt_md).length }} 字<textarea :value="compiled.interaction_prompt_md" readonly rows="8" aria-label="主播互动元提示词"></textarea></label>
        <label>主播话术生成提示词 · {{ Array.from(compiled.speech_prompt_md).length }} 字<textarea :value="compiled.speech_prompt_md" readonly rows="8" aria-label="主播话术生成提示词"></textarea></label>
      </details>
    </div>

    <PlanPromptTemplatePicker v-if="selection" :key="selection.planId + ':' + selection.page" :plan-id="selection.planId" :page="selection.page" :label="pageLabel(selection.page)" @cancel="selectionHandlers.cancel" @apply="selectionHandlers.apply" />
  </section>
</template>

<style scoped>
.plan-prompt-editor{box-sizing:border-box;display:grid;gap:20px;min-width:0;width:100%;color:#26324d;font-size:16px;line-height:1.65}
.plan-prompt-actions{display:flex;flex:none;align-items:center;gap:10px;max-width:100%}
.plan-prompt-editor button,.plan-prompt-confirm button{box-sizing:border-box;min-height:44px;padding:10px 16px;border:1px solid transparent;border-radius:12px;font:inherit;font-size:16px;font-weight:650;line-height:1.4;white-space:nowrap;cursor:pointer}
.plan-prompt-primary{background:#6554ec;color:#fff;box-shadow:0 6px 16px #6554ec20}.plan-prompt-secondary{background:#fff;color:#596682;border-color:#dce2f4!important}
.plan-prompt-editor button:disabled{opacity:.55;cursor:wait;box-shadow:none}.plan-prompt-editor button:focus-visible,.plan-prompt-confirm button:focus-visible{outline:3px solid #c5bfff;outline-offset:3px}
.plan-prompt-editor .plan-prompt-template-button{display:inline-flex;align-items:center;justify-content:center;gap:9px;margin-left:8px;padding:10px 20px;border-color:#91d5cc;border-radius:999px;background:linear-gradient(135deg,#edfcf7,#dcf5f1);color:#116f69;font-size:18px;box-shadow:0 3px 10px #187b7110}.plan-prompt-template-button:hover:not(:disabled){border-color:#45aa9e;background:#d4f1e9;color:#095a54}.template-button-icon{width:21px;height:21px;flex:none}.template-button-arrow{width:16px;height:16px;flex:none}
.plan-prompt-content{display:grid;gap:10px;min-width:0}.plan-prompt-content-heading{display:flex;align-items:center;justify-content:flex-end;gap:12px;font-size:16px}.plan-prompt-content-heading span{color:#78859d;white-space:nowrap}
.plan-prompt-tab-toolbar{display:flex;flex-wrap:wrap;align-items:flex-start;gap:16px;margin-bottom:6px}.plan-prompt-tabs{display:flex;flex-wrap:wrap;gap:10px;margin-bottom:6px}.plan-prompt-tabs button{background:#f5f6ff;color:#596682;border-color:#e4e8fa}.plan-prompt-tabs button[aria-selected=true]{background:#6554ec;color:white;border-color:#6554ec}.plan-prompt-preview{min-width:0;padding:16px;border:1px solid #dfe3f4;border-radius:12px}.plan-prompt-preview summary{cursor:pointer;font-weight:650}.plan-prompt-preview p{color:#71819d}.plan-prompt-preview label{display:block;margin-top:16px}.plan-prompt-preview textarea{min-height:180px;margin-top:8px;font-size:16px}
.plan-prompt-content textarea{box-sizing:border-box;display:block;min-width:0;width:100%;min-height:460px;max-width:100%;padding:20px;border:1px solid #dfe3f4;border-radius:14px;background:#fff;color:#26324d;font:inherit;font-size:20px;line-height:1.8;resize:vertical;outline:none}.plan-prompt-content textarea:focus{border-color:#8273f6;box-shadow:0 0 0 3px #6554ec15}.plan-prompt-content textarea[readonly]{background:#f8f9fd}.plan-prompt-content textarea::placeholder{color:#8894aa;font-size:16px}
.plan-prompt-error{padding:14px 18px;border:1px solid #f1cbd2;border-radius:12px;background:#fff5f6;color:#a64256;font-size:16px;overflow-wrap:anywhere}.plan-prompt-error p{margin:4px 0 0}.plan-prompt-error button{margin-top:10px;background:white;color:inherit;border-color:#e8bcc6}
.plan-prompt-notice{margin:0;padding:14px 18px;border:1px solid #d6e8e2;border-radius:12px;background:#f0faf6;color:#307c69;font-size:16px;overflow-wrap:anywhere}
.plan-prompt-warning{padding:14px 18px;border:1px solid #ead7af;border-radius:12px;background:#fffaf0;color:#845d20;font-size:16px;overflow-wrap:anywhere}.plan-prompt-warning ul{margin:6px 0 0;padding-left:22px}
.plan-prompt-skeleton{display:grid;gap:18px;min-height:420px;padding:24px;border:1px solid #e2e6f3;border-radius:14px;background:#fafbff}.plan-prompt-skeleton p{margin:0;color:#71819d}.plan-prompt-skeleton span{height:18px;max-width:92%;border-radius:7px;background:linear-gradient(90deg,#e9edf7,#f4f6fc,#e9edf7);background-size:200% 100%;animation:prompt-loading 1.5s ease-in-out infinite}.plan-prompt-skeleton span:last-child{max-width:60%}
.plan-prompt-progress{display:flex;align-items:center;gap:10px;margin:0;color:#6554d8;font-size:16px}.plan-prompt-progress i{width:16px;height:16px;flex:none;border:2px solid #d8d2fc;border-top-color:#6554ec;border-radius:50%;animation:prompt-spin .8s linear infinite}
.plan-prompt-confirm{box-sizing:border-box;width:min(520px,calc(100vw - 32px));max-height:calc(100dvh - 32px);margin:auto;padding:28px;border:1px solid #dfe3f4;border-radius:22px;background:#fff;color:#26324d;font:inherit;font-size:16px;line-height:1.7;box-shadow:0 24px 80px #26324d33;overflow:auto}.plan-prompt-confirm::backdrop{background:#26324d66;backdrop-filter:blur(4px)}.plan-prompt-confirm h2{margin:0;font-size:22px}.plan-prompt-confirm p{margin:14px 0 24px;color:#68758e;font-size:16px;overflow-wrap:anywhere}.plan-prompt-confirm footer{display:flex;justify-content:flex-end;gap:10px}
@keyframes prompt-loading{to{background-position:-200% 0}}@keyframes prompt-spin{to{transform:rotate(360deg)}}
@media(max-width:900px){.plan-prompt-content textarea{padding:16px}.plan-prompt-confirm{padding:22px}}
@media(prefers-reduced-motion:reduce){.plan-prompt-skeleton span,.plan-prompt-progress i{animation:none}}
</style>
