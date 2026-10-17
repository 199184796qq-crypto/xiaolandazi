<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { adoptLiveAgentPlanFacts } from '../api'
import { factCategoryGroups, factCategoryState, loadFactCategories } from '../factCategories'
import type { AgentLearningSession } from '../types'

const props = defineProps<{ roomId: number; planId: number; item: AgentLearningSession }>()
const emit = defineEmits<{ close: []; saved: [result: { roomId: number; planId: number; tenantId: number; itemId: number; unchanged: boolean }] }>()
// Keep the question's original scope even if the parent changes while saving.
const scope = { roomId: props.roomId, planId: props.planId, tenantId: props.item.tenant_id, itemId: props.item.id }
const dialog = ref<HTMLDialogElement | null>(null)
const valueInput = ref<HTMLTextAreaElement | null>(null)
const category = ref('other')
const key = ref(Array.from(props.item.review?.needed_facts?.[0] || props.item.question || '').slice(0, 255).join(''))
const value = ref('')
const saving = ref(false)
const error = ref('')
const groups = computed(() => factCategoryGroups())
const categoryReady = computed(() => !factCategoryState.loading && !factCategoryState.error && factCategoryState.items.some(item => item.value === category.value))
const canSave = computed(() => categoryReady.value && !!key.value.trim() && !!value.value.trim() && !saving.value)
let disposed = false
let previousFocus: HTMLElement | null = null

async function refreshCategories() {
  await loadFactCategories({ planId: scope.planId, suppressErrorDialog: true })
  if (!disposed && !factCategoryState.items.some(item => item.value === category.value)) category.value = factCategoryState.items[0]?.value || 'other'
}

onMounted(() => {
  previousFocus = document.activeElement as HTMLElement | null
  dialog.value?.showModal()
  valueInput.value?.focus({ preventScroll: true })
  void refreshCategories()
})
onBeforeUnmount(() => {
  disposed = true
  dialog.value?.close()
  if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true })
})

function close() { if (!saving.value) emit('close') }

function onKeydown(event: KeyboardEvent) {
  if (event.key !== 'Tab') return
  const fields = Array.from(dialog.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled)') || [])
    .filter(field => field.getClientRects().length)
  const first = fields[0], last = fields[fields.length - 1]
  if (!first) { event.preventDefault(); return }
  if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog.value)) {
    event.preventDefault(); last?.focus()
  } else if (!event.shiftKey && (document.activeElement === last || document.activeElement === dialog.value)) {
    event.preventDefault(); first.focus()
  }
}

async function save() {
  if (!canSave.value || disposed) return
  saving.value = true
  error.value = ''
  try {
    const result = await adoptLiveAgentPlanFacts(scope.planId, [{
      category: category.value,
      key: key.value.trim(),
      value: value.value.trim(),
      status: 'pending',
      review_bucket: 'adoptable',
      source_quote: '',
    }], scope.tenantId, `manual:interaction-learning:room:${scope.roomId}:record:${scope.itemId}`, { suppressErrorDialog: true })
    if (disposed) return
    const unchanged = result.results?.[0]?.status === 'unchanged'
    if (!result.adopted && !unchanged) throw new Error(result.results?.[0]?.message || '事实未保存，请核对后重试')
    emit('saved', { ...scope, unchanged })
  } catch (cause) {
    if (!disposed) error.value = cause instanceof Error ? cause.message : '保存事实失败，请重试'
  } finally { if (!disposed) saving.value = false }
}
</script>

<template>
  <Teleport to="body">
    <dialog ref="dialog" class="interaction-fact-dialog" tabindex="-1" aria-labelledby="interaction-fact-title" aria-describedby="interaction-fact-help" :aria-busy="saving" @cancel.prevent="close" @keydown="onKeydown">
      <header>
        <div><span class="dialog-eyebrow">把事实补充完整</span><h2 id="interaction-fact-title">补充事实依据</h2></div>
        <button type="button" class="dialog-close" aria-label="关闭补充事实依据" :disabled="saving" @click="close">×</button>
      </header>
      <div class="question-context">
        <strong>{{ item.question }}</strong>
        <p v-if="item.review?.needed_facts?.length">建议补充：{{ item.review.needed_facts.join('；') }}</p>
      </div>
      <p id="interaction-fact-help" class="dialog-help">填写你已确认的业务事实，保存到当前智能体方案。</p>
      <form @submit.prevent="save">
        <div class="fact-fields">
          <label class="category-field">事实分类
            <select v-model="category" aria-label="事实分类" :disabled="saving || !categoryReady" required>
              <option v-if="!factCategoryState.items.length" value="other">正在读取分类…</option>
              <optgroup v-for="group in groups" :key="group.label" :label="group.label"><option v-for="option in group.options" :key="option.value" :value="option.value">{{ option.label }}</option></optgroup>
            </select>
          </label>
          <label class="name-field">事实名称
            <input v-model="key" maxlength="255" :disabled="saving" required placeholder="给这条事实起个名字" />
          </label>
        </div>
        <p v-if="factCategoryState.error" class="dialog-error" role="alert">{{ factCategoryState.error }} <button type="button" :disabled="saving || factCategoryState.loading" @click="refreshCategories">重试读取分类</button></p>
        <label class="value-field">事实内容
          <textarea ref="valueInput" v-model="value" rows="5" maxlength="4000" :disabled="saving" required placeholder="请填写真实情况，例如具体的口味、工艺或适用条件。" />
        </label>
        <div class="content-length">{{ value.length }} / 4000</div>
        <p v-if="error" class="dialog-error" role="alert">{{ error }}</p>
        <footer>
          <span class="dialog-tip" role="status">{{ saving ? '正在保存事实，稍等一下哦～' : '保存后会留在当前问题，核对好再标记完成。' }}</span>
          <div class="dialog-actions"><button type="button" :disabled="saving" @click="close">取消</button><button type="submit" class="save-fact" :disabled="!canSave">{{ saving ? '保存中…' : '保存事实依据' }}</button></div>
        </footer>
      </form>
    </dialog>
  </Teleport>
</template>

<style scoped>
.interaction-fact-dialog{box-sizing:border-box;width:min(700px,calc(100vw - 32px));max-height:calc(100dvh - 40px);margin:auto;padding:28px;border:1px solid #dbe4fb;border-radius:24px;background:linear-gradient(145deg,#fff,#f8faff);color:#283a59;font-family:inherit;font-size:16px;box-shadow:0 28px 100px #20306340;overflow:auto}
.interaction-fact-dialog::backdrop{background:#18274966;backdrop-filter:blur(4px)}
header{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;margin-bottom:20px}.dialog-eyebrow{font-size:13px;color:#8494b2}h2{margin:6px 0 0;font-size:25px;line-height:1.4}.dialog-close{width:36px;height:36px;flex-shrink:0;padding:0;font-size:25px;line-height:1}
.question-context{padding:16px 18px;background:#f0f2ff;border:1px solid #e4e7ff;border-radius:14px;overflow-wrap:anywhere}.question-context strong{display:block;font-size:17px}.question-context p{margin:8px 0 0;line-height:1.7;color:#687b9a}.dialog-help{color:#7485a2;line-height:1.7;margin:16px 0}
.fact-fields{display:grid;grid-template-columns:170px minmax(0,1fr);gap:16px}label{display:flex;flex-direction:column;gap:9px;font-weight:700;min-width:0}input,select,textarea{box-sizing:border-box;width:100%;padding:12px;border:1px solid #d8e1f6;border-radius:11px;background:white;color:inherit;font:inherit;font-weight:400}input,select{height:46px}.value-field{margin-top:18px}textarea{line-height:1.7;resize:vertical;min-height:150px}.content-length{margin-top:5px;text-align:right;font-size:12px;color:#93a1bb}.dialog-error{padding:12px;border-radius:10px;background:#fff0f3;color:#b52f50;line-height:1.7;overflow-wrap:anywhere}
footer{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-top:20px}.dialog-tip{font-size:13px;color:#8493b0;line-height:1.7}.dialog-actions{display:flex;gap:10px;flex-shrink:0}button{padding:11px 16px;border:1px solid #d7dff7;border-radius:11px;background:white;color:#5b61cc;font:inherit;cursor:pointer}.save-fact{color:white;border-color:transparent;background:linear-gradient(110deg,#7089ff,#5756ed);font-weight:700;box-shadow:0 6px 18px #5c6aed28}button:disabled{opacity:.55;cursor:not-allowed}input:focus-visible,select:focus-visible,textarea:focus-visible,button:focus-visible{outline:2px solid #7785f8;outline-offset:2px}
@media(max-width:600px){.interaction-fact-dialog{padding:20px;width:calc(100vw - 24px);max-height:calc(100dvh - 24px);border-radius:20px}h2{font-size:22px}.fact-fields{grid-template-columns:1fr;gap:14px}footer{flex-direction:column;align-items:stretch;gap:12px}.dialog-actions{justify-content:flex-end}}
</style>
