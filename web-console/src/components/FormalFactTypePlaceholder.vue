<script setup lang="ts">
withDefaults(defineProps<{ label?: string }>(), { label: '事实类型' })
</script>

<template>
  <div class="formal-fact-type-placeholder">
    <select :aria-label="label" disabled><option value=""></option></select>
  </div>
</template>

<style scoped>
.formal-fact-type-placeholder { position:relative; min-width:0; }
.formal-fact-type-placeholder select { appearance:none; box-sizing:border-box; width:100%; min-height:42px; padding:7px 30px 7px 12px; border:1px solid transparent; border-radius:13px; background:var(--formal-scope-bg,#eef3ff); color:#243b67; font:inherit; font-size:16px; opacity:1; cursor:default; }
.formal-fact-type-placeholder::after { content:''; position:absolute; top:16px; right:14px; width:7px; height:7px; border-right:2px solid var(--formal-fact-accent,#8e9fc5); border-bottom:2px solid var(--formal-fact-accent,#8e9fc5); transform:rotate(45deg); pointer-events:none; }
</style>
