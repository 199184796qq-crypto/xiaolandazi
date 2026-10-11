<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { factCategoryGroups, factCategoryState, loadFactCategories } from '../factCategories'

type FactDraft = { category: string; key: string; value: string; forbidden_wording: string; safe_rewrite: string }
const draft = defineModel<FactDraft>({ required: true })
const props = withDefaults(defineProps<{ saving: boolean; nameLoading: boolean; nameError: string; error: string; guidance?: string }>(), { guidance: '' })
const emit = defineEmits<{ close: []; save: []; nameEdited: []; retryName: [] }>()
const dialog = ref<HTMLDialogElement | null>(null)
const contentInput = ref<HTMLTextAreaElement | null>(null)
const categoryReady = computed(() => !factCategoryState.loading && !factCategoryState.error && factCategoryState.items.some(item => item.value === draft.value.category))
const canSave = computed(() => !props.saving && categoryReady.value && !!draft.value.value.trim())
let previousFocus: HTMLElement | null = null
let previousOverflow = ''

onMounted(() => {
  previousFocus = document.activeElement as HTMLElement | null
  previousOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  dialog.value?.showModal()
  contentInput.value?.focus({ preventScroll: true })
})
onBeforeUnmount(() => {
  dialog.value?.close()
  document.body.style.overflow = previousOverflow
  if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true })
})
function close() { if (!props.saving) emit('close') }
function save() { if (canSave.value) emit('save') }
function onBackdropClick(event: MouseEvent) {
  const element = dialog.value
  if (!element || event.target !== element) return
  const bounds = element.getBoundingClientRect()
  if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) close()
}
function onKeydown(event: KeyboardEvent) {
  if (event.key !== 'Tab') return
  const fields = Array.from(dialog.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled)') || [])
    .filter(field => field.getClientRects().length)
  const first = fields[0], last = fields[fields.length - 1]
  if (!first) { event.preventDefault(); return }
  if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog.value)) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && (document.activeElement === last || document.activeElement === dialog.value)) { event.preventDefault(); first.focus() }
}
</script>

<template>
  <Teleport to="body">
    <dialog ref="dialog" class="formal-fact-create-dialog" tabindex="-1" aria-labelledby="formal-fact-create-title" aria-describedby="formal-fact-create-subtitle" :aria-busy="saving" @cancel.prevent="close" @click="onBackdropClick" @keydown="onKeydown">
      <header class="fact-create-header">
        <div class="fact-create-brand">
          <i aria-hidden="true"><svg viewBox="0 0 32 32"><path d="M7 3h12l6 6v9M19 3v7h6M7 3v25h10M11 15h9M11 20h5M24 20v10M19 25h10" /></svg></i>
          <div><h2 id="formal-fact-create-title">添加事实</h2><p id="formal-fact-create-subtitle">补充新的事实内容，让智能体回答更准确</p></div>
        </div>
        <p class="fact-create-header-tip"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9 18h6M10 21h4M8 14a6 6 0 1 1 8 0l-1 3H9z" /></svg><span>提示：请尽量填写完整的信息，帮助智能体更好地理解和使用</span></p>
        <button class="fact-create-close" type="button" aria-label="关闭添加事实" :disabled="saving" @click="close">×</button>
      </header>
      <form @submit.prevent="save">
        <div v-if="guidance" class="fact-create-guidance"><strong>轮盘发现的待补事实</strong><p>{{ guidance }}</p><span>请核对真实资料后填写答案；不要把问题或猜测直接保存成事实。</span></div>
        <div class="fact-create-fields">
          <label class="fact-create-field is-category">
            <span class="fact-create-field-title"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z" /></svg><strong>事实分类</strong><b aria-hidden="true">*</b></span>
            <select v-model="draft.category" aria-label="事实分类" :disabled="saving || factCategoryState.loading || !!factCategoryState.error" required>
              <optgroup v-for="group in factCategoryGroups(draft.category)" :key="group.label" :label="group.label"><option v-for="option in group.options" :key="option.value" :value="option.value">{{ option.label }}</option></optgroup>
            </select>
          </label>
          <label class="fact-create-field is-name">
            <span class="fact-create-field-title"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m3 11 8-8h9v9l-8 8z" /><circle cx="16" cy="7" r="1" /></svg><strong>事实名称</strong><b aria-hidden="true">*</b></span>
            <input v-model="draft.key" aria-label="事实名称" aria-describedby="fact-create-name-help" maxlength="255" :disabled="saving" placeholder="请输入事实名称" @input="emit('nameEdited')" />
            <small id="fact-create-name-help">{{ nameLoading ? '正在提炼名称…' : '填写内容后会自动生成名称，也可自行修改' }}</small>
          </label>
          <label class="fact-create-field is-content">
            <span class="fact-create-field-title"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6 3h9l4 4v14H6zM14 3v5h5M9 12h7M9 16h5" /></svg><strong>事实内容（可以说）</strong><b aria-hidden="true">*</b></span>
            <span class="fact-create-textarea"><textarea ref="contentInput" v-model="draft.value" aria-label="事实内容（可以说）" :disabled="saving" rows="2" maxlength="4000" required placeholder="填写已确认的正向或负向事实，保存时会自动整理…"></textarea><small>{{ draft.value.length }}/4000</small></span>
          </label>
          <label class="fact-create-field is-forbidden">
            <span class="fact-create-field-title"><i aria-hidden="true"><svg viewBox="0 0 24 24"><path d="m6 6 12 12M18 6 6 18" /></svg></i><strong>不要这样说（选填）</strong></span>
            <span class="fact-create-textarea"><textarea v-model="draft.forbidden_wording" aria-label="不要这样说（选填）" :disabled="saving" rows="2" maxlength="4000" placeholder="填写不能说什么，保存时整理为自然语言负向约束…"></textarea><small>{{ draft.forbidden_wording.length }}/4000</small></span>
          </label>
          <label class="fact-create-field is-rewrite">
            <span class="fact-create-field-title"><i aria-hidden="true"><svg viewBox="0 0 24 24"><path d="m5 12 4 4L19 6" /></svg></i><strong>改成这样说（选填）</strong></span>
            <span class="fact-create-textarea"><textarea v-model="draft.safe_rewrite" aria-label="改成这样说（选填）" :disabled="saving" rows="2" maxlength="4000" placeholder="填写正向口播例子，也可留空由模型基于事实举例…"></textarea><small>{{ draft.safe_rewrite.length }}/4000</small></span>
          </label>
        </div>
        <p v-if="factCategoryState.error" class="fact-create-error" role="alert">分类配置读取失败：{{ factCategoryState.error }} <button type="button" :disabled="saving || factCategoryState.loading" @click="loadFactCategories({ suppressErrorDialog: true })">重新加载</button></p>
        <p v-if="nameError" class="fact-create-error" role="alert">{{ nameError }} <button type="button" :disabled="saving || nameLoading" @click="emit('retryName')">重试提炼名称</button></p>
        <p v-if="error" class="fact-create-error" role="alert">{{ error }}</p>
        <footer class="fact-create-footer">
          <p><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9" /><path d="M12 11v6M12 7h.01" /></svg><span>{{ saving ? '正在整理三栏并复核，请稍候…' : !draft.value.trim() ? '先写一点真实内容，就可以保存啦～' : '保存后将用于当前方案的主线口播与智能问答，支持随时修改。' }}</span></p>
          <div><button type="button" class="fact-create-cancel" :disabled="saving" @click="close">取消</button><button class="fact-create-save" type="submit" :disabled="!canSave"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 3h12l4 4v14H3V3zM7 3v6h10V3M7 21v-8h10v8" /></svg>{{ saving ? '保存中…' : '保存事实依据' }}</button></div>
        </footer>
      </form>
    </dialog>
  </Teleport>
</template>

<style scoped>
.formal-fact-create-dialog { box-sizing:border-box; width:min(1320px,calc(100vw - 48px)); max-height:calc(100dvh - 40px); margin:auto; padding:26px; border:1px solid #e0e7ff; border-radius:24px; background:linear-gradient(135deg,#fff,#f8faff 65%,#f6f8ff); color:#253b60; font-family:inherit; font-size:15px; box-shadow:0 30px 100px #1e305047; overflow:auto; }
.formal-fact-create-dialog::backdrop { background:#24304970; backdrop-filter:blur(5px); }
.fact-create-header { position:relative; display:flex; align-items:center; justify-content:space-between; gap:24px; margin-bottom:20px; padding-right:16px; }
.fact-create-brand { display:flex; align-items:center; gap:18px; min-width:0; }
.fact-create-brand>i { display:grid; flex:0 0 64px; width:64px; height:64px; place-items:center; border:1px solid #d9dcff; border-radius:17px; background:linear-gradient(145deg,#e4e4ff,#b9b9ff); color:#5f60eb; box-shadow:0 6px 14px #8585ef24; }
.fact-create-brand svg { width:34px; height:34px; fill:none; stroke:currentColor; stroke-width:2; stroke-linecap:round; stroke-linejoin:round; }
.fact-create-brand h2 { margin:0; color:#15274a; font-size:27px; font-weight:850; line-height:1.35; }
.fact-create-brand p { margin:6px 0 0; color:#627493; font-size:14px; line-height:1.6; }
.fact-create-header-tip { display:flex; align-items:center; gap:9px; max-width:440px; margin:0; padding:12px 14px; border-radius:13px; background:linear-gradient(110deg,#edf3ff,#e9efff); color:#7587ed; font-size:12px; line-height:1.6; }
.fact-create-header-tip svg { flex:0 0 19px; width:19px; height:19px; fill:none; stroke:currentColor; stroke-width:1.8; stroke-linecap:round; stroke-linejoin:round; }
.fact-create-close { position:absolute; top:-11px; right:-12px; display:grid; place-items:center; width:30px; height:30px; padding:0; border:1px solid #e5eaff; border-radius:50%; background:#fff; color:#8292b1; font-size:24px; line-height:1; box-shadow:0 2px 8px #8e9ec517; cursor:pointer; }
.fact-create-fields { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:13px; }
.fact-create-field { display:flex; flex-direction:column; gap:9px; min-width:0; padding:15px 16px; border-radius:14px; background:linear-gradient(115deg,#eef3ff,#f0f4ff); }
.fact-create-field.is-category { background:linear-gradient(115deg,#eaf3ff,#edf6ff); }
.fact-create-field.is-content { grid-column:1 / -1; }
.fact-create-field.is-forbidden { background:linear-gradient(115deg,#fff0f5,#fdf4f7); }
.fact-create-field.is-rewrite { background:linear-gradient(115deg,#eafbF2,#effcf7); }
.fact-create-field-title { display:flex; align-items:center; gap:9px; color:#48618a; font-size:15px; line-height:1.5; }
.fact-create-field-title>svg { width:23px; height:23px; flex:0 0 23px; fill:none; stroke:#6f79e4; stroke-width:2; stroke-linecap:round; stroke-linejoin:round; }
.fact-create-field.is-category .fact-create-field-title>svg { stroke:#5b95ff; }
.fact-create-field-title>b { color:#f15c72; font-size:17px; }
.fact-create-field-title i { display:grid; place-items:center; flex:0 0 23px; width:23px; height:23px; border-radius:50%; background:#fc6588; color:#fff; }
.fact-create-field.is-rewrite .fact-create-field-title i { background:#20bd80; }
.fact-create-field-title i svg { width:16px; height:16px; fill:none; stroke:currentColor; stroke-width:2.3; stroke-linecap:round; stroke-linejoin:round; }
.fact-create-field input,.fact-create-field select,.fact-create-field textarea { box-sizing:border-box; width:100%; border:1px solid #dce4f3; border-radius:10px; padding:11px 12px; background:#fff; color:#263c61; font:inherit; font-weight:400; }
.fact-create-field input,.fact-create-field select { height:44px; }
.fact-create-field>small { color:#8294b7; font-size:12px; line-height:1.5; }
.fact-create-textarea { position:relative; display:block; }
.fact-create-field textarea { display:block; min-height:88px; padding-bottom:30px; line-height:1.65; resize:vertical; }
.fact-create-textarea>small { position:absolute; right:16px; bottom:10px; padding-left:4px; border-radius:4px; background:#fff; color:#8694b1; font-size:12px; font-variant-numeric:tabular-nums; pointer-events:none; }
.fact-create-field input::placeholder,.fact-create-field textarea::placeholder { color:#a0acc2; }
.fact-create-guidance { margin-bottom:14px; padding:14px 16px; border:1px solid #dbe5fc; border-radius:13px; background:#eef4ff; color:#5c74a4; line-height:1.65; }
.fact-create-guidance p { margin:5px 0; }
.fact-create-error { display:flex; align-items:center; gap:10px; flex-wrap:wrap; margin:12px 0 0; padding:10px 12px; border-radius:10px; background:#fff0f4; color:#c64968; font-size:13px; line-height:1.6; overflow-wrap:anywhere; }
.fact-create-error button { padding:5px 9px; border:1px solid #f5c3d1; border-radius:7px; background:white; color:inherit; font:inherit; cursor:pointer; }
.fact-create-footer { display:flex; align-items:center; justify-content:space-between; gap:20px; margin-top:14px; padding-top:17px; border-top:1px solid #e7ecf8; }
.fact-create-footer>p { display:flex; align-items:center; gap:9px; margin:0; color:#8494b4; font-size:12px; line-height:1.6; }
.fact-create-footer>p svg { width:19px; height:19px; flex:0 0 19px; fill:none; stroke:currentColor; stroke-width:1.8; stroke-linecap:round; }
.fact-create-footer>div { display:flex; flex-shrink:0; align-items:center; gap:12px; }
.fact-create-footer button { display:inline-flex; align-items:center; justify-content:center; gap:9px; height:48px; border:1px solid #dce4f7; border-radius:12px; background:#fff; color:#334971; font:inherit; font-weight:700; white-space:nowrap; cursor:pointer; }
.fact-create-cancel { width:90px; }
.fact-create-footer .fact-create-save { width:180px; border-color:transparent; background:linear-gradient(120deg,#727af6,#414bdd); color:#fff; box-shadow:0 6px 14px #636beb2e; }
.fact-create-save svg { width:21px; height:21px; fill:none; stroke:currentColor; stroke-width:1.8; stroke-linecap:round; stroke-linejoin:round; }
button:disabled,input:disabled,select:disabled,textarea:disabled { opacity:.58; cursor:not-allowed; }
input:focus-visible,select:focus-visible,textarea:focus-visible,button:focus-visible { outline:2px solid #8d9df8; outline-offset:2px; }
@media(max-width:760px) {
  .formal-fact-create-dialog { width:calc(100vw - 24px); max-height:calc(100dvh - 24px); padding:20px 16px; border-radius:20px; }
  .fact-create-header { align-items:flex-start; flex-direction:column; gap:13px; padding-right:10px; }
  .fact-create-brand { gap:12px; }
  .fact-create-brand>i { flex-basis:48px; width:48px; height:48px; border-radius:13px; }
  .fact-create-brand h2 { font-size:23px; }
  .fact-create-brand p { font-size:12px; }
  .fact-create-header-tip { max-width:none; padding:9px 11px; font-size:11px; }
  .fact-create-fields { grid-template-columns:1fr; }
  .fact-create-field { padding:13px; }
  .fact-create-footer { flex-direction:column; align-items:stretch; gap:13px; }
  .fact-create-footer>div { justify-content:flex-end; gap:10px; }
  .fact-create-footer .fact-create-save { width:170px; }
}
</style>
