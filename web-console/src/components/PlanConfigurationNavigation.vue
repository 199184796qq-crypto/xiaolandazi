<script setup lang="ts">
import { computed, ref } from 'vue'
import FactRiskTabNotice from './FactRiskTabNotice.vue'
import PlanConfigurationIcon from './PlanConfigurationIcon.vue'
import { planConfigurationSection, type PlanConfigurationMode, type PlanConfigurationSection } from '../livePlanNavigation'

const props = withDefaults(defineProps<{
  mode: string
  productRiskCount?: number
  benefitRiskCount?: number
  factRiskCount?: number
  interactionPending?: number
  factRiskLoading?: boolean
  factRiskError?: string
}>(), { productRiskCount: 0, benefitRiskCount: 0, factRiskCount: 0, interactionPending: 0, factRiskLoading: false, factRiskError: '' })
const emit = defineEmits<{
  selectSection: [section: PlanConfigurationSection]
  selectMode: [mode: PlanConfigurationMode]
}>()
const section = computed(() => planConfigurationSection(props.mode))
const factsPending = computed(() => props.productRiskCount + props.benefitRiskCount + props.factRiskCount)
const factFocused = ref(false)
const sections: { key: PlanConfigurationSection; label: string; icon?: string }[] = [
  { key: 'business', label: '业务模式' },
  { key: 'facts', label: '商品与事实' },
  { key: 'interaction', label: '互动规则' },
  { key: 'reading', label: '读写规则' },
  { key: 'style', label: '主播风格' },
  { key: 'voice', label: '声音' },
  { key: 'live', label: '直播模式', icon: 'preview' },
]
const factTabs: { mode: PlanConfigurationMode; label: string }[] = [
  { mode: 'products', label: '商品资料' },
  { mode: 'benefits', label: '活动福利' },
  { mode: 'supplement', label: '商品资料补充' },
  { mode: 'knowledge', label: '事实依据' },
  { mode: 'plan_prompt', label: '方案提示词' },
]
function pending(mode: PlanConfigurationMode) {
  return mode === 'products' ? props.productRiskCount : mode === 'benefits' ? props.benefitRiskCount : 0
}
</script>

<template>
  <div class="plan-configuration-navigation">
    <nav class="plan-configuration-sections" aria-label="智能体方案配置分类">
      <button v-for="item in sections" :key="item.key" type="button" :class="{ active: section === item.key }" :aria-current="section === item.key ? 'page' : undefined" @click="emit('selectSection', item.key)">
        <PlanConfigurationIcon :name="item.icon || item.key" />
        {{ item.label }}
        <span v-if="item.key === 'facts' && factsPending" class="plan-navigation-count" :aria-label="`${factsPending}项待处理`">{{ factsPending }}</span>
        <span v-if="item.key === 'interaction' && interactionPending" class="plan-navigation-count" :aria-label="`${interactionPending}条互动待纠正`">{{ interactionPending > 99 ? '99+' : interactionPending }}</span>
      </button>
      <div v-if="$slots.actions" class="plan-navigation-actions"><slot name="actions" /></div>
    </nav>
    <slot name="feedback" />
    <nav v-if="section === 'facts'" class="plan-configuration-fact-tabs" aria-label="商品与事实栏目">
      <button v-for="item in factTabs" :key="item.mode" type="button" :class="{ active: mode === item.mode }" :aria-current="mode === item.mode ? 'page' : undefined" @focus="factFocused = item.mode === 'knowledge'" @blur="factFocused = false" @click="emit('selectMode', item.mode)">
        <PlanConfigurationIcon :name="item.mode" />
        <FactRiskTabNotice v-if="item.mode === 'knowledge'" :count="factRiskCount" :loading="factRiskLoading" :error="factRiskError" :focused="factFocused" />
        <template v-else>{{ item.label }}<span v-if="pending(item.mode)" class="plan-navigation-count">{{ pending(item.mode) }}项待处理</span></template>
      </button>
    </nav>
  </div>
</template>

<style scoped>
.plan-configuration-navigation{min-width:0;background:transparent}
.plan-configuration-sections,.plan-configuration-fact-tabs{display:flex;align-items:stretch;gap:14px;flex-wrap:wrap}
.plan-configuration-sections{padding:0 10px;border:1px solid #e5e8fd;border-radius:17px;background:linear-gradient(115deg,#fffffffa,#fcfdff);box-shadow:0 8px 24px #8499eb08}
.plan-navigation-actions{display:flex;flex:0 1 240px;align-items:center;min-width:0;max-width:100%;padding:10px 0}
.plan-configuration-sections button{display:flex;align-items:center;gap:12px;min-height:78px;padding:19px 25px;border:0;border-bottom:4px solid transparent;background:transparent;color:#56648e;font:inherit;font-size:20px;font-weight:700;cursor:pointer;transition:color .15s,background .15s}
.plan-configuration-sections button.active{border-bottom-color:#4d49ff;color:#4545f4}
.plan-configuration-sections button:hover{color:#4d49eb;background:#f5f6ff}.plan-configuration-sections button:first-child{border-bottom-left-radius:10px}
.plan-configuration-sections :deep(.plan-configuration-icon){width:27px;height:27px;stroke-width:1.9}
.plan-configuration-fact-tabs{gap:14px;margin-top:18px;padding:18px 24px;border:1px solid #e5e8fd;border-bottom:0;border-radius:17px 17px 0 0;background:#fff}
.plan-configuration-fact-tabs>button{display:flex;align-items:center;gap:10px;min-height:52px;padding:12px 22px;border:1px solid #e9ecfe;border-radius:12px;background:#f5f7ff;color:#495883;font:inherit;font-size:18px;font-weight:650;cursor:pointer;transition:box-shadow .15s,background .15s}
.plan-configuration-fact-tabs>button.active{border-color:#6963ff;background:linear-gradient(115deg,#8e88fa,#4644ff);color:#fff;box-shadow:0 6px 14px #6259e52b}
.plan-configuration-fact-tabs>button:hover:not(.active){background:#ecefff;border-color:#dce1fa}.plan-configuration-fact-tabs :deep(.plan-configuration-icon){width:24px;height:24px}
.plan-navigation-count{padding:2px 6px;border-radius:7px;background:#fff0f2;color:#b9445b;font-size:11px;white-space:nowrap}
.plan-configuration-fact-tabs :deep(.fact-risk-tab-notice){flex-direction:row;gap:7px;align-items:center}
.plan-configuration-fact-tabs :deep(.fact-risk-tab-pill){padding:2px 5px;font-size:10px}
.plan-configuration-fact-tabs>button.active :deep(.fact-risk-tab-text){color:inherit}
.plan-configuration-sections button:focus-visible,.plan-configuration-fact-tabs>button:focus-visible{outline:3px solid #a9a7ff;outline-offset:3px}
@media(max-width:1400px){.plan-configuration-sections button{padding:16px 18px;font-size:16px;gap:10px}.plan-configuration-fact-tabs>button{padding:11px 16px;font-size:14px}}
@media(max-width:700px){.plan-configuration-sections{padding:0 4px;gap:0;border-radius:13px}.plan-configuration-sections button{flex:1 1 calc(50% - 8px);justify-content:center;min-height:65px;padding:10px 5px;font-size:16px;gap:5px}.plan-configuration-sections :deep(.plan-configuration-icon){width:19px;height:19px}.plan-configuration-fact-tabs{gap:8px;padding:14px 12px;margin-top:12px}.plan-configuration-fact-tabs>button{flex:1 1 calc(50% - 8px);justify-content:center;min-height:44px;padding:9px 8px;font-size:13px;gap:7px}.plan-configuration-fact-tabs :deep(.fact-risk-tab-notice){flex-wrap:wrap}}
@media(max-width:700px){.plan-navigation-actions{flex:1 1 100%;justify-content:center;padding:8px}}
@media(prefers-reduced-motion:reduce){.plan-configuration-sections button,.plan-configuration-fact-tabs>button{transition:none}}
</style>
