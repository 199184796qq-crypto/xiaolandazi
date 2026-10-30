<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { request } from '../api'
import type { AdoptLiveAgentPlanProductLinksOutput, LiveAgentPlanProductLink } from '../types'

const props = defineProps<{ planId: number; tenantId: number; scopeKey: string; items: LiveAgentPlanProductLink[] }>()
const emit = defineEmits<{ created: [item: LiveAgentPlanProductLink] }>()
const opened = ref(false), saving = ref(false), error = ref('')
const dialog = ref<HTMLDialogElement | null>(null), nameInput = ref<HTMLInputElement | null>(null)
const draft = reactive({ link_key: '', product_name: '', spec: '', daily_price: '', quantity: '', audience: '' })
const canSave = computed(() => !saving.value && !!draft.link_key.trim() && !!draft.product_name.trim())
let epoch = 0, previousFocus: HTMLElement | null = null, previousOverflow = ''

function dismiss() {
  if (!opened.value) return
  dialog.value?.close()
  opened.value = false
  document.body.style.overflow = previousOverflow
  if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true })
}
function close() { if (!saving.value) dismiss() }
async function open() {
  if (!props.planId || !props.tenantId || opened.value) return
  const token = ++epoch
  Object.assign(draft, { link_key: '', product_name: '', spec: '', daily_price: '', quantity: '', audience: '' })
  error.value = ''
  previousFocus = document.activeElement as HTMLElement | null
  previousOverflow = document.body.style.overflow
  opened.value = true
  document.body.style.overflow = 'hidden'
  await nextTick()
  if (token !== epoch || !opened.value) return
  dialog.value?.showModal()
  nameInput.value?.focus({ preventScroll: true })
}
async function save() {
  if (!canSave.value) return
  const planId = props.planId, tenantId = props.tenantId, token = epoch
  const candidate = Object.fromEntries(Object.entries(draft).map(([key, value]) => [key, value.trim()]))
  if (props.items.some(item => item.link_key.trim() === candidate.link_key)) {
    error.value = '这个商品编号已存在，请填写其他编号，或直接编辑已有商品卡。'
    return
  }
  saving.value = true
  error.value = ''
  try {
    const result = await request<AdoptLiveAgentPlanProductLinksOutput>(`/api/v1/live-agent-plans/${planId}/product-links/adopt`, {
      method: 'POST', suppressErrorDialog: true,
      body: JSON.stringify({ tenant_id: tenantId, source_ref: 'manual:product-card', links: [{ ...candidate, import_mode: 'merge', review_bucket: 'adoptable', review_reason: '用户手动填写', source_quotes: ['用户手动添加商品卡'] }] }),
    })
    if (token !== epoch) return
    const item = result.results?.[0]
    if (item?.status !== 'adopted' || !item.saved) throw new Error(item?.message || '商品卡未保存，请核对后重试')
    if (item.saved.plan_id !== planId || item.saved.tenant_id !== tenantId || !(item.saved.id > 0)) throw new Error('商品卡保存回执不匹配，请重新读取当前方案')
    emit('created', item.saved)
    dismiss()
  } catch (err) {
    if (token === epoch) error.value = err instanceof Error ? err.message : '添加商品卡失败，请重试'
  } finally { if (token === epoch) saving.value = false }
}
function backdrop(event: MouseEvent) {
  if (event.target !== dialog.value || !dialog.value) return
  const box = dialog.value.getBoundingClientRect()
  if (event.clientX < box.left || event.clientX > box.right || event.clientY < box.top || event.clientY > box.bottom) close()
}
watch(() => [props.planId, props.tenantId, props.scopeKey], () => { ++epoch; dismiss(); saving.value = false; error.value = '' }, { flush: 'sync' })
onBeforeUnmount(() => { ++epoch; dismiss() })
</script>

<template>
  <button type="button" class="product-card-add-button" :disabled="!planId || !tenantId" @click="open"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>添加商品卡</button>
  <Teleport to="body">
    <dialog v-if="opened" ref="dialog" class="product-card-create-dialog" aria-labelledby="product-card-create-title" aria-describedby="product-card-create-description" :aria-busy="saving" @cancel.prevent="close" @click="backdrop">
      <header><div><h2 id="product-card-create-title">添加商品卡</h2><p id="product-card-create-description">填写真实商品资料，保存后可继续补充个性属性。</p></div><button type="button" class="product-card-create-close" aria-label="关闭添加商品卡" :disabled="saving" @click="close">×</button></header>
      <form @submit.prevent="save">
        <fieldset :disabled="saving">
          <label><span>商品编号 <b>*</b></span><input v-model="draft.link_key" required maxlength="64" placeholder="例如：1号链接" /></label>
          <label><span>商品名称 <b>*</b></span><input ref="nameInput" v-model="draft.product_name" required maxlength="255" placeholder="填写商品名称" /></label>
          <label><span>规格（选填）</span><input v-model="draft.spec" maxlength="255" placeholder="例如：500克/袋" /></label>
          <label><span>销售价（选填）</span><input v-model="draft.daily_price" maxlength="255" placeholder="例如：39.9元" /></label>
          <label><span>数量（选填）</span><input v-model="draft.quantity" maxlength="255" placeholder="例如：2袋/份" /></label>
          <label><span>适用说明（选填）</span><textarea v-model="draft.audience" maxlength="512" rows="2" placeholder="填写适用人群或使用场景" /></label>
        </fieldset>
        <p v-if="error" class="product-card-create-error" role="alert">{{ error }}</p>
        <footer><button type="button" :disabled="saving" @click="close">取消</button><button type="submit" class="product-card-create-save" :disabled="!canSave">{{ saving ? '保存中…' : '保存商品卡' }}</button></footer>
      </form>
    </dialog>
  </Teleport>
</template>

<style scoped>
.product-card-add-button{display:inline-flex;align-items:center;justify-content:center;gap:7px;flex:none;min-height:42px;padding:9px 16px;border:0;border-radius:11px;background:linear-gradient(125deg,#8574ff,#5545f4);color:#fff;font:inherit;font-size:16px;font-weight:750;cursor:pointer;box-shadow:0 5px 14px #6655f52b}
.product-card-add-button svg{width:19px;height:19px;fill:none;stroke:currentColor;stroke-width:2;stroke-linecap:round}
.product-card-create-dialog{box-sizing:border-box;width:min(720px,calc(100vw - 32px));max-height:calc(100dvh - 32px);padding:26px;border:1px solid #e1e5fb;border-radius:22px;background:#fff;color:#243451;font-family:inherit;font-size:16px;box-shadow:0 24px 90px #26345a3b;overflow:auto}
.product-card-create-dialog::backdrop{background:#26345166;backdrop-filter:blur(3px)}
.product-card-create-dialog header{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;margin-bottom:22px}
h2{margin:0;font-size:23px;line-height:1.4}header p{margin:8px 0 0;color:#73819b;font-size:16px;line-height:1.6}
fieldset{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:18px;margin:0;padding:0;border:0;min-width:0}
label{display:grid;align-content:start;gap:8px;min-width:0;color:#405070;font-size:16px;font-weight:650}label b{color:#cf4d68}
input,textarea{box-sizing:border-box;width:100%;min-width:0;min-height:44px;padding:11px 12px;border:1px solid #dce2f4;border-radius:10px;background:#fafbff;color:#263451;font:inherit;font-size:16px;line-height:1.5}textarea{resize:vertical}
footer{display:flex;justify-content:flex-end;gap:12px;margin-top:24px}button{min-height:42px;padding:10px 20px;border:1px solid #dce2f4;border-radius:10px;background:#fff;color:#536382;font:inherit;font-size:16px;cursor:pointer}
.product-card-create-save{background:#6354f4;border-color:#6354f4;color:#fff;font-weight:700}.product-card-create-close{flex:none;min-width:42px;padding:3px;font-size:26px;line-height:1}
.product-card-create-error{margin:18px 0 0;color:#ba3550;font-size:16px;line-height:1.6;overflow-wrap:anywhere}button:disabled,fieldset:disabled{opacity:.55;cursor:not-allowed}
button:focus-visible,input:focus-visible,textarea:focus-visible{outline:2px solid #8d7bff;outline-offset:3px}
@media(max-width:600px){.product-card-create-dialog{padding:20px 16px}fieldset{grid-template-columns:minmax(0,1fr)}header p{font-size:16px}}
</style>
