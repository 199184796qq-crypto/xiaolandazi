<script setup lang="ts">
import { computed, ref } from 'vue'

const props = withDefaults(defineProps<{
  modelValue: number
  min?: number
  max?: number
  step?: number
  disabled?: boolean
  compact?: boolean
  leftLabel?: string
  rightLabel?: string
  ariaLabel?: string
}>(), {
  min: 0,
  max: 100,
  step: 1,
  disabled: false,
  compact: false,
  leftLabel: '少一些',
  rightLabel: '多一些',
  ariaLabel: '互动偏好',
})

const emit = defineEmits<{
  (event: 'update:modelValue', value: number): void
  (event: 'commit', value: number): void
}>()

const trackRef = ref<HTMLElement | null>(null)
const dragging = ref(false)

const value = computed(() => clampAndSnap(props.modelValue))
const percent = computed(() => {
  const span = props.max - props.min
  if (span <= 0) return 0
  return ((value.value - props.min) / span) * 100
})

function clampAndSnap(raw: number) {
  const safe = Math.max(props.min, Math.min(props.max, Number(raw) || 0))
  const step = Math.max(0.0001, props.step)
  const snapped = Math.round((safe - props.min) / step) * step + props.min
  return Number(snapped.toFixed(6))
}

function valueFromPointer(event: PointerEvent) {
  const track = trackRef.value
  if (!track) return value.value
  const rect = track.getBoundingClientRect()
  if (rect.width <= 0) return value.value
  const ratio = Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width))
  return clampAndSnap(props.min + ratio * (props.max - props.min))
}

function updateFromPointer(event: PointerEvent) {
  if (props.disabled) return
  emit('update:modelValue', valueFromPointer(event))
}

function onPointerDown(event: PointerEvent) {
  if (props.disabled) return
  dragging.value = true
  trackRef.value?.setPointerCapture?.(event.pointerId)
  updateFromPointer(event)
}

function onPointerMove(event: PointerEvent) {
  if (!dragging.value || props.disabled) return
  updateFromPointer(event)
}

function finishPointer(event: PointerEvent) {
  if (!dragging.value) return
  dragging.value = false
  updateFromPointer(event)
  emit('commit', valueFromPointer(event))
  trackRef.value?.releasePointerCapture?.(event.pointerId)
}

function onKeydown(event: KeyboardEvent) {
  if (props.disabled) return
  let next = value.value
  if (event.key === 'ArrowLeft' || event.key === 'ArrowDown') next -= props.step
  else if (event.key === 'ArrowRight' || event.key === 'ArrowUp') next += props.step
  else if (event.key === 'Home') next = props.min
  else if (event.key === 'End') next = props.max
  else return
  event.preventDefault()
  next = clampAndSnap(next)
  emit('update:modelValue', next)
  emit('commit', next)
}
</script>

<template>
  <div class="preference-slider" :class="{ compact, disabled, off: value === min }">
    <span class="preference-slider-label">{{ leftLabel }}</span>

    <div class="preference-slider-body">
      <div
        ref="trackRef"
        class="preference-slider-track"
        :class="{ dragging }"
        role="slider"
        :tabindex="disabled ? -1 : 0"
        :aria-label="ariaLabel"
        :aria-valuemin="min"
        :aria-valuemax="max"
        :aria-valuenow="value"
        :aria-disabled="disabled"
        @pointerdown.prevent="onPointerDown"
        @pointermove.prevent="onPointerMove"
        @pointerup.prevent="finishPointer"
        @pointercancel="finishPointer"
        @keydown="onKeydown"
      >
        <span class="preference-slider-fill" :style="{ width: percent + '%' }"></span>
        <span class="preference-slider-thumb" :style="{ left: percent + '%' }"></span>
      </div>
      <small v-if="value === min" class="preference-slider-off">已关闭</small>
    </div>

    <span class="preference-slider-label">{{ rightLabel }}</span>
  </div>
</template>

<style scoped>
.preference-slider{
  display:grid;
  grid-template-columns:52px minmax(96px,1fr) 52px;
  align-items:center;
  gap:10px;
  width:100%;
  min-width:0;
}
.preference-slider-label{
  color:#203963;
  font-size:14px;
  font-weight:900;
  line-height:1;
  text-align:center;
  white-space:nowrap;
}
.preference-slider-body{
  position:relative;
  min-width:0;
  height:32px;
  display:flex;
  align-items:center;
}
.preference-slider-track{
  position:relative;
  width:100%;
  height:7px;
  border-radius:999px;
  background:#e7ecf4;
  box-shadow:inset 0 1px 2px rgba(58,73,111,.08);
  outline:none;
  cursor:pointer;
  touch-action:none;
}
.preference-slider-track:focus-visible{
  box-shadow:0 0 0 4px rgba(91,112,240,.12),inset 0 1px 2px rgba(58,73,111,.08);
}
.preference-slider-fill{
  position:absolute;
  inset:0 auto 0 0;
  border-radius:inherit;
  background:linear-gradient(90deg,#4b8dff 0%,#6a70f4 100%);
  pointer-events:none;
}
.preference-slider-thumb{
  position:absolute;
  top:50%;
  width:28px;
  height:28px;
  border:1px solid #d5deef;
  border-radius:50%;
  background:#fff;
  box-shadow:0 3px 12px rgba(67,86,145,.22);
  transform:translate(-50%,-50%);
  pointer-events:none;
  transition:box-shadow .14s ease,transform .14s ease;
}
.preference-slider-track.dragging .preference-slider-thumb{
  box-shadow:0 0 0 5px rgba(88,110,238,.10),0 3px 12px rgba(67,86,145,.22);
  transform:translate(-50%,-50%) scale(1.04);
}
.preference-slider-off{
  position:absolute;
  left:0;
  top:25px;
  color:#9099aa;
  font-size:9px;
  font-weight:850;
  line-height:1;
  white-space:nowrap;
}
.preference-slider.disabled{
  opacity:.52;
  pointer-events:none;
}
.preference-slider.compact{
  grid-template-columns:44px minmax(86px,1fr) 44px;
  gap:7px;
}
.preference-slider.compact .preference-slider-label{font-size:11px}
.preference-slider.compact .preference-slider-body{height:28px}
.preference-slider.compact .preference-slider-track{height:6px}
.preference-slider.compact .preference-slider-thumb{width:24px;height:24px}
.preference-slider.compact .preference-slider-off{top:22px;font-size:8px}
@media (max-width:760px){
  .preference-slider{grid-template-columns:48px minmax(90px,1fr) 48px;gap:8px}
  .preference-slider-label{font-size:12px}
}
</style>
