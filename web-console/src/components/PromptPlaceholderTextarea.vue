<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { PlanPromptPlaceholder } from '../planPromptPlaceholders'

const props = withDefaults(defineProps<{
  modelValue: string
  placeholders: readonly PlanPromptPlaceholder[]
  id: string
  label: string
  readonly?: boolean
  maxlength?: number
  rows?: number
}>(), { readonly: false, maxlength: 20000, rows: 20 })
const emit = defineEmits<{ 'update:modelValue': [value: string]; input: [] }>()
const textarea = ref<HTMLTextAreaElement | null>(null)
const menu = ref<HTMLElement | null>(null)
const trigger = ref<{ start: number; end: number; query: string } | null>(null)
const active = ref(0)
const message = ref('')
const placement = ref({ left: '0px', top: '0px', width: '0px', maxHeight: '300px', visibility: 'hidden' as 'hidden' | 'visible' })
const menuId = computed(() => props.id + '-placeholders')
const matches = computed(() => {
  const query = trigger.value?.query.toLocaleLowerCase() || ''
  return props.placeholders.filter(item => !query || [item.key, item.label, item.insert_text, item.description, item.group].some(value => value.toLocaleLowerCase().includes(query)))
})
let composing = false
let dismissed = ''
let frame = 0
let disposed = false
let observer: ResizeObserver | undefined

function signature() {
  const el = textarea.value
  return el ? `${el.selectionStart}:${el.selectionEnd}:${el.value}` : ''
}
function close(dismiss = false) {
  if (dismiss) dismissed = signature()
  trigger.value = null
}
function currentTrigger() {
  const el = textarea.value
  if (!el || props.readonly || composing || !props.placeholders.length || document.activeElement !== el || el.selectionStart !== el.selectionEnd) return null
  const end = el.selectionStart
  const start = el.value.lastIndexOf('@', end - 1)
  if (start < 0 || start >= end) return null
  const query = el.value.slice(start + 1, end)
  if (query.length > 40 || /[^\p{L}\p{N}_-]/u.test(query)) return null
  return { start, end, query }
}
function sync() {
  if (disposed || signature() === dismissed) return
  dismissed = ''
  const next = currentTrigger()
  if (!next) { close(); return }
  if (!trigger.value || trigger.value.start !== next.start || trigger.value.query !== next.query) active.value = 0
  trigger.value = next
  schedulePosition()
}
function schedulePosition(event?: Event) {
  if (event?.target instanceof Node && menu.value?.contains(event.target)) return
  cancelAnimationFrame(frame)
  frame = requestAnimationFrame(position)
}
function position() {
  const el = textarea.value
  if (!el || !trigger.value || disposed) return
  // A same-width mirror measures native wrapping without changing the editor or selection.
  const style = getComputedStyle(el)
  const mirror = document.createElement('div')
  const properties = ['boxSizing', 'width', 'fontFamily', 'fontSize', 'fontWeight', 'fontStyle', 'lineHeight', 'letterSpacing', 'textTransform', 'textIndent', 'textAlign', 'tabSize', 'paddingTop', 'paddingRight', 'paddingBottom', 'paddingLeft', 'borderTopWidth', 'borderRightWidth', 'borderBottomWidth', 'borderLeftWidth'] as const
  for (const key of properties) mirror.style[key] = style[key]
  Object.assign(mirror.style, { position: 'fixed', left: '0', top: '0', height: 'auto', visibility: 'hidden', whiteSpace: 'pre-wrap', overflowWrap: 'break-word', borderStyle: 'solid', pointerEvents: 'none' })
  // Match the textarea's content width even when its vertical scrollbar takes space.
  mirror.style.width = `${el.clientWidth + parseFloat(style.borderLeftWidth) + parseFloat(style.borderRightWidth)}px`
  mirror.textContent = el.value.slice(0, el.selectionStart)
  const marker = document.createElement('span')
  marker.textContent = '\u200b'
  mirror.append(marker)
  document.body.append(mirror)
  const offset = marker.getBoundingClientRect()
  const bounds = el.getBoundingClientRect()
  const x = bounds.left + offset.left - el.scrollLeft
  const y = bounds.top + offset.top - el.scrollTop
  mirror.remove()
  const line = parseFloat(style.lineHeight) || parseFloat(style.fontSize) * 1.8
  const viewportHeight = document.documentElement.clientHeight
  const viewportWidth = document.documentElement.clientWidth
  if (y + line < Math.max(0, bounds.top) || y > Math.min(viewportHeight, bounds.bottom)) {
    placement.value.visibility = 'hidden'
    return
  }
  const width = Math.min(480, viewportWidth - 24)
  const below = viewportHeight - Math.max(12, y + line + 6) - 12
  const above = Math.min(viewportHeight - 12, y - 6) - 12
  const flip = below < 200 && above > below
  const height = Math.max(0, Math.min(320, flip ? above : below))
  const top = flip ? Math.max(12, y - 6 - height) : Math.max(12, y + line + 6)
  placement.value = {
    left: `${Math.max(12, Math.min(x, viewportWidth - width - 12))}px`,
    top: `${top}px`, width: `${width}px`, maxHeight: `${height}px`, visibility: height >= 80 ? 'visible' : 'hidden',
  }
}
function input() {
  dismissed = ''
  message.value = ''
  emit('update:modelValue', textarea.value!.value)
  emit('input')
  void nextTick(sync)
}
async function choose(item: PlanPromptPlaceholder) {
  const el = textarea.value
  const at = currentTrigger()
  if (!el || !at || !trigger.value || at.start !== trigger.value.start || at.end !== trigger.value.end || at.query !== trigger.value.query || !matches.value.includes(item)) { close(); return }
  const value = el.value.slice(0, at.start) + item.insert_text + el.value.slice(at.end)
  if (value.length > props.maxlength) { message.value = `插入后超过 ${props.maxlength} 字符限制，请先删减正文。`; return }
  const scrollTop = el.scrollTop
  const scrollLeft = el.scrollLeft
  const caret = at.start + item.insert_text.length
  close()
  message.value = ''
  emit('update:modelValue', value)
  emit('input')
  await nextTick()
  if (disposed || textarea.value !== el) return
  el.focus({ preventScroll: true })
  el.setSelectionRange(caret, caret)
  el.scrollTop = scrollTop
  el.scrollLeft = scrollLeft
}
function keydown(event: KeyboardEvent) {
  if (composing || event.isComposing || event.keyCode === 229 || !trigger.value) return
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(true); return }
  if (!matches.value.length) return
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    active.value = (active.value + (event.key === 'ArrowDown' ? 1 : -1) + matches.value.length) % matches.value.length
    void nextTick(() => menu.value?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' }))
  } else if (event.key === 'Enter' || event.key === 'Tab') {
    event.preventDefault()
    void choose(matches.value[active.value]!)
  }
}
function compositionStart() { composing = true; close() }
function compositionEnd() { composing = false; void nextTick(sync) }
watch(() => [props.readonly, props.placeholders], () => close())
onMounted(() => {
  window.addEventListener('resize', schedulePosition)
  window.addEventListener('scroll', schedulePosition, true)
  document.addEventListener('selectionchange', sync)
  observer = new ResizeObserver(() => schedulePosition())
  if (textarea.value) observer.observe(textarea.value)
})
onBeforeUnmount(() => {
  disposed = true
  cancelAnimationFrame(frame)
  observer?.disconnect()
  window.removeEventListener('resize', schedulePosition)
  window.removeEventListener('scroll', schedulePosition, true)
  document.removeEventListener('selectionchange', sync)
})
</script>

<template>
  <div class="placeholder-editor">
    <textarea :id="id" ref="textarea" :value="modelValue" :aria-label="label" :readonly="readonly" :maxlength="maxlength" :rows="rows" spellcheck="false"
      aria-autocomplete="list" aria-haspopup="listbox" :aria-expanded="!!trigger" :aria-controls="trigger ? menuId : undefined"
      :aria-activedescendant="trigger && matches.length ? `${menuId}-${active}` : undefined"
      @input="input" @keydown="keydown" @keyup="sync" @click="sync" @select="sync" @blur="close(true)"
      @compositionstart="compositionStart" @compositionend="compositionEnd"></textarea>
    <p v-if="message" class="insertion-error" role="status">{{ message }}</p>
    <Teleport to="body">
      <div v-if="trigger" class="prompt-placeholder-popup" :style="placement" @pointerdown.prevent>
        <div class="popup-heading">内置占位符 <span>↑↓ 选择 · Enter / Tab 插入 · Esc 关闭</span></div>
        <div :id="menuId" ref="menu" role="listbox" aria-label="内置占位符" class="placeholder-options">
          <div v-for="(item, index) in matches" :id="`${menuId}-${index}`" :key="item.key" role="option" :aria-selected="active === index" class="placeholder-option" @pointermove="active = index" @click="choose(item)">
            <div class="option-title"><strong>{{ item.label }}</strong><span>{{ item.group }}</span></div>
            <code>{{ item.insert_text }}</code><p>{{ item.description }}</p>
          </div>
          <p v-if="!matches.length" class="empty-options" role="status">没有匹配的占位符，继续输入或按 Esc 关闭。</p>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.placeholder-editor{min-width:0}textarea{box-sizing:border-box;display:block;width:100%;max-width:100%;min-width:0;min-height:540px;padding:20px;border:1px solid #dfe3f4;border-radius:14px;background:#fff;color:#26324d;font:inherit;font-size:20px;line-height:1.8;resize:vertical;outline:none}textarea:focus{border-color:#8273f6;box-shadow:0 0 0 3px #6554ec15}textarea[readonly]{background:#f8f9fd}.insertion-error{color:#a64256;font-size:16px;margin:8px 0}
.prompt-placeholder-popup{position:fixed;z-index:10000;box-sizing:border-box;display:flex;flex-direction:column;overflow:hidden;border:1px solid #d8d4f8;border-radius:14px;background:#fff;color:#26324d;box-shadow:0 12px 36px #26324d2b;font:16px/1.5 system-ui,sans-serif;overflow-wrap:anywhere}.popup-heading{padding:10px 14px;border-bottom:1px solid #eceaf9;font-weight:650;flex-shrink:0}.popup-heading span{display:block;color:#71819d;font-size:12px;font-weight:400}.placeholder-options{min-height:0;overflow-y:auto;overscroll-behavior:contain;padding:4px}.placeholder-option{padding:10px;border-radius:9px;cursor:pointer}.placeholder-option[aria-selected="true"]{background:#f0edff;outline:1px solid #d8d0ff;outline-offset:-1px}.option-title{display:flex;align-items:baseline;justify-content:space-between;gap:8px}.option-title span{font-size:12px;color:#71819d}.placeholder-option code{white-space:pre-wrap;color:#6554cc;font-size:14px}.placeholder-option p{margin:3px 0 0;color:#66738d;font-size:13px}.empty-options{margin:8px 10px;padding:8px;color:#71819d}@media(max-width:900px){textarea{padding:16px}}
</style>
