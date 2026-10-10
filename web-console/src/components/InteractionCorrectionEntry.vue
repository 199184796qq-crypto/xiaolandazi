<script setup lang="ts">
import type { AgentLearningCorrection } from '../types'
defineProps<{ correction: AgentLearningCorrection }>()
</script>

<template>
  <div class="correction-entry" :data-correction-result-id="correction.result_id">
    <section class="correction-user"><strong>用户填写的纠正</strong><p>{{ correction.user_feedback || '该次未保存用户纠正原文。' }}</p></section>
    <section class="correction-result"><strong>纠正预览</strong><p>{{ correction.preview_text || '该次未保存纠正预览。' }}</p></section>
  </div>
</template>

<style scoped>
.correction-entry{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;margin:14px 0;min-width:0}
.correction-entry section{min-width:0;padding:15px 17px;border:1px solid #dae4fa;border-radius:13px;background:#f4f8ff}
.correction-entry .correction-result{background:#f5f3ff;border-color:#e2ddfc}
.correction-entry strong{color:#4d67a0;font-size:14px}.correction-result strong{color:#6c5aa9}
.correction-entry p{margin:9px 0 0;white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.8;color:#304666;font-size:14px}
@media(max-width:900px){.correction-entry{grid-template-columns:minmax(0,1fr)}.correction-entry section{padding:12px 14px}}
</style>
