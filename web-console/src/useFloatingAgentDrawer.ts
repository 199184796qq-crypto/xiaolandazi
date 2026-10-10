import { computed, onBeforeUnmount, onMounted, ref, watch, type CSSProperties, type Ref } from 'vue'

type Position = { left: number; top: number; width: number; height: number }
export type DrawerResizeEdge = 'n' | 's' | 'e' | 'w' | 'ne' | 'nw' | 'se' | 'sw'
const margin = 8, edgeHandleWidth = 28, floatingHeight = 820
const minimumWidth = 360, minimumHeight = 560
const clamp = (value: number, lower: number, upper: number) => Math.max(lower, Math.min(value, upper))

export function useFloatingAgentDrawer(open: Ref<boolean>) {
  const element = ref<HTMLElement | null>(null)
  const position = ref<Position | null>(null)
  const dragging = ref(false)
  const resizing = ref(false)
  let gesture: { id: number; x: number; y: number; origin: Position; handle: HTMLElement; kind: 'drag' | DrawerResizeEdge } | null = null
  let suppressClick = false

  function fit(value: Position): Position {
    const width = Math.min(value.width, Math.max(1, window.innerWidth - margin * 2 - edgeHandleWidth))
    const height = Math.min(value.height, Math.max(1, window.innerHeight - margin * 2))
    return {
      width, height,
      left: Math.max(margin + edgeHandleWidth, Math.min(value.left, window.innerWidth - width - margin)),
      top: Math.max(margin, Math.min(value.top, window.innerHeight - height - margin)),
    }
  }
  function stopDrag(event?: PointerEvent) {
    if (event && gesture && event.pointerId !== gesture.id) return
    if (event?.type === 'pointercancel') suppressClick = false
    if (gesture?.handle.hasPointerCapture(gesture.id)) gesture.handle.releasePointerCapture(gesture.id)
    gesture = null; dragging.value = false; resizing.value = false
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', stopDrag)
    window.removeEventListener('pointercancel', stopDrag)
  }
  function move(event: PointerEvent) {
    if (!gesture || event.pointerId !== gesture.id) return
    const dx = event.clientX - gesture.x, dy = event.clientY - gesture.y
    if (!dragging.value && !resizing.value && Math.hypot(dx, dy) < 4) return
    if (gesture.kind === 'drag') {
      dragging.value = true; suppressClick = true
      position.value = fit({ ...gesture.origin, left: gesture.origin.left + dx, top: gesture.origin.top + dy })
    } else {
      resizing.value = true
      position.value = resizePosition(gesture.origin, dx, dy, gesture.kind)
    }
    event.preventDefault()
  }
  function beginGesture(event: PointerEvent, kind: 'drag' | DrawerResizeEdge) {
    if (!open.value || !element.value || !event.isPrimary || event.button !== 0 || gesture) return
    suppressClick = false
    const box = element.value.getBoundingClientRect(), handle = event.currentTarget as HTMLElement
    const origin = position.value || { left: box.left, top: box.top, width: box.width, height: Math.min(box.height, floatingHeight) }
    gesture = { id: event.pointerId, x: event.clientX, y: event.clientY, origin, handle, kind }
    handle.setPointerCapture(event.pointerId)
    window.addEventListener('pointermove', move, { passive: false })
    window.addEventListener('pointerup', stopDrag)
    window.addEventListener('pointercancel', stopDrag)
  }
  function startDrag(event: PointerEvent) { beginGesture(event, 'drag') }
  function startResize(event: PointerEvent, edge: DrawerResizeEdge) {
    if (!position.value) return
    event.stopPropagation()
    beginGesture(event, edge)
  }
  function resizePosition(origin: Position, dx: number, dy: number, edge: DrawerResizeEdge): Position {
    const value = { ...origin }, right = origin.left + origin.width, bottom = origin.top + origin.height
    if (edge.includes('e')) {
      const maximum = window.innerWidth - margin - origin.left
      value.width = clamp(origin.width + dx, Math.min(minimumWidth, maximum), maximum)
    }
    if (edge.includes('w')) {
      const minimum = Math.min(minimumWidth, right - margin - edgeHandleWidth)
      value.left = clamp(origin.left + dx, margin + edgeHandleWidth, right - minimum)
      value.width = right - value.left
    }
    if (edge.includes('s')) {
      const maximum = window.innerHeight - margin - origin.top
      value.height = clamp(origin.height + dy, Math.min(minimumHeight, maximum), maximum)
    }
    if (edge.includes('n')) {
      const minimum = Math.min(minimumHeight, bottom - margin)
      value.top = clamp(origin.top + dy, margin, bottom - minimum)
      value.height = bottom - value.top
    }
    return value
  }
  function resizeWithKeyboard(event: KeyboardEvent, edge: DrawerResizeEdge) {
    if (!open.value || !position.value || gesture) return
    const step = event.shiftKey ? 32 : 16
    const dx = event.key === 'ArrowRight' ? step : event.key === 'ArrowLeft' ? -step : 0
    const dy = event.key === 'ArrowDown' ? step : event.key === 'ArrowUp' ? -step : 0
    if (!(dx && (edge.includes('e') || edge.includes('w'))) && !(dy && (edge.includes('n') || edge.includes('s')))) return
    event.preventDefault()
    position.value = resizePosition(position.value, dx, dy, edge)
  }
  function consumeDragClick() {
    const consumed = suppressClick
    suppressClick = false
    return consumed
  }
  function reset() { stopDrag(); position.value = null; suppressClick = false }
  function resize() { stopDrag(); if (position.value) position.value = fit(position.value) }
  watch(open, value => { if (!value) reset() }, { flush: 'sync' })
  onMounted(() => window.addEventListener('resize', resize))
  onBeforeUnmount(() => { stopDrag(); window.removeEventListener('resize', resize) })
  const style = computed<CSSProperties>(() => position.value ? {
    position: 'fixed', left: `${position.value.left}px`, top: `${position.value.top}px`,
    width: `${position.value.width}px`, height: `${position.value.height}px`, right: 'auto', bottom: 'auto',
  } : {})
  return { element, style, dragging, resizing, floating: computed(() => !!position.value), startDrag, startResize, resizeWithKeyboard, consumeDragClick, reset }
}
