<script setup lang="ts">
import type { LiveAnchorAppliedTraining } from '../types'

defineProps<{ items?: LiveAnchorAppliedTraining[] }>()
</script>

<template>
  <section class="anchor-training-receipt" aria-label="本次使用的训练">
    <header><strong>本次使用的训练</strong><span v-if="items">共 {{ items.length }} 条</span></header>
    <p v-if="!items" class="receipt-notice">这次生成没有返回训练清单，请重新生成后查看。</p>
    <p v-else-if="!items.length" class="receipt-notice">本次没有使用反馈训练调整。</p>
    <template v-else>
      <p class="receipt-notice">以下要求参与了这次生成，是否做到还需要你对照文案检查。这些说明不会读进直播间。</p>
      <ol>
        <li v-for="(item, index) in items" :key="`${item.saved}:${item.id}:${index}`">
          <div class="receipt-heading"><strong>训练 {{ index + 1 }} · {{ item.label }}</strong><span :class="{ pending: !item.saved }">{{ item.saved ? '生成时已保存' : '本轮未保存' }}</span></div>
          <p><b>你的要求：</b>{{ item.feedback }}</p>
          <p><b>生成时采用的说法要求：</b>{{ item.mainline_instruction }}</p>
          <details v-if="item.diagnosis"><summary>查看系统理解</summary><p>{{ item.diagnosis }}</p></details>
        </li>
      </ol>
    </template>
  </section>
</template>

<style scoped>
.anchor-training-receipt{display:grid;gap:10px;padding:16px;border:1px solid #dce3f4;border-radius:14px;background:#f9faff;color:#405170}
.anchor-training-receipt>header,.receipt-heading{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap}
.anchor-training-receipt>header strong{font-size:16px;color:#314466}
.anchor-training-receipt>header span,.receipt-notice{color:#7b879e;font-size:12px;line-height:1.6}
.anchor-training-receipt p{margin:0;white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.7}
.anchor-training-receipt ol{display:grid;gap:10px;margin:0;padding:0;list-style:none}
.anchor-training-receipt li{display:grid;gap:7px;padding:13px;border:1px solid #e0e6f3;border-radius:10px;background:#fff;font-size:13px}
.receipt-heading span{padding:3px 7px;border-radius:6px;background:#eaf7f0;color:#33815b;font-size:11px}
.receipt-heading span.pending{background:#fff4e3;color:#986d28}
.anchor-training-receipt summary{cursor:pointer;color:#687bb6;font-size:12px}
.anchor-training-receipt details p{margin-top:5px;color:#748099}
</style>
