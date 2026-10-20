<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'

const name = defineModel<string>('name', { required: true })
const description = defineModel<string>('description', { required: true })
const props = defineProps<{ busy: boolean; roomName?: string; error: string; returnFocus?: HTMLElement | null }>()
const emit = defineEmits<{ close: []; create: [] }>()
const dialog = ref<HTMLDialogElement | null>(null)
const nameInput = ref<HTMLInputElement | null>(null)
const canCreate = computed(() => !props.busy && !!name.value.trim())
let previousFocus: HTMLElement | null = null
let previousOverflow = ''

onMounted(() => {
  previousFocus = props.returnFocus || document.activeElement as HTMLElement | null
  previousOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  dialog.value?.showModal()
  nameInput.value?.focus({ preventScroll: true })
  nameInput.value?.select()
})
onBeforeUnmount(() => {
  dialog.value?.close()
  document.body.style.overflow = previousOverflow
  const menu = previousFocus?.closest('details')
  const target = menu && !menu.open ? menu.querySelector('summary') : previousFocus
  void nextTick(() => { if (target?.isConnected) target.focus({ preventScroll: true }) })
})
function close() { if (!props.busy) emit('close') }
function create() { if (canCreate.value) emit('create') }
function onBackdropClick(event: MouseEvent) {
  const element = dialog.value
  if (!element || event.target !== element) return
  const bounds = element.getBoundingClientRect()
  if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) close()
}
function onKeydown(event: KeyboardEvent) {
  if (event.key !== 'Tab') return
  const fields = Array.from(dialog.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),textarea:not(:disabled)') || [])
    .filter(field => field.getClientRects().length)
  const first = fields[0], last = fields[fields.length - 1]
  if (!first) { event.preventDefault(); return }
  if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog.value)) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && (document.activeElement === last || document.activeElement === dialog.value)) { event.preventDefault(); first.focus() }
}
</script>

<template>
  <Teleport to="body">
    <dialog ref="dialog" class="plan-create-dialog" tabindex="-1" aria-labelledby="plan-create-title" aria-describedby="plan-create-description" :aria-busy="busy" @cancel.prevent="close" @click="onBackdropClick" @keydown="onKeydown">
      <header>
        <div><h2 id="plan-create-title">新建方案</h2><p id="plan-create-description">{{ roomName ? `创建后将绑定到直播间“${roomName}”` : '填写方案名称和说明' }}</p></div>
        <button class="plan-create-close" type="button" aria-label="关闭新建方案" :disabled="busy" @click="close">×</button>
      </header>
      <form @submit.prevent="create">
        <label><span>方案名称</span><input ref="nameInput" v-model="name" maxlength="160" required :disabled="busy" placeholder="请输入方案名称" /></label>
        <label><span>方案说明<span class="plan-create-optional">（选填）</span></span><textarea v-model="description" maxlength="2000" rows="3" :disabled="busy" placeholder="简单介绍这个方案"></textarea></label>
        <p v-if="error" class="plan-create-error" role="alert">{{ error }}</p>
        <footer>
          <button class="plan-create-cancel" type="button" :disabled="busy" @click="close">取消</button>
          <button class="plan-create-submit" type="submit" :disabled="!canCreate">{{ busy ? '创建中…' : roomName ? '创建并绑定当前直播间' : '创建方案' }}</button>
        </footer>
      </form>
    </dialog>
  </Teleport>
</template>

<style scoped>
.plan-create-dialog{box-sizing:border-box;width:min(600px,calc(100vw - 32px));max-height:calc(100dvh - 32px);margin:auto;padding:28px;border:1px solid #dce2ff;border-radius:24px;background:linear-gradient(145deg,#fff,#f7f8ff);color:#26354f;box-shadow:0 24px 80px #293e7033;font-family:inherit;font-size:16px;line-height:1.6;overflow:auto}
.plan-create-dialog::backdrop{background:#26324e66;backdrop-filter:blur(5px)}
.plan-create-dialog header{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;margin-bottom:24px}
.plan-create-dialog h2{margin:0;font-size:24px;line-height:1.4;color:#192b49}
.plan-create-dialog header p{margin:8px 0 0;font-size:16px;color:#69768e;overflow-wrap:anywhere}
.plan-create-dialog button,.plan-create-dialog input,.plan-create-dialog textarea{box-sizing:border-box;font:inherit;font-size:16px}
.plan-create-close{flex:none;width:36px;height:36px;border:0;border-radius:10px;background:#eef0ff;color:#647198;font-size:26px!important;line-height:1;cursor:pointer}
.plan-create-dialog form{display:grid;gap:20px}
.plan-create-dialog label{display:grid;gap:8px;font-size:16px;font-weight:600}
.plan-create-optional{font-weight:400;color:#7a879d}
.plan-create-dialog input,.plan-create-dialog textarea{width:100%;padding:12px 14px;border:1px solid #d9e0f2;border-radius:12px;outline:none;background:#fff;color:#26354f;line-height:1.5;font-weight:400}
.plan-create-dialog textarea{resize:vertical;min-height:112px}
.plan-create-dialog input:focus,.plan-create-dialog textarea:focus{border-color:#6966ff;box-shadow:0 0 0 3px #6966ff1a}
.plan-create-dialog button:focus-visible{outline:3px solid #a6acff;outline-offset:3px}
.plan-create-error{margin:0;padding:10px 14px;border-radius:10px;background:#fff0f2;color:#be344b;font-size:16px;overflow-wrap:anywhere}
.plan-create-dialog footer{display:flex;justify-content:flex-end;gap:12px;margin-top:4px}
.plan-create-cancel,.plan-create-submit{min-height:46px;padding:10px 20px;border:1px solid #d9e0f2;border-radius:12px;cursor:pointer;font-weight:600;line-height:1.5}
.plan-create-cancel{background:#fff;color:#596780}
.plan-create-submit{background:#5751ff;border-color:#5751ff;color:#fff;box-shadow:0 6px 16px #5751ff20}
.plan-create-dialog button:disabled{opacity:.55;cursor:not-allowed}
@media(max-width:480px){.plan-create-dialog{padding:20px;border-radius:20px}.plan-create-dialog footer{flex-wrap:wrap}.plan-create-cancel{flex:1}.plan-create-submit{flex:2}}
</style>
