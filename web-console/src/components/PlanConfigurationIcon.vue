<script setup lang="ts">
defineProps<{ name: string }>()
const paths: Record<string, string> = {
  business: 'M4 7h16v14H4zM8 7V3h8v4M4 12h16M10 12v3h4v-3',
  facts: 'M6 3h9l4 4v14H6zM15 3v5h4M9 12h7M9 16h7',
  interaction: 'M4 4h16v13h-9l-5 4v-4H4zM8 10h.01M12 10h.01M16 10h.01',
  style: 'm12 3 2.8 5.8 6.4.9-4.6 4.5 1.1 6.3L12 17.6l-5.7 2.9 1.1-6.3-4.6-4.5 6.4-.9z',
  voice: 'M12 3a3 3 0 0 0-3 3v6a3 3 0 0 0 6 0V6a3 3 0 0 0-3-3zM6 10v2a6 6 0 0 0 12 0v-2M12 18v3M9 21h6',
  products: 'm12 3 9 5v8l-9 5-9-5V8zM3 8l9 5 9-5M12 13v8M7.5 5.5l9 5',
  supplement: 'M4 4h16v13h-9l-5 4v-4H4zM8 10h8M12 6v8',
  benefits: 'M3 8h18v4H3zM5 12v9h14v-9M12 8v13M12 8H8a3 3 0 1 1 3-3l1 3zm0 0h4a3 3 0 1 0-3-3l-1 3z',
  knowledge: 'M5 3h14v18H5zM8 7h8M8 11h8M8 15h6M8 18h4',
  plan_prompt: 'm15 4 5 5M4 20l4-1L20 7a2.8 2.8 0 0 0-4-4L4 15z',
  miner: 'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0zM15.5 8.5l-2 5-5 2 2-5z',
  preview: 'M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12zM15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0z',
  save: 'M4 3h13l4 4v14H3V3zM7 3v6h10V3M7 21v-8h10v8',
  edit: 'm15 4 5 5M4 20l4-1L20 7a2.8 2.8 0 0 0-4-4L4 15z',
  link: 'M10 13a5 5 0 0 0 7 .1l3-3a5 5 0 0 0-7-7l-2 2M14 11a5 5 0 0 0-7-.1l-3 3a5 5 0 0 0 7 7l2-2',
  chevron: 'm5 9 7 7 7-7',
  next: 'm9 5 7 7-7 7',
  sparkle: 'm12 2 2.7 6.8L22 12l-7.3 3.2L12 22l-2.7-6.8L2 12l7.3-3.2zM20 2v4M18 4h4',
}
</script>

<template>
  <svg class="plan-configuration-icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path :d="paths[name] || paths.facts" /></svg>
</template>

<style scoped>
.plan-configuration-icon{display:block;flex-shrink:0;width:24px;height:24px;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}
</style>
