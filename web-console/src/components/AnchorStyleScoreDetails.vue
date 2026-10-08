<script setup lang="ts">
import { computed, ref } from 'vue'
import type { AnchorStyleSpeechScoreResponse } from '../types'
const props = defineProps<{ result?: AnchorStyleSpeechScoreResponse; error?: string; pending?: boolean; canApplyFeedback?: boolean }>()
defineEmits<{ retry: []; 'apply-feedback': [prompt: string] }>()
const copyStatus = ref('')
const scoreJSON = computed(() => {
  if (!props.result) return ''
  const { baseline_json: _baseline, ...details } = props.result
  return JSON.stringify(details, null, 2)
})
async function copyFeedback() {
  try {
    await navigator.clipboard.writeText(props.result?.feedback?.prompt || '')
    copyStatus.value = '已复制'
  } catch { copyStatus.value = '复制失败，请手动复制下方文本' }
}
</script>

<template>
  <section v-if="result" class="speech-score-details">
    <h3>本段评分 JSON · {{ result.score.overall_score }}/100</h3>
    <pre class="score-json">{{ scoreJSON }}</pre>
    <div class="feedback-draft">
      <h4>发现问题 → 回灌提示词</h4>
      <template v-if="result.feedback?.status === 'ready'">
        <p>只选扣分最多的一维中的一项：{{ result.feedback.dimension }}／{{ result.feedback.issue }}。程序填模板，不调用模型；尚未发送。</p>
        <textarea :value="result.feedback.prompt" rows="5" readonly spellcheck="false" aria-label="程序生成的回灌提示词"></textarea>
        <div class="feedback-actions">
          <button type="button" @click="copyFeedback">复制回灌提示词</button>
          <button v-if="canApplyFeedback !== undefined" type="button" :disabled="!canApplyFeedback" @click="$emit('apply-feedback', result.feedback.prompt)">填入下一轮回灌框</button>
          <span>{{ copyStatus }}</span>
        </div>
      </template>
      <p v-else-if="result.feedback?.status === 'no_issue'">本段没有扣分，不生成回灌提示词。</p>
      <p v-else-if="result.feedback?.status === 'template_error'" class="score-error">评分成功；回灌模板读取失败：{{ result.feedback.error }} <button type="button" :disabled="pending" @click="$emit('retry')">重新评分并生成回灌</button></p>
      <p v-else-if="result.feedback?.status === 'unmapped_issue'">发现问题，但暂无匹配模板，不发送回灌。</p>
      <p v-else>这是更新前的评分，重新打分可生成回灌提示词。 <button type="button" :disabled="pending" @click="$emit('retry')">重新打分</button></p>
    </div>
    <details><summary>本次使用的评分基准 JSON</summary><pre>{{ result.baseline_json }}</pre></details>
  </section>
  <p v-else-if="error" class="score-error">打分失败：{{ error }} <button type="button" :disabled="pending" @click="$emit('retry')">重试打分</button></p>
  <p v-else class="score-pending">{{ pending ? '正在计算本段评分 JSON 和单项回灌草稿…' : '等待本段评分' }}</p>
</template>

<style scoped>
.speech-score-details{margin-top:16px;padding:16px;border:1px solid #a7cfc6;border-radius:12px;background:#f4faf8}h3,h4{margin:0}.speech-score-details p{font-size:13px;line-height:1.65;color:#61747b}pre{max-height:440px;overflow:auto;white-space:pre-wrap;word-break:break-word;background:#131e26;color:#d7e9e1;border-radius:8px;padding:12px;font:13px/1.6 Consolas,"Microsoft YaHei",monospace}.feedback-draft{margin-top:14px}.feedback-draft textarea{width:100%;box-sizing:border-box;padding:12px;border:1px solid #bad5cc;border-radius:8px;font:14px/1.7 "Microsoft YaHei",sans-serif;background:#fff;resize:vertical}.feedback-actions{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin-top:8px}.feedback-actions span{font-size:12px;color:#61747b}button{padding:8px 12px;border:1px solid #bad5cc;border-radius:8px;background:#fff;color:#147d70;font-weight:700;cursor:pointer}button:disabled{opacity:.45;cursor:not-allowed}details{margin-top:14px;font-size:13px}.score-error{padding:12px;background:#fff0f0;color:#a33434!important}.score-pending{padding:12px;background:#edf6f4;color:#50786d}
</style>
