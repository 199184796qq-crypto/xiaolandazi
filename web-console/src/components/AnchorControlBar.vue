<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'

const props = withDefaults(defineProps<{
  initializing: boolean
  initializeProgress?: number
  initializeLabel?: string
  saving: boolean
  saveLabel?: string
  initializeDisabled: boolean
  saveDisabled: boolean
  saveHint?: string
  mode?: 'all' | 'initialize' | 'save'
  compact?: boolean
}>(), { initializeProgress: 0, initializeLabel: '', saveLabel: '', saveHint: '', mode: 'all', compact: false })
const emit = defineEmits<{ initialize: []; forceInitialize: []; save: [] }>()
const initializeWrap = ref<HTMLElement | null>(null)
const menuToggle = ref<HTMLButtonElement | null>(null)
const resetItem = ref<HTMLButtonElement | null>(null)
const menuOpen = ref(false)
const menuId = useId()

function closeMenu(restoreFocus = false) {
  menuOpen.value = false
  if (restoreFocus) menuToggle.value?.focus()
}
async function openMenu() {
  if (props.initializeDisabled || props.initializing) return
  menuOpen.value = true
  await nextTick()
  resetItem.value?.focus()
}
function toggleMenu() {
  if (menuOpen.value) closeMenu(true)
  else void openMenu()
}
function initialize() {
  if (props.initializeDisabled || props.initializing) return
  closeMenu()
  emit('initialize')
}
function forceInitialize() {
  if (props.initializeDisabled || props.initializing) return
  closeMenu(true)
  emit('forceInitialize')
}
function onOutsidePointer(event: PointerEvent) {
  if (!initializeWrap.value?.contains(event.target as Node)) closeMenu()
}
function onFocusOut(event: FocusEvent) {
  if (!initializeWrap.value?.contains(event.relatedTarget as Node | null)) closeMenu()
}
watch(() => [props.initializing, props.initializeDisabled, props.mode], () => closeMenu())
onMounted(() => document.addEventListener('pointerdown', onOutsidePointer, true))
onBeforeUnmount(() => document.removeEventListener('pointerdown', onOutsidePointer, true))
</script>

<template>
  <section class="agent-control-bar" :class="{ 'is-compact': compact }" aria-label="直播智能体操作">
    <div class="agent-control-grid" :class="{ 'initialize-only': mode === 'initialize', 'save-only': mode === 'save' }">
      <div v-if="mode !== 'save'" ref="initializeWrap" class="control-initialize-wrap" @focusout="onFocusOut" @keydown.esc.stop.prevent="closeMenu(true)">
        <button type="button" class="agent-control-card control-initialize" :class="{ 'is-initializing': initializing }" :style="initializing ? { '--initialize-progress': Math.max(0, Math.min(100, initializeProgress)) + '%' } : undefined" :disabled="initializeDisabled || initializing" :aria-busy="initializing" title="增量初始化，保留可复用的已生成内容" @click="initialize">
          <template v-if="initializing">
            <span class="control-progress-fill" aria-hidden="true"></span>
            <span class="control-progress-copy" aria-live="polite">
              <strong>{{ initializeLabel || '主播风格初始化…' }}</strong>
              <em>{{ Math.round(Math.max(0, Math.min(100, initializeProgress))) }}%</em>
            </span>
          </template>
          <template v-else>
            <span class="control-icon initialize-icon" aria-hidden="true">
              <svg viewBox="0 0 48 48"><path fill="currentColor" d="m12 33 19-19a4 4 0 0 1 6 6L18 39a4 4 0 0 1-6-6Zm17-16 6 6 3-3-6-6-3 3Z"/><path fill="currentColor" d="m13 5 2.2 5.8L21 13l-5.8 2.2L13 21l-2.2-5.8L5 13l5.8-2.2L13 5Zm25 24 1.7 4.3L44 35l-4.3 1.7L38 41l-1.7-4.3L32 35l4.3-1.7L38 29ZM28 3l1.2 3.2L33 8l-3.8 1.2L28 13l-1.2-3.8L23 8l3.8-1.8L28 3Z"/></svg>
            </span>
            <strong>初始化直播智能体</strong>
          </template>
        </button>
        <button ref="menuToggle" type="button" class="control-menu-toggle" :disabled="initializeDisabled || initializing" aria-label="更多初始化操作" aria-haspopup="menu" :aria-expanded="menuOpen" :aria-controls="menuId" title="更多初始化操作" @click="toggleMenu" @keydown.down.prevent="openMenu" @keydown.up.prevent="openMenu">
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m7 10 5 5 5-5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>
        </button>
        <div v-if="menuOpen" :id="menuId" class="control-initialize-menu" role="menu" aria-label="初始化操作">
          <button ref="resetItem" type="button" class="control-reset" role="menuitem" :disabled="initializeDisabled || initializing" @click="forceInitialize" @keydown.down.prevent="resetItem?.focus()" @keydown.up.prevent="resetItem?.focus()" @keydown.home.prevent="resetItem?.focus()" @keydown.end.prevent="resetItem?.focus()">
            <span class="reset-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><path fill="currentColor" d="M12 5V2L7 6l5 4V7a5 5 0 1 1-5 5H5a7 7 0 1 0 7-7Z"/></svg></span>
            <span class="control-reset-copy"><strong>全部重置</strong><span>全部重新生成</span></span>
          </button>
        </div>
      </div>
      <button v-if="mode !== 'initialize'" type="button" class="agent-control-card control-save" :disabled="saveDisabled" :aria-busy="saving" :title="saveHint" @click="$emit('save')">
        <span class="control-icon save-icon" aria-hidden="true"><svg viewBox="0 0 48 48"><defs><linearGradient id="anchor-control-document" x1="0" y1="0" x2="1" y2="1"><stop stop-color="#456dff"/><stop offset="1" stop-color="#357bff"/></linearGradient></defs><path d="M12 5h17l9 9v29H12a3 3 0 0 1-3-3V8a3 3 0 0 1 3-3Z" fill="url(#anchor-control-document)"/><path d="M29 5v9h9" fill="#b7caff"/><path d="M17 24h14M17 31h10" stroke="#fff" stroke-width="2.8" stroke-linecap="round"/></svg></span>
        <strong>{{ saveLabel || (saving ? '保存中…' : '保存方案') }}</strong>
      </button>
    </div>
  </section>
</template>

<style scoped>
.agent-control-bar{width:100%;min-width:0;container-type:inline-size}
.agent-control-grid{display:grid;grid-template-columns:minmax(0,1fr) max-content;gap:14px;align-items:stretch}
.agent-control-grid.initialize-only{grid-template-columns:minmax(0,1fr)}.agent-control-grid.save-only{display:flex;justify-content:flex-end}
.control-initialize-wrap{position:relative;display:flex;align-items:stretch;min-width:min(100%,320px);width:50%;justify-self:start;border-radius:14px;background:linear-gradient(112deg,#58bcfa 0%,#4388fa 27%,#4d5df4 62%,#683bee 100%);box-shadow:0 12px 23px #477aea30}
.control-initialize-wrap .control-initialize{flex:1 1 auto;width:auto;min-width:0}
.agent-control-card{position:relative;isolation:isolate;display:flex;align-items:center;gap:10px;min-width:0;min-height:clamp(62px,4.1vw,76px);padding:10px 16px;border:1px solid transparent;border-radius:14px;font:inherit;text-align:left;cursor:pointer;overflow:hidden;transition:transform .18s ease,box-shadow .18s ease,opacity .18s ease}
.agent-control-card:hover:not(:disabled){transform:translateY(-2px)}.agent-control-card:focus-visible{outline:3px solid #8ea3ff;outline-offset:3px}.agent-control-card:disabled{opacity:.55;cursor:not-allowed;transform:none;box-shadow:none}
.agent-control-card strong{position:relative;z-index:1;min-width:0;font-size:clamp(16px,1.05vw,20px);line-height:1.2;font-weight:850;white-space:nowrap;color:inherit}
.control-icon{position:relative;z-index:1;display:grid;place-items:center;flex:0 0 clamp(38px,2.7vw,48px);width:clamp(38px,2.7vw,48px);height:clamp(38px,2.7vw,48px);border-radius:50%}.control-icon svg{width:58%;height:58%}
.control-initialize{color:#fff;background:transparent;border-radius:14px 0 0 14px;box-shadow:none}
.control-initialize:hover:not(:disabled){transform:none;background:#ffffff12}
.control-initialize.is-initializing{--initialize-progress:0%;justify-content:center;background:#e8edff;cursor:wait}
.control-initialize.is-initializing:disabled{opacity:1;box-shadow:0 12px 23px #477aea30}
.control-progress-fill{position:absolute;inset:0 auto 0 0;z-index:0;width:var(--initialize-progress);background:linear-gradient(112deg,#52b9fb 0%,#407ff7 43%,#6440ef 100%);transition:width .55s cubic-bezier(.22,.75,.25,1)}
.control-progress-fill::after{content:"";position:absolute;top:0;right:-34px;width:68px;height:100%;background:linear-gradient(90deg,transparent,#ffffff4f,transparent);transform:skewX(-13deg);animation:initialize-progress-shimmer 1.15s linear infinite}
.control-progress-copy{position:relative;z-index:2;display:flex;align-items:center;justify-content:center;gap:16px;width:100%;text-align:center;color:#fff;text-shadow:0 1px 8px #243d9b4d}
.control-progress-copy strong{font-size:clamp(18px,1.35vw,24px)}.control-progress-copy em{min-width:44px;padding:4px 8px;border-radius:999px;background:#ffffff26;font-size:16px;font-style:normal;font-weight:850;font-variant-numeric:tabular-nums}
@keyframes initialize-progress-shimmer{from{transform:translateX(-45px) skewX(-13deg)}to{transform:translateX(45px) skewX(-13deg)}}
.control-initialize::before,.control-initialize::after,.control-publish::before,.control-publish::after{content:"";position:absolute;pointer-events:none;z-index:0;border-radius:50%;background:#ffffff0c;transform:rotate(-24deg)}
.control-initialize::before{width:58%;height:120%;right:-14%;bottom:-76%}.control-initialize::after{width:58%;height:90%;right:-5%;bottom:-68%}
.initialize-icon{background:linear-gradient(140deg,#ffffff43,#ffffff17);box-shadow:inset 0 1px 2px #ffffff2e}.control-initialize strong{font-size:clamp(18px,1.35vw,26px);letter-spacing:.01em}
.control-menu-toggle{display:grid;place-items:center;flex:0 0 44px;min-height:44px;padding:0;border:0;border-left:1px solid #ffffff38;border-radius:0 14px 14px 0;background:transparent;color:#fff;cursor:pointer;transition:background .18s ease}
.control-menu-toggle svg{width:22px;height:22px;transition:transform .18s ease}.control-menu-toggle[aria-expanded="true"] svg{transform:rotate(180deg)}
.control-menu-toggle:hover:not(:disabled),.control-menu-toggle[aria-expanded="true"]{background:#ffffff20}.control-menu-toggle:disabled{opacity:.55;cursor:not-allowed}
.control-menu-toggle:focus-visible{outline:3px solid #8ea3ff;outline-offset:3px}
.control-initialize-menu{position:absolute;z-index:40;top:calc(100% + 8px);right:0;box-sizing:border-box;width:max(100%,224px);padding:6px;border:1px solid #e3e7fb;border-radius:12px;background:#fff;box-shadow:0 12px 32px #27396724}
.control-save{border-color:#dce4fa;color:#172544;background:linear-gradient(125deg,#fff,#f3f6ff);box-shadow:0 6px 18px #5564a00b}.save-icon{background:linear-gradient(125deg,#ecf2ff,#e0e8fe)}
.control-reset{display:flex;align-items:center;gap:10px;box-sizing:border-box;width:100%;min-height:66px;padding:10px 12px;border:0;border-radius:8px;font:inherit;text-align:left;cursor:pointer;color:#d8404a;background:transparent;transition:background .18s ease}
.control-reset:hover:not(:disabled),.control-reset:focus-visible{background:#fff2f3}.control-reset:focus-visible{outline:2px solid #f1a1aa;outline-offset:-2px}.control-reset:disabled{opacity:.5;cursor:not-allowed}
.control-reset-copy{display:flex;flex-direction:column;gap:3px}.control-reset-copy strong{font-size:16px;line-height:1.4;font-weight:700;white-space:nowrap}.control-reset-copy>span{font-size:16px;line-height:1.4;color:#7c8499;white-space:nowrap}
.reset-icon{display:grid;place-items:center;flex:0 0 26px;width:26px;height:26px;border-radius:8px;background:#fbe4e6}.reset-icon svg{width:16px;height:16px}
.save-only .control-save{flex:0 0 224px;box-sizing:border-box;width:224px;max-width:100%;height:58px;min-height:58px;justify-content:center}
.is-compact .agent-control-grid{display:flex;flex-wrap:wrap;gap:10px}.is-compact .agent-control-card{width:auto;min-width:0;min-height:44px;padding:9px 12px;box-shadow:none}.is-compact .agent-control-card strong,.is-compact .control-progress-copy strong{font-size:16px;line-height:1.4;font-weight:650;white-space:nowrap}.is-compact .control-icon{width:24px;height:24px;flex-basis:24px;border-radius:6px;background:transparent;box-shadow:none}.is-compact .control-progress-copy{gap:8px}.is-compact .control-initialize.is-initializing{min-width:0;background:#e8edff}
.is-compact .control-initialize-wrap{width:auto;max-width:100%;min-width:0;justify-self:auto;border-radius:9px;background:#4a60de;box-shadow:none}.is-compact .control-initialize{gap:8px;border-radius:9px 0 0 9px}.is-compact .control-menu-toggle{flex-basis:40px;border-radius:0 9px 9px 0}.is-compact .control-save{border-radius:7px}
.is-compact .control-initialize.is-initializing .control-progress-copy{flex-wrap:wrap}.is-compact .control-progress-copy strong{white-space:normal}
.control-publish{color:#fff;background:linear-gradient(125deg,#ba8cf3,#9653ed 70%,#b15af1);box-shadow:0 12px 22px #9363ec29}.publish-icon{background:linear-gradient(145deg,#a978ec,#9060e2)}.control-publish::before{width:80%;height:110%;right:-22%;bottom:-65%;background:#ffffff0d}.control-publish::after{width:85%;height:65%;right:-24%;bottom:-47%;background:#ffffff0b}
@container(max-width:1050px){.agent-control-grid{grid-template-columns:minmax(0,1fr) max-content;gap:10px}.agent-control-card{padding:9px 12px;gap:8px}.agent-control-card strong{font-size:16px}.control-initialize strong{font-size:20px}.control-icon{width:38px;height:38px;flex-basis:38px}}
@container(max-width:880px){.agent-control-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.control-initialize-wrap{grid-column:1/-1;width:100%}.agent-control-card{min-height:62px}.control-initialize strong{font-size:21px}}
@container(max-width:460px){.agent-control-grid{grid-template-columns:1fr}.control-initialize-wrap{grid-column:auto;width:100%}.agent-control-card{min-height:58px;padding:8px 12px}.control-initialize strong{font-size:18px}}
@media(prefers-reduced-motion:reduce){.agent-control-card,.control-progress-fill,.control-menu-toggle,.control-menu-toggle svg,.control-reset{transition:none}.agent-control-card:hover:not(:disabled){transform:none}.control-progress-fill::after{animation:none}}
.is-compact .control-initialize strong,.is-compact .control-progress-copy em{font-size:16px}
</style>
