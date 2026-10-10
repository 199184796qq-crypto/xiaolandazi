<script setup lang="ts">
import { computed } from 'vue'
import type { LiveRuntimeSnapshot, SpeechProgramSnapshot } from '../types'
import { livePlanVersionStatus, type PlanVersionIdentity } from '../livePlanVersionStatus'

const props = withDefaults(defineProps<{
  planId: number
  saved: PlanVersionIdentity | null
  runtime?: LiveRuntimeSnapshot | null
  program?: SpeechProgramSnapshot | null
  savedOnly?: boolean
  loading?: boolean
  error?: boolean
}>(), { runtime: null, program: null, savedOnly: false, loading: false, error: false })
const status = computed(() => livePlanVersionStatus(props.planId, props.saved, props.runtime, props.program))
</script>

<template>
  <div v-if="planId" class="plan-version-badge" :class="{ 'is-different': !savedOnly && status.state === 'different', 'is-compact': !savedOnly }" aria-label="直播方案版本" aria-live="polite">
    <div class="plan-version-values">
      <span>方案 #{{ planId }}</span>
      <span>最新保存 <strong>{{ status.latest ? 'V' + status.latest.version_no : error ? '读取失败' : loading ? '读取中…' : '尚未保存' }}</strong></span>
      <span v-if="!savedOnly">本场加载 <strong>{{ status.loaded ? 'V' + status.loaded.version_no : status.active ? '待确认' : '未开播' }}</strong></span>
    </div>
    <small>{{ savedOnly ? '同一方案的所有房间共用；每次成功保存，版本号递增' : error ? '保存版本读取失败，暂时无法核对' : status.hint }}</small>
  </div>
</template>

<style scoped>
.plan-version-badge{box-sizing:border-box;display:grid;gap:6px;padding:11px 14px;border:1px solid #dce4f8;border-radius:12px;color:#627394;background:#f5f8ff;text-align:left}
.plan-version-values{display:flex;flex-wrap:wrap;gap:8px 18px;align-items:center;font-size:12px}
.plan-version-values strong{margin-left:5px;color:#4c5ee0;font-size:15px;font-variant-numeric:tabular-nums}
.plan-version-badge small{color:#71819d;font-size:11px;line-height:1.45}
.is-different{border-color:#ecd393;background:#fffaf0}.is-different small{color:#966818}
.is-compact{gap:4px;padding:8px 10px}.is-compact .plan-version-values{gap:5px 12px;font-size:10px}.is-compact strong{font-size:12px}.is-compact small{font-size:10px}
</style>
