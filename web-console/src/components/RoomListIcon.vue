<script setup lang="ts">
defineProps<{ name: string }>()
const paths: Record<string, string> = {
  video: 'M15 8V6a2 2 0 0 0-2-2H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h9a2 2 0 0 0 2-2v-2l7 4V4l-7 4Z',
  phone: 'M7 3H4a1 1 0 0 0-1 1c0 9.4 7.6 17 17 17a1 1 0 0 0 1-1v-3l-5-2-2 2a15 15 0 0 1-7-7l2-2-2-5Z',
  copy: 'M9 9h12v12H9V9ZM15 9V3H3v12h6',
  users: 'M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8Zm6 0a3 3 0 1 0 0-6M2 21v-3a5 5 0 0 1 5-5h4a5 5 0 0 1 5 5v3H2Zm17 0h3v-3a5 5 0 0 0-4-5',
  arrow: 'M4 12h16m-7-7 7 7-7 7',
  link: 'm10 14 4-4m-6 5-2 2a4 4 0 0 1-6-6l5-5a4 4 0 0 1 6 0m2 3 2-2a4 4 0 0 1 6 6l-5 5a4 4 0 0 1-6 0',
  stop: 'M5 5h14v14H5V5Z',
  trash: 'M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7m4-7v7',
  strategy: 'M4 20h17M6 16V9m6 7V4m6 12v-5',
}
</script>
<template><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path v-if="paths[name]" :d="paths[name]"/><g v-else-if="name === 'more'" fill="currentColor" stroke="none"><circle cx="5" cy="12" r="2"/><circle cx="12" cy="12" r="2"/><circle cx="19" cy="12" r="2"/></g></svg></template>
