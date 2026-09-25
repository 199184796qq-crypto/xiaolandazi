<script setup lang="ts">
import { computed } from 'vue'
import type { Receipt } from '../customerBusinessApi'

const props = defineProps<{
  receipt: Receipt
  finance: boolean
  canReview: boolean
  actorUserId?: number
  requireDistinctReviewer: boolean
  policyReady: boolean
}>()
const emit = defineEmits<{ review: [] }>()
const pending = computed(() => ['pending', 'posting_failed'].includes(props.receipt.status))
const blockedReason = computed(() => {
  if (!props.finance) return '由财务审核人员处理，当前账号只能查看收款进度。'
  if (!props.canReview) return '当前账号没有收款审核权限，请由有审核权限的财务处理。'
  if (!props.actorUserId) return '登录状态待确认，请刷新后重试。'
  if (!props.policyReady) return '审核配置暂不可用，请刷新后重试。'
  if (props.requireDistinctReviewer && (props.receipt.requester_user_id === props.actorUserId || props.receipt.last_submitter_user_id === props.actorUserId)) {
    return '这是本人提交或补件的收款单，须由另一位有审核权限的财务处理。'
  }
  return ''
})
const shortReason = computed(() => {
  if (!props.finance) return '等待财务审核'
  if (!props.canReview) return '无审核权限'
  if (!props.policyReady) return '审核配置未就绪'
  return blockedReason.value ? '当前强制分人审核' : ''
})
function review() {
  if (pending.value && !blockedReason.value) emit('review')
}
</script>

<template>
  <div v-if="pending" class="receipt-review-action">
    <button v-if="finance" type="button" class="receipt-review-button" :disabled="Boolean(blockedReason)" :title="blockedReason || '核对凭据后选择入账、补件或驳回'" @click="review">核实 / 审核入账</button>
    <span v-if="blockedReason" class="receipt-review-reason" :title="blockedReason" :aria-label="blockedReason">{{ shortReason }}</span>
  </div>
</template>

<style scoped>
.receipt-review-action {display:inline-flex;flex-direction:column;align-items:flex-start;gap:3px;max-width:220px}
.receipt-review-reason {display:block;color:#6b7790;font-size:16px;line-height:1.35;white-space:nowrap}
.receipt-review-button {font-size:18px;line-height:1.4;min-height:38px;border:1px solid #90b7fb;border-radius:8px;padding:6px 10px;background:#edf5ff;color:#2658aa;font-weight:600;white-space:nowrap}
.receipt-review-button:not(:disabled):hover,.receipt-review-button:focus-visible {outline:none;border-color:#5595ff;box-shadow:0 0 0 3px #4285ff22,0 5px 18px #3388ff2b}
.receipt-review-button:disabled {cursor:not-allowed;opacity:.65}
</style>
