import { computed, onBeforeUnmount, onMounted, ref, watch, type CSSProperties, type Ref } from 'vue'

type Position = { left: number; top: number; width: number; height: number }
const margin = 8, edgeHandleWidth = 28, floatingHeight = 820

export function useFloatingAgentDrawer(open: Ref<boolean>) {
  const element = ref<HTMLElement | null>(null)
  const position = ref<Position | null>(null)
  const dragging = ref(false)
  let gesture: { id: number; x: number; y: number; origin: Position; handle: HTMLElement } | null = null
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
    gesture = null; dragging.value = false
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', stopDrag)
    window.removeEventListener('pointercancel', stopDrag)
  }
  function move(event: PointerEvent) {
    if (!gesture || event.pointerId !== gesture.id) return
    const dx = event.clientX - gesture.x, dy = event.clientY - gesture.y
    if (!dragging.value && Math.hypot(dx, dy) < 4) return
    dragging.value = true; suppressClick = true
    position.value = fit({ ...gesture.origin, left: gesture.origin.left + dx, top: gesture.origin.top + dy })
    event.preventDefault()
  }
  function startDrag(event: PointerEvent) {
    if (!open.value || !element.value || !event.isPrimary || event.button !== 0 || gesture) return
    suppressClick = false
    const box = element.value.getBoundingClientRect(), handle = event.currentTarget as HTMLElement
    const origin = position.value || { left: box.left, top: box.top, width: box.width, height: Math.min(box.height, floatingHeight) }
    gesture = { id: event.pointerId, x: event.clientX, y: event.clientY, origin, handle }
    handle.setPointerCapture(event.pointerId)
    window.addEventListener('pointermove', move, { passive: false })
    window.addEventListener('pointerup', stopDrag)
    window.addEventListener('pointercancel', stopDrag)
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
  return { element, style, dragging, floating: computed(() => !!position.value), startDrag, consumeDragClick, reset }
}
