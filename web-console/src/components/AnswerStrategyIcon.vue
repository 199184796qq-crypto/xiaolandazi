<script setup lang="ts">
import type { AnswerStrategyCategory } from '../answerStrategyCategories'
defineProps<{ category: AnswerStrategyCategory }>()
</script>

<template>
  <svg viewBox="0 0 32 32" aria-hidden="true" class="answer-strategy-icon">
    <template v-if="category === 'logistics'"><path d="M3 7h17v16H3zM20 13h5l4 5v5h-9M23 14v5h5" /><circle cx="9" cy="24" r="3" /><circle cx="24" cy="24" r="3" /></template>
    <template v-else-if="category === 'product'"><path d="m16 3 12 6-12 6L4 9zM4 9v14l12 6 12-6V9M16 15v14M10 6l12 6" /></template>
    <template v-else-if="category === 'children'"><circle cx="16" cy="17" r="11" /><path d="M5 14c-6 0-6 6 0 6M27 14c6 0 6 6 0 6M14 7c6 0 5-7 1-5M12 20q4 5 8 0" /><circle cx="12" cy="15" r="1" /><circle cx="21" cy="15" r="1" /></template>
    <template v-else-if="category === 'audience'"><circle cx="16" cy="10" r="5" /><path d="M7 29v-4a9 9 0 0 1 18 0v4M6 6a4 4 0 0 0 0 8M2 24v-3a6 6 0 0 1 5-6M26 6a4 4 0 0 1 0 8M30 24v-3a6 6 0 0 0-5-6" /></template>
    <template v-else-if="category === 'safety'"><path d="m16 2 12 5v9c0 7-12 14-12 14S4 23 4 16V7z" /><path d="M16 9v9M16 23h.01" /></template>
    <template v-else-if="category === 'timing'"><path d="M4 7h24v22H4zM4 13h24M10 3v7M22 3v7" /><circle cx="18" cy="21" r="5" /><path d="M18 18v3l2 1" /></template>
    <template v-else-if="category === 'after_sales'"><path d="m16 2 12 5v9c0 7-12 14-12 14S4 23 4 16V7z" /><path d="m10 16 4 4 9-9" /></template>
    <template v-else-if="category === 'consultation'"><path d="M5 18v-4a11 11 0 0 1 22 0v4M5 14H2v10h6V14zM27 14h3v10h-6V14zM27 24c0 4-4 5-9 5" /><path d="M13 12h7M13 16h5" /></template>
    <template v-else-if="category === 'language'"><path d="M4 4h24v18H13l-7 6v-6H4z" /><circle cx="10" cy="13" r="1" /><circle cx="16" cy="13" r="1" /><circle cx="22" cy="13" r="1" /></template>
    <template v-else-if="category === 'price'"><path d="m6 3 10 10L26 3M16 13v17M7 17h18M7 23h18" /></template>
    <template v-else-if="category === 'benefits'"><path d="M3 11h26v6H3zM6 17v12h20V17M16 11v18M16 11H9a4 4 0 1 1 4-4l3 4h7a4 4 0 1 0-4-4z" /></template>
    <template v-else><rect x="3" y="3" width="10" height="10" rx="3" /><rect x="19" y="3" width="10" height="10" rx="3" /><rect x="3" y="19" width="10" height="10" rx="3" /><rect x="19" y="19" width="10" height="10" rx="3" /></template>
  </svg>
</template>

<style scoped>
.answer-strategy-icon{width:32px;height:32px;fill:none;stroke:currentColor;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}
</style>
