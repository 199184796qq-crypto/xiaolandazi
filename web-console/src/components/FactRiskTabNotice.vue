<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'

const props = defineProps<{ count: number; loading?: boolean; error?: string; focused?: boolean }>()
const root = ref<HTMLElement | null>(null)
const opened = ref(false), left = ref(0), top = ref(0), above = ref(false)
const pending = computed(() => Number.isInteger(props.count) && props.count > 0 ? props.count : 0)
const message = computed(() => pending.value
  ? `有 ${pending.value} 项风险或资质待您处理，点击红色提示直接定位问题；全部处理后提示自动消失。`
  : props.error ? '风险登记读取失败，请点击“事实依据”重新读取，尚不能确认全部已处理。' : props.loading ? '正在核查当前方案的风险与资质待处理项。' : '')
function position() {
  if (!root.value) return
  const rect = root.value.getBoundingClientRect()
  left.value = Math.max(8, Math.min(rect.left, window.innerWidth - Math.min(320, window.innerWidth - 16) - 8))
  above.value = window.innerHeight - rect.bottom < 110 && rect.top > 110
  top.value = above.value ? rect.top - 10 : rect.bottom + 10
}
function show() {
  if (!message.value) return
  position(); opened.value = true
  window.addEventListener('scroll', hide, true)
  window.addEventListener('resize', hide)
}
function hide() {
  opened.value = false
  if (typeof window !== 'undefined') { window.removeEventListener('scroll', hide, true); window.removeEventListener('resize', hide) }
}
watch(message, (value) => { if (!value) hide() })
watch(() => props.focused, (value) => { if (value) show(); else hide() })
onBeforeUnmount(hide)
</script>

<template>
  <span ref="root" class="fact-risk-tab-notice" :title="message || undefined" @mouseenter="show" @mouseleave="hide" @focusin="show" @focusout="hide" @click="hide" @keydown.esc="hide">
    <span class="strategy-workflow-label fact-risk-tab-text">事实依据</span>
    <span v-if="pending" class="fact-risk-tab-pill" role="status" aria-live="polite"><span aria-hidden="true">!</span>{{ pending }}项待处理</span>
    <span v-else-if="error" class="fact-risk-tab-pill" role="status">读取失败</span>
    <span v-else-if="loading" class="fact-risk-tab-checking" role="status">核查中</span>
    <Teleport to="body">
      <span v-if="opened && message" class="fact-risk-tab-bubble" :class="{ above }" role="tooltip" :style="{ left: left + 'px', top: top + 'px' }">{{ message }}</span>
    </Teleport>
  </span>
</template>

<style scoped>
.fact-risk-tab-notice{display:flex;flex-direction:column;align-items:flex-start;justify-content:center;gap:5px;min-width:0;max-width:100%}.fact-risk-tab-text{display:block;max-width:100%}.fact-risk-tab-pill{display:inline-flex;align-items:center;gap:4px;flex-shrink:0;padding:3px 6px;border:1px solid #f2b4c1;border-radius:8px;background:#fff0f3;color:#c41d45;font-size:11px;font-weight:800;line-height:1.2;white-space:nowrap}.fact-risk-tab-pill>span{display:grid;place-items:center;width:12px;height:12px;border-radius:50%;background:#c41d45;color:white;font-size:10px}.fact-risk-tab-checking{font-size:11px;color:#7b87a3;white-space:nowrap}.fact-risk-tab-bubble{position:fixed;z-index:10000;box-sizing:border-box;width:320px;max-width:calc(100vw - 16px);padding:12px 14px;border:1px solid #efadbd;border-radius:12px;background:#fff5f7;color:#ae2443;box-shadow:0 8px 28px #661b3226;font-size:13px;font-weight:600;line-height:1.65;white-space:normal;pointer-events:none}.fact-risk-tab-bubble::before{content:'';position:absolute;left:18px;top:-5px;width:8px;height:8px;background:#fff5f7;border-left:1px solid #efadbd;border-top:1px solid #efadbd;transform:rotate(45deg)}.fact-risk-tab-bubble.above{transform:translateY(-100%)}.fact-risk-tab-bubble.above::before{top:auto;bottom:-5px;transform:rotate(225deg)}
</style>
