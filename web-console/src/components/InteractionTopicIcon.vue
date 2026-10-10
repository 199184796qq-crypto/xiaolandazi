<script setup lang="ts">
import type { InteractionSuggestionTopic } from '../interactionSuggestionTopics'
defineProps<{ topic: InteractionSuggestionTopic | 'all' }>()
</script>
<template>
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <template v-if="topic === 'price'"><path d="m7 4 5 7 5-7M12 11v10M7 12h10M7 16h10" /></template>
    <template v-else-if="topic === 'shipping'"><path d="M2 5h12v12H2zM14 9h4l4 4v4h-8" /><circle cx="6" cy="18" r="2" /><circle cx="18" cy="18" r="2" /></template>
    <template v-else-if="topic === 'after_sales'"><path d="m12 2 8 4v6c0 5-8 10-8 10S4 17 4 12V6zM8 12l3 3 5-6" /></template>
    <template v-else-if="topic === 'gift'"><path d="M3 9h18v4H3zM5 13v8h14v-8M12 9v12" /><path d="M12 9C2 9 4 1 8 4l4 5Zm0 0c10 0 8-8 4-5l-4 5Z" /></template>
    <template v-else><path d="M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z" /></template>
  </svg>
</template>
<style scoped>
svg { width:22px; height:22px; flex:0 0 22px; fill:none; stroke:currentColor; stroke-width:1.8; stroke-linecap:round; stroke-linejoin:round; }
</style>
