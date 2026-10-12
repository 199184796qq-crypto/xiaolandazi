<script setup lang="ts">
import { computed, ref } from 'vue'
import FactRiskTabNotice from './FactRiskTabNotice.vue'
import { planConfigurationSection, type PlanConfigurationMode, type PlanConfigurationSection } from '../livePlanNavigation'

const props = withDefaults(defineProps<{
  mode: string
  productRiskCount?: number
  benefitRiskCount?: number
  factRiskCount?: number
  directionRiskCount?: number
  interactionPending?: number
  factRiskLoading?: boolean
  factRiskError?: string
}>(), { productRiskCount: 0, benefitRiskCount: 0, factRiskCount: 0, directionRiskCount: 0, interactionPending: 0, factRiskLoading: false, factRiskError: '' })
const emit = defineEmits<{
  selectSection: [section: PlanConfigurationSection]
  selectMode: [mode: PlanConfigurationMode]
}>()
const section = computed(() => planConfigurationSection(props.mode))
const factsPending = computed(() => props.productRiskCount + props.benefitRiskCount + props.factRiskCount + props.directionRiskCount)
const factFocused = ref(false)
const sections: { key: PlanConfigurationSection; label: string }[] = [
  { key: 'facts', label: '事实与内容' },
  { key: 'interaction', label: '互动规则' },
  { key: 'style', label: '主播风格' },
  { key: 'voice', label: '声音' },
]
const factTabs: { mode: PlanConfigurationMode; label: string }[] = [
  { mode: 'products', label: '商品资料' },
  { mode: 'benefits', label: '活动福利' },
  { mode: 'knowledge', label: '事实依据' },
  { mode: 'miner', label: '内容方向' },
]
function pending(mode: PlanConfigurationMode) {
  return mode === 'products' ? props.productRiskCount : mode === 'benefits' ? props.benefitRiskCount : mode === 'miner' ? props.directionRiskCount : 0
}
</script>

<template>
  <div class="plan-configuration-navigation">
    <nav class="plan-configuration-sections" aria-label="智能体方案配置分类">
      <button v-for="item in sections" :key="item.key" type="button" :class="{ active: section === item.key }" :aria-current="section === item.key ? 'page' : undefined" @click="emit('selectSection', item.key)">
        {{ item.label }}
        <span v-if="item.key === 'facts' && factsPending" class="plan-navigation-count" :aria-label="`${factsPending}项待处理`">{{ factsPending }}</span>
        <span v-if="item.key === 'interaction' && interactionPending" class="plan-navigation-count" :aria-label="`${interactionPending}条互动待纠正`">{{ interactionPending > 99 ? '99+' : interactionPending }}</span>
      </button>
    </nav>
    <nav v-if="section === 'facts'" class="plan-configuration-fact-tabs" aria-label="事实与内容栏目">
      <button v-for="item in factTabs" :key="item.mode" type="button" :class="{ active: mode === item.mode }" :aria-current="mode === item.mode ? 'page' : undefined" @focus="factFocused = item.mode === 'knowledge'" @blur="factFocused = false" @click="emit('selectMode', item.mode)">
        <FactRiskTabNotice v-if="item.mode === 'knowledge'" :count="factRiskCount" :loading="factRiskLoading" :error="factRiskError" :focused="factFocused" />
        <template v-else>{{ item.label }}<span v-if="pending(item.mode)" class="plan-navigation-count">{{ pending(item.mode) }}项待处理</span></template>
      </button>
    </nav>
  </div>
</template>

<style scoped>
.plan-configuration-navigation{min-width:0;background:#fff;border-bottom:1px solid #e4e9f2}
.plan-configuration-sections,.plan-configuration-fact-tabs{display:flex;align-items:stretch;gap:8px;flex-wrap:wrap;padding:0 22px}
.plan-configuration-sections{border-bottom:1px solid #edf0f5}
.plan-configuration-sections button{display:flex;align-items:center;gap:8px;min-height:52px;padding:12px 16px;border:0;border-bottom:2px solid transparent;background:transparent;color:#6d7890;font:inherit;font-size:14px;font-weight:600;cursor:pointer}
.plan-configuration-sections button.active{border-bottom-color:#4e63dc;color:#4055cf}
.plan-configuration-fact-tabs{gap:5px;padding-top:10px;padding-bottom:10px}
.plan-configuration-fact-tabs>button{display:flex;align-items:center;gap:7px;min-height:36px;padding:6px 11px;border:1px solid transparent;border-radius:6px;background:transparent;color:#68748a;font:inherit;font-size:13px;cursor:pointer}
.plan-configuration-fact-tabs>button.active{background:#edf1ff;color:#4055cf}
.plan-navigation-count{padding:2px 6px;border-radius:7px;background:#fff0f2;color:#b9445b;font-size:11px;white-space:nowrap}
.plan-configuration-fact-tabs :deep(.fact-risk-tab-notice){flex-direction:row;gap:7px;align-items:center}
.plan-configuration-fact-tabs :deep(.fact-risk-tab-pill){padding:2px 5px;font-size:10px}
@media(max-width:700px){.plan-configuration-sections,.plan-configuration-fact-tabs{padding-left:12px;padding-right:12px;gap:2px}.plan-configuration-sections button{padding:11px 10px;font-size:13px}.plan-configuration-fact-tabs>button{padding:7px 9px}.plan-configuration-fact-tabs :deep(.fact-risk-tab-notice){flex-wrap:wrap}}
@media(pointer:coarse){.plan-configuration-fact-tabs>button{min-height:44px}}
</style>
