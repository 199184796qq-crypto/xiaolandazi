<script setup lang="ts">
import { computed } from 'vue'
const props = defineProps<{ category: string; label: string; family?: string }>()
const tone = computed(() => {
  const family = props.family || props.category
  return ['product', 'link', 'trade', 'fulfillment', 'identity_location'].includes(family) ? family : 'other'
})
</script>

<template>
  <span class="fact-category-badge" :class="'is-' + tone">
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <template v-if="tone === 'fulfillment'"><path d="M2 5h12v12H2zM14 9h4l4 4v4h-8"/><circle cx="6" cy="18" r="2"/><circle cx="18" cy="18" r="2"/></template>
      <template v-else-if="tone === 'product'"><path d="m12 2 8 4v6c0 5-8 10-8 10S4 17 4 12V6zM8 12l3 3 5-6"/></template>
      <template v-else-if="tone === 'trade'"><path d="m3 11 8-8h9v9l-8 8z"/><circle cx="16" cy="7" r="1"/><path d="m8 11 5 5m0-5-5 5"/></template>
      <template v-else-if="tone === 'link'"><path d="m9 15 6-6M8 16l-2 2a4 4 0 0 1-6-6l5-5a4 4 0 0 1 6 0M16 8l2-2a4 4 0 0 1 6 6l-5 5a4 4 0 0 1-6 0"/></template>
      <template v-else-if="tone === 'identity_location'"><path d="M12 22s8-8 8-13A8 8 0 0 0 4 9c0 5 8 13 8 13z"/><circle cx="12" cy="9" r="3"/></template>
      <template v-else><path d="M6 3h9l4 4v14H6zM14 3v5h5M9 12h7M9 16h5"/></template>
    </svg>
    <span>{{ label }}</span>
  </span>
</template>

<style scoped>
.fact-category-badge { display:inline-flex; align-items:center; gap:8px; max-width:100%; box-sizing:border-box; padding:9px 11px; border-radius:12px; background:linear-gradient(135deg,#ece8ff,#f6f2ff); color:#7254db; font-size:14px; font-weight:800; white-space:nowrap; }
.fact-category-badge svg { width:22px; height:22px; flex:0 0 22px; fill:none; stroke:currentColor; stroke-width:1.8; stroke-linecap:round; stroke-linejoin:round; }
.fact-category-badge>span { min-width:0; white-space:normal; overflow-wrap:anywhere; line-height:1.45; }
.fact-category-badge.is-fulfillment,.fact-category-badge.is-link { background:linear-gradient(135deg,#dfedff,#eef5ff); color:#2475ef; }
.fact-category-badge.is-product { background:linear-gradient(135deg,#def9ec,#f0fcf5); color:#0baf70; }
.fact-category-badge.is-trade { background:linear-gradient(135deg,#fff0d8,#fff8eb); color:#ef930d; }
.fact-category-badge.is-identity_location { background:linear-gradient(135deg,#e2f7fa,#effbfd); color:#1599a8; }
</style>
