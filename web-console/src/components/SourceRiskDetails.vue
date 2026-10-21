<script setup lang="ts">
import { ref } from 'vue'
import type { RiskReview } from '../riskSources'
const props = defineProps<{ reviews: RiskReview[]; loading?: boolean; canConfirm?: boolean; locatedHash?: string }>()
const emit = defineEmits<{ confirm: [review: RiskReview]; edit: [review: RiskReview]; appeal: [review: RiskReview, reason: string] }>()
const appeals = ref<Record<string, string>>({})
function canConfirm(review: RiskReview) { return props.canConfirm && (review.severity !== 'P0' || review.semantic_decision !== 'hard') }
function title(review: RiskReview) {
  if (review.status === 'confirmed') return '用户已确认'
  return canConfirm(review) ? '待确认' : review.severity === 'P0' ? '待修改或后台复核' : '待用户确认'
}
</script>
<template>
  <section v-if="reviews.length" class="source-risk-details" aria-label="当前内容风险提示">
    <details v-for="review in reviews" :key="review.claim_sha256" :open="locatedHash === review.claim_sha256" :data-source-risk-hash="review.claim_sha256" :class="{ confirmed: review.status === 'confirmed', located: locatedHash === review.claim_sha256 }" tabindex="-1">
      <summary>{{ title(review) }} · {{ review.text }}</summary>
      <p>{{ review.reason }}</p>
      <p v-if="review.status === 'confirmed'">{{ review.evidence }}</p>
      <div v-else class="source-risk-actions">
        <button type="button" :disabled="loading" @click="emit('edit', review)">修改原始内容</button>
        <button v-if="canConfirm(review)" type="button" class="confirm" :disabled="loading" @click="emit('confirm', review)">我已知晓并确认</button>
        <template v-else-if="props.canConfirm && review.severity === 'P0'">
          <span v-if="review.appeal_status === 'pending'">后台复核申请已提交</span>
          <template v-else><textarea v-model="appeals[review.claim_sha256]" maxlength="2000" aria-label="后台复核申请原因" placeholder="填写认为误拦的原因" /><button type="button" :disabled="loading || !appeals[review.claim_sha256]?.trim()" @click="emit('appeal', review, appeals[review.claim_sha256].trim())">申请后台复核</button></template>
        </template>
      </div>
    </details>
  </section>
</template>
<style scoped>
.source-risk-details{display:grid;gap:6px;margin:8px 0}.source-risk-details details{padding:10px;border:1px solid #efb8c6;border-left:3px solid #db4770;border-radius:10px;background:#fff5f8;color:#aa2850;font-size:12px;line-height:1.6}.source-risk-details details.confirmed{border-color:#bce4d0;border-left-color:#45a974;background:#f2fbf6;color:#317550}.source-risk-details details.located{outline:2px solid #db4770;outline-offset:3px}.source-risk-details summary{cursor:pointer;font-weight:800;overflow-wrap:anywhere}.source-risk-details p{margin:8px 0;color:#4b566f;overflow-wrap:anywhere}.source-risk-actions{display:flex;flex-wrap:wrap;gap:7px}.source-risk-actions button{padding:6px 9px;border:1px solid #d6def2;border-radius:8px;background:#fff;color:#4262cc;cursor:pointer}.source-risk-actions button.confirm{background:#526bd8;color:#fff}.source-risk-actions button:disabled{opacity:.5;cursor:default}.source-risk-actions textarea{box-sizing:border-box;width:100%;min-height:65px;padding:8px;border:1px solid #d6def2;border-radius:8px;resize:vertical}
</style>
