<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import InteractionLearningPanel from './InteractionLearningPanel.vue'
import { request } from '../api'
import type { AgentLearningSession } from '../types'

const props = defineProps<{ roomId: number | null; planId: number | null; locationRequest?: number }>()
const emit = defineEmits<{ refresh: []; adopted: []; factsUpdated: [scope: { roomId: number; planId: number; tenantId: number }] }>()
const tabs = [
  { key: 'simulation', label: '模拟挖掘' },
  { key: 'adopted', label: '互动采纳' },
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
  selectTab('adopted')
  emit('adopted')
}
</script>

<template>
  <section ref="workspaceElement" class="interaction-rules-workspace" aria-label="已采用的互动规则">
    <header class="rules-head">
      <div>
        <h3>已采用的互动规则</h3>
        <p>模拟常见问法，确认互动规则，处理直播中留下的问题；模型建议由你决定是否采用。</p>
      </div>
      <button v-if="activeTab === 'adopted'" type="button" class="refresh-rules" @click="emit('refresh')">刷新规则</button>
    </header>
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
      >{{ tab.label }}</button>
    </nav>
    <p v-if="locationNotice" class="location-notice" role="status">{{ locationNotice }}</p>
    <div id="interaction-rule-content" role="tabpanel" :aria-labelledby="`interaction-rule-tab-${activeTab}`">
      <div v-show="activeTab === 'adopted'"><slot /></div>
      <InteractionLearningPanel
        ref="learningPanel"
        v-show="activeTab !== 'adopted'"
        :room-id="roomId"
        :plan-id="planId"
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
.rule-tabs { display: flex; flex-wrap: wrap; gap: 8px; padding: 6px; margin-bottom: 20px; width: fit-content; max-width: 100%; border: 1px solid #dfe5fb; border-radius: 15px; background: #f4f6ff; }
.rule-tabs button { padding: 11px 24px; border: 0; border-radius: 11px; background: transparent; color: #71819d; font: inherit; font-weight: 700; cursor: pointer; }
.rule-tabs button.active { color: #fff; background: #6260df; box-shadow: 0 4px 12px #6260df26; }
.rule-tabs button:focus-visible { outline: 2px solid #4e5bd6; outline-offset: 3px; }
@media (max-width: 600px) { .rules-head { flex-wrap: wrap; } .rule-tabs { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); width: 100%; box-sizing: border-box; gap: 4px; } .rule-tabs button { padding: 11px 6px; } }
</style>
