<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import { renderLiveAgentPlanPrompt } from '../api'

const content = defineModel<string>({ required: true })
const props = defineProps<{ planId: number | null; loaded: boolean; loading: boolean; saving: boolean; error: string; notice: string }>()
const emit = defineEmits<{ save: []; retry: []; clearFeedback: [] }>()
const generating = ref(false)
const generationError = ref('')
const generationNotice = ref('')
const confirmation = ref<HTMLDialogElement | null>(null)
const cancelButton = ref<HTMLButtonElement | null>(null)
const regenerateButton = ref<HTMLButtonElement | null>(null)
const editor = ref<HTMLTextAreaElement | null>(null)
const hasContent = computed(() => Boolean(content.value.trim()))
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
    if (typeof result.prompt_md !== 'string' || !result.prompt_md.trim()) throw new Error('生成结果为空，请重试')
    if (result.prompt_md.length > 20000) throw new Error('生成内容超过 20000 字，请调整方案资料后重试')
    content.value = result.prompt_md
    generationNotice.value = '已按商品卡、活动福利和事实依据生成初稿，检查后保存'
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
  if (unavailable.value || !hasContent.value || confirmation.value?.open) return
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
  if (unavailable.value || !hasContent.value) return
  generationError.value = ''
  generationNotice.value = ''
  emit('save')
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
        <p>按商品卡、活动福利和事实依据生成初稿，你改完再保存；它只约束当前方案，不改主播风格和事实本身。</p>
      </div>
      <div class="plan-prompt-actions">
        <template v-if="hasContent">
          <button ref="regenerateButton" type="button" class="plan-prompt-secondary" :disabled="unavailable" @click="requestRegeneration">{{ generating ? '生成中…' : '重新生成' }}</button>
          <button type="button" class="plan-prompt-primary" :disabled="unavailable" @click="save">{{ saving ? '保存中…' : '保存方案提示词' }}</button>
        </template>
        <button v-else type="button" class="plan-prompt-primary" :disabled="unavailable" @click="generate">{{ generating ? '生成中…' : '建立提示词' }}</button>
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
      <div class="plan-prompt-content-heading"><label for="plan-prompt-content">提示词内容</label><span>{{ content.length }} / 20000</span></div>
      <p v-if="generating" class="plan-prompt-progress" role="status"><i aria-hidden="true"></i>正在按当前方案资料生成初稿…</p>
      <textarea id="plan-prompt-content" ref="editor" v-model="content" :readonly="unavailable" maxlength="20000" rows="18" spellcheck="false" placeholder="可点击“建立提示词”生成初稿，也可以在这里直接填写。" @input="edited"></textarea>
    </div>

    <Teleport to="body">
      <dialog ref="confirmation" class="plan-prompt-confirm" aria-labelledby="plan-prompt-confirm-heading" aria-describedby="plan-prompt-confirm-description" @cancel.prevent="cancelRegeneration" @click="backdrop">
        <h2 id="plan-prompt-confirm-heading">重新生成方案提示词？</h2>
        <p id="plan-prompt-confirm-description">会覆盖当前内容，包括尚未保存的修改。生成成功后仍需你检查并手动保存。</p>
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
