<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import InteractionLearningPanel from './InteractionLearningPanel.vue'
import { request } from '../api'
import type { AgentLearningSession, LiveAgentPlanFact } from '../types'

const props = defineProps<{ roomId: number | null; planId: number | null; facts?: LiveAgentPlanFact[]; locationRequest?: number }>()
const emit = defineEmits<{ refresh: []; adopted: []; factsUpdated: [scope: { roomId: number; planId: number; tenantId: number }] }>()
const tabs = [
  { key: 'simulation', label: '模拟挖掘' },
  { key: 'adopted', label: '回答策略' },
  { key: 'live', label: '直播互动' },
  { key: 'history', label: '处理历史' },
] as const
type Tab = typeof tabs[number]['key']
const activeTab = ref<Tab>(props.locationRequest ? 'live' : 'adopted')
const learningPanel = ref<InstanceType<typeof InteractionLearningPanel> | null>(null)
const workspaceElement = ref<HTMLElement | null>(null)
const locationNotice = ref('')
let locationSequence = 0
watch(() => [props.roomId, props.planId], () => { locationSequence++; locationNotice.value = '' })
onBeforeUnmount(() => { locationSequence++ })
onMounted(() => {
  watch(() => props.locationRequest, requestId => {
    if (requestId) void locatePending()
  }, { immediate: true, flush: 'post' })
})

async function locatePending() {
  const room = props.roomId, plan = props.planId, sequence = ++locationSequence
  if (!room || !plan) return false
  locationNotice.value = '正在定位待处理问题…'
  workspaceElement.value?.scrollIntoView({ behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth', block: 'start', inline: 'nearest' })
  try {
    const result = await request<{ items: AgentLearningSession[] }>(`/api/v1/live/rooms/${room}/interaction-learning?plan_id=${plan}&status=pending&page=1`, { suppressErrorDialog: true })
    if (sequence !== locationSequence) return false
    const item = result.items?.find(item => item.status === 'editing')
    if (!item) { locationNotice.value = '当前待处理问题已处理完毕。'; window.dispatchEvent(new Event('interaction-review-updated')); return false }
    activeTab.value = item.source_type === 'initial_simulation' ? 'simulation' : 'live'
    await nextTick()
    const found = await learningPanel.value?.locatePending(item.id)
    if (sequence !== locationSequence) return false
    locationNotice.value = found ? '已定位待处理问题，带红点的记录需要核对。' : '暂时无法定位这条记录，请点击红点重试。'
    return !!found
  } catch (err) {
    if (sequence === locationSequence) locationNotice.value = err instanceof Error ? err.message : '读取待处理问题失败，请重试。'
    return false
  }
}

defineExpose({ locatePending })

function selectTab(tab: Tab) {
  locationSequence++
  learningPanel.value?.cancelLocation()
  activeTab.value = tab
  locationNotice.value = ''
}

async function onTabKeydown(event: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const index = tabs.findIndex(tab => tab.key === activeTab.value)
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length
  selectTab(tabs[next]!.key)
  const tablist = (event.currentTarget as HTMLElement).closest('[role="tablist"]')
  await nextTick()
  tablist?.querySelector<HTMLButtonElement>(`[data-interaction-tab="${activeTab.value}"]`)?.focus()
}

function onAdopted() {
  emit('adopted')
}
</script>

<template>
  <section ref="workspaceElement" class="interaction-rules-workspace" aria-label="互动策略工作区">
    <header v-if="activeTab !== 'simulation' && activeTab !== 'adopted'" class="rules-head">
      <div>
        <h3>{{ activeTab === 'history' ? '处理历史' : '已采用的互动规则' }}</h3>
        <p>{{ activeTab === 'history' ? '回看互动问题及纠正处理记录，可按关键词和状态查找。' : '模拟常见问法，确认互动规则，处理直播中留下的问题；模型建议由你决定是否采用。' }}</p>
      </div>
    </header>
    <div class="rule-navigation">
    <nav class="rule-tabs" role="tablist" aria-label="互动规则">
      <button
        v-for="tab in tabs"
        :id="`interaction-rule-tab-${tab.key}`"
        :key="tab.key"
        type="button"
        role="tab"
        :data-interaction-tab="tab.key"
        :aria-selected="activeTab === tab.key"
        aria-controls="interaction-rule-content"
        :tabindex="activeTab === tab.key ? 0 : -1"
        :class="{ active: activeTab === tab.key }"
        @click="selectTab(tab.key)"
        @keydown="onTabKeydown"
      ><svg viewBox="0 0 24 24" aria-hidden="true"><template v-if="tab.key === 'simulation'"><path d="m9 2 2.5 5.5L17 10l-5.5 2.5L9 18l-2.5-5.5L1 10l5.5-2.5zM19 15l1.2 2.8L23 19l-2.8 1.2L19 23l-1.2-2.8L15 19l2.8-1.2z" /></template><template v-else-if="tab.key === 'adopted'"><path d="M3 3h18v14H9l-5 4V3zM7 9h.01M12 9h.01M17 9h.01" /></template><template v-else-if="tab.key === 'live'"><path d="M4 20V9h3v11M10 20V3h3v17M16 20V6h3v14M2 21h20" /></template><template v-else><circle cx="12" cy="12" r="9" /><path d="M12 6v7l4 2" /></template></svg>{{ tab.label }}</button>
    </nav>
    <div v-if="activeTab === 'simulation'" class="rule-navigation-assistant" aria-hidden="true"><p>从用户问题中发现更多优化建议<br />让直播说话更专业、更准确</p><svg viewBox="0 0 100 80"><ellipse cx="54" cy="74" rx="30" ry="4" fill="#dce9ff" /><g transform="rotate(9 55 40)"><path d="M51 12V5" stroke="#8cabfc" stroke-width="4" stroke-linecap="round" /><circle cx="51" cy="4" r="4" fill="#9c96ff" /><rect x="20" y="14" width="66" height="54" rx="23" fill="#fff" stroke="#e0eaff" stroke-width="3" /><rect x="30" y="25" width="45" height="30" rx="13" fill="#182d67" /><ellipse cx="43" cy="38" rx="4" ry="6" fill="#70d8ff" /><ellipse cx="63" cy="38" rx="4" ry="6" fill="#70d8ff" /><path d="M37 19h20" stroke="#b7c9ff" stroke-width="4" stroke-linecap="round" /><path d="M38 68v7m30-7v7" stroke="#b4caff" stroke-width="8" stroke-linecap="round" /></g><path d="m9 19 2 5 5 2-5 2-2 5-2-5-5-2 5-2M91 56l1 3 3 1-3 1-1 3-1-3-3-1 3-1" fill="#a29bff" /></svg></div>
    </div>
    <p v-if="locationNotice" class="location-notice" role="status">{{ locationNotice }}</p>
    <div id="interaction-rule-content" role="tabpanel" :aria-labelledby="`interaction-rule-tab-${activeTab}`">
      <div v-show="activeTab === 'adopted'"><slot /></div>
      <InteractionLearningPanel
        ref="learningPanel"
        v-show="activeTab !== 'adopted'"
        :room-id="roomId"
        :plan-id="planId"
        :facts="facts"
        :source-type="activeTab === 'history' ? undefined : activeTab === 'simulation' ? 'initial_simulation' : 'live_uncertain'"
        :history="activeTab === 'history'"
        :show-simulation="activeTab === 'simulation'"
        @adopted="onAdopted"
        @facts-updated="emit('factsUpdated', $event)"
      />
    </div>
  </section>
</template>

<style scoped>
.interaction-rules-workspace { min-width: 0; }
.location-notice { padding:12px 14px; border:1px solid #e5dce9; border-radius:12px; background:#fff8fa; color:#965471; line-height:1.6; }
.rules-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 20px; }
.rules-head h3 { margin: 0 0 8px; font-size: 22px; color: #1f3150; }
.rules-head p { margin: 0; font-size: 16px; line-height: 1.7; color: #7b8eab; }
.refresh-rules { flex-shrink: 0; padding: 10px 16px; border: 1px solid #e1e7f4; border-radius: 12px; background: #fff; color: #536884; font: inherit; cursor: pointer; }
.rule-navigation{display:flex;align-items:center;justify-content:space-between;gap:18px;margin-bottom:16px}
.rule-tabs { display: flex; flex-wrap: wrap; gap: 10px; margin:0; width: fit-content; max-width: 100%; }
.rule-tabs button { display:inline-flex;align-items:center;justify-content:center;gap:10px;min-height:47px;padding:11px 22px;border:1px solid #fff;border-radius:13px;background:linear-gradient(135deg,#fff,#f7faff);color:#526e9b;font:inherit;font-weight:750;cursor:pointer;box-shadow:0 3px 12px #7d9aca09;white-space:nowrap }
.rule-tabs button svg{width:22px;height:22px;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}
.rule-tabs button.active { color: #fff; background:linear-gradient(135deg,#8580fb,#645ced);box-shadow:0 5px 12px #7164e62b;border-color:#918afe }
.rule-tabs button[data-interaction-tab="simulation"] svg{fill:currentColor;stroke-width:1}
.rule-navigation-assistant{display:flex;align-items:center;gap:8px;flex-shrink:0}.rule-navigation-assistant p{margin:0;padding:10px 20px;border-radius:999px;background:#ffffffb8;color:#798db5;font-size:12px;line-height:1.7;text-align:center}.rule-navigation-assistant>svg{width:88px;height:71px;margin-block:-12px}
.rule-tabs button:focus-visible { outline: 2px solid #4e5bd6; outline-offset: 3px; }
@media(max-width:1100px){.rule-navigation-assistant{display:none}}
@media (max-width: 600px) { .rules-head { flex-wrap: wrap; } .rule-navigation{display:block}.rule-tabs { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); width: 100%; box-sizing: border-box; gap:8px; } .rule-tabs button { padding: 10px 6px;gap:8px;font-size:16px }.rule-tabs button svg{width:20px;height:20px} }
</style>
