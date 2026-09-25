<script setup lang="ts">
import { useRoute } from 'vue-router'
import WorkInboxPanel from '../components/WorkInboxPanel.vue'
const route=useRoute()
</script>
<template><div class="management-page"><WorkInboxPanel :initial-group="String(route.query.group||'')" :initial-category="String(route.query.category||'')"/></div></template>
