<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { annotateAnchorStyleRisks } from '../api'
import { riskChunks } from '../riskHighlight'
import type { RiskHit } from '../riskHighlight'

const props = withDefaults(defineProps<{ planId: number; tenantId?: number; text: string; box?: boolean; autoScroll?: boolean; label?: string; legend?: boolean }>(), { label: '生成话术', legend: true })
const body = ref<HTMLDivElement | null>(null), hits = ref<RiskHit[]>([]), error = ref(''), loading = ref(false)
const chunks = computed(() => riskChunks(props.text, hits.value))
let version = 0, timer: ReturnType<typeof setTimeout> | undefined, controller: AbortController | undefined
async function annotate(current: number) {
  if (!props.planId || !props.text.trim()) { loading.value = false; return }
  controller = new AbortController()
  try {
    const result = await annotateAnchorStyleRisks(props.planId, props.text, controller.signal, props.tenantId)
    if (current === version) hits.value = result.hits
  } catch (value) {
    if (current === version) error.value = value instanceof Error ? value.message : '风险标注失败'
  } finally { if (current === version) loading.value = false }
}
function refresh() {
  const current = ++version
  clearTimeout(timer); controller?.abort(); hits.value = []; error.value = ''; loading.value = !!props.text.trim()
  timer = setTimeout(() => void annotate(current), 180)
}
watch(() => [props.planId, props.tenantId, props.text], () => {
  refresh()
  if (props.autoScroll) void nextTick(() => { if (body.value) body.value.scrollTop = body.value.scrollHeight })
}, { immediate: true })
onBeforeUnmount(() => { ++version; clearTimeout(timer); controller?.abort() })
</script>

<template>
  <div class="risk-text-wrap">
    <p v-if="legend" class="risk-legend"><span class="P1">P1 粉红：需核对资质</span><span class="P2">P2 大红：需检查表达</span><span class="P0">P0：硬拦规则命中</span></p>
    <div ref="body" class="risk-text" :class="{ box }" role="textbox" aria-readonly="true" aria-multiline="true" :aria-label="label" tabindex="0"><span v-for="(chunk, index) in chunks" :key="index" :class="chunk.tier" :title="chunk.title">{{ chunk.text }}</span><span v-if="!text" class="placeholder">完整一段返回后追加显示；标色不改变原始文字。</span></div>
    <p v-if="error" class="risk-error">风险标注暂不可用：{{ error }} <button type="button" @click="refresh">重新标注</button></p>
    <p v-else-if="loading" class="risk-state" role="status">正在标注风险文字…</p>
    <p v-else-if="legend && text" class="risk-state">仅按当前母版标注候选词；不代表已核验资质或判定违规。</p>
  </div>
</template>

<style scoped>
.risk-text { white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.85; }
.risk-text.box { box-sizing: border-box; width: 100%; min-height: 440px; max-height: 65vh; overflow: auto; padding: 20px; border: 1px solid #cdd9df; border-radius: 12px; font-size: 19px; background: white; }
.P1 { color: #db2777; font-weight: 700; }.P2 { color: #d00000; font-weight: 700; }.P0 { color: #9f1239; font-weight: 700; background: #ffe4e6; }
.risk-legend { display: flex; flex-wrap: wrap; gap: 16px; font-size: 13px; margin: 8px 0; }.risk-legend .P0 { background: none; }
.risk-error { color: #b91c1c; font-size: 13px; }.risk-state,.placeholder { color: #6b7c8d; font-size: 13px; }.risk-state { margin: 6px 0; }
button { cursor: pointer; margin-left: 6px; }
</style>
