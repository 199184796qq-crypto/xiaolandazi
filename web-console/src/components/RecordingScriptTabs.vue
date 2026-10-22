<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'

type Tab = { id: string; title: string }
type DragState = {
  id: string
  pointerId: number
  startX: number
  startY: number
  x: number
  y: number
  offsetX: number
  offsetY: number
  width: number
  height: number
  started: boolean
}

const props = defineProps<{ tabs: readonly Tab[]; activeId: string; disabled?: boolean }>()
const emit = defineEmits<{
  select: [id: string]
  add: []
  remove: [id: string]
  reorder: [ids: string[]]
}>()
const root = ref<HTMLElement | null>(null)
const deleteMode = ref(false)
const previewOrder = ref<string[] | null>(null)
const drag = ref<DragState | null>(null)
const announcement = ref('')
const instructionsId = useId()
let suppressClickUntil = 0

const orderedTabs = computed(() => {
  if (!previewOrder.value) return props.tabs
  const byId = new Map(props.tabs.map(tab => [tab.id, tab]))
  return previewOrder.value.map(id => byId.get(id)).filter((tab): tab is Tab => Boolean(tab))
})
const draggedTitle = computed(() => props.tabs.find(tab => tab.id === drag.value?.id)?.title || '未命名')
const ghostStyle = computed(() => drag.value ? {
  left: `${drag.value.x - drag.value.offsetX}px`,
  top: `${drag.value.y - drag.value.offsetY}px`,
  width: `${drag.value.width}px`,
  height: `${drag.value.height}px`,
} : {})

function tabName(tab: Tab): string {
  return tab.title || '未命名'
}

function onDeleteModeClick(event: MouseEvent): void {
  if (!deleteMode.value) return
  const removeButton = event.target instanceof Element
    ? event.target.closest('.recording-script-tab-remove')
    : null
  if (removeButton && root.value?.contains(removeButton)) return
  deleteMode.value = false
  // Dismissing delete mode must not also activate the control beneath this click.
  event.preventDefault()
  event.stopImmediatePropagation()
}

function onDeleteModePointerDown(event: PointerEvent): void {
  // Native disabled controls do not dispatch click, but still dismiss this mode.
  if (event.target instanceof Element && event.target.closest('button:disabled,input:disabled,select:disabled,textarea:disabled')) {
    onDeleteModeClick(event)
  }
}

function selectTab(id: string): void {
  if (props.disabled || performance.now() < suppressClickUntil) return
  emit('select', id)
}

function removeTab(tab: Tab): void {
  if (props.disabled || props.tabs.length <= 1) return
  finishDrag(false)
  emit('remove', tab.id)
  announcement.value = `已删除${tabName(tab)}`
}

function focusTab(id: string): void {
  void nextTick(() => {
    const buttons = root.value?.querySelectorAll<HTMLButtonElement>('[role="tab"]')
    Array.from(buttons || []).find(button => button.dataset.tabId === id)?.focus()
  })
}

function onTabKeydown(event: KeyboardEvent, id: string): void {
  if (props.disabled) return
  const index = props.tabs.findIndex(tab => tab.id === id)
  if (index < 0) return
  if (event.altKey && (event.key === 'ArrowLeft' || event.key === 'ArrowRight')) {
    event.preventDefault()
    const target = index + (event.key === 'ArrowLeft' ? -1 : 1)
    if (target < 0 || target >= props.tabs.length) return
    const ids = props.tabs.map(tab => tab.id)
    ids.splice(target, 0, ids.splice(index, 1)[0]!)
    emit('reorder', ids)
    announcement.value = `${tabName(props.tabs[index]!)}已移到第${target + 1}位`
    focusTab(id)
    return
  }
  let target = index
  if (event.key === 'ArrowLeft') target = (index - 1 + props.tabs.length) % props.tabs.length
  else if (event.key === 'ArrowRight') target = (index + 1) % props.tabs.length
  else if (event.key === 'Home') target = 0
  else if (event.key === 'End') target = props.tabs.length - 1
  else return
  event.preventDefault()
  const next = props.tabs[target]
  if (next) {
    emit('select', next.id)
    focusTab(next.id)
  }
}

function onPointerDown(event: PointerEvent, id: string): void {
  if (deleteMode.value || props.disabled || props.tabs.length < 2 || event.button !== 0 || !event.isPrimary) return
  const button = (event.currentTarget as HTMLElement).closest<HTMLButtonElement>('.recording-script-tab-select')
  if (!button) return
  finishDrag(false)
  const bounds = button.getBoundingClientRect()
  drag.value = {
    id, pointerId: event.pointerId,
    startX: event.clientX, startY: event.clientY, x: event.clientX, y: event.clientY,
    offsetX: event.clientX - bounds.left, offsetY: event.clientY - bounds.top,
    width: bounds.width, height: bounds.height, started: false,
  }
  window.addEventListener('pointermove', onPointerMove, { passive: false })
  window.addEventListener('pointerup', onPointerUp)
  window.addEventListener('pointercancel', onPointerCancel)
  window.addEventListener('keydown', onDragKeydown)
  window.addEventListener('blur', onPointerCancel)
}

function onPointerMove(event: PointerEvent): void {
  const current = drag.value
  if (!current || current.pointerId !== event.pointerId) return
  if (!current.started) {
    if (Math.hypot(event.clientX - current.startX, event.clientY - current.startY) < 6) return
    current.started = true
    previewOrder.value = props.tabs.map(tab => tab.id)
  }
  event.preventDefault()
  current.x = event.clientX
  current.y = event.clientY
  const target = document.elementFromPoint(event.clientX, event.clientY)
    ?.closest<HTMLElement>('[data-recording-tab-id]')
  if (!target || !root.value?.contains(target)) return
  const targetId = target.dataset.recordingTabId
  const ids = previewOrder.value
  if (!targetId || targetId === current.id || !ids) return
  const from = ids.indexOf(current.id)
  const to = ids.indexOf(targetId)
  if (from < 0 || to < 0) return
  const bounds = target.getBoundingClientRect()
  const middle = bounds.left + bounds.width / 2
  if ((from < to && event.clientX < middle) || (from > to && event.clientX > middle)) return
  const next = [...ids]
  next.splice(to, 0, next.splice(from, 1)[0]!)
  previewOrder.value = next
}

function onPointerUp(event: PointerEvent): void {
  if (!drag.value || drag.value.pointerId !== event.pointerId) return
  const bounds = root.value?.getBoundingClientRect()
  const inside = Boolean(bounds && event.clientX >= bounds.left && event.clientX <= bounds.right && event.clientY >= bounds.top && event.clientY <= bounds.bottom)
  finishDrag(inside)
}

function onPointerCancel(): void {
  finishDrag(false)
}

function onDragKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') finishDrag(false)
}

function finishDrag(commit: boolean): void {
  const current = drag.value
  const ids = previewOrder.value
  const title = props.tabs.find(tab => tab.id === current?.id)?.title || '未命名'
  drag.value = null
  window.removeEventListener('pointermove', onPointerMove)
  window.removeEventListener('pointerup', onPointerUp)
  window.removeEventListener('pointercancel', onPointerCancel)
  window.removeEventListener('keydown', onDragKeydown)
  window.removeEventListener('blur', onPointerCancel)
  if (current?.started) suppressClickUntil = performance.now() + 250
  if (commit && current?.started && ids && !props.disabled && ids.join('\0') !== props.tabs.map(tab => tab.id).join('\0')) {
    emit('reorder', [...ids])
    announcement.value = `${title}的顺序已调整`
    void nextTick(() => { if (!drag.value) previewOrder.value = null })
  } else {
    previewOrder.value = null
  }
}

watch(() => props.tabs.map(tab => tab.id).join('\0'), () => finishDrag(false))
watch(deleteMode, active => {
  if (active) {
    finishDrag(false)
    document.addEventListener('click', onDeleteModeClick, true)
    document.addEventListener('pointerdown', onDeleteModePointerDown, true)
  } else {
    document.removeEventListener('click', onDeleteModeClick, true)
    document.removeEventListener('pointerdown', onDeleteModePointerDown, true)
  }
})
watch(() => props.disabled, disabled => {
  if (disabled) {
    finishDrag(false)
    deleteMode.value = false
  }
})
onBeforeUnmount(() => {
  finishDrag(false)
  document.removeEventListener('click', onDeleteModeClick, true)
  document.removeEventListener('pointerdown', onDeleteModePointerDown, true)
})
</script>

<template>
  <div ref="root" class="recording-script-tabs" :class="{ 'is-deleting': deleteMode }" @keydown.esc="deleteMode = false">
    <TransitionGroup name="recording-tab" tag="div" class="recording-script-tab-list" role="tablist" aria-label="话术标签" :aria-describedby="instructionsId">
      <div v-for="tab in orderedTabs" :key="tab.id" class="recording-script-tab-item" :class="{ 'is-dragging': drag?.started && drag.id === tab.id }" :data-recording-tab-id="tab.id" role="presentation">
        <button type="button" role="tab" class="recording-script-tab-select" :class="{ active: tab.id === activeId }" :data-tab-id="tab.id" :aria-selected="tab.id === activeId" :tabindex="tab.id === activeId ? 0 : -1" :disabled="disabled" :draggable="false" @click="selectTab(tab.id)" @keydown="onTabKeydown($event, tab.id)">
          <span class="recording-script-tab-grip" aria-hidden="true" @pointerdown.stop="onPointerDown($event, tab.id)"><svg viewBox="0 0 12 18"><circle v-for="n in 6" :key="n" :cx="n % 2 ? 3 : 9" :cy="3 + Math.floor((n - 1) / 2) * 6" r="1.3" /></svg></span>
          <span class="recording-script-tab-name">{{ tabName(tab) }}</span>
        </button>
        <button v-if="deleteMode" type="button" class="recording-script-tab-remove" :aria-label="`删除话术：${tabName(tab)}`" :title="tabs.length <= 1 ? '至少保留一个话术标签' : `删除${tabName(tab)}`" :disabled="disabled || tabs.length <= 1" @click.stop="removeTab(tab)">
          <svg viewBox="0 0 16 16" aria-hidden="true"><path d="m5 5 6 6m0-6-6 6" /></svg>
        </button>
      </div>
    </TransitionGroup>
    <div class="recording-script-tab-actions">
      <button type="button" class="recording-script-tab-add" aria-label="新增话术标签" :disabled="disabled || tabs.length >= 20" :title="tabs.length >= 20 ? '最多20个话术标签' : '新增话术标签'" @click="emit('add')">+</button>
      <button type="button" class="recording-script-tab-delete-toggle" :class="{ active: deleteMode }" aria-label="管理标签删除" :aria-pressed="deleteMode" :title="deleteMode ? '完成删除' : '删除标签'" :disabled="disabled" @click="deleteMode = !deleteMode">
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13M10 11v5m4-5v5" /></svg>
      </button>
    </div>
    <span :id="instructionsId" class="recording-script-sr-only">按住标签左侧六点把手拖动可排序，点击文字切换标签。按住Alt和左右方向键也可调整顺序。</span>
    <span class="recording-script-sr-only" aria-live="polite">{{ announcement }}</span>
    <Teleport to="body">
      <div v-if="drag?.started" class="recording-script-tab-ghost" :style="ghostStyle" aria-hidden="true"><span>⠿</span>{{ draggedTitle }}</div>
    </Teleport>
  </div>
</template>

<style scoped>
.recording-script-tabs{display:flex;flex-wrap:wrap;align-items:center;gap:12px;min-width:0;padding:4px 0}
.recording-script-tab-list{position:relative;display:flex;flex-wrap:wrap;align-items:center;gap:12px;max-width:100%;min-width:0;padding:5px 2px}
.recording-script-tab-item{position:relative;flex:0 0 auto;max-width:100%}
.recording-script-tab-select{display:flex;align-items:center;gap:7px;box-sizing:border-box;min-height:42px;max-width:100%;padding:8px 14px 8px 10px;border:1px solid #d5ddeb;border-radius:12px;background:#fff;color:#536079;font:inherit;font-size:16px;font-weight:800;cursor:pointer;user-select:none;transition:background .18s,border-color .18s,box-shadow .18s}
.recording-script-tab-select:hover:not(.active):not(:disabled){border-color:#bbc7dc;background:#f8faff;box-shadow:0 4px 12px #344b7810}
.recording-script-tab-select.active{border-color:#d93842;background:linear-gradient(135deg,#df3b45,#d52d39);color:#fff;box-shadow:0 5px 13px #d4383f2e}
.recording-script-tab-select.active:hover:not(:disabled){color:#fff}
.recording-script-tab-name{overflow:hidden;max-width:240px;color:inherit;text-overflow:ellipsis;white-space:nowrap}
.recording-script-tab-grip{display:flex;flex:0 0 12px;align-items:center;justify-content:center;width:12px;min-height:24px;opacity:.5;cursor:grab;touch-action:none}
.recording-script-tab-grip svg{width:10px;height:16px;fill:currentColor;pointer-events:none}
.recording-script-tabs.is-deleting .recording-script-tab-grip{cursor:pointer}
.recording-script-tab-select:disabled .recording-script-tab-grip{cursor:not-allowed}
.recording-script-tab-item.is-dragging .recording-script-tab-select{opacity:.35;border-style:dashed;box-shadow:none}
.recording-script-tab-actions{display:flex;align-items:center;gap:8px;flex:0 0 auto}
.recording-script-tab-add,.recording-script-tab-delete-toggle{display:grid;flex:0 0 42px;box-sizing:border-box;place-items:center;width:42px;height:42px;padding:0;border:1px solid #d5ddeb;border-radius:12px;background:#fff;color:#6c7895;cursor:pointer;transition:color .18s,background .18s,border-color .18s}
.recording-script-tab-add{border-style:dashed;font:inherit;font-size:24px;font-weight:750;line-height:1}
.recording-script-tab-delete-toggle svg{width:19px;height:19px;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}
.recording-script-tab-add:hover:not(:disabled),.recording-script-tab-delete-toggle:hover:not(:disabled),.recording-script-tab-delete-toggle.active{border-color:#edb7bd;background:#fff1f2;color:#d13240}
.recording-script-tab-remove{position:absolute;z-index:2;top:-8px;right:-7px;display:grid;box-sizing:border-box;place-items:center;width:23px;height:23px;padding:0;border:2px solid #fff;border-radius:50%;background:#e43b48;color:#fff;box-shadow:0 2px 7px #bf253d3d;cursor:pointer;transition:transform .15s,background .15s}
.recording-script-tab-remove svg{width:14px;height:14px;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round}
.recording-script-tab-remove:hover:not(:disabled){background:#c52333;transform:scale(1.12)}
.recording-script-tab-select:focus-visible,.recording-script-tab-actions button:focus-visible,.recording-script-tab-remove:focus-visible{outline:3px solid #9587ff;outline-offset:3px}
.recording-script-tabs button:disabled{cursor:not-allowed;opacity:.45}
.recording-tab-move{transition:transform .22s cubic-bezier(.2,.8,.2,1)}
.recording-tab-enter-active,.recording-tab-leave-active{transition:transform .18s,opacity .18s}
.recording-tab-enter-from,.recording-tab-leave-to{opacity:0;transform:translateY(-5px) scale(.94)}
.recording-tab-leave-active{position:absolute;pointer-events:none}
.recording-script-tab-ghost{position:fixed;z-index:10020;display:flex;align-items:center;gap:8px;box-sizing:border-box;overflow:hidden;padding:8px 14px 8px 10px;border:1px solid #d93842;border-radius:12px;background:linear-gradient(135deg,#df3b45,#d52d39);color:#fff;box-shadow:0 12px 28px #b12d4b40;font-size:16px;font-weight:800;white-space:nowrap;pointer-events:none;transform:rotate(-2deg) scale(1.04)}
.recording-script-tab-ghost>span{opacity:.65}
.recording-script-sr-only{position:absolute;width:1px;height:1px;overflow:hidden;padding:0;clip-path:inset(50%);white-space:nowrap;border:0}
@media(max-width:600px){.recording-script-tab-name{max-width:170px}.recording-script-tabs{gap:8px}.recording-script-tab-list{gap:10px}}
@media(prefers-reduced-motion:reduce){.recording-tab-move,.recording-tab-enter-active,.recording-tab-leave-active,.recording-script-tabs button{transition:none}.recording-script-tab-ghost{transform:none}}
</style>
