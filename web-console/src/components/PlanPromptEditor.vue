<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import { renderLiveAgentPlanPrompt } from '../api'
import { compilePlanPrompts, planPromptTabs, planPromptValidation, type PlanPromptSections } from '../planPrompt'

const content = defineModel<PlanPromptSections>({ required: true })
const props = defineProps<{ planId: number | null; loaded: boolean; loading: boolean; saving: boolean; error: string; notice: string; legacyPrompt?: string }>()
const emit = defineEmits<{ save: []; retry: []; clearFeedback: [] }>()
const generating = ref(false)
const generationError = ref('')
const generationNotice = ref('')
const confirmation = ref<HTMLDialogElement | null>(null)
const cancelButton = ref<HTMLButtonElement | null>(null)
const regenerateButton = ref<HTMLButtonElement | null>(null)
const editor = ref<HTMLTextAreaElement | null>(null)
const activeTab = ref<keyof PlanPromptSections>('persona_md')
const selectedTab = computed(() => planPromptTabs.find(tab => tab.key === activeTab.value)!)
const activeContent = computed({ get: () => content.value[activeTab.value], set: value => { content.value = { ...content.value, [activeTab.value]: value } } })
const compiled = computed(() => compilePlanPrompts(content.value))
const validation = computed(() => planPromptValidation(content.value))
const busy = computed(() => props.loading || props.saving || generating.value)
const unavailable = computed(() => !props.planId || !props.loaded || busy.value)
let mounted = true
let requestSequence = 0

onBeforeUnmount(() => {
  mounted = false
  requestSequence++
  confirmation.value?.close()
})

function edited() {
  generationError.value = ''
  generationNotice.value = ''
  emit('clearFeedback')
}

async function generate() {
  if (unavailable.value || confirmation.value?.open) return
  const planId = props.planId!
  const sequence = ++requestSequence
  const current = () => mounted && sequence === requestSequence && props.planId === planId
  generating.value = true
  generationError.value = ''
  generationNotice.value = ''
  emit('clearFeedback')
  try {
    const result = await renderLiveAgentPlanPrompt(planId)
    if (!current()) return
    if (typeof result.facts_md !== 'string') throw new Error('未返回事实依据，当前内容已保留，请重试')
    const next = { ...content.value, facts_md: result.facts_md }
    const error = planPromptValidation(next)
    if (error) throw new Error(error)
    content.value = next
    activeTab.value = 'facts_md'
    generationNotice.value = result.facts_md.trim() ? '已更新事实依据，其余三个文本框保持不变，检查后保存' : '当前方案没有可用商品卡、福利或事实，事实依据已清空，检查后保存'
  } catch (err) {
    if (current()) generationError.value = err instanceof Error ? err.message : '生成方案提示词失败，请重试'
  } finally {
    if (current()) {
      generating.value = false
      if (!generationError.value) { await nextTick(); if (current()) editor.value?.focus({ preventScroll: true }) }
    }
  }
}

function requestRegeneration() {
  if (unavailable.value || confirmation.value?.open) return
  if (!content.value.facts_md.trim()) { void generate(); return }
  confirmation.value?.showModal()
  cancelButton.value?.focus()
}
function cancelRegeneration() {
  confirmation.value?.close()
  regenerateButton.value?.focus({ preventScroll: true })
}
function confirmRegeneration() {
  confirmation.value?.close()
  void generate()
}
function save() {
  if (unavailable.value || validation.value) return
  generationError.value = ''
  generationNotice.value = ''
  emit('save')
}
function moveTab(event: KeyboardEvent, index: number) {
  const keys = ['ArrowLeft', 'ArrowRight', 'Home', 'End']
  if (!keys.includes(event.key)) return
  event.preventDefault()
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? 3 : (index + (event.key === 'ArrowRight' ? 1 : -1) + 4) % 4
  activeTab.value = planPromptTabs[next]!.key
  ;(event.currentTarget as HTMLElement).parentElement?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus()
}
function backdrop(event: MouseEvent) {
  const dialog = confirmation.value
  if (!dialog || event.target !== dialog) return
  const box = dialog.getBoundingClientRect()
  if (event.clientX < box.left || event.clientX > box.right || event.clientY < box.top || event.clientY > box.bottom) cancelRegeneration()
}
</script>

<template>
  <section class="plan-prompt-editor" aria-labelledby="plan-prompt-heading" :aria-busy="busy">
    <header class="plan-prompt-heading">
      <div>
        <h3 id="plan-prompt-heading">方案提示词</h3>
        <p>四页原文分别保存、原样回显；两份元提示词单独生成。重新生成只替换事实依据。</p>
      </div>
      <div class="plan-prompt-actions">
          <button ref="regenerateButton" type="button" class="plan-prompt-secondary" :disabled="unavailable" @click="requestRegeneration">{{ generating ? '生成中…' : '重新生成' }}</button>
          <button type="button" class="plan-prompt-primary" :disabled="unavailable || !!validation" @click="save">{{ saving ? '保存中…' : '保存方案提示词' }}</button>
      </div>
    </header>

    <div v-if="error" class="plan-prompt-error" role="alert">
      <strong>{{ loaded ? '保存未完成' : '读取未完成' }}</strong>
      <p>{{ error }}</p>
      <button v-if="!loaded" type="button" :disabled="busy" @click="emit('retry')">重新读取</button>
    </div>
    <div v-if="generationError" class="plan-prompt-error" role="alert"><strong>生成未完成</strong><p>{{ generationError }}</p><p>当前内容已保留，可再次尝试。</p></div>
    <p v-if="generationNotice || notice" class="plan-prompt-notice" role="status">{{ generationNotice || notice }}</p>
    <div v-if="loading" class="plan-prompt-skeleton" role="status">
      <p>正在读取当前方案提示词…</p>
      <span v-for="index in 5" :key="index" aria-hidden="true"></span>
    </div>
    <div v-else class="plan-prompt-content">
      <details v-if="legacyPrompt" class="plan-prompt-preview">
        <summary>查看旧版完整提示词</summary>
        <p>旧版没有保存四页原文，因此保留整份内容供你查看，不自动拆分到四页。填写四页并保存后使用新组合。</p>
        <textarea :value="legacyPrompt" readonly rows="8" aria-label="旧版完整提示词"></textarea>
      </details>
      <div class="plan-prompt-tabs" role="tablist" aria-label="方案提示词分区">
        <button v-for="(tab, index) in planPromptTabs" :id="'plan-prompt-tab-' + tab.key" :key="tab.key" type="button" role="tab" :aria-selected="activeTab === tab.key" aria-controls="plan-prompt-panel" :tabindex="activeTab === tab.key ? 0 : -1" @click="activeTab = tab.key" @keydown="moveTab($event, index)">{{ tab.label }}</button>
      </div>
      <div class="plan-prompt-content-heading"><label for="plan-prompt-content">{{ selectedTab.label }}</label><span>{{ Array.from(activeContent).length }} 字</span></div>
      <p v-if="generating" class="plan-prompt-progress" role="status"><i aria-hidden="true"></i>正在按当前方案资料生成初稿…</p>
      <div id="plan-prompt-panel" role="tabpanel" :aria-labelledby="'plan-prompt-tab-' + activeTab">
        <textarea id="plan-prompt-content" ref="editor" v-model="activeContent" :readonly="unavailable" maxlength="20000" rows="18" spellcheck="false" :placeholder="'请填写' + selectedTab.label" @input="edited"></textarea>
      </div>
      <p v-if="validation" class="plan-prompt-error" role="alert">{{ validation }}</p>
      <details class="plan-prompt-preview">
        <summary>查看两份元提示词合成预览</summary>
        <p>保存后生效；每份最多20000字。</p>
        <label>主播互动元提示词 · {{ Array.from(compiled.interaction_prompt_md).length }} 字<textarea :value="compiled.interaction_prompt_md" readonly rows="8" aria-label="主播互动元提示词"></textarea></label>
        <label>主播话术生成提示词 · {{ Array.from(compiled.speech_prompt_md).length }} 字<textarea :value="compiled.speech_prompt_md" readonly rows="8" aria-label="主播话术生成提示词"></textarea></label>
      </details>
    </div>

    <Teleport to="body">
      <dialog ref="confirmation" class="plan-prompt-confirm" aria-labelledby="plan-prompt-confirm-heading" aria-describedby="plan-prompt-confirm-description" @cancel.prevent="cancelRegeneration" @click="backdrop">
        <h2 id="plan-prompt-confirm-heading">重新生成事实依据？</h2>
        <p id="plan-prompt-confirm-description">将按当前商品卡、活动福利和正式事实，替换“事实依据”中的内容，包括未保存的修改。另外三个文本框保持不变，生成后仍需保存。</p>
        <footer><button ref="cancelButton" type="button" class="plan-prompt-secondary" @click="cancelRegeneration">取消</button><button type="button" class="plan-prompt-primary" @click="confirmRegeneration">确认重新生成</button></footer>
      </dialog>
    </Teleport>
  </section>
</template>

<style scoped>
.plan-prompt-editor{box-sizing:border-box;display:grid;gap:20px;min-width:0;width:100%;color:#26324d;font-size:16px;line-height:1.65}
.plan-prompt-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;min-width:0}
.plan-prompt-heading>div:first-child{min-width:0;max-width:780px}.plan-prompt-heading h3{margin:0;font-size:24px;line-height:1.4}.plan-prompt-heading p{margin:10px 0 0;color:#71819d;font-size:16px;overflow-wrap:anywhere}
.plan-prompt-actions{display:flex;flex:none;align-items:center;gap:10px;max-width:100%}
.plan-prompt-editor button,.plan-prompt-confirm button{box-sizing:border-box;min-height:44px;padding:10px 16px;border:1px solid transparent;border-radius:12px;font:inherit;font-size:16px;font-weight:650;line-height:1.4;white-space:nowrap;cursor:pointer}
.plan-prompt-primary{background:#6554ec;color:#fff;box-shadow:0 6px 16px #6554ec20}.plan-prompt-secondary{background:#fff;color:#596682;border-color:#dce2f4!important}
.plan-prompt-editor button:disabled{opacity:.55;cursor:wait;box-shadow:none}.plan-prompt-editor button:focus-visible,.plan-prompt-confirm button:focus-visible{outline:3px solid #c5bfff;outline-offset:3px}
.plan-prompt-content{display:grid;gap:10px;min-width:0}.plan-prompt-content-heading{display:flex;align-items:center;justify-content:space-between;gap:12px;font-size:16px}.plan-prompt-content-heading label{font-weight:650}.plan-prompt-content-heading span{color:#78859d;white-space:nowrap}
.plan-prompt-tabs{display:flex;flex-wrap:wrap;gap:10px;margin-bottom:6px}.plan-prompt-tabs button{background:#f5f6ff;color:#596682;border-color:#e4e8fa}.plan-prompt-tabs button[aria-selected=true]{background:#6554ec;color:white;border-color:#6554ec}.plan-prompt-preview{min-width:0;padding:16px;border:1px solid #dfe3f4;border-radius:12px}.plan-prompt-preview summary{cursor:pointer;font-weight:650}.plan-prompt-preview p{color:#71819d}.plan-prompt-preview label{display:block;margin-top:16px}.plan-prompt-preview textarea{min-height:180px;margin-top:8px;font-size:16px}
.plan-prompt-content textarea{box-sizing:border-box;display:block;min-width:0;width:100%;min-height:460px;max-width:100%;padding:20px;border:1px solid #dfe3f4;border-radius:14px;background:#fff;color:#26324d;font:inherit;font-size:20px;line-height:1.8;resize:vertical;outline:none}.plan-prompt-content textarea:focus{border-color:#8273f6;box-shadow:0 0 0 3px #6554ec15}.plan-prompt-content textarea[readonly]{background:#f8f9fd}.plan-prompt-content textarea::placeholder{color:#8894aa;font-size:16px}
.plan-prompt-error{padding:14px 18px;border:1px solid #f1cbd2;border-radius:12px;background:#fff5f6;color:#a64256;font-size:16px;overflow-wrap:anywhere}.plan-prompt-error p{margin:4px 0 0}.plan-prompt-error button{margin-top:10px;background:white;color:inherit;border-color:#e8bcc6}
.plan-prompt-notice{margin:0;padding:14px 18px;border:1px solid #d6e8e2;border-radius:12px;background:#f0faf6;color:#307c69;font-size:16px;overflow-wrap:anywhere}
.plan-prompt-skeleton{display:grid;gap:18px;min-height:420px;padding:24px;border:1px solid #e2e6f3;border-radius:14px;background:#fafbff}.plan-prompt-skeleton p{margin:0;color:#71819d}.plan-prompt-skeleton span{height:18px;max-width:92%;border-radius:7px;background:linear-gradient(90deg,#e9edf7,#f4f6fc,#e9edf7);background-size:200% 100%;animation:prompt-loading 1.5s ease-in-out infinite}.plan-prompt-skeleton span:last-child{max-width:60%}
.plan-prompt-progress{display:flex;align-items:center;gap:10px;margin:0;color:#6554d8;font-size:16px}.plan-prompt-progress i{width:16px;height:16px;flex:none;border:2px solid #d8d2fc;border-top-color:#6554ec;border-radius:50%;animation:prompt-spin .8s linear infinite}
.plan-prompt-confirm{box-sizing:border-box;width:min(520px,calc(100vw - 32px));max-height:calc(100dvh - 32px);margin:auto;padding:28px;border:1px solid #dfe3f4;border-radius:22px;background:#fff;color:#26324d;font:inherit;font-size:16px;line-height:1.7;box-shadow:0 24px 80px #26324d33;overflow:auto}.plan-prompt-confirm::backdrop{background:#26324d66;backdrop-filter:blur(4px)}.plan-prompt-confirm h2{margin:0;font-size:22px}.plan-prompt-confirm p{margin:14px 0 24px;color:#68758e;font-size:16px;overflow-wrap:anywhere}.plan-prompt-confirm footer{display:flex;justify-content:flex-end;gap:10px}
@keyframes prompt-loading{to{background-position:-200% 0}}@keyframes prompt-spin{to{transform:rotate(360deg)}}
@media(max-width:900px){.plan-prompt-heading{flex-direction:column;gap:16px}.plan-prompt-content textarea{padding:16px}.plan-prompt-confirm{padding:22px}.plan-prompt-heading h3{font-size:22px}}
@media(prefers-reduced-motion:reduce){.plan-prompt-skeleton span,.plan-prompt-progress i{animation:none}}
</style>
